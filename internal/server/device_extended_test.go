package server

import (
	"errors"
	"testing"

	"github.com/raystone-ai/clawmini/internal/protocol"
)

func TestDeviceStoreDeleteDevice(t *testing.T) {
	db := openTestDB(t)
	store := NewDeviceStore(db)
	if err := store.EnsureSchema(); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}

	// Seed device
	if err := store.UpsertDevice(protocol.RegisterPayload{
		DeviceID:      "dev-del",
		Hostname:      "host-a",
		OS:            "linux",
		Arch:          "amd64",
		ClientVersion: "0.1.0",
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	// Delete
	if err := store.DeleteDevice("dev-del"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	// Verify gone
	_, err := store.GetDevice("dev-del")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}

	// List should be empty
	list, err := store.ListDevices()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected 0 devices, got %d", len(list))
	}
}

func TestDeviceStoreDeleteNonExistent(t *testing.T) {
	db := openTestDB(t)
	store := NewDeviceStore(db)
	if err := store.EnsureSchema(); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}

	err := store.DeleteDevice("non-existent")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestDeviceStoreDeleteDoubleDelete(t *testing.T) {
	db := openTestDB(t)
	store := NewDeviceStore(db)
	if err := store.EnsureSchema(); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}

	if err := store.UpsertDevice(protocol.RegisterPayload{
		DeviceID:      "dev-dd",
		Hostname:      "host",
		OS:            "linux",
		Arch:          "amd64",
		ClientVersion: "0.1.0",
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	if err := store.DeleteDevice("dev-dd"); err != nil {
		t.Fatalf("first delete: %v", err)
	}

	err := store.DeleteDevice("dev-dd")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete expected ErrNotFound, got %v", err)
	}
}

