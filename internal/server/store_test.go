package server

import "testing"

func TestMemoryStoreRunLifecycle(t *testing.T) {
	store, err := NewMemoryFileStore("")
	if err != nil {
		t.Fatalf("NewMemoryFileStore error: %v", err)
	}
	meta := RunMeta{
		RunID:       "run_test_1",
		Status:      "queued",
		Source:      "test",
		CreatorType: "admin",
		CreatedAt:   nowRFC3339(),
	}
	if err := store.CreateRun(meta); err != nil {
		t.Fatalf("CreateRun error: %v", err)
	}
	event, err := store.AppendRunEvent(meta.RunID, "queue", "queued", nil)
	if err != nil {
		t.Fatalf("AppendRunEvent error: %v", err)
	}
	if event.Seq != 1 {
		t.Fatalf("expected first seq=1, got %d", event.Seq)
	}
	updated, err := store.UpdateRun(meta.RunID, func(item *RunMeta) {
		item.Status = "running"
	})
	if err != nil {
		t.Fatalf("UpdateRun error: %v", err)
	}
	if updated.Status != "running" {
		t.Fatalf("expected status running, got %s", updated.Status)
	}
}
