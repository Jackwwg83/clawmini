package server

import (
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/raystone-ai/clawmini/internal/protocol"
)

// --- Password Hashing Edge Cases ---

func TestCreateUser_EmptyPassword(t *testing.T) {
	db := openTestDB(t)
	users := NewUserStore(db)
	if err := users.EnsureSchema(); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	_, err := users.CreateUser("alice", "", RoleUser, "Alice")
	if err == nil {
		t.Fatalf("expected error for empty password")
	}
	if !strings.Contains(err.Error(), "password is required") {
		t.Fatalf("expected 'password is required' error, got: %v", err)
	}
}

func TestCreateUser_EmptyUsername(t *testing.T) {
	db := openTestDB(t)
	users := NewUserStore(db)
	if err := users.EnsureSchema(); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	_, err := users.CreateUser("", "password", RoleUser, "Name")
	if err == nil {
		t.Fatalf("expected error for empty username")
	}
	if !strings.Contains(err.Error(), "username is required") {
		t.Fatalf("expected 'username is required' error, got: %v", err)
	}
}

func TestCreateUser_WhitespaceOnlyUsername(t *testing.T) {
	db := openTestDB(t)
	users := NewUserStore(db)
	if err := users.EnsureSchema(); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	_, err := users.CreateUser("   ", "password", RoleUser, "Name")
	if err == nil {
		t.Fatalf("expected error for whitespace-only username")
	}
}

func TestCreateUser_UnicodePassword(t *testing.T) {
	db := openTestDB(t)
	users := NewUserStore(db)
	if err := users.EnsureSchema(); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	user, err := users.CreateUser("unicode-user", "密码你好世界🔑", RoleUser, "Unicode")
	if err != nil {
		t.Fatalf("create user with unicode password: %v", err)
	}
	if _, err := users.Authenticate("unicode-user", "密码你好世界🔑"); err != nil {
		t.Fatalf("authenticate with unicode password: %v", err)
	}
	valid, err := users.VerifyPassword(user.ID, "密码你好世界🔑")
	if err != nil {
		t.Fatalf("verify unicode password: %v", err)
	}
	if !valid {
		t.Fatalf("unicode password should be valid")
	}
}

func TestCreateUser_LongPassword_Rejected(t *testing.T) {
	db := openTestDB(t)
	users := NewUserStore(db)
	if err := users.EnsureSchema(); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	// bcrypt has a 72-byte limit; passwords exceeding it should be rejected
	longPass := strings.Repeat("a", 100)
	_, err := users.CreateUser("long-pw-user", longPass, RoleUser, "Long PW")
	if err == nil {
		t.Fatalf("expected error for password exceeding 72 bytes")
	}
	// Verify a 72-byte password works fine
	exactPass := strings.Repeat("b", 72)
	_, err = users.CreateUser("exact-pw-user", exactPass, RoleUser, "Exact PW")
	if err != nil {
		t.Fatalf("72-byte password should work: %v", err)
	}
	if _, err := users.Authenticate("exact-pw-user", exactPass); err != nil {
		t.Fatalf("authenticate with 72-byte password: %v", err)
	}
}

func TestAuthenticate_EmptyUsernameAndPassword(t *testing.T) {
	db := openTestDB(t)
	users := NewUserStore(db)
	if err := users.EnsureSchema(); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	_, err := users.Authenticate("", "")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got: %v", err)
	}
}

func TestAuthenticate_NonExistentUser(t *testing.T) {
	db := openTestDB(t)
	users := NewUserStore(db)
	if err := users.EnsureSchema(); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	_, err := users.Authenticate("nonexistent", "password")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got: %v", err)
	}
}

// --- Duplicate Username ---

func TestCreateUser_DuplicateUsername(t *testing.T) {
	db := openTestDB(t)
	users := NewUserStore(db)
	if err := users.EnsureSchema(); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	if _, err := users.CreateUser("dupe", "pass1", RoleUser, "Dupe 1"); err != nil {
		t.Fatalf("create first user: %v", err)
	}
	_, err := users.CreateUser("dupe", "pass2", RoleUser, "Dupe 2")
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("expected ErrConflict for duplicate username, got: %v", err)
	}
}

