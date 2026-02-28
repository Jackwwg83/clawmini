package server

import (
	"errors"
	"testing"

	"github.com/raystone-ai/clawmini/internal/protocol"
)

func TestDeviceStoreCRUD(t *testing.T) {
	db := openTestDB(t)
	store := NewDeviceStore(db)
	if err := store.EnsureSchema(); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}

	reg := protocol.RegisterPayload{
		DeviceID:        "dev-1",
		Hostname:        "host-a",
		OS:              "linux",
		Arch:            "amd64",
		HasOpenClaw:     true,
		OpenClawVersion: "1.0.0",
		ClientVersion:   "0.1.0",
	}
	if err := store.UpsertDevice(reg); err != nil {
		t.Fatalf("upsert device: %v", err)
	}

	snap, err := store.GetDevice("dev-1")
	if err != nil {
		t.Fatalf("get device: %v", err)
	}
	if snap.ID != reg.DeviceID || snap.Hostname != reg.Hostname {
		t.Fatalf("snapshot mismatch: %+v", snap)
	}
	if !snap.HasOpenClaw {
		t.Fatalf("expected hasOpenClaw=true from registration")
	}
	if snap.Status != nil {
		t.Fatalf("expected nil status before heartbeat, got %+v", snap.Status)
	}
	if !snap.Online {
		t.Fatalf("expected device to be online immediately after register")
	}

	hb := protocol.HeartbeatPayload{
		DeviceID: "dev-1",
		System: protocol.SystemInfo{
			CPUUsage:  25.5,
			MemTotal:  1024,
			MemUsed:   256,
			DiskTotal: 4096,
			DiskUsed:  2000,
			Uptime:    123,
		},
		OpenClaw: protocol.OpenClawInfo{
			Installed:       true,
			Version:         "2.0.0",
			GatewayStatus:   "healthy",
			UpdateAvailable: "stable",
			Channels: []protocol.ChannelInfo{
				{Name: "alpha", Status: "ok", Messages: 7},
			},
		},
	}
	if err := store.UpdateHeartbeat(hb); err != nil {
		t.Fatalf("update heartbeat: %v", err)
	}

	snap, err = store.GetDevice("dev-1")
	if err != nil {
		t.Fatalf("get device after heartbeat: %v", err)
	}
	if snap.OpenClawVersion != "2.0.0" {
		t.Fatalf("openclaw version not updated: got %q", snap.OpenClawVersion)
	}
	if !snap.HasOpenClaw {
		t.Fatalf("expected hasOpenClaw=true after heartbeat")
	}
	if snap.Status == nil {
		t.Fatalf("expected status after heartbeat")
	}
	if snap.Status.CPUUsage != 25.5 || snap.Status.MemUsed != 256 || snap.Status.DiskUsed != 2000 {
		t.Fatalf("unexpected system status: %+v", snap.Status)
	}
	if !snap.Status.OpenClawInfo.Installed || snap.Status.OpenClawInfo.GatewayStatus != "healthy" {
		t.Fatalf("unexpected openclaw status: %+v", snap.Status.OpenClawInfo)
	}
	if len(snap.Status.OpenClawInfo.Channels) != 1 || snap.Status.OpenClawInfo.Channels[0].Name != "alpha" {
		t.Fatalf("channels not persisted: %+v", snap.Status.OpenClawInfo.Channels)
	}

	reg.Hostname = "host-b"
	reg.ClientVersion = "0.2.0"
	reg.HasOpenClaw = false
	reg.OpenClawVersion = "2.1.0"
	if err := store.UpsertDevice(reg); err != nil {
		t.Fatalf("upsert device update: %v", err)
	}
	snap, err = store.GetDevice("dev-1")
	if err != nil {
		t.Fatalf("get device after upsert update: %v", err)
	}
	if snap.Hostname != "host-b" || snap.ClientVersion != "0.2.0" || snap.OpenClawVersion != "2.1.0" {
		t.Fatalf("upsert update not applied: %+v", snap)
	}
	if snap.HasOpenClaw {
		t.Fatalf("expected hasOpenClaw=false after upsert update")
	}

	list, err := store.ListDevices()
	if err != nil {
		t.Fatalf("list devices: %v", err)
	}
	if len(list) != 1 || list[0].ID != "dev-1" {
		t.Fatalf("unexpected list result: %+v", list)
	}
}

func TestDeviceStoreNotFoundAndHeartbeatValidation(t *testing.T) {
	db := openTestDB(t)
	store := NewDeviceStore(db)
	if err := store.EnsureSchema(); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}

	_, err := store.GetDevice("does-not-exist")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}

	err = store.UpdateHeartbeat(protocol.HeartbeatPayload{
		DeviceID: "missing-device",
		System:   protocol.SystemInfo{CPUUsage: 1},
		OpenClaw: protocol.OpenClawInfo{GatewayStatus: "unknown"},
	})
	if err == nil {
		t.Fatalf("expected heartbeat update error for missing device")
	}
}
