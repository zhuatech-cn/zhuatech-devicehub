package hub

import "testing"

func TestDeviceTwinTelemetryAndCommand(t *testing.T) {
	s := NewService(Snapshot{})
	d, secret, err := s.Enroll("冷链传感器", "ZT-T100", "上海仓", "enroll-token")
	if err != nil || len(secret) < 8 {
		t.Fatalf("enroll failed: %v", err)
	}
	if _, err = s.UpdateDesired(d.ID, map[string]any{"sampleSeconds": 30}); err != nil {
		t.Fatal(err)
	}
	delta, err := s.Heartbeat(d.ID, "1.2.0", map[string]any{"sampleSeconds": 60})
	if err != nil || delta["sampleSeconds"] != 30 {
		t.Fatalf("invalid delta: %#v %v", delta, err)
	}
	alert, err := s.IngestTelemetry(d.ID, "temperature", 9.5, 8)
	if err != nil || alert == nil || alert.Severity != "warning" {
		t.Fatalf("alert failed: %#v %v", alert, err)
	}
	c1, err := s.IssueCommand(d.ID, "reboot", "key-1", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	c2, _ := s.IssueCommand(d.ID, "reboot", "key-1", map[string]any{})
	if c1.ID != c2.ID {
		t.Fatal("command is not idempotent")
	}
}

func TestFirmwareRolloutBatches(t *testing.T) {
	s := NewService(Snapshot{})
	a, _, _ := s.Enroll("设备A", "ZT-A", "总部", "token-123")
	b, _, _ := s.Enroll("设备B", "ZT-A", "总部", "token-456")
	r, err := s.CreateRollout("2.0.0", "1234567890abcdef", []string{a.ID, b.ID}, 1)
	if err != nil {
		t.Fatal(err)
	}
	batch, _ := s.NextRolloutBatch(r.ID)
	if len(batch) != 1 {
		t.Fatalf("want 1, got %d", len(batch))
	}
	s.NextRolloutBatch(r.ID)
	if s.Snapshot().Rollouts[0].Status != "completed" {
		t.Fatal("rollout should complete")
	}
}