func TestCreateUser_DuplicateUsernameCasePreserved(t *testing.T) {
	db := openTestDB(t)
	users := NewUserStore(db)
	if err := users.EnsureSchema(); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	// SQLite UNIQUE is case-sensitive by default, so "Alice" and "alice" are different
	if _, err := users.CreateUser("alice", "pass1", RoleUser, "A"); err != nil {
		t.Fatalf("create alice: %v", err)
	}
	// This should succeed because SQLite treats "Alice" as different from "alice"
	_, err := users.CreateUser("Alice", "pass2", RoleUser, "A")
	if err != nil {
		// If it fails, it means there's case-insensitive uniqueness
		if errors.Is(err, ErrConflict) {
			// This is actually a good security practice, just note it
			t.Logf("username uniqueness is case-insensitive (good)")
		} else {
			t.Fatalf("unexpected error: %v", err)
		}
	} else {
		t.Logf("WARNING: usernames are case-sensitive — 'alice' and 'Alice' are separate users")
	}
}

// --- Invalid Role ---

func TestCreateUser_InvalidRole(t *testing.T) {
	db := openTestDB(t)
	users := NewUserStore(db)
	if err := users.EnsureSchema(); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	_, err := users.CreateUser("badrole", "pass", "superadmin", "Bad Role")
	if err == nil {
		t.Fatalf("expected error for invalid role")
	}
	if !strings.Contains(err.Error(), "invalid role") {
		t.Fatalf("expected 'invalid role' error, got: %v", err)
	}
}

func TestCreateUser_RoleNormalization(t *testing.T) {
	db := openTestDB(t)
	users := NewUserStore(db)
	if err := users.EnsureSchema(); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	u, err := users.CreateUser("upper", "pass", "ADMIN", "Upper Admin")
	if err != nil {
		t.Fatalf("create user with uppercase role: %v", err)
	}
	if u.Role != RoleAdmin {
		t.Fatalf("expected role 'admin', got %q", u.Role)
	}

	u2, err := users.CreateUser("mixed", "pass", " User ", "Mixed User")
	if err != nil {
		t.Fatalf("create user with mixed-case role: %v", err)
	}
	if u2.Role != RoleUser {
		t.Fatalf("expected role 'user', got %q", u2.Role)
	}
}

// --- Default Admin Creation ---

func TestEnsureDefaultAdmin_CreatesOnEmpty(t *testing.T) {
	db := openTestDB(t)
	users := NewUserStore(db)
	if err := users.EnsureSchema(); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	created, err := users.EnsureDefaultAdmin()
	if err != nil {
		t.Fatalf("ensure default admin: %v", err)
	}
	if !created {
		t.Fatalf("expected default admin to be created")
	}
	admin, err := users.Authenticate("admin", "admin")
	if err != nil {
		t.Fatalf("authenticate default admin: %v", err)
	}
	if admin.Role != RoleAdmin {
		t.Fatalf("expected admin role, got %q", admin.Role)
	}
	if admin.DisplayName != "Administrator" {
		t.Fatalf("expected 'Administrator' display name, got %q", admin.DisplayName)
	}
}

func TestEnsureDefaultAdmin_SkipsWhenUsersExist(t *testing.T) {
	db := openTestDB(t)
	users := NewUserStore(db)
	if err := users.EnsureSchema(); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	if _, err := users.CreateUser("existing", "pass", RoleUser, "Existing"); err != nil {
		t.Fatalf("create user: %v", err)
	}
	created, err := users.EnsureDefaultAdmin()
	if err != nil {
		t.Fatalf("ensure default admin: %v", err)
	}
	if created {
		t.Fatalf("expected default admin NOT to be created when users exist")
	}
}

func TestEnsureDefaultAdmin_Idempotent(t *testing.T) {
	db := openTestDB(t)
	users := NewUserStore(db)
	if err := users.EnsureSchema(); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	created1, err := users.EnsureDefaultAdmin()
	if err != nil {
		t.Fatalf("first ensure: %v", err)
	}
	if !created1 {
		t.Fatalf("first call should create admin")
	}
	created2, err := users.EnsureDefaultAdmin()
	if err != nil {
		t.Fatalf("second ensure: %v", err)
	}
	if created2 {
		t.Fatalf("second call should not create admin (users exist)")
	}
}

