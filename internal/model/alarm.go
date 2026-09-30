package model

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

type AlarmSeverity string

const (
	SeverityWarning  AlarmSeverity = "warning"
	SeverityCritical AlarmSeverity = "critical"
)

// Alarm 是一次原始的报警触发记录。同一故障在状态抖动期间可能产生多条 Alarm，
// 它们会按处理线、报警类型与设备归并到同一个 Incident 中，原始记录不会丢失。
type Alarm struct {
	ID       uuid.UUID     `json:"id"`
	LineID   LineID        `json:"line_id"`
	Code     string        `json:"code"`
	DeviceID string        `json:"device_id"`
	Message  string        `json:"message"`
	Severity AlarmSeverity `json:"severity"`
	RaisedAt time.Time     `json:"raised_at"`
}

func NewAlarm(lineID LineID, code, deviceID, message string, severity AlarmSeverity, now time.Time) (Alarm, error) {
	if lineID == "" || code == "" || deviceID == "" {
		return Alarm{}, errors.New("alarm identity is incomplete")
	}
	return Alarm{
		ID: uuid.New(), LineID: lineID, Code: code, DeviceID: deviceID,
		Message: message, Severity: severity, RaisedAt: now.UTC(),
	}, nil
}

type IncidentStatus string

const (
	// IncidentActive 已触发但尚未确认。
	IncidentActive IncidentStatus = "active"
	// IncidentAcknowledged 已确认、尚未恢复。
	IncidentAcknowledged IncidentStatus = "acknowledged"
	// IncidentRecovered 已恢复，故障段闭环。
	IncidentRecovered IncidentStatus = "recovered"
)

type AlarmEventType string

const (
	AlarmEventRaised       AlarmEventType = "raised"
	AlarmEventRepeated     AlarmEventType = "repeated"
	AlarmEventAcknowledged AlarmEventType = "acknowledged"
	AlarmEventNote         AlarmEventType = "note"
	AlarmEventRecovered    AlarmEventType = "recovered"
)

// AlarmEvent 是故障段上的一条时间线记录，完整保留触发、再次触发、确认、
// 处置说明补充与恢复的先后顺序。
type AlarmEvent struct {
	Type    AlarmEventType `json:"type"`
	At      time.Time      `json:"at"`
	AlarmID uuid.UUID      `json:"alarm_id,omitempty"`
	Actor   string         `json:"actor,omitempty"`
	Note    string         `json:"note,omitempty"`
}

// Incident 是按处理线 + 报警类型 + 设备归并出的一段完整故障。
type Incident struct {
	ID             uuid.UUID      `json:"id"`
	LineID         LineID         `json:"line_id"`
	Code           string         `json:"code"`
	DeviceID       string         `json:"device_id"`
	Message        string         `json:"message"`
	Severity       AlarmSeverity  `json:"severity"`
	Status         IncidentStatus `json:"status"`
	FirstRaisedAt  time.Time      `json:"first_raised_at"`
	LastRaisedAt   time.Time      `json:"last_raised_at"`
	RaiseCount     int            `json:"raise_count"`
	AcknowledgedBy string         `json:"acknowledged_by,omitempty"`
	AcknowledgedAt *time.Time     `json:"acknowledged_at,omitempty"`
	Resolution     string         `json:"resolution,omitempty"`
	RecoveredAt    *time.Time     `json:"recovered_at,omitempty"`
	Occurrences    []Alarm        `json:"occurrences"`
	Timeline       []AlarmEvent   `json:"timeline"`
}

// NewIncident 用首次报警触发创建一段故障。
func NewIncident(first Alarm) Incident {
	return Incident{
		ID: uuid.New(), LineID: first.LineID, Code: first.Code, DeviceID: first.DeviceID,
		Message: first.Message, Severity: first.Severity, Status: IncidentActive,
		FirstRaisedAt: first.RaisedAt, LastRaisedAt: first.RaisedAt, RaiseCount: 1,
		Occurrences: []Alarm{first},
		Timeline:    []AlarmEvent{{Type: AlarmEventRaised, At: first.RaisedAt, AlarmID: first.ID}},
	}
}

// AttachRaise 把同故障的再次触发并入本段：保留原始记录并追加时间线。
func (in Incident) AttachRaise(alarm Alarm) Incident {
	in.Occurrences = append(in.Occurrences, alarm)
	in.Timeline = append(in.Timeline, AlarmEvent{Type: AlarmEventRepeated, At: alarm.RaisedAt, AlarmID: alarm.ID})
	in.LastRaisedAt = alarm.RaisedAt
	in.RaiseCount++
	// 最新一次触发的描述通常包含当前测量值，用于刷新故障摘要。
	in.Message = alarm.Message
	return in
}

// Acknowledge 确认故障。重复确认是安全的：已确认时保留首次确认人与时间，
// 若本次带有新的处置说明，则作为补充说明追加到时间线。
func (in Incident) Acknowledge(actor, note string, now time.Time) Incident {
	stamp := now.UTC()
	if in.AcknowledgedAt == nil {
		in.AcknowledgedAt = &stamp
		in.AcknowledgedBy = actor
		in.Timeline = append(in.Timeline, AlarmEvent{Type: AlarmEventAcknowledged, At: stamp, Actor: actor, Note: note})
	} else if note != "" && note != in.Resolution {
		in.Timeline = append(in.Timeline, AlarmEvent{Type: AlarmEventNote, At: stamp, Actor: actor, Note: note})
	}
	if note != "" {
		in.Resolution = note
	}
	if in.RecoveredAt == nil {
		in.Status = IncidentAcknowledged
	}
	return in
}

// Recover 恢复故障。重复恢复是安全的：已恢复时保留首次恢复时间，不做任何改动。
func (in Incident) Recover(note string, now time.Time) Incident {
	if in.RecoveredAt != nil {
		return in
	}
	stamp := now.UTC()
	if note != "" {
		in.Resolution = note
	}
	in.RecoveredAt = &stamp
	in.Status = IncidentRecovered
	in.Timeline = append(in.Timeline, AlarmEvent{Type: AlarmEventRecovered, At: stamp, Note: note})
	return in
}

// Active 表示故障尚未恢复（无论是否已确认）。
func (in Incident) Active() bool {
	return in.RecoveredAt == nil
}

// AlarmKey 是故障归并键：同一处理线、同一报警类型、同一设备。
type AlarmKey struct {
	LineID   LineID
	Code     string
	DeviceID string
}

func (a Alarm) Key() AlarmKey {
	return AlarmKey{LineID: a.LineID, Code: a.Code, DeviceID: a.DeviceID}
}
