package server

import (
	"testing"

	"github.com/raystone-ai/clawmini/internal/protocol"
)

func TestUserStoreCreateAuthenticateAndPassword(t *testing.T) {
	db := openTestDB(t)
	devices := NewDeviceStore(db)
	if err := devices.EnsureSchema(); err != nil {
		t.Fatalf("ensure device schema: %v", err)
	}
	users := NewUserStore(db)
	if err := users.EnsureSchema(); err != nil {
		t.Fatalf("ensure user schema: %v", err)
	}

	created, err := users.CreateUser("alice", "alice-pass", RoleUser, "Alice")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if created.Username != "alice" || created.Role != RoleUser {
		t.Fatalf("unexpected user values: %+v", created)
	}

	if _, err := users.Authenticate("alice", "bad-pass"); err == nil {
		t.Fatalf("expected auth to fail with wrong password")
	}
	if _, err := users.Authenticate("alice", "alice-pass"); err != nil {
		t.Fatalf("expected auth success: %v", err)
	}

	if err := users.SetPassword(created.ID, "new-pass"); err != nil {
		t.Fatalf("set password: %v", err)
	}
	valid, err := users.VerifyPassword(created.ID, "alice-pass")
	if err != nil {
		t.Fatalf("verify old password: %v", err)
	}
	if valid {
		t.Fatalf("old password should no longer be valid")
	}
	valid, err = users.VerifyPassword(created.ID, "new-pass")
	if err != nil {
		t.Fatalf("verify new password: %v", err)
	}
	if !valid {
		t.Fatalf("new password should be valid")
	}
}

func TestUserStoreBindAndUnbindDevice(t *testing.T) {
	db := openTestDB(t)
	devices := NewDeviceStore(db)
	if err := devices.EnsureSchema(); err != nil {
		t.Fatalf("ensure device schema: %v", err)
	}
	users := NewUserStore(db)
	if err := users.EnsureSchema(); err != nil {
		t.Fatalf("ensure user schema: %v", err)
	}

	user, err := users.CreateUser("bob", "bob-pass", RoleUser, "Bob")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := devices.UpsertDevice(protocol.RegisterPayload{
		DeviceID:      "dev-user-bind",
		Hostname:      "host",
		OS:            "linux",
		Arch:          "amd64",
		ClientVersion: "0.1.0",
	}); err != nil {
		t.Fatalf("create device: %v", err)
	}

	if err := users.BindDevice(user.ID, "dev-user-bind"); err != nil {
		t.Fatalf("bind device: %v", err)
	}
	bound, err := users.IsDeviceBoundToUser(user.ID, "dev-user-bind")
	if err != nil {
		t.Fatalf("check binding: %v", err)
	}
	if !bound {
		t.Fatalf("expected device to be bound")
	}

	list, err := users.ListDevicesByUser(user.ID)
	if err != nil {
		t.Fatalf("list devices by user: %v", err)
	}
	if len(list) != 1 || list[0].ID != "dev-user-bind" {
		t.Fatalf("unexpected list: %+v", list)
	}

	if err := users.UnbindDevice(user.ID, "dev-user-bind"); err != nil {
		t.Fatalf("unbind device: %v", err)
	}
	bound, err = users.IsDeviceBoundToUser(user.ID, "dev-user-bind")
	if err != nil {
		t.Fatalf("check binding after unbind: %v", err)
	}
	if bound {
		t.Fatalf("expected device to be unbound")
	}
}