// --- GetUserByID ---

func TestGetUserByID_Found(t *testing.T) {
	db := openTestDB(t)
	users := NewUserStore(db)
	if err := users.EnsureSchema(); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	created, err := users.CreateUser("findme", "pass", RoleUser, "Find Me")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	found, err := users.GetUserByID(created.ID)
	if err != nil {
		t.Fatalf("get user by id: %v", err)
	}
	if found.Username != "findme" || found.DisplayName != "Find Me" {
		t.Fatalf("unexpected user: %+v", found)
	}
}

func TestGetUserByID_NotFound(t *testing.T) {
	db := openTestDB(t)
	users := NewUserStore(db)
	if err := users.EnsureSchema(); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	_, err := users.GetUserByID("nonexistent-id")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got: %v", err)
	}
}

func TestGetUserByID_EmptyID(t *testing.T) {
	db := openTestDB(t)
	users := NewUserStore(db)
	if err := users.EnsureSchema(); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	_, err := users.GetUserByID("")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for empty ID, got: %v", err)
	}
}

// --- DeleteUser ---

func TestDeleteUser_Success(t *testing.T) {
	db := openTestDB(t)
	devices := NewDeviceStore(db)
	if err := devices.EnsureSchema(); err != nil {
		t.Fatalf("ensure device schema: %v", err)
	}
	users := NewUserStore(db)
	if err := users.EnsureSchema(); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	created, err := users.CreateUser("deleteme", "pass", RoleUser, "Delete Me")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := users.DeleteUser(created.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	_, err = users.GetUserByID(created.ID)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected deleted user to not be found, got: %v", err)
	}
}

func TestDeleteUser_NotFound(t *testing.T) {
	db := openTestDB(t)
	devices := NewDeviceStore(db)
	if err := devices.EnsureSchema(); err != nil {
		t.Fatalf("ensure device schema: %v", err)
	}
	users := NewUserStore(db)
	if err := users.EnsureSchema(); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	if err := users.DeleteUser("nonexistent-id"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got: %v", err)
	}
}

func TestDeleteUser_EmptyID(t *testing.T) {
	db := openTestDB(t)
	devices := NewDeviceStore(db)
	if err := devices.EnsureSchema(); err != nil {
		t.Fatalf("ensure device schema: %v", err)
	}
	users := NewUserStore(db)
	if err := users.EnsureSchema(); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	if err := users.DeleteUser(""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for empty ID, got: %v", err)
	}
}

// --- UpdateUser ---

func TestUpdateUser_DisplayNameOnly(t *testing.T) {
	db := openTestDB(t)
	users := NewUserStore(db)
	if err := users.EnsureSchema(); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	created, err := users.CreateUser("updatable", "pass", RoleUser, "Original")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	newName := "Updated"
	updated, err := users.UpdateUser(created.ID, UpdateUserInput{DisplayName: &newName})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.DisplayName != "Updated" {
		t.Fatalf("expected 'Updated', got %q", updated.DisplayName)
	}
	if updated.Role != RoleUser {
		t.Fatalf("role should remain unchanged, got %q", updated.Role)
	}
}

func TestUpdateUser_RoleOnly(t *testing.T) {
	db := openTestDB(t)
	users := NewUserStore(db)
	if err := users.EnsureSchema(); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	created, err := users.CreateUser("role-update", "pass", RoleUser, "Role Test")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	newRole := "admin"
	updated, err := users.UpdateUser(created.ID, UpdateUserInput{Role: &newRole})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Role != RoleAdmin {
		t.Fatalf("expected 'admin', got %q", updated.Role)
	}
}

func TestUpdateUser_InvalidRole(t *testing.T) {
	db := openTestDB(t)
	users := NewUserStore(db)
	if err := users.EnsureSchema(); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	created, err := users.CreateUser("badrole-update", "pass", RoleUser, "Bad Role")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	badRole := "moderator"
	_, err = users.UpdateUser(created.ID, UpdateUserInput{Role: &badRole})
	if err == nil {
		t.Fatalf("expected error for invalid role update")
	}
}

