package server

import (
	"errors"
	"testing"
	"time"
)

func TestIMConfigJobStore_PersistsAcrossRecreation(t *testing.T) {
	db := openTestDB(t)

	store := NewIMConfigJobStore(db)
	if err := store.EnsureSchema(); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}

	job, err := store.Create("dev-1", "dingtalk", []IMConfigStep{
		{
			Key:            "install-plugin",
			Title:          "Install plugin",
			DisplayCommand: "openclaw plugins install clawdbot-dingtalk",
			Status:         "pending",
		},
	})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}

	if err := store.Update(job.ID, func(job *IMConfigJob) {
		job.Status = "running"
		job.Error = ""
		job.Steps[0].Status = "running"
		job.Steps[0].Record = &CommandRecord{
			ID:       "cmd-1",
			DeviceID: "dev-1",
			Command:  "openclaw",
			Args:     []string{"plugins", "install", "clawdbot-dingtalk"},
			Status:   "sent",
		}
	}); err != nil {
		t.Fatalf("update job: %v", err)
	}

	recreated := NewIMConfigJobStore(db)
	if err := recreated.EnsureSchema(); err != nil {
		t.Fatalf("ensure schema recreated: %v", err)
	}
	got, err := recreated.Get("dev-1", job.ID)
	if err != nil {
		t.Fatalf("get persisted job: %v", err)
	}

	if got.Status != "running" {
		t.Fatalf("status = %q, want %q", got.Status, "running")
	}
	if len(got.Steps) != 1 {
		t.Fatalf("steps len = %d, want 1", len(got.Steps))
	}
	if got.Steps[0].Record == nil || got.Steps[0].Record.ID != "cmd-1" {
		t.Fatalf("expected persisted command record, got %+v", got.Steps[0].Record)
	}
}

func TestIMConfigJobStore_GCDeletesOldJobs(t *testing.T) {
	db := openTestDB(t)

	store := newIMConfigJobStoreWithIntervals(db, 20*time.Millisecond, 60*time.Millisecond)
	if err := store.EnsureSchema(); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}

	oldJob, err := store.Create("dev-old", "dingtalk", []IMConfigStep{
		{Key: "old", Title: "Old", DisplayCommand: "old", Status: "pending"},
	})
	if err != nil {
		t.Fatalf("create old job: %v", err)
	}
	recentJob, err := store.Create("dev-new", "dingtalk", []IMConfigStep{
		{Key: "new", Title: "New", DisplayCommand: "new", Status: "pending"},
	})
	if err != nil {
		t.Fatalf("create recent job: %v", err)
	}

	stale := nowUnix() - 3600
	if _, err := db.Exec(`UPDATE im_config_jobs SET created_at = ?, updated_at = ? WHERE id = ?;`, stale, stale, oldJob.ID); err != nil {
		t.Fatalf("mark old job stale: %v", err)
	}

	store.Start()
	defer store.Stop()

	waitForCondition(t, 2*time.Second, func() bool {
		_, err := store.Get("dev-old", oldJob.ID)
		return errors.Is(err, ErrNotFound)
	}, "old IM config job should be garbage-collected")

	if _, err := store.Get("dev-new", recentJob.ID); err != nil {
		t.Fatalf("recent job should remain, got error: %v", err)
	}
}
