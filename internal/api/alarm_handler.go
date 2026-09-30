package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/wyw14/cry-97/internal/alarm"
)

type alarmActionRequest struct {
	AcknowledgedBy string `json:"acknowledged_by"`
	Resolution     string `json:"resolution"`
}

func (s *Server) alarmList(w http.ResponseWriter, r *http.Request) {
	filter := alarm.FilterAll
	switch raw := strings.TrimSpace(r.URL.Query().Get("status")); raw {
	case "":
	case string(alarm.FilterActive), string(alarm.FilterAcknowledged), string(alarm.FilterRecovered):
		filter = alarm.Filter(raw)
	default:
		writeError(w, http.StatusBadRequest, errors.New(`status must be one of active, acknowledged, recovered`))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"incidents": s.plant.Incidents("", filter)})
}

func (s *Server) alarmDetail(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "alarmID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	incident, err := s.plant.Incident(id)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, incident)
}

func (s *Server) acknowledgeAlarm(w http.ResponseWriter, r *http.Request) {
	id, input, ok := decodeAlarmAction(w, r)
	if !ok {
		return
	}
	// 重复确认是安全的：服务端保留首次确认人与时间，幂等返回当前故障段。
	incident, err := s.plant.AcknowledgeAlarm(id, strings.TrimSpace(input.AcknowledgedBy), strings.TrimSpace(input.Resolution))
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, incident)
}

func (s *Server) recoverAlarm(w http.ResponseWriter, r *http.Request) {
	id, input, ok := decodeAlarmAction(w, r)
	if !ok {
		return
	}
	// 重复恢复请求是安全的：已恢复时原样返回，不覆盖首次恢复时间。
	incident, err := s.plant.RecoverAlarm(id, strings.TrimSpace(input.Resolution))
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, incident)
}

func decodeAlarmAction(w http.ResponseWriter, r *http.Request) (uuid.UUID, alarmActionRequest, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "alarmID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return uuid.Nil, alarmActionRequest{}, false
	}
	var input alarmActionRequest
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, err)
		return uuid.Nil, alarmActionRequest{}, false
	}
	return id, input, true
}