func TestUpdateUser_NotFound(t *testing.T) {
	db := openTestDB(t)
	users := NewUserStore(db)
	if err := users.EnsureSchema(); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	name := "nobody"
	_, err := users.UpdateUser("nonexistent", UpdateUserInput{DisplayName: &name})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got: %v", err)
	}
}

// --- ListUsers ---

func TestListUsers_WithDeviceCounts(t *testing.T) {
	db := openTestDB(t)
	devices := NewDeviceStore(db)
	if err := devices.EnsureSchema(); err != nil {
		t.Fatalf("ensure device schema: %v", err)
	}
	users := NewUserStore(db)
	if err := users.EnsureSchema(); err != nil {
		t.Fatalf("ensure user schema: %v", err)
	}

	u1, err := users.CreateUser("list-user1", "pass", RoleAdmin, "User 1")
	if err != nil {
		t.Fatalf("create user1: %v", err)
	}
	u2, err := users.CreateUser("list-user2", "pass", RoleUser, "User 2")
	if err != nil {
		t.Fatalf("create user2: %v", err)
	}

	for i, id := range []string{"dev-list-1", "dev-list-2"} {
		if err := devices.UpsertDevice(protocol.RegisterPayload{
			DeviceID: id, Hostname: id, OS: "linux", Arch: "amd64", ClientVersion: "0.1.0",
		}); err != nil {
			t.Fatalf("create device %d: %v", i, err)
		}
	}
	if err := users.BindDevice(u1.ID, "dev-list-1"); err != nil {
		t.Fatalf("bind: %v", err)
	}
	if err := users.BindDevice(u1.ID, "dev-list-2"); err != nil {
		t.Fatalf("bind: %v", err)
	}

	summaries, err := users.ListUsers()
	if err != nil {
		t.Fatalf("list users: %v", err)
	}
	if len(summaries) != 2 {
		t.Fatalf("expected 2 users, got %d", len(summaries))
	}

	var u1Summary, u2Summary *UserSummary
	for i := range summaries {
		if summaries[i].ID == u1.ID {
			u1Summary = &summaries[i]
		}
		if summaries[i].ID == u2.ID {
			u2Summary = &summaries[i]
		}
	}
	if u1Summary == nil || u2Summary == nil {
		t.Fatalf("could not find both users in list")
	}
	if u1Summary.DeviceCount != 2 {
		t.Fatalf("expected user1 to have 2 devices, got %d", u1Summary.DeviceCount)
	}
	if u2Summary.DeviceCount != 0 {
		t.Fatalf("expected user2 to have 0 devices, got %d", u2Summary.DeviceCount)
	}
}

// --- SetPassword Edge Cases ---

func TestSetPassword_EmptyPassword(t *testing.T) {
	db := openTestDB(t)
	users := NewUserStore(db)
	if err := users.EnsureSchema(); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	created, err := users.CreateUser("set-pw", "original", RoleUser, "Set PW")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	err = users.SetPassword(created.ID, "")
	if err == nil {
		t.Fatalf("expected error for empty password")
	}
	if !strings.Contains(err.Error(), "password is required") {
		t.Fatalf("expected 'password is required', got: %v", err)
	}
}

func TestSetPassword_NonExistentUser(t *testing.T) {
	db := openTestDB(t)
	users := NewUserStore(db)
	if err := users.EnsureSchema(); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	err := users.SetPassword("nonexistent", "newpass")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got: %v", err)
	}
}

func TestVerifyPassword_NonExistentUser(t *testing.T) {
	db := openTestDB(t)
	users := NewUserStore(db)
	if err := users.EnsureSchema(); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	valid, err := users.VerifyPassword("nonexistent", "pass")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got: %v", err)
	}
	if valid {
		t.Fatalf("should not be valid for nonexistent user")
	}
}

func TestVerifyPassword_EmptyUserID(t *testing.T) {
	db := openTestDB(t)
	users := NewUserStore(db)
	if err := users.EnsureSchema(); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	valid, err := users.VerifyPassword("", "pass")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got: %v", err)
	}
	if valid {
		t.Fatalf("should not be valid for empty user ID")
	}
}

// --- Device Binding Edge Cases ---

