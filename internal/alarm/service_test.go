package alarm

import (
	"testing"
	"time"

	"github.com/wyw14/cry-97/internal/model"
)

func testAlarm(line model.LineID, code, device string, at time.Time) model.Alarm {
	raised, err := model.NewAlarm(line, code, device, "blanket high", model.SeverityWarning, at)
	if err != nil {
		panic(err)
	}
	return raised
}

func TestRaiseMergesRepeatedTriggersByLineCodeDevice(t *testing.T) {
	svc := NewService()
	base := time.Date(2026, 9, 30, 8, 0, 0, 0, time.UTC)

	first := svc.Raise(testAlarm("line-1", "SETTLING_BLANKET_HIGH", "settler-line-1", base))
	svc.Raise(testAlarm("line-1", "SETTLING_BLANKET_HIGH", "settler-line-1", base.Add(time.Minute)))
	// 同类型但不同设备，必须独立成段。
	other := svc.Raise(testAlarm("line-1", "SETTLING_BLANKET_HIGH", "settler-line-2", base.Add(2*time.Minute)))

	if first.ID == other.ID {
		t.Fatal("different device must open a separate incident")
	}
	got, err := svc.Get(first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.RaiseCount != 2 || len(got.Occurrences) != 2 {
		t.Fatalf("expected 2 merged occurrences, got count=%d len=%d", got.RaiseCount, len(got.Occurrences))
	}
	if !got.LastRaisedAt.Equal(base.Add(time.Minute)) {
		t.Fatalf("last raised at not updated: %v", got.LastRaisedAt)
	}
	if len(got.Timeline) != 2 || got.Timeline[0].Type != model.AlarmEventRaised || got.Timeline[1].Type != model.AlarmEventRepeated {
		t.Fatalf("timeline must keep raised then repeated, got %#v", got.Timeline)
	}
	if got.Status != model.IncidentActive {
		t.Fatalf("fresh incident must be active, got %s", got.Status)
	}
}

func TestRecoveryClosesIncidentAndReopensOnNextTrigger(t *testing.T) {
	svc := NewService()
	base := time.Date(2026, 9, 30, 8, 0, 0, 0, time.UTC)

	first := svc.Raise(testAlarm("line-1", "SETTLING_BLANKET_HIGH", "settler-line-1", base))
	recovered, err := svc.Recover(first.ID, "drained settler", base.Add(5*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Status != model.IncidentRecovered || recovered.RecoveredAt == nil {
		t.Fatalf("incident must be recovered: %#v", recovered)
	}
	// 恢复后再次触发是一段新故障，历史段不被改动。
	next := svc.Raise(testAlarm("line-1", "SETTLING_BLANKET_HIGH", "settler-line-1", base.Add(10*time.Minute)))
	if next.ID == first.ID {
		t.Fatal("raise after recovery must open a new incident")
	}
	if len(svc.List("", FilterRecovered)) != 1 || len(svc.List("", FilterActive)) != 1 {
		t.Fatal("filters must separate recovered and active incidents")
	}
}

func TestDuplicateAcknowledgeKeepsFirstResponder(t *testing.T) {
	svc := NewService()
	base := time.Date(2026, 9, 30, 8, 0, 0, 0, time.UTC)
	id := svc.Raise(testAlarm("line-1", "CODE", "dev", base)).ID

	first, err := svc.Acknowledge(id, "alice", "checking pump", base.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	again, err := svc.Acknowledge(id, "bob", "still checking", base.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if again.AcknowledgedBy != "alice" || !again.AcknowledgedAt.Equal(*first.AcknowledgedAt) {
		t.Fatalf("repeat ack must preserve first responder/time, got %s %v", again.AcknowledgedBy, again.AcknowledgedAt)
	}
	if again.Resolution != "still checking" {
		t.Fatalf("repeat ack may refresh the resolution note, got %q", again.Resolution)
	}
	// 时间线应有：raised、acknowledged、note 三条，确认事件只记一次。
	acks, notes := 0, 0
	for _, event := range again.Timeline {
		switch event.Type {
		case model.AlarmEventAcknowledged:
			acks++
		case model.AlarmEventNote:
			notes++
		}
	}
	if acks != 1 || notes != 1 {
		t.Fatalf("expected one ack event and one note event, got acks=%d notes=%d", acks, notes)
	}
}

func TestDuplicateRecoverKeepsFirstRecoveryTime(t *testing.T) {
	svc := NewService()
	base := time.Date(2026, 9, 30, 8, 0, 0, 0, time.UTC)
	id := svc.Raise(testAlarm("line-1", "CODE", "dev", base)).ID

	first, err := svc.Recover(id, "", base.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	again, err := svc.Recover(id, "second request", base.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if !again.RecoveredAt.Equal(*first.RecoveredAt) {
		t.Fatal("repeat recover must not overwrite first recovery time")
	}
	if again.Resolution != "" {
		t.Fatal("repeat recover must not mutate the closed incident")
	}
	recoverEvents := 0
	for _, event := range again.Timeline {
		if event.Type == model.AlarmEventRecovered {
			recoverEvents++
		}
	}
	if recoverEvents != 1 {
		t.Fatalf("expected exactly one recovered event, got %d", recoverEvents)
	}
}

func TestAcknowledgeThenRepeatedTriggerStaysMergedUntilRecovery(t *testing.T) {
	svc := NewService()
	base := time.Date(2026, 9, 30, 8, 0, 0, 0, time.UTC)
	id := svc.Raise(testAlarm("line-1", "CODE", "dev", base)).ID
	if _, err := svc.Acknowledge(id, "alice", "", base.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	// 已确认但未恢复期间的再次触发仍并入同一段。
	svc.Raise(testAlarm("line-1", "CODE", "dev", base.Add(2*time.Minute)))
	got, err := svc.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if got.RaiseCount != 2 || got.Status != model.IncidentAcknowledged {
		t.Fatalf("repeated trigger after ack must merge and stay acknowledged, got %d %s", got.RaiseCount, got.Status)
	}
}
