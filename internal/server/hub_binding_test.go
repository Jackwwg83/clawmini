package server

import (
	"testing"
	"time"

	"github.com/raystone-ai/clawmini/internal/protocol"
)

func TestHubRegisterDeviceMetadata_AutoBindsJoinTokenUser(t *testing.T) {
	db := openTestDB(t)
	devices := NewDeviceStore(db)
	if err := devices.EnsureSchema(); err != nil {
		t.Fatalf("ensure device schema: %v", err)
	}
	users := NewUserStore(db)
	if err := users.EnsureSchema(); err != nil {
		t.Fatalf("ensure user schema: %v", err)
	}
	commands := NewCommandStore(db)
	if err := commands.EnsureSchema(); err != nil {
		t.Fatalf("ensure command schema: %v", err)
	}
	joinTokens := NewJoinTokenStore(db)
	if err := joinTokens.EnsureSchema(); err != nil {
		t.Fatalf("ensure join token schema: %v", err)
	}

	user, err := users.CreateUser("worker", "worker-pass", RoleUser, "Worker")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	auth := NewTokenAuth("device-token", []byte("hub-binding-secret"), users)
	hub := NewHub(devices, commands, joinTokens, users, auth)

	token, err := joinTokens.CreateToken("worker-device", time.Hour, user.ID)
	if err != nil {
		t.Fatalf("create join token: %v", err)
	}

	err = hub.registerDeviceMetadata(protocol.RegisterPayload{
		DeviceID:      "dev-bind-1",
		Hostname:      "host-bind",
		Token:         token.ID,
		OS:            "linux",
		Arch:          "amd64",
		ClientVersion: "0.1.0",
	})
	if err != nil {
		t.Fatalf("register device metadata: %v", err)
	}

	bound, err := users.IsDeviceBoundToUser(user.ID, "dev-bind-1")
	if err != nil {
		t.Fatalf("check user-device binding: %v", err)
	}
	if !bound {
		t.Fatalf("expected device to auto-bind to user")
	}
}