func TestBindDevice_NonExistentUser(t *testing.T) {
	db := openTestDB(t)
	devices := NewDeviceStore(db)
	if err := devices.EnsureSchema(); err != nil {
		t.Fatalf("ensure device schema: %v", err)
	}
	users := NewUserStore(db)
	if err := users.EnsureSchema(); err != nil {
		t.Fatalf("ensure user schema: %v", err)
	}
	if err := devices.UpsertDevice(protocol.RegisterPayload{
		DeviceID: "dev-orphan", Hostname: "host", OS: "linux", Arch: "amd64", ClientVersion: "0.1.0",
	}); err != nil {
		t.Fatalf("create device: %v", err)
	}
	err := users.BindDevice("nonexistent-user-id", "dev-orphan")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for nonexistent user, got: %v", err)
	}
}

func TestBindDevice_NonExistentDevice(t *testing.T) {
	db := openTestDB(t)
	devices := NewDeviceStore(db)
	if err := devices.EnsureSchema(); err != nil {
		t.Fatalf("ensure device schema: %v", err)
	}
	users := NewUserStore(db)
	if err := users.EnsureSchema(); err != nil {
		t.Fatalf("ensure user schema: %v", err)
	}
	user, err := users.CreateUser("bind-user", "pass", RoleUser, "Bind User")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	err = users.BindDevice(user.ID, "nonexistent-device")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for nonexistent device, got: %v", err)
	}
}

func TestBindDevice_DoubleBind(t *testing.T) {
	db := openTestDB(t)
	devices := NewDeviceStore(db)
	if err := devices.EnsureSchema(); err != nil {
		t.Fatalf("ensure device schema: %v", err)
	}
	users := NewUserStore(db)
	if err := users.EnsureSchema(); err != nil {
		t.Fatalf("ensure user schema: %v", err)
	}
	user, err := users.CreateUser("dbl-bind", "pass", RoleUser, "Dbl Bind")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := devices.UpsertDevice(protocol.RegisterPayload{
		DeviceID: "dev-dbl-bind", Hostname: "host", OS: "linux", Arch: "amd64", ClientVersion: "0.1.0",
	}); err != nil {
		t.Fatalf("create device: %v", err)
	}
	if err := users.BindDevice(user.ID, "dev-dbl-bind"); err != nil {
		t.Fatalf("first bind: %v", err)
	}
	err = users.BindDevice(user.ID, "dev-dbl-bind")
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("expected ErrConflict for double bind, got: %v", err)
	}
}

func TestBindDevice_EmptyIDs(t *testing.T) {
	db := openTestDB(t)
	devices := NewDeviceStore(db)
	if err := devices.EnsureSchema(); err != nil {
		t.Fatalf("ensure device schema: %v", err)
	}
	users := NewUserStore(db)
	if err := users.EnsureSchema(); err != nil {
		t.Fatalf("ensure user schema: %v", err)
	}

	if err := users.BindDevice("", "dev"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for empty userID, got: %v", err)
	}
	if err := users.BindDevice("user", ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for empty deviceID, got: %v", err)
	}
}

func TestUnbindDevice_NotBound(t *testing.T) {
	db := openTestDB(t)
	devices := NewDeviceStore(db)
	if err := devices.EnsureSchema(); err != nil {
		t.Fatalf("ensure device schema: %v", err)
	}
	users := NewUserStore(db)
	if err := users.EnsureSchema(); err != nil {
		t.Fatalf("ensure user schema: %v", err)
	}
	user, err := users.CreateUser("unbind-user", "pass", RoleUser, "Unbind User")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	err = users.UnbindDevice(user.ID, "nonexistent-device")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got: %v", err)
	}
}

