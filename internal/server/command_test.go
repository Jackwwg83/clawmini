package server

import (
	"errors"
	"testing"

	"github.com/raystone-ai/clawmini/internal/protocol"
)

func TestCommandStoreOperations(t *testing.T) {
	db := openTestDB(t)

	deviceStore := NewDeviceStore(db)
	if err := deviceStore.EnsureSchema(); err != nil {
		t.Fatalf("ensure device schema: %v", err)
	}
	commandStore := NewCommandStore(db)
	if err := commandStore.EnsureSchema(); err != nil {
		t.Fatalf("ensure command schema: %v", err)
	}

	if err := deviceStore.UpsertDevice(protocol.RegisterPayload{
		DeviceID:      "dev-1",
		Hostname:      "host-a",
		OS:            "linux",
		Arch:          "amd64",
		ClientVersion: "0.1.0",
	}); err != nil {
		t.Fatalf("seed device: %v", err)
	}

	rec, err := commandStore.Create("dev-1", "openclaw", []string{"status", "--json"}, 0)
	if err != nil {
		t.Fatalf("create command: %v", err)
	}
	if rec.ID == "" {
		t.Fatalf("expected command id to be set")
	}
	if rec.Status != "queued" {
		t.Fatalf("unexpected create status: %q", rec.Status)
	}
	if rec.Timeout != 60 {
		t.Fatalf("expected default timeout=60, got %d", rec.Timeout)
	}

	got, err := commandStore.GetByDeviceAndID("dev-1", rec.ID)
	if err != nil {
		t.Fatalf("get command: %v", err)
	}
	if got.Command != "openclaw" || len(got.Args) != 2 || got.Args[0] != "status" {
		t.Fatalf("stored command mismatch: %+v", got)
	}

	if err := commandStore.MarkSent(rec.ID); err != nil {
		t.Fatalf("mark sent: %v", err)
	}
	got, err = commandStore.GetByDeviceAndID("dev-1", rec.ID)
	if err != nil {
		t.Fatalf("get after mark sent: %v", err)
	}
	if got.Status != "sent" {
		t.Fatalf("expected sent status, got %q", got.Status)
	}

	result := protocol.ResultPayload{
		CommandID:  rec.ID,
		ExitCode:   0,
		Stdout:     "ok",
		Stderr:     "",
		DurationMs: 150,
	}
	if err := commandStore.Complete("dev-1", result); err != nil {
		t.Fatalf("complete command: %v", err)
	}
	got, err = commandStore.GetByDeviceAndID("dev-1", rec.ID)
	if err != nil {
		t.Fatalf("get after complete: %v", err)
	}
	if got.Status != "completed" {
		t.Fatalf("expected completed status, got %q", got.Status)
	}
	if got.ExitCode == nil || *got.ExitCode != 0 {
		t.Fatalf("expected exit code 0, got %+v", got.ExitCode)
	}
	if got.DurationMs == nil || *got.DurationMs != 150 {
		t.Fatalf("expected duration 150ms, got %+v", got.DurationMs)
	}
	if got.Stdout != "ok" {
		t.Fatalf("stdout mismatch: %q", got.Stdout)
	}

	failed, err := commandStore.Create("dev-1", "openclaw", []string{"doctor"}, 5)
	if err != nil {
		t.Fatalf("create second command: %v", err)
	}
	if err := commandStore.MarkFailed(failed.ID, "device offline"); err != nil {
		t.Fatalf("mark failed: %v", err)
	}
	failedGot, err := commandStore.GetByDeviceAndID("dev-1", failed.ID)
	if err != nil {
		t.Fatalf("get failed command: %v", err)
	}
	if failedGot.Status != "failed" || failedGot.Stderr != "device offline" {
		t.Fatalf("failed command mismatch: %+v", failedGot)
	}
}

func TestCommandStoreGetNotFound(t *testing.T) {
	db := openTestDB(t)

	deviceStore := NewDeviceStore(db)
	if err := deviceStore.EnsureSchema(); err != nil {
		t.Fatalf("ensure device schema: %v", err)
	}
	commandStore := NewCommandStore(db)
	if err := commandStore.EnsureSchema(); err != nil {
		t.Fatalf("ensure command schema: %v", err)
	}

	_, err := commandStore.GetByDeviceAndID("dev-1", "cmd-missing")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestCommandStoreFailExpiredSent(t *testing.T) {
	db := openTestDB(t)

	deviceStore := NewDeviceStore(db)
	if err := deviceStore.EnsureSchema(); err != nil {
		t.Fatalf("ensure device schema: %v", err)
	}
	commandStore := NewCommandStore(db)
	if err := commandStore.EnsureSchema(); err != nil {
		t.Fatalf("ensure command schema: %v", err)
	}

	if err := deviceStore.UpsertDevice(protocol.RegisterPayload{
		DeviceID:      "dev-1",
		Hostname:      "host-a",
		OS:            "linux",
		Arch:          "amd64",
		ClientVersion: "0.1.0",
	}); err != nil {
		t.Fatalf("seed device: %v", err)
	}

	expired, err := commandStore.Create("dev-1", "openclaw", []string{"status"}, 5)
	if err != nil {
		t.Fatalf("create expired command: %v", err)
	}
	if err := commandStore.MarkSent(expired.ID); err != nil {
		t.Fatalf("mark sent expired command: %v", err)
	}
	if _, err := db.Exec(`UPDATE commands SET updated_at=? WHERE id=?;`, nowUnix()-40, expired.ID); err != nil {
		t.Fatalf("age expired command: %v", err)
	}

	active, err := commandStore.Create("dev-1", "openclaw", []string{"status"}, 60)
	if err != nil {
		t.Fatalf("create active command: %v", err)
	}
	if err := commandStore.MarkSent(active.ID); err != nil {
		t.Fatalf("mark sent active command: %v", err)
	}

	affected, err := commandStore.FailExpiredSent(30)
	if err != nil {
		t.Fatalf("fail expired sent: %v", err)
	}
	if affected != 1 {
		t.Fatalf("expected 1 affected row, got %d", affected)
	}

	expiredGot, err := commandStore.GetByDeviceAndID("dev-1", expired.ID)
	if err != nil {
		t.Fatalf("get expired command: %v", err)
	}
	if expiredGot.Status != "failed" || expiredGot.Stderr != "timeout" {
		t.Fatalf("expected expired command failed/timeout, got %+v", expiredGot)
	}

	activeGot, err := commandStore.GetByDeviceAndID("dev-1", active.ID)
	if err != nil {
		t.Fatalf("get active command: %v", err)
	}
	if activeGot.Status != "sent" {
		t.Fatalf("expected active command to remain sent, got %q", activeGot.Status)
	}
}
