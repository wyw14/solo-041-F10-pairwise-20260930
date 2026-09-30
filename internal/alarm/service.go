package alarm

import (
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/wyw14/cry-97/internal/model"
)

// Filter 控制故障段列表的状态过滤。
type Filter string

const (
	FilterAll          Filter = ""
	FilterActive       Filter = Filter(model.IncidentActive)
	FilterAcknowledged Filter = Filter(model.IncidentAcknowledged)
	FilterRecovered    Filter = Filter(model.IncidentRecovered)
)

var ErrIncidentNotFound = errors.New("alarm incident is not found")

type Service struct {
	mu        sync.RWMutex
	incidents map[uuid.UUID]model.Incident
	// open 指向每个归并键当前尚未恢复的故障段；恢复后清空，下一次触发开新段。
	open map[model.AlarmKey]uuid.UUID
}

func NewService() *Service {
	return &Service{incidents: make(map[uuid.UUID]model.Incident), open: make(map[model.AlarmKey]uuid.UUID)}
}

// Raise 接收一条原始报警。若同处理线、同类型、同设备存在尚未恢复的故障段，
// 则作为"再次触发"并入该段；否则开启一段新故障。返回归并后的故障段。
func (s *Service) Raise(alarm model.Alarm) model.Incident {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := alarm.Key()
	if id, ok := s.open[key]; ok {
		incident := s.incidents[id].AttachRaise(alarm)
		s.incidents[id] = incident
		return incident
	}
	incident := model.NewIncident(alarm)
	s.incidents[incident.ID] = incident
	s.open[key] = incident.ID
	return incident
}

// Acknowledge 确认故障段。重复确认是安全的：保留首次确认人与时间，
// 仅在提供了新处置说明时把补充说明追加到时间线。
func (s *Service) Acknowledge(id uuid.UUID, actor, note string, now time.Time) (model.Incident, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	incident, ok := s.incidents[id]
	if !ok {
		return model.Incident{}, ErrIncidentNotFound
	}
	incident = incident.Acknowledge(actor, note, now)
	s.incidents[id] = incident
	return incident, nil
}

// Recover 恢复故障段。重复恢复请求是安全的：已恢复时原样返回当前故障段，
// 不覆盖首次恢复时间。
func (s *Service) Recover(id uuid.UUID, note string, now time.Time) (model.Incident, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	incident, ok := s.incidents[id]
	if !ok {
		return model.Incident{}, ErrIncidentNotFound
	}
	if incident.RecoveredAt == nil {
		incident = incident.Recover(note, now)
		s.incidents[id] = incident
		delete(s.open, model.AlarmKey{LineID: incident.LineID, Code: incident.Code, DeviceID: incident.DeviceID})
	}
	return incident, nil
}

// Active 返回处理线上所有尚未恢复的故障段（含已确认待恢复的）。
func (s *Service) Active(lineID model.LineID) []model.Incident {
	return s.list(lineID, FilterActive)
}

// ActiveCount 用于状态面板计数。
func (s *Service) ActiveCount(lineID model.LineID) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	count := 0
	for _, incident := range s.incidents {
		if incident.LineID == lineID && incident.Active() {
			count++
		}
	}
	return count
}

// List 按状态过滤故障段：active / acknowledged / recovered，空过滤返回全部。
// lineID 为空时跨所有处理线返回。
func (s *Service) List(lineID model.LineID, filter Filter) []model.Incident {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.listLocked(lineID, filter)
}

func (s *Service) list(lineID model.LineID, filter Filter) []model.Incident {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.listLocked(lineID, filter)
}

func (s *Service) listLocked(lineID model.LineID, filter Filter) []model.Incident {
	result := make([]model.Incident, 0)
	for _, incident := range s.incidents {
		if lineID != "" && incident.LineID != lineID {
			continue
		}
		if filter != FilterAll && incident.Status != model.IncidentStatus(filter) {
			continue
		}
		result = append(result, incident)
	}
	// 最近首次触发的故障段排在前面；同段内的时间线保持写入顺序。
	sort.Slice(result, func(i, j int) bool {
		return result[i].FirstRaisedAt.After(result[j].FirstRaisedAt)
	})
	return result
}

// Get 返回单段故障（含完整原始时间线）。
func (s *Service) Get(id uuid.UUID) (model.Incident, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	incident, ok := s.incidents[id]
	if !ok {
		return model.Incident{}, ErrIncidentNotFound
	}
	return incident, nil
}