func TestDeleteUser_CascadesDeviceBindings(t *testing.T) {
	db := openTestDB(t)
	devices := NewDeviceStore(db)
	if err := devices.EnsureSchema(); err != nil {
		t.Fatalf("ensure device schema: %v", err)
	}
	users := NewUserStore(db)
	if err := users.EnsureSchema(); err != nil {
		t.Fatalf("ensure user schema: %v", err)
	}
	user, err := users.CreateUser("cascade-user", "pass", RoleUser, "Cascade")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := devices.UpsertDevice(protocol.RegisterPayload{
		DeviceID: "dev-cascade", Hostname: "host", OS: "linux", Arch: "amd64", ClientVersion: "0.1.0",
	}); err != nil {
		t.Fatalf("create device: %v", err)
	}
	if err := users.BindDevice(user.ID, "dev-cascade"); err != nil {
		t.Fatalf("bind: %v", err)
	}

	// Delete user — should cascade
	if err := users.DeleteUser(user.ID); err != nil {
		t.Fatalf("delete user: %v", err)
	}

	bound, err := users.IsDeviceBoundToUser(user.ID, "dev-cascade")
	if err != nil {
		t.Fatalf("check binding after delete: %v", err)
	}
	if bound {
		t.Fatalf("binding should be deleted when user is deleted")
	}
}

// --- IsDeviceBoundToUser Edge Cases ---

