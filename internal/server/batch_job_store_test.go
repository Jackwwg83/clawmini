package server

import (
	"errors"
	"testing"
)

func TestBatchJobStoreCreateGetUpdate(t *testing.T) {
	db := openTestDB(t)
	store := NewBatchJobStore(db)
	if err := store.EnsureSchema(); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}

	job, err := store.Create("openclaw gateway restart", []string{"dev-a", "dev-b"})
	if err != nil {
		t.Fatalf("create batch job: %v", err)
	}
	if job.ID == "" {
		t.Fatalf("expected job id")
	}
	if job.Status != "queued" {
		t.Fatalf("expected queued status, got %q", job.Status)
	}
	if job.TotalCount != 2 || job.PendingCount != 2 {
		t.Fatalf("unexpected initial counts: %+v", job)
	}

	if err := store.SetStatus(job.ID, "running"); err != nil {
		t.Fatalf("set status: %v", err)
	}
	if err := store.UpdateItem(job.ID, "dev-a", "success", "cmd-a", ""); err != nil {
		t.Fatalf("update item dev-a: %v", err)
	}
	if err := store.UpdateItem(job.ID, "dev-b", "error", "cmd-b", "offline"); err != nil {
		t.Fatalf("update item dev-b: %v", err)
	}

	got, err := store.Get(job.ID)
	if err != nil {
		t.Fatalf("get batch job: %v", err)
	}
	if got.SuccessCount != 1 || got.ErrorCount != 1 || got.PendingCount != 0 {
		t.Fatalf("unexpected counts after updates: %+v", got)
	}
	if got.Items[0].CommandID == "" || got.Items[1].CommandID == "" {
		t.Fatalf("expected command ids to be persisted")
	}
}

func TestBatchJobStoreNotFound(t *testing.T) {
	db := openTestDB(t)
	store := NewBatchJobStore(db)
	if err := store.EnsureSchema(); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}

	_, err := store.Get("missing")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