func TestDeviceStoreDeleteCascadesStatus(t *testing.T) {
	db := openTestDB(t)
	store := NewDeviceStore(db)
	if err := store.EnsureSchema(); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}

	// Seed device with heartbeat
	if err := store.UpsertDevice(protocol.RegisterPayload{
		DeviceID:      "dev-cascade",
		Hostname:      "host",
		OS:            "linux",
		Arch:          "amd64",
		ClientVersion: "0.1.0",
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := store.UpdateHeartbeat(protocol.HeartbeatPayload{
		DeviceID: "dev-cascade",
		System:   protocol.SystemInfo{CPUUsage: 50},
		OpenClaw: protocol.OpenClawInfo{Installed: true, Version: "1.0"},
	}); err != nil {
		t.Fatalf("heartbeat: %v", err)
	}

	// Verify status exists
	snap, err := store.GetDevice("dev-cascade")
	if err != nil {
		t.Fatalf("get before delete: %v", err)
	}
	if snap.Status == nil {
		t.Fatalf("expected status before delete")
	}

	// Delete device
	if err := store.DeleteDevice("dev-cascade"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	// device_status should also be gone (FK cascade)
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM device_status WHERE device_id = 'dev-cascade';`).Scan(&count); err != nil {
		t.Fatalf("count status: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected device_status to be cascaded, got %d rows", count)
	}
}

func TestDeviceStoreDeleteCascadesCommands(t *testing.T) {
	db := openTestDB(t)
	deviceStore := NewDeviceStore(db)
	if err := deviceStore.EnsureSchema(); err != nil {
		t.Fatalf("ensure device schema: %v", err)
	}
	commandStore := NewCommandStore(db)
	if err := commandStore.EnsureSchema(); err != nil {
		t.Fatalf("ensure command schema: %v", err)
	}

	// Seed device and command
	if err := deviceStore.UpsertDevice(protocol.RegisterPayload{
		DeviceID:      "dev-cmd-cascade",
		Hostname:      "host",
		OS:            "linux",
		Arch:          "amd64",
		ClientVersion: "0.1.0",
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	cmd, err := commandStore.Create("dev-cmd-cascade", "openclaw", []string{"status"}, 60)
	if err != nil {
		t.Fatalf("create command: %v", err)
	}

	// Delete device (FK ON DELETE CASCADE should remove commands)
	if err := deviceStore.DeleteDevice("dev-cmd-cascade"); err != nil {
		t.Fatalf("delete device: %v", err)
	}

	_, err = commandStore.GetByDeviceAndID("dev-cmd-cascade", cmd.ID)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected command to be cascade deleted, got %v", err)
	}
}

func TestDeviceStoreDeleteEmptyID(t *testing.T) {
	db := openTestDB(t)
	store := NewDeviceStore(db)
	if err := store.EnsureSchema(); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}

	err := store.DeleteDevice("")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for empty id, got %v", err)
	}
}

func TestCommandStoreDeleteByDevice(t *testing.T) {
	db := openTestDB(t)
	deviceStore := NewDeviceStore(db)
	if err := deviceStore.EnsureSchema(); err != nil {
		t.Fatalf("ensure device schema: %v", err)
	}
	commandStore := NewCommandStore(db)
	if err := commandStore.EnsureSchema(); err != nil {
		t.Fatalf("ensure command schema: %v", err)
	}

	// Create two devices
	for _, id := range []string{"dev-a", "dev-b"} {
		if err := deviceStore.UpsertDevice(protocol.RegisterPayload{
			DeviceID: id, Hostname: "host", OS: "linux", Arch: "amd64", ClientVersion: "0.1.0",
		}); err != nil {
			t.Fatalf("upsert %s: %v", id, err)
		}
	}

	// Create commands for each
	cmdA, err := commandStore.Create("dev-a", "openclaw", []string{"status"}, 60)
	if err != nil {
		t.Fatalf("create cmd-a: %v", err)
	}
	cmdB, err := commandStore.Create("dev-b", "openclaw", []string{"status"}, 60)
	if err != nil {
		t.Fatalf("create cmd-b: %v", err)
	}

	// Delete commands for dev-a only
	if err := commandStore.DeleteByDevice("dev-a"); err != nil {
		t.Fatalf("delete by device: %v", err)
	}

	// dev-a command should be gone
	_, err = commandStore.GetByDeviceAndID("dev-a", cmdA.ID)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected dev-a command deleted, got %v", err)
	}

	// dev-b command should still exist
	got, err := commandStore.GetByDeviceAndID("dev-b", cmdB.ID)
	if err != nil {
		t.Fatalf("dev-b command should exist: %v", err)
	}
	if got.ID != cmdB.ID {
		t.Fatalf("dev-b command mismatch")
	}
}

func TestDeviceStoreMultipleDevicesListOrder(t *testing.T) {
	db := openTestDB(t)
	store := NewDeviceStore(db)
	if err := store.EnsureSchema(); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}

	// Create 3 devices
	for _, id := range []string{"dev-1", "dev-2", "dev-3"} {
		if err := store.UpsertDevice(protocol.RegisterPayload{
			DeviceID: id, Hostname: "host-" + id, OS: "linux", Arch: "amd64", ClientVersion: "0.1.0",
		}); err != nil {
			t.Fatalf("upsert %s: %v", id, err)
		}
	}

	list, err := store.ListDevices()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("expected 3, got %d", len(list))
	}

	// Delete middle device
	if err := store.DeleteDevice("dev-2"); err != nil {
		t.Fatalf("delete dev-2: %v", err)
	}

	list, err = store.ListDevices()
	if err != nil {
		t.Fatalf("list after delete: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 after delete, got %d", len(list))
	}

	ids := map[string]bool{}
	for _, snap := range list {
		ids[snap.ID] = true
	}
	if ids["dev-2"] {
		t.Fatalf("deleted device should not appear in list")
	}
	if !ids["dev-1"] || !ids["dev-3"] {
		t.Fatalf("remaining devices missing from list: %+v", ids)
	}
}

func TestDeviceStoreUpdateOpenClawState(t *testing.T) {
	db := openTestDB(t)
	store := NewDeviceStore(db)
	if err := store.EnsureSchema(); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}

	if err := store.UpsertDevice(protocol.RegisterPayload{
		DeviceID:      "dev-openclaw-state",
		Hostname:      "host",
		OS:            "linux",
		Arch:          "amd64",
		ClientVersion: "0.1.0",
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	if err := store.UpdateOpenClawState("dev-openclaw-state", true, "openclaw v9.9.9"); err != nil {
		t.Fatalf("update openclaw state: %v", err)
	}

	snap, err := store.GetDevice("dev-openclaw-state")
	if err != nil {
		t.Fatalf("get device: %v", err)
	}
	if !snap.HasOpenClaw {
		t.Fatalf("expected hasOpenClaw=true")
	}
	if snap.OpenClawVersion != "openclaw v9.9.9" {
		t.Fatalf("unexpected version: %q", snap.OpenClawVersion)
	}
	if snap.Status == nil {
		t.Fatalf("expected device status row to exist")
	}
	if !snap.Status.OpenClawInfo.Installed {
		t.Fatalf("expected status.openclaw.installed=true")
	}
}