func TestIsDeviceBoundToUser_EmptyIDs(t *testing.T) {
	db := openTestDB(t)
	users := NewUserStore(db)
	if err := users.EnsureSchema(); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	bound, err := users.IsDeviceBoundToUser("", "dev-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if bound {
		t.Fatalf("empty user ID should return false")
	}

	bound, err = users.IsDeviceBoundToUser("user-1", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if bound {
		t.Fatalf("empty device ID should return false")
	}
}

// --- ListBoundDeviceIDs ---

func TestListBoundDeviceIDs_MultipleDevices(t *testing.T) {
	db := openTestDB(t)
	devices := NewDeviceStore(db)
	if err := devices.EnsureSchema(); err != nil {
		t.Fatalf("ensure device schema: %v", err)
	}
	users := NewUserStore(db)
	if err := users.EnsureSchema(); err != nil {
		t.Fatalf("ensure user schema: %v", err)
	}
	user, err := users.CreateUser("list-bound", "pass", RoleUser, "List Bound")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	for _, id := range []string{"dev-b1", "dev-b2", "dev-b3"} {
		if err := devices.UpsertDevice(protocol.RegisterPayload{
			DeviceID: id, Hostname: id, OS: "linux", Arch: "amd64", ClientVersion: "0.1.0",
		}); err != nil {
			t.Fatalf("create device %s: %v", id, err)
		}
		if err := users.BindDevice(user.ID, id); err != nil {
			t.Fatalf("bind %s: %v", id, err)
		}
	}

	ids, err := users.ListBoundDeviceIDs(user.ID)
	if err != nil {
		t.Fatalf("list bound: %v", err)
	}
	if len(ids) != 3 {
		t.Fatalf("expected 3, got %d", len(ids))
	}
	// Should be ordered ASC
	if ids[0] != "dev-b1" || ids[1] != "dev-b2" || ids[2] != "dev-b3" {
		t.Fatalf("unexpected order: %v", ids)
	}
}

func TestListBoundDeviceIDs_EmptyUserID(t *testing.T) {
	db := openTestDB(t)
	users := NewUserStore(db)
	if err := users.EnsureSchema(); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	ids, err := users.ListBoundDeviceIDs("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ids) != 0 {
		t.Fatalf("expected empty list, got %v", ids)
	}
}

// --- FilterBoundDeviceIDs ---

func TestFilterBoundDeviceIDs_Mixed(t *testing.T) {
	db := openTestDB(t)
	devices := NewDeviceStore(db)
	if err := devices.EnsureSchema(); err != nil {
		t.Fatalf("ensure device schema: %v", err)
	}
	users := NewUserStore(db)
	if err := users.EnsureSchema(); err != nil {
		t.Fatalf("ensure user schema: %v", err)
	}
	user, err := users.CreateUser("filter-user", "pass", RoleUser, "Filter User")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	for _, id := range []string{"dev-f1", "dev-f2", "dev-f3"} {
		if err := devices.UpsertDevice(protocol.RegisterPayload{
			DeviceID: id, Hostname: id, OS: "linux", Arch: "amd64", ClientVersion: "0.1.0",
		}); err != nil {
			t.Fatalf("create device: %v", err)
		}
	}
	if err := users.BindDevice(user.ID, "dev-f1"); err != nil {
		t.Fatalf("bind: %v", err)
	}
	if err := users.BindDevice(user.ID, "dev-f3"); err != nil {
		t.Fatalf("bind: %v", err)
	}

	result, err := users.FilterBoundDeviceIDs(user.ID, []string{"dev-f1", "dev-f2", "dev-f3", "dev-f4"})
	if err != nil {
		t.Fatalf("filter: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 bound devices, got %d", len(result))
	}
	if !result["dev-f1"] || !result["dev-f3"] {
		t.Fatalf("expected dev-f1 and dev-f3, got %v", result)
	}
}

func TestFilterBoundDeviceIDs_EmptyInput(t *testing.T) {
	db := openTestDB(t)
	users := NewUserStore(db)
	if err := users.EnsureSchema(); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	result, err := users.FilterBoundDeviceIDs("some-user", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 0 {
		t.Fatalf("expected empty result, got %v", result)
	}
}

func TestFilterBoundDeviceIDs_DuplicatesAndWhitespace(t *testing.T) {
	db := openTestDB(t)
	devices := NewDeviceStore(db)
	if err := devices.EnsureSchema(); err != nil {
		t.Fatalf("ensure device schema: %v", err)
	}
	users := NewUserStore(db)
	if err := users.EnsureSchema(); err != nil {
		t.Fatalf("ensure user schema: %v", err)
	}
	user, err := users.CreateUser("filter-dup", "pass", RoleUser, "Dup")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := devices.UpsertDevice(protocol.RegisterPayload{
		DeviceID: "dev-dup", Hostname: "host", OS: "linux", Arch: "amd64", ClientVersion: "0.1.0",
	}); err != nil {
		t.Fatalf("create device: %v", err)
	}
	if err := users.BindDevice(user.ID, "dev-dup"); err != nil {
		t.Fatalf("bind: %v", err)
	}

	result, err := users.FilterBoundDeviceIDs(user.ID, []string{"dev-dup", " dev-dup ", "dev-dup", "", "  "})
	if err != nil {
		t.Fatalf("filter: %v", err)
	}
	if len(result) != 1 || !result["dev-dup"] {
		t.Fatalf("expected single dev-dup, got %v", result)
	}
}

// --- CountUsers / CountAdmins ---

func TestCountUsersAndAdmins(t *testing.T) {
	db := openTestDB(t)
	users := NewUserStore(db)
	if err := users.EnsureSchema(); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}

	count, err := users.CountUsers()
	if err != nil {
		t.Fatalf("count users: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0 users, got %d", count)
	}

	if _, err := users.CreateUser("a1", "pass", RoleAdmin, "Admin 1"); err != nil {
		t.Fatalf("create admin: %v", err)
	}
	if _, err := users.CreateUser("u1", "pass", RoleUser, "User 1"); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if _, err := users.CreateUser("u2", "pass", RoleUser, "User 2"); err != nil {
		t.Fatalf("create user: %v", err)
	}

	count, err = users.CountUsers()
	if err != nil {
		t.Fatalf("count users: %v", err)
	}
	if count != 3 {
		t.Fatalf("expected 3, got %d", count)
	}

	admins, err := users.CountAdmins()
	if err != nil {
		t.Fatalf("count admins: %v", err)
	}
	if admins != 1 {
		t.Fatalf("expected 1 admin, got %d", admins)
	}
}

// --- Concurrent User Creation ---

func TestConcurrentUserCreation(t *testing.T) {
	db := openTestDB(t)
	users := NewUserStore(db)
	if err := users.EnsureSchema(); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}

	var wg sync.WaitGroup
	errs := make([]error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			username := strings.Repeat("u", 4) + strings.Repeat("0", 4-len(string(rune('0'+idx%10)))) + string(rune('0'+idx%10))
			// Use unique usernames based on index
			name := "concurrent-" + string(rune('a'+idx))
			_, errs[idx] = users.CreateUser(name, "pass", RoleUser, username)
		}(i)
	}
	wg.Wait()

	successCount := 0
	for _, err := range errs {
		if err == nil {
			successCount++
		}
	}
	if successCount == 0 {
		t.Fatalf("expected at least some concurrent creates to succeed")
	}
}

// --- SQL Injection Attempts ---

