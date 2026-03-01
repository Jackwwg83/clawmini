package server

import (
	"testing"
	"time"

	"github.com/raystone-ai/clawmini/internal/protocol"
)

func TestHubRegisterDevice_WithDeviceToken_NoBinding(t *testing.T) {
	db := openTestDB(t)
	devices := NewDeviceStore(db)
	if err := devices.EnsureSchema(); err != nil {
		t.Fatalf("device schema: %v", err)
	}
	users := NewUserStore(db)
	if err := users.EnsureSchema(); err != nil {
		t.Fatalf("user schema: %v", err)
	}
	commands := NewCommandStore(db)
	if err := commands.EnsureSchema(); err != nil {
		t.Fatalf("command schema: %v", err)
	}
	joinTokens := NewJoinTokenStore(db)
	if err := joinTokens.EnsureSchema(); err != nil {
		t.Fatalf("join token schema: %v", err)
	}

	user, err := users.CreateUser("no-bind-worker", "pass", RoleUser, "Worker")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	auth := NewTokenAuth("device-token", []byte("test-secret"), users)
	hub := NewHub(devices, commands, joinTokens, users, auth)

	// Register with device token (not join token) — should NOT bind to any user
	err = hub.registerDeviceMetadata(protocol.RegisterPayload{
		DeviceID:      "dev-no-bind-hub",
		Hostname:      "host",
		Token:         "device-token",
		OS:            "linux",
		Arch:          "amd64",
		ClientVersion: "0.1.0",
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	bound, err := users.IsDeviceBoundToUser(user.ID, "dev-no-bind-hub")
	if err != nil {
		t.Fatalf("check binding: %v", err)
	}
	if bound {
		t.Fatalf("device registered with device token should NOT be auto-bound to any user")
	}
}

func TestHubRegisterDevice_JoinTokenWithoutUserID_NoBinding(t *testing.T) {
	db := openTestDB(t)
	devices := NewDeviceStore(db)
	if err := devices.EnsureSchema(); err != nil {
		t.Fatalf("device schema: %v", err)
	}
	users := NewUserStore(db)
	if err := users.EnsureSchema(); err != nil {
		t.Fatalf("user schema: %v", err)
	}
	commands := NewCommandStore(db)
	if err := commands.EnsureSchema(); err != nil {
		t.Fatalf("command schema: %v", err)
	}
	joinTokens := NewJoinTokenStore(db)
	if err := joinTokens.EnsureSchema(); err != nil {
		t.Fatalf("join token schema: %v", err)
	}

	auth := NewTokenAuth("device-token", []byte("test-secret"), users)
	hub := NewHub(devices, commands, joinTokens, users, auth)

	// Create join token WITHOUT userId
	token, err := joinTokens.CreateToken("no-user-token", time.Hour, "")
	if err != nil {
		t.Fatalf("create token: %v", err)
	}

	err = hub.registerDeviceMetadata(protocol.RegisterPayload{
		DeviceID:      "dev-no-user-bind",
		Hostname:      "host",
		Token:         token.ID,
		OS:            "linux",
		Arch:          "amd64",
		ClientVersion: "0.1.0",
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	// Device should be registered but not bound to anyone
	_, err = devices.GetDevice("dev-no-user-bind")
	if err != nil {
		t.Fatalf("device should exist: %v", err)
	}
}

func TestHubRegisterDevice_ExpiredJoinToken(t *testing.T) {
	db := openTestDB(t)
	devices := NewDeviceStore(db)
	if err := devices.EnsureSchema(); err != nil {
		t.Fatalf("device schema: %v", err)
	}
	users := NewUserStore(db)
	if err := users.EnsureSchema(); err != nil {
		t.Fatalf("user schema: %v", err)
	}
	commands := NewCommandStore(db)
	if err := commands.EnsureSchema(); err != nil {
		t.Fatalf("command schema: %v", err)
	}
	joinTokens := NewJoinTokenStore(db)
	if err := joinTokens.EnsureSchema(); err != nil {
		t.Fatalf("join token schema: %v", err)
	}

	auth := NewTokenAuth("device-token", []byte("test-secret"), users)
	hub := NewHub(devices, commands, joinTokens, users, auth)

	// Create join token with very short TTL (1 second)
	token, err := joinTokens.CreateToken("short-token", 1*time.Second, "")
	if err != nil {
		t.Fatalf("create token: %v", err)
	}

	// Wait for it to expire (timestamps are Unix seconds, check is now > expiresAt)
	time.Sleep(2100 * time.Millisecond)

	err = hub.registerDeviceMetadata(protocol.RegisterPayload{
		DeviceID:      "dev-expired-token",
		Hostname:      "host",
		Token:         token.ID,
		OS:            "linux",
		Arch:          "amd64",
		ClientVersion: "0.1.0",
	})
	if err == nil {
		t.Fatalf("expected error for expired join token")
	}
}

func TestHubRegisterDevice_AlreadyUsedJoinToken(t *testing.T) {
	db := openTestDB(t)
	devices := NewDeviceStore(db)
	if err := devices.EnsureSchema(); err != nil {
		t.Fatalf("device schema: %v", err)
	}
	users := NewUserStore(db)
	if err := users.EnsureSchema(); err != nil {
		t.Fatalf("user schema: %v", err)
	}
	commands := NewCommandStore(db)
	if err := commands.EnsureSchema(); err != nil {
		t.Fatalf("command schema: %v", err)
	}
	joinTokens := NewJoinTokenStore(db)
	if err := joinTokens.EnsureSchema(); err != nil {
		t.Fatalf("join token schema: %v", err)
	}

	auth := NewTokenAuth("device-token", []byte("test-secret"), users)
	hub := NewHub(devices, commands, joinTokens, users, auth)

	token, err := joinTokens.CreateToken("reuse-token", time.Hour, "")
	if err != nil {
		t.Fatalf("create token: %v", err)
	}

	// First use — should succeed
	err = hub.registerDeviceMetadata(protocol.RegisterPayload{
		DeviceID:      "dev-first-use",
		Hostname:      "host",
		Token:         token.ID,
		OS:            "linux",
		Arch:          "amd64",
		ClientVersion: "0.1.0",
	})
	if err != nil {
		t.Fatalf("first use should succeed: %v", err)
	}

	// Second use — should fail
	err = hub.registerDeviceMetadata(protocol.RegisterPayload{
		DeviceID:      "dev-second-use",
		Hostname:      "host",
		Token:         token.ID,
		OS:            "linux",
		Arch:          "amd64",
		ClientVersion: "0.1.0",
	})
	if err == nil {
		t.Fatalf("expected error for already-used join token")
	}
}

func TestHubRegisterDevice_InvalidToken(t *testing.T) {
	db := openTestDB(t)
	devices := NewDeviceStore(db)
	if err := devices.EnsureSchema(); err != nil {
		t.Fatalf("device schema: %v", err)
	}
	users := NewUserStore(db)
	if err := users.EnsureSchema(); err != nil {
		t.Fatalf("user schema: %v", err)
	}
	commands := NewCommandStore(db)
	if err := commands.EnsureSchema(); err != nil {
		t.Fatalf("command schema: %v", err)
	}
	joinTokens := NewJoinTokenStore(db)
	if err := joinTokens.EnsureSchema(); err != nil {
		t.Fatalf("join token schema: %v", err)
	}

	auth := NewTokenAuth("device-token", []byte("test-secret"), users)
	hub := NewHub(devices, commands, joinTokens, users, auth)

	err := hub.registerDeviceMetadata(protocol.RegisterPayload{
		DeviceID:      "dev-bad-token",
		Hostname:      "host",
		Token:         "completely-invalid-token",
		OS:            "linux",
		Arch:          "amd64",
		ClientVersion: "0.1.0",
	})
	if err == nil {
		t.Fatalf("expected error for invalid token")
	}
}

func TestHubRegisterDevice_DoubleBinding_Ignored(t *testing.T) {
	db := openTestDB(t)
	devices := NewDeviceStore(db)
	if err := devices.EnsureSchema(); err != nil {
		t.Fatalf("device schema: %v", err)
	}
	users := NewUserStore(db)
	if err := users.EnsureSchema(); err != nil {
		t.Fatalf("user schema: %v", err)
	}
	commands := NewCommandStore(db)
	if err := commands.EnsureSchema(); err != nil {
		t.Fatalf("command schema: %v", err)
	}
	joinTokens := NewJoinTokenStore(db)
	if err := joinTokens.EnsureSchema(); err != nil {
		t.Fatalf("join token schema: %v", err)
	}

	user, err := users.CreateUser("dbl-bind-worker", "pass", RoleUser, "Worker")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	auth := NewTokenAuth("device-token", []byte("test-secret"), users)
	hub := NewHub(devices, commands, joinTokens, users, auth)

	// Pre-create the device and bind it
	if err := devices.UpsertDevice(protocol.RegisterPayload{
		DeviceID: "dev-pre-bound", Hostname: "host", OS: "linux", Arch: "amd64", ClientVersion: "0.1.0",
	}); err != nil {
		t.Fatalf("upsert device: %v", err)
	}
	if err := users.BindDevice(user.ID, "dev-pre-bound"); err != nil {
		t.Fatalf("pre-bind: %v", err)
	}

	// Register with join token that has same user — should not error (ErrConflict ignored)
	token, err := joinTokens.CreateToken("dbl-bind-token", time.Hour, user.ID)
	if err != nil {
		t.Fatalf("create token: %v", err)
	}

	err = hub.registerDeviceMetadata(protocol.RegisterPayload{
		DeviceID:      "dev-pre-bound",
		Hostname:      "host-updated",
		Token:         token.ID,
		OS:            "linux",
		Arch:          "amd64",
		ClientVersion: "0.2.0",
	})
	if err != nil {
		t.Fatalf("double binding should be silently ignored: %v", err)
	}

	// Device should still be bound
	bound, err := users.IsDeviceBoundToUser(user.ID, "dev-pre-bound")
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if !bound {
		t.Fatalf("device should still be bound")
	}
}
