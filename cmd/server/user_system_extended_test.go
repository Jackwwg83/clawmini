package main

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/raystone-ai/clawmini/internal/protocol"
	"github.com/raystone-ai/clawmini/internal/server"
)

// --- Password Change Endpoint Tests ---

func TestChangeMyPassword_Success(t *testing.T) {
	app, r := setupTestApp(t)
	if _, err := app.users.CreateUser("pw-user", "old-pass", server.RoleUser, "PW User"); err != nil {
		t.Fatalf("create user: %v", err)
	}
	token := issueUserTokenForTest(t, app, "pw-user", "old-pass")

	rr := doRequest(t, r, http.MethodPut, "/api/me/password",
		`{"oldPassword":"old-pass","newPassword":"new-pass"}`, token)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	// Verify new password works
	if _, err := app.users.Authenticate("pw-user", "new-pass"); err != nil {
		t.Fatalf("expected new password to work: %v", err)
	}
	// Verify old password no longer works
	if _, err := app.users.Authenticate("pw-user", "old-pass"); err == nil {
		t.Fatalf("expected old password to fail")
	}
}

func TestChangeMyPassword_WrongOldPassword(t *testing.T) {
	app, r := setupTestApp(t)
	if _, err := app.users.CreateUser("pw-wrong", "correct-pass", server.RoleUser, "PW Wrong"); err != nil {
		t.Fatalf("create user: %v", err)
	}
	token := issueUserTokenForTest(t, app, "pw-wrong", "correct-pass")

	rr := doRequest(t, r, http.MethodPut, "/api/me/password",
		`{"oldPassword":"wrong-pass","newPassword":"new-pass"}`, token)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestChangeMyPassword_EmptyNewPassword(t *testing.T) {
	app, r := setupTestApp(t)
	if _, err := app.users.CreateUser("pw-empty", "old-pass", server.RoleUser, "PW Empty"); err != nil {
		t.Fatalf("create user: %v", err)
	}
	token := issueUserTokenForTest(t, app, "pw-empty", "old-pass")

	rr := doRequest(t, r, http.MethodPut, "/api/me/password",
		`{"oldPassword":"old-pass","newPassword":""}`, token)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestChangeMyPassword_Unauthenticated(t *testing.T) {
	_, r := setupTestApp(t)

	rr := doRequest(t, r, http.MethodPut, "/api/me/password",
		`{"oldPassword":"a","newPassword":"b"}`, "")
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

// --- Get /api/me Tests ---

func TestGetMe_Success(t *testing.T) {
	app, r := setupTestApp(t)
	if _, err := app.users.CreateUser("me-user", "pass", server.RoleUser, "Me User"); err != nil {
		t.Fatalf("create user: %v", err)
	}
	token := issueUserTokenForTest(t, app, "me-user", "pass")

	rr := doRequest(t, r, http.MethodGet, "/api/me", "", token)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var resp struct {
		User    server.User             `json:"user"`
		Devices []server.DeviceSnapshot `json:"devices"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.User.Username != "me-user" {
		t.Fatalf("expected username 'me-user', got %q", resp.User.Username)
	}
	if len(resp.Devices) != 0 {
		t.Fatalf("expected 0 devices, got %d", len(resp.Devices))
	}
}

func TestGetMe_WithBoundDevices(t *testing.T) {
	app, r := setupTestApp(t)
	created, err := app.users.CreateUser("me-devs", "pass", server.RoleUser, "Me Devs")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := app.devices.UpsertDevice(protocol.RegisterPayload{
		DeviceID: "dev-me-1", Hostname: "host", OS: "linux", Arch: "amd64", ClientVersion: "0.1.0",
	}); err != nil {
		t.Fatalf("create device: %v", err)
	}
	if err := app.users.BindDevice(created.ID, "dev-me-1"); err != nil {
		t.Fatalf("bind: %v", err)
	}
	token := issueUserTokenForTest(t, app, "me-devs", "pass")

	rr := doRequest(t, r, http.MethodGet, "/api/me", "", token)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var resp struct {
		Devices []server.DeviceSnapshot `json:"devices"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Devices) != 1 {
		t.Fatalf("expected 1 device, got %d", len(resp.Devices))
	}
}

func TestGetMe_Unauthenticated(t *testing.T) {
	_, r := setupTestApp(t)
	rr := doRequest(t, r, http.MethodGet, "/api/me", "", "")
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

// --- RBAC: User Accessing Admin Endpoints ---

func TestNonAdmin_CannotCreateUser(t *testing.T) {
	app, r := setupTestApp(t)
	if _, err := app.users.CreateUser("non-admin", "pass", server.RoleUser, "Non Admin"); err != nil {
		t.Fatalf("create user: %v", err)
	}
	token := issueUserTokenForTest(t, app, "non-admin", "pass")

	rr := doRequest(t, r, http.MethodPost, "/api/users",
		`{"username":"new","password":"pass","role":"user","displayName":"New"}`, token)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestNonAdmin_CannotUpdateUser(t *testing.T) {
	app, r := setupTestApp(t)
	if _, err := app.users.CreateUser("non-admin-upd", "pass", server.RoleUser, "Non Admin"); err != nil {
		t.Fatalf("create user: %v", err)
	}
	token := issueUserTokenForTest(t, app, "non-admin-upd", "pass")

	rr := doRequest(t, r, http.MethodPut, "/api/users/some-id",
		`{"displayName":"Hacked"}`, token)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestNonAdmin_CannotDeleteUser(t *testing.T) {
	app, r := setupTestApp(t)
	if _, err := app.users.CreateUser("non-admin-del", "pass", server.RoleUser, "Non Admin"); err != nil {
		t.Fatalf("create user: %v", err)
	}
	token := issueUserTokenForTest(t, app, "non-admin-del", "pass")

	rr := doRequest(t, r, http.MethodDelete, "/api/users/some-id", "", token)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestNonAdmin_CannotBindDevice(t *testing.T) {
	app, r := setupTestApp(t)
	if _, err := app.users.CreateUser("non-admin-bind", "pass", server.RoleUser, "Non Admin"); err != nil {
		t.Fatalf("create user: %v", err)
	}
	token := issueUserTokenForTest(t, app, "non-admin-bind", "pass")

	rr := doRequest(t, r, http.MethodPost, "/api/users/some-id/devices",
		`{"deviceId":"dev-1"}`, token)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestNonAdmin_CannotUnbindDevice(t *testing.T) {
	app, r := setupTestApp(t)
	if _, err := app.users.CreateUser("non-admin-unbind", "pass", server.RoleUser, "Non Admin"); err != nil {
		t.Fatalf("create user: %v", err)
	}
	token := issueUserTokenForTest(t, app, "non-admin-unbind", "pass")

	rr := doRequest(t, r, http.MethodDelete, "/api/users/some-id/devices/dev-1", "", token)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

// --- CRUD Edge Cases ---

func TestCreateUser_DuplicateUsername(t *testing.T) {
	_, r := setupTestApp(t)

	rr := doRequest(t, r, http.MethodPost, "/api/users",
		`{"username":"dup","password":"pass1","role":"user","displayName":"Dup 1"}`, testAdminAliasToken)
	if rr.Code != http.StatusCreated {
		t.Fatalf("first create expected 201, got %d", rr.Code)
	}

	rr = doRequest(t, r, http.MethodPost, "/api/users",
		`{"username":"dup","password":"pass2","role":"user","displayName":"Dup 2"}`, testAdminAliasToken)
	if rr.Code != http.StatusConflict {
		t.Fatalf("expected 409 for duplicate username, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestCreateUser_InvalidRole(t *testing.T) {
	_, r := setupTestApp(t)

	rr := doRequest(t, r, http.MethodPost, "/api/users",
		`{"username":"badrole","password":"pass","role":"superadmin","displayName":"Bad"}`, testAdminAliasToken)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestCreateUser_MissingPassword(t *testing.T) {
	_, r := setupTestApp(t)

	rr := doRequest(t, r, http.MethodPost, "/api/users",
		`{"username":"nopw","password":"","role":"user","displayName":"No PW"}`, testAdminAliasToken)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestCreateUser_MissingUsername(t *testing.T) {
	_, r := setupTestApp(t)

	rr := doRequest(t, r, http.MethodPost, "/api/users",
		`{"username":"","password":"pass","role":"user","displayName":"No Name"}`, testAdminAliasToken)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
}

// --- Delete Self Prevention ---

func TestDeleteUser_CannotDeleteSelf(t *testing.T) {
	app, r := setupTestApp(t)
	adminUser, err := app.users.Authenticate("admin", "admin")
	if err != nil {
		t.Fatalf("authenticate admin: %v", err)
	}

	rr := doRequest(t, r, http.MethodDelete, "/api/users/"+adminUser.ID, "", testAdminAliasToken)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 (cannot delete self), got %d: %s", rr.Code, rr.Body.String())
	}
	var errResp map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if errResp["error"] != "cannot delete current user" {
		t.Fatalf("unexpected error message: %q", errResp["error"])
	}
}

// --- Last Admin Protection ---

func TestDeleteUser_CannotDeleteLastAdmin(t *testing.T) {
	app, r := setupTestApp(t)
	// Create a second admin who will try to delete the default admin
	second, err := app.users.CreateUser("admin2", "pass", server.RoleAdmin, "Admin 2")
	if err != nil {
		t.Fatalf("create admin2: %v", err)
	}
	admin2Token := issueUserTokenForTest(t, app, "admin2", "pass")

	// Delete default admin (leaving only admin2)
	defaultAdmin, err := app.users.Authenticate("admin", "admin")
	if err != nil {
		t.Fatalf("auth default admin: %v", err)
	}
	rr := doRequest(t, r, http.MethodDelete, "/api/users/"+defaultAdmin.ID, "", admin2Token)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected to delete first admin (2 admins), got %d: %s", rr.Code, rr.Body.String())
	}

	// Create a normal user
	normalUser, err := app.users.CreateUser("normie", "pass", server.RoleUser, "Normie")
	if err != nil {
		t.Fatalf("create normal user: %v", err)
	}

	// Try to delete normie — should work (not last admin issue)
	rr = doRequest(t, r, http.MethodDelete, "/api/users/"+normalUser.ID, "", admin2Token)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for deleting non-admin, got %d", rr.Code)
	}

	// Cannot delete self (the only admin)
	rr = doRequest(t, r, http.MethodDelete, "/api/users/"+second.ID, "", admin2Token)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 (cannot delete self), got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestUpdateUser_CannotDemoteLastAdmin(t *testing.T) {
	app, r := setupTestApp(t)
	// Default admin is the only admin
	defaultAdmin, err := app.users.Authenticate("admin", "admin")
	if err != nil {
		t.Fatalf("auth admin: %v", err)
	}

	rr := doRequest(t, r, http.MethodPut, "/api/users/"+defaultAdmin.ID,
		`{"role":"user"}`, testAdminAliasToken)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 (cannot demote last admin), got %d: %s", rr.Code, rr.Body.String())
	}
	var errResp map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if errResp["error"] != "cannot demote the last admin" {
		t.Fatalf("unexpected error: %q", errResp["error"])
	}
}

func TestUpdateUser_CanDemoteAdminWhenMultiple(t *testing.T) {
	app, r := setupTestApp(t)
	created, err := app.users.CreateUser("admin-demote", "pass", server.RoleAdmin, "Demotable")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	rr := doRequest(t, r, http.MethodPut, "/api/users/"+created.ID,
		`{"role":"user"}`, testAdminAliasToken)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 (two admins, can demote one), got %d: %s", rr.Code, rr.Body.String())
	}
	var updated server.User
	if err := json.Unmarshal(rr.Body.Bytes(), &updated); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if updated.Role != server.RoleUser {
		t.Fatalf("expected role 'user' after demotion, got %q", updated.Role)
	}
}

// --- Update User with Password ---

func TestUpdateUser_WithPassword(t *testing.T) {
	app, r := setupTestApp(t)
	created, err := app.users.CreateUser("update-pw", "old-pw", server.RoleUser, "Update PW")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	rr := doRequest(t, r, http.MethodPut, "/api/users/"+created.ID,
		`{"password":"new-pw","displayName":"Updated PW"}`, testAdminAliasToken)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	// Verify new password works
	if _, err := app.users.Authenticate("update-pw", "new-pw"); err != nil {
		t.Fatalf("new password should work: %v", err)
	}
	// Old password should not work
	if _, err := app.users.Authenticate("update-pw", "old-pw"); err == nil {
		t.Fatalf("old password should not work after update")
	}
}

// --- Get User Not Found ---

func TestGetUser_NotFound(t *testing.T) {
	_, r := setupTestApp(t)
	rr := doRequest(t, r, http.MethodGet, "/api/users/nonexistent-id", "", testAdminAliasToken)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

// --- Delete User Not Found ---

func TestDeleteUser_NotFound(t *testing.T) {
	_, r := setupTestApp(t)
	rr := doRequest(t, r, http.MethodDelete, "/api/users/nonexistent-id", "", testAdminAliasToken)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

// --- Update User Not Found ---

func TestUpdateUser_NotFound(t *testing.T) {
	_, r := setupTestApp(t)
	rr := doRequest(t, r, http.MethodPut, "/api/users/nonexistent-id",
		`{"displayName":"Ghost"}`, testAdminAliasToken)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

// --- Bind Device Edge Cases ---

func TestBindDevice_NotFoundUser(t *testing.T) {
	_, r := setupTestApp(t)
	rr := doRequest(t, r, http.MethodPost, "/api/users/nonexistent/devices",
		`{"deviceId":"dev-1"}`, testAdminAliasToken)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestBindDevice_NotFoundDevice(t *testing.T) {
	app, r := setupTestApp(t)
	created, err := app.users.CreateUser("bind-nodev", "pass", server.RoleUser, "Bind No Dev")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	rr := doRequest(t, r, http.MethodPost, "/api/users/"+created.ID+"/devices",
		`{"deviceId":"nonexistent-device"}`, testAdminAliasToken)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestBindDevice_AlreadyBound(t *testing.T) {
	app, r := setupTestApp(t)
	created, err := app.users.CreateUser("bind-dup", "pass", server.RoleUser, "Bind Dup")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := app.devices.UpsertDevice(protocol.RegisterPayload{
		DeviceID: "dev-bind-dup", Hostname: "host", OS: "linux", Arch: "amd64", ClientVersion: "0.1.0",
	}); err != nil {
		t.Fatalf("create device: %v", err)
	}

	rr := doRequest(t, r, http.MethodPost, "/api/users/"+created.ID+"/devices",
		`{"deviceId":"dev-bind-dup"}`, testAdminAliasToken)
	if rr.Code != http.StatusOK {
		t.Fatalf("first bind expected 200, got %d", rr.Code)
	}

	rr = doRequest(t, r, http.MethodPost, "/api/users/"+created.ID+"/devices",
		`{"deviceId":"dev-bind-dup"}`, testAdminAliasToken)
	if rr.Code != http.StatusConflict {
		t.Fatalf("expected 409 for double bind, got %d: %s", rr.Code, rr.Body.String())
	}
}

// --- Unbind Device Edge Cases ---

func TestUnbindDevice_NotBound(t *testing.T) {
	app, r := setupTestApp(t)
	created, err := app.users.CreateUser("unbind-nodev", "pass", server.RoleUser, "Unbind")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	rr := doRequest(t, r, http.MethodDelete, "/api/users/"+created.ID+"/devices/nonexistent", "", testAdminAliasToken)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

// --- RBAC Device Filtering: Admin Sees All ---

func TestAdmin_SeesAllDevices(t *testing.T) {
	app, r := setupTestApp(t)
	for _, id := range []string{"dev-admin-1", "dev-admin-2", "dev-admin-3"} {
		if err := app.devices.UpsertDevice(protocol.RegisterPayload{
			DeviceID: id, Hostname: id, OS: "linux", Arch: "amd64", ClientVersion: "0.1.0",
		}); err != nil {
			t.Fatalf("create device: %v", err)
		}
	}

	rr := doRequest(t, r, http.MethodGet, "/api/devices", "", testAdminAliasToken)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var resp struct {
		Devices []server.DeviceSnapshot `json:"devices"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Devices) != 3 {
		t.Fatalf("admin should see all 3 devices, got %d", len(resp.Devices))
	}
}

// --- RBAC Device Filtering: User With No Devices ---

func TestUser_WithNoBindings_SeesNoDevices(t *testing.T) {
	app, r := setupTestApp(t)
	if err := app.devices.UpsertDevice(protocol.RegisterPayload{
		DeviceID: "dev-no-bind", Hostname: "host", OS: "linux", Arch: "amd64", ClientVersion: "0.1.0",
	}); err != nil {
		t.Fatalf("create device: %v", err)
	}
	if _, err := app.users.CreateUser("no-bind-user", "pass", server.RoleUser, "No Bind"); err != nil {
		t.Fatalf("create user: %v", err)
	}
	token := issueUserTokenForTest(t, app, "no-bind-user", "pass")

	rr := doRequest(t, r, http.MethodGet, "/api/devices", "", token)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var resp struct {
		Devices []server.DeviceSnapshot `json:"devices"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Devices) != 0 {
		t.Fatalf("user with no bindings should see 0 devices, got %d", len(resp.Devices))
	}
}

// --- RBAC: User Accessing Another User's Device ---

func TestUser_CannotAccessOtherUsersDevice(t *testing.T) {
	app, r := setupTestApp(t)
	user1, err := app.users.CreateUser("user1-rbac", "pass1", server.RoleUser, "User 1")
	if err != nil {
		t.Fatalf("create user1: %v", err)
	}
	if _, err := app.users.CreateUser("user2-rbac", "pass2", server.RoleUser, "User 2"); err != nil {
		t.Fatalf("create user2: %v", err)
	}
	if err := app.devices.UpsertDevice(protocol.RegisterPayload{
		DeviceID: "dev-user1-only", Hostname: "host", OS: "linux", Arch: "amd64", ClientVersion: "0.1.0",
	}); err != nil {
		t.Fatalf("create device: %v", err)
	}
	if err := app.users.BindDevice(user1.ID, "dev-user1-only"); err != nil {
		t.Fatalf("bind: %v", err)
	}

	user2Token := issueUserTokenForTest(t, app, "user2-rbac", "pass2")

	// User2 tries to access User1's device
	rr := doRequest(t, r, http.MethodGet, "/api/devices/dev-user1-only", "", user2Token)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}

	// User2 tries to exec on User1's device
	rr = doRequest(t, r, http.MethodPost, "/api/devices/dev-user1-only/exec",
		`{"command":"openclaw","args":["status"]}`, user2Token)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for exec, got %d", rr.Code)
	}

	// User2 tries to delete User1's device
	rr = doRequest(t, r, http.MethodDelete, "/api/devices/dev-user1-only", "", user2Token)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for delete, got %d", rr.Code)
	}
}

// --- Login Handler ---

func TestLogin_EmptyCredentials(t *testing.T) {
	app, _ := setupTestApp(t)
	loginR := chi.NewRouter()
	loginR.Post("/api/auth/login", app.handleLogin)

	rr := doRequest(t, loginR, http.MethodPost, "/api/auth/login",
		`{"username":"","password":""}`, "")
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestLogin_InvalidJSON(t *testing.T) {
	app, _ := setupTestApp(t)
	loginR := chi.NewRouter()
	loginR.Post("/api/auth/login", app.handleLogin)

	rr := doRequest(t, loginR, http.MethodPost, "/api/auth/login",
		`not json`, "")
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestLogin_NonExistentUser(t *testing.T) {
	app, _ := setupTestApp(t)
	loginR := chi.NewRouter()
	loginR.Post("/api/auth/login", app.handleLogin)

	rr := doRequest(t, loginR, http.MethodPost, "/api/auth/login",
		`{"username":"ghost","password":"pass"}`, "")
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestLogin_ReturnsValidJWT(t *testing.T) {
	app, _ := setupTestApp(t)
	loginR := chi.NewRouter()
	loginR.Post("/api/auth/login", app.handleLogin)

	rr := doRequest(t, loginR, http.MethodPost, "/api/auth/login",
		`{"username":"admin","password":"admin"}`, "")
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var resp struct {
		Token string      `json:"token"`
		User  server.User `json:"user"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Token == "" {
		t.Fatalf("expected non-empty token")
	}

	// Verify the returned token is actually usable
	parsed, err := app.auth.ParseUserToken(resp.Token)
	if err != nil {
		t.Fatalf("returned token is not valid: %v", err)
	}
	if parsed.Username != "admin" {
		t.Fatalf("token username mismatch: %q", parsed.Username)
	}
}

// --- Join Token API with userId ---

func TestJoinTokenAPI_CreateWithInvalidUserID(t *testing.T) {
	_, r := setupTestApp(t)
	// Create a join token with a non-existent userId — API validates userId
	rr := doRequest(t, r, http.MethodPost, "/api/join-tokens",
		`{"label":"bad-user","expiresInHours":1,"userId":"nonexistent-user"}`, testAdminAliasToken)
	if rr.Code != http.StatusBadRequest && rr.Code != http.StatusNotFound {
		t.Fatalf("expected 400 or 404 for invalid userId, got %d: %s", rr.Code, rr.Body.String())
	}
}

// --- List Users ---

func TestListUsers_Success(t *testing.T) {
	app, r := setupTestApp(t)
	if _, err := app.users.CreateUser("list-u1", "pass", server.RoleUser, "U1"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := app.users.CreateUser("list-u2", "pass", server.RoleUser, "U2"); err != nil {
		t.Fatalf("create: %v", err)
	}

	rr := doRequest(t, r, http.MethodGet, "/api/users", "", testAdminAliasToken)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var resp struct {
		Users []server.UserSummary `json:"users"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// +1 for the default admin
	if len(resp.Users) != 3 {
		t.Fatalf("expected 3 users (default admin + 2), got %d", len(resp.Users))
	}
}

// --- Update User Invalid Role ---

func TestUpdateUser_InvalidRoleViaAPI(t *testing.T) {
	app, r := setupTestApp(t)
	created, err := app.users.CreateUser("bad-role-upd", "pass", server.RoleUser, "BR")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	rr := doRequest(t, r, http.MethodPut, "/api/users/"+created.ID,
		`{"role":"moderator"}`, testAdminAliasToken)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
}