func TestCreateUser_SQLInjectionInUsername(t *testing.T) {
	db := openTestDB(t)
	users := NewUserStore(db)
	if err := users.EnsureSchema(); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}

	// These should be safely handled by parameterized queries
	injections := []string{
		"'; DROP TABLE users; --",
		"\" OR 1=1 --",
		"admin'--",
		"user; DELETE FROM users WHERE ''='",
		"Robert'); DROP TABLE users;--",
	}

	for _, injection := range injections {
		_, err := users.CreateUser(injection, "password", RoleUser, "Injection Test")
		if err != nil {
			t.Logf("injection attempt %q got error (expected): %v", injection, err)
			continue
		}
		// If it succeeded, verify the user was created with the literal string
		user, err := users.Authenticate(injection, "password")
		if err != nil {
			t.Fatalf("could not authenticate with injection username %q: %v", injection, err)
		}
		if user.Username != injection {
			t.Fatalf("username mismatch: expected %q, got %q", injection, user.Username)
		}
	}

	// Verify users table still exists
	count, err := users.CountUsers()
	if err != nil {
		t.Fatalf("count users after injection attempts (table may be dropped!): %v", err)
	}
	if count == 0 {
		t.Fatalf("expected users to exist after injection attempts")
	}
}

// --- GetUserDetail ---

func TestGetUserDetail_WithDevices(t *testing.T) {
	db := openTestDB(t)
	devices := NewDeviceStore(db)
	if err := devices.EnsureSchema(); err != nil {
		t.Fatalf("ensure device schema: %v", err)
	}
	users := NewUserStore(db)
	if err := users.EnsureSchema(); err != nil {
		t.Fatalf("ensure user schema: %v", err)
	}

	user, err := users.CreateUser("detail-user", "pass", RoleUser, "Detail User")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := devices.UpsertDevice(protocol.RegisterPayload{
		DeviceID: "dev-detail", Hostname: "host-detail", OS: "linux", Arch: "amd64", ClientVersion: "0.1.0",
	}); err != nil {
		t.Fatalf("create device: %v", err)
	}
	if err := users.BindDevice(user.ID, "dev-detail"); err != nil {
		t.Fatalf("bind: %v", err)
	}

	detail, err := users.GetUserDetail(user.ID)
	if err != nil {
		t.Fatalf("get detail: %v", err)
	}
	if detail.Username != "detail-user" {
		t.Fatalf("expected username 'detail-user', got %q", detail.Username)
	}
	if len(detail.Devices) != 1 {
		t.Fatalf("expected 1 device, got %d", len(detail.Devices))
	}
	if detail.Devices[0].ID != "dev-detail" {
		t.Fatalf("expected device 'dev-detail', got %q", detail.Devices[0].ID)
	}
}

func TestGetUserDetail_NotFound(t *testing.T) {
	db := openTestDB(t)
	users := NewUserStore(db)
	if err := users.EnsureSchema(); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	_, err := users.GetUserDetail("nonexistent")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got: %v", err)
	}
}

// --- DisplayName Defaults to Username ---

func TestCreateUser_EmptyDisplayNameDefaultsToUsername(t *testing.T) {
	db := openTestDB(t)
	users := NewUserStore(db)
	if err := users.EnsureSchema(); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	user, err := users.CreateUser("defaultname", "pass", RoleUser, "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if user.DisplayName != "defaultname" {
		t.Fatalf("expected displayName to default to username, got %q", user.DisplayName)
	}
}

// --- EnsureSchema Idempotent ---

func TestEnsureSchema_Idempotent(t *testing.T) {
	db := openTestDB(t)
	devices := NewDeviceStore(db)
	if err := devices.EnsureSchema(); err != nil {
		t.Fatalf("ensure device schema: %v", err)
	}
	users := NewUserStore(db)
	if err := users.EnsureSchema(); err != nil {
		t.Fatalf("first ensure: %v", err)
	}
	if err := users.EnsureSchema(); err != nil {
		t.Fatalf("second ensure should be idempotent: %v", err)
	}
	if _, err := users.CreateUser("idempotent", "pass", RoleUser, "Idempotent"); err != nil {
		t.Fatalf("create after double schema init: %v", err)
	}
}
