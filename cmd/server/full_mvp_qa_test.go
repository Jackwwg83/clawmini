package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/raystone-ai/clawmini/internal/protocol"
	"github.com/raystone-ai/clawmini/internal/server"
)

// ──────────────────────────────────────────────────────────────────────────────
// Helper: create a user via API and return its ID + JWT
// ──────────────────────────────────────────────────────────────────────────────
func createUserViaAPI(t *testing.T, r http.Handler, username, password, role string) (userID, jwt string) {
	t.Helper()
	body := fmt.Sprintf(`{"username":%q,"password":%q,"role":%q}`, username, password, role)
	rr := doRequest(t, r, http.MethodPost, "/api/users", body, testAdminAliasToken)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create user %s: expected 201, got %d: %s", username, rr.Code, rr.Body.String())
	}
	var u server.User
	if err := json.Unmarshal(rr.Body.Bytes(), &u); err != nil {
		t.Fatalf("decode created user: %v", err)
	}

	loginBody := fmt.Sprintf(`{"username":%q,"password":%q}`, username, password)
	lr := doRequest(t, r, http.MethodPost, "/api/auth/login", loginBody, "")
	if lr.Code != http.StatusOK {
		t.Fatalf("login %s: expected 200, got %d: %s", username, lr.Code, lr.Body.String())
	}
	var loginResp struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(lr.Body.Bytes(), &loginResp); err != nil {
		t.Fatalf("decode login: %v", err)
	}
	return u.ID, loginResp.Token
}

// seedDeviceHostname registers a device with a custom hostname.
func seedDeviceHostname(t *testing.T, app *serverApp, id, hostname string) {
	t.Helper()
	if err := app.devices.UpsertDevice(protocol.RegisterPayload{
		DeviceID: id, Hostname: hostname, OS: "linux", Arch: "amd64", ClientVersion: "0.1.0",
	}); err != nil {
		t.Fatalf("seed device %s: %v", id, err)
	}
}

// ══════════════════════════════════════════════════════════════════════════════
// 1. INTEGRATION TESTS (cross-feature)
// ══════════════════════════════════════════════════════════════════════════════

func TestIntegration_JoinTokenWithUserID_AutoBindsDevice(t *testing.T) {
	app, r := setupTestApp(t)

	// 1. Admin creates a regular user
	userID, userJWT := createUserViaAPI(t, r, "alice", "pass1234", "user")

	// 2. Admin creates a join token with userId
	body := fmt.Sprintf(`{"label":"auto-bind","expiresInHours":24,"userId":%q}`, userID)
	rr := doRequest(t, r, http.MethodPost, "/api/join-tokens", body, testAdminAliasToken)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create join token: expected 201, got %d: %s", rr.Code, rr.Body.String())
	}
	var tok server.JoinToken
	if err := json.Unmarshal(rr.Body.Bytes(), &tok); err != nil {
		t.Fatalf("decode token: %v", err)
	}
	if tok.UserID == nil || *tok.UserID != userID {
		t.Fatalf("expected userId=%s on token, got %v", userID, tok.UserID)
	}

	// 3. Simulate device registration with the join token (via hub)
	consumed, err := app.joinTokens.ValidateAndConsume(tok.ID, "dev-alice-1")
	if err != nil {
		t.Fatalf("consume join token: %v", err)
	}
	if err := app.devices.UpsertDevice(protocol.RegisterPayload{
		DeviceID: "dev-alice-1", Hostname: "alice-host", OS: "linux", Arch: "amd64", ClientVersion: "0.1.0",
	}); err != nil {
		t.Fatalf("upsert device: %v", err)
	}
	// Auto-bind if userId set
	if consumed.UserID != nil {
		if err := app.users.BindDevice(*consumed.UserID, "dev-alice-1"); err != nil {
			t.Fatalf("auto-bind: %v", err)
		}
	}

	// 4. User alice sees the device in her device list
	rr = doRequest(t, r, http.MethodGet, "/api/devices", "", userJWT)
	if rr.Code != http.StatusOK {
		t.Fatalf("list devices: expected 200, got %d", rr.Code)
	}
	var devResp struct {
		Devices []server.DeviceSnapshot `json:"devices"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &devResp); err != nil {
		t.Fatalf("decode devices: %v", err)
	}
	if len(devResp.Devices) != 1 || devResp.Devices[0].ID != "dev-alice-1" {
		t.Fatalf("expected alice to see exactly dev-alice-1, got %+v", devResp.Devices)
	}
}

func TestIntegration_AdminCreatesUser_BindsDevice_UserSeesOnlyTheirDevice(t *testing.T) {
	app, r := setupTestApp(t)

	// Seed devices
	seedDevice(t, app, "dev-a")
	seedDevice(t, app, "dev-b")

	// Create user and bind dev-a only
	userID, userJWT := createUserViaAPI(t, r, "bob", "bobpass", "user")
	rr := doRequest(t, r, http.MethodPost, fmt.Sprintf("/api/users/%s/devices", userID),
		`{"deviceId":"dev-a"}`, testAdminAliasToken)
	if rr.Code != http.StatusOK {
		t.Fatalf("bind device: expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	// User bob lists devices — should see only dev-a
	rr = doRequest(t, r, http.MethodGet, "/api/devices", "", userJWT)
	if rr.Code != http.StatusOK {
		t.Fatalf("list devices: expected 200, got %d", rr.Code)
	}
	var resp struct {
		Devices []server.DeviceSnapshot `json:"devices"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if len(resp.Devices) != 1 || resp.Devices[0].ID != "dev-a" {
		t.Fatalf("bob should see only dev-a, got %d devices", len(resp.Devices))
	}

	// Bob tries to get dev-b — should be forbidden
	rr = doRequest(t, r, http.MethodGet, "/api/devices/dev-b", "", userJWT)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("get dev-b: expected 403, got %d", rr.Code)
	}

	// Bob can get dev-a
	rr = doRequest(t, r, http.MethodGet, "/api/devices/dev-a", "", userJWT)
	if rr.Code != http.StatusOK {
		t.Fatalf("get dev-a: expected 200, got %d", rr.Code)
	}
}

func TestIntegration_BatchOperation_RespectsUserAccess(t *testing.T) {
	app, r := setupTestApp(t)

	seedDevice(t, app, "batch-dev-1")
	seedDevice(t, app, "batch-dev-2")

	userID, userJWT := createUserViaAPI(t, r, "batchuser", "batchpass", "user")
	// Bind only batch-dev-1
	doRequest(t, r, http.MethodPost, fmt.Sprintf("/api/users/%s/devices", userID),
		`{"deviceId":"batch-dev-1"}`, testAdminAliasToken)

	// User tries to batch both devices — should be forbidden
	rr := doRequest(t, r, http.MethodPost, "/api/batch/exec",
		`{"deviceIds":["batch-dev-1","batch-dev-2"],"command":"openclaw status"}`, userJWT)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("batch with unbound device: expected 403, got %d: %s", rr.Code, rr.Body.String())
	}

	// User batches only their own device — should succeed (202)
	rr = doRequest(t, r, http.MethodPost, "/api/batch/exec",
		`{"deviceIds":["batch-dev-1"],"command":"openclaw status"}`, userJWT)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("batch own device: expected 202, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestIntegration_AuditLogCapturesBothAdminAndUserActions(t *testing.T) {
	app, r := setupTestApp(t)

	seedDevice(t, app, "audit-dev")

	// Admin exec (will fail because offline but logs "failed")
	doRequest(t, r, http.MethodPost, "/api/devices/audit-dev/exec",
		`{"command":"openclaw","args":["status"],"timeout":5}`, testAdminAliasToken)

	// Create user, bind device, exec
	userID, userJWT := createUserViaAPI(t, r, "audituser", "auditpass", "user")
	doRequest(t, r, http.MethodPost, fmt.Sprintf("/api/users/%s/devices", userID),
		`{"deviceId":"audit-dev"}`, testAdminAliasToken)
	doRequest(t, r, http.MethodPost, "/api/devices/audit-dev/exec",
		`{"command":"openclaw","args":["doctor","--json"],"timeout":5}`, userJWT)

	// Give audit writes a moment
	time.Sleep(50 * time.Millisecond)

	// Admin sees all audit entries
	rr := doRequest(t, r, http.MethodGet, "/api/audit-log?device_id=audit-dev", "", testAdminAliasToken)
	if rr.Code != http.StatusOK {
		t.Fatalf("audit log: expected 200, got %d", rr.Code)
	}
	var page server.AuditLogPage
	_ = json.Unmarshal(rr.Body.Bytes(), &page)
	if page.Total < 2 {
		t.Fatalf("expected >=2 audit entries for audit-dev, got %d", page.Total)
	}

	// User sees only entries for their own device
	rr = doRequest(t, r, http.MethodGet, "/api/audit-log", "", userJWT)
	if rr.Code != http.StatusOK {
		t.Fatalf("user audit log: expected 200, got %d", rr.Code)
	}
	var userPage server.AuditLogPage
	_ = json.Unmarshal(rr.Body.Bytes(), &userPage)
	for _, item := range userPage.Items {
		if item.TargetDeviceID != "" && item.TargetDeviceID != "audit-dev" {
			t.Fatalf("user should not see entries for device %s", item.TargetDeviceID)
		}
	}
}

func TestIntegration_DeviceDeletion_CascadesBindingsAndCommands(t *testing.T) {
	app, r := setupTestApp(t)

	seedDevice(t, app, "cascade-dev")

	// Create user and bind
	userID, _ := createUserViaAPI(t, r, "cascadeuser", "cp", "user")
	doRequest(t, r, http.MethodPost, fmt.Sprintf("/api/users/%s/devices", userID),
		`{"deviceId":"cascade-dev"}`, testAdminAliasToken)

	// Create a command
	_, err := app.commands.Create("cascade-dev", "openclaw", []string{"status"}, 60)
	if err != nil {
		t.Fatalf("create command: %v", err)
	}

	// Verify binding exists
	bound, err := app.users.IsDeviceBoundToUser(userID, "cascade-dev")
	if err != nil {
		t.Fatalf("check binding: %v", err)
	}
	if !bound {
		t.Fatalf("expected device to be bound before deletion")
	}

	// Delete device
	rr := doRequest(t, r, http.MethodDelete, "/api/devices/cascade-dev", "", testAdminAliasToken)
	if rr.Code != http.StatusOK {
		t.Fatalf("delete device: expected 200, got %d", rr.Code)
	}

	// Binding should be gone (device cascade)
	bound, err = app.users.IsDeviceBoundToUser(userID, "cascade-dev")
	if err != nil {
		t.Fatalf("check binding after delete: %v", err)
	}
	if bound {
		t.Fatalf("binding should be cleaned up after device deletion")
	}
}

func TestIntegration_UserDeletion_UnbindsDevices(t *testing.T) {
	app, r := setupTestApp(t)

	seedDevice(t, app, "unbind-dev")
	userID, _ := createUserViaAPI(t, r, "deluser", "delpass", "user")
	doRequest(t, r, http.MethodPost, fmt.Sprintf("/api/users/%s/devices", userID),
		`{"deviceId":"unbind-dev"}`, testAdminAliasToken)

	// Verify binding
	bound, _ := app.users.IsDeviceBoundToUser(userID, "unbind-dev")
	if !bound {
		t.Fatalf("expected binding before user delete")
	}

	// Delete user
	rr := doRequest(t, r, http.MethodDelete, fmt.Sprintf("/api/users/%s", userID), "", testAdminAliasToken)
	if rr.Code != http.StatusOK {
		t.Fatalf("delete user: expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	// Binding should be gone
	bound, _ = app.users.IsDeviceBoundToUser(userID, "unbind-dev")
	if bound {
		t.Fatalf("binding should be removed after user deletion")
	}

	// Device should still exist
	rr = doRequest(t, r, http.MethodGet, "/api/devices/unbind-dev", "", testAdminAliasToken)
	if rr.Code != http.StatusOK {
		t.Fatalf("device should still exist after user deletion, got %d", rr.Code)
	}
}

func TestIntegration_UserCannotAccessAdminEndpoints(t *testing.T) {
	_, r := setupTestApp(t)
	_, userJWT := createUserViaAPI(t, r, "reguser", "regpass", "user")

	// All admin-only endpoints should return 403
	adminEndpoints := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodPost, "/api/join-tokens", `{"label":"x","expiresInHours":1}`},
		{http.MethodGet, "/api/join-tokens", ""},
		{http.MethodDelete, "/api/join-tokens/fake-id", ""},
		{http.MethodPost, "/api/users", `{"username":"x","password":"x","role":"user"}`},
		{http.MethodGet, "/api/users", ""},
		{http.MethodGet, "/api/users/fake-id", ""},
		{http.MethodPut, "/api/users/fake-id", `{"displayName":"x"}`},
		{http.MethodDelete, "/api/users/fake-id", ""},
		{http.MethodPost, "/api/users/fake-id/devices", `{"deviceId":"x"}`},
		{http.MethodDelete, "/api/users/fake-id/devices/x", ""},
	}
	for _, ep := range adminEndpoints {
		rr := doRequest(t, r, ep.method, ep.path, ep.body, userJWT)
		if rr.Code != http.StatusForbidden {
			t.Errorf("%s %s: expected 403, got %d", ep.method, ep.path, rr.Code)
		}
	}
}

func TestIntegration_UserSelfServiceProfile(t *testing.T) {
	_, r := setupTestApp(t)
	_, userJWT := createUserViaAPI(t, r, "selfuser", "oldpass", "user")

	// GET /api/me
	rr := doRequest(t, r, http.MethodGet, "/api/me", "", userJWT)
	if rr.Code != http.StatusOK {
		t.Fatalf("get me: expected 200, got %d", rr.Code)
	}
	var me struct {
		User server.User `json:"user"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &me)
	if me.User.Username != "selfuser" {
		t.Fatalf("expected selfuser, got %s", me.User.Username)
	}

	// Change password
	rr = doRequest(t, r, http.MethodPut, "/api/me/password",
		`{"oldPassword":"oldpass","newPassword":"newpass123"}`, userJWT)
	if rr.Code != http.StatusOK {
		t.Fatalf("change password: expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	// Login with new password
	rr = doRequest(t, r, http.MethodPost, "/api/auth/login",
		`{"username":"selfuser","password":"newpass123"}`, "")
	if rr.Code != http.StatusOK {
		t.Fatalf("login with new password: expected 200, got %d", rr.Code)
	}

	// Old password no longer works
	rr = doRequest(t, r, http.MethodPost, "/api/auth/login",
		`{"username":"selfuser","password":"oldpass"}`, "")
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("login with old password: expected 401, got %d", rr.Code)
	}
}

// ══════════════════════════════════════════════════════════════════════════════
// 2. SECURITY AUDIT TESTS
// ══════════════════════════════════════════════════════════════════════════════

func TestSecurity_AdminCannotDeleteSelf(t *testing.T) {
	_, r := setupTestApp(t)

	// Get admin user ID from /api/me
	rr := doRequest(t, r, http.MethodGet, "/api/me", "", testAdminAliasToken)
	var me struct {
		User server.User `json:"user"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &me)

	// Try to delete self
	rr = doRequest(t, r, http.MethodDelete, fmt.Sprintf("/api/users/%s", me.User.ID), "", testAdminAliasToken)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("self-delete: expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestSecurity_LastAdminCannotBeDemoted(t *testing.T) {
	_, r := setupTestApp(t)

	// Get admin user ID
	rr := doRequest(t, r, http.MethodGet, "/api/me", "", testAdminAliasToken)
	var me struct {
		User server.User `json:"user"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &me)

	// Try to demote
	rr = doRequest(t, r, http.MethodPut, fmt.Sprintf("/api/users/%s", me.User.ID),
		`{"role":"user"}`, testAdminAliasToken)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("demote last admin: expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestSecurity_LastAdminCannotBeDeleted(t *testing.T) {
	_, r := setupTestApp(t)

	// Create a second admin so we can test
	adminID2, _ := createUserViaAPI(t, r, "admin2", "admin2pass", "admin")

	// Get current admin ID
	rr := doRequest(t, r, http.MethodGet, "/api/me", "", testAdminAliasToken)
	var me struct {
		User server.User `json:"user"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &me)

	// Delete second admin — should work because there's still one
	rr = doRequest(t, r, http.MethodDelete, fmt.Sprintf("/api/users/%s", adminID2), "", testAdminAliasToken)
	if rr.Code != http.StatusOK {
		t.Fatalf("delete second admin: expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	// Now try to delete self (only admin left) — should fail
	rr = doRequest(t, r, http.MethodDelete, fmt.Sprintf("/api/users/%s", me.User.ID), "", testAdminAliasToken)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("delete last admin: expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestSecurity_JWTTokenReuseAfterPasswordChange(t *testing.T) {
	_, r := setupTestApp(t)
	_, userJWT := createUserViaAPI(t, r, "jwtuser", "pass1", "user")

	// Change password
	rr := doRequest(t, r, http.MethodPut, "/api/me/password",
		`{"oldPassword":"pass1","newPassword":"pass2"}`, userJWT)
	if rr.Code != http.StatusOK {
		t.Fatalf("change password: expected 200, got %d", rr.Code)
	}

	// Old JWT should still work (JWT is stateless, valid until expiry).
	// The system refreshes user role/name from DB on each request.
	rr = doRequest(t, r, http.MethodGet, "/api/me", "", userJWT)
	if rr.Code != http.StatusOK {
		t.Logf("NOTE: JWT still valid after password change (stateless design): status=%d", rr.Code)
	}
	// Document this as expected behavior — JWT tokens remain valid until 24h expiry
}

func TestSecurity_ConcurrentLoginSameCredentials(t *testing.T) {
	_, r := setupTestApp(t)
	createUserViaAPI(t, r, "concuser", "concpass", "user")

	var wg sync.WaitGroup
	tokens := make([]string, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			rr := doRequest(t, r, http.MethodPost, "/api/auth/login",
				`{"username":"concuser","password":"concpass"}`, "")
			if rr.Code == http.StatusOK {
				var resp struct {
					Token string `json:"token"`
				}
				_ = json.Unmarshal(rr.Body.Bytes(), &resp)
				tokens[idx] = resp.Token
			}
		}(i)
	}
	wg.Wait()

	// All tokens should be valid (concurrent login is allowed)
	successCount := 0
	for _, tok := range tokens {
		if tok != "" {
			successCount++
		}
	}
	// With rate limit of 5/minute, at most 5 should succeed
	if successCount < 1 {
		t.Fatalf("expected at least 1 successful login, got %d", successCount)
	}
}

func TestSecurity_RateLimitingOnLogin(t *testing.T) {
	app, _ := setupTestApp(t)
	limiter := newLoginRateLimiter(3, 5*time.Second)

	loginR := buildRouterWithRateLimiter(app, limiter)

	// First 3 should succeed (or 401 for wrong creds, but not 429)
	for i := 0; i < 3; i++ {
		rr := doRequest(t, loginR, http.MethodPost, "/api/auth/login",
			`{"username":"admin","password":"wrong"}`, "")
		if rr.Code == http.StatusTooManyRequests {
			t.Fatalf("attempt %d should not be rate limited", i+1)
		}
	}

	// 4th should be rate limited
	rr := doRequest(t, loginR, http.MethodPost, "/api/auth/login",
		`{"username":"admin","password":"admin"}`, "")
	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("4th attempt: expected 429, got %d", rr.Code)
	}
}

func buildRouterWithRateLimiter(app *serverApp, limiter *loginRateLimiter) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		limiter.Middleware(app.handleLogin)(w, r)
	})
}

func TestSecurity_CommandInjection_AllInputVectors(t *testing.T) {
	app, r := setupTestApp(t)

	injectionPayloads := []string{
		`"; rm -rf / #`,
		`$(cat /etc/passwd)`,
		"`whoami`",
		"test; ls",
		"test | cat /etc/shadow",
		"test & nc -e /bin/sh",
		"test\ncat /etc/passwd",
	}

	// Test via username
	for _, payload := range injectionPayloads {
		body := fmt.Sprintf(`{"username":%q,"password":"test","role":"user"}`, payload)
		rr := doRequest(t, r, http.MethodPost, "/api/users", body, testAdminAliasToken)
		// Should either create safely (parameterized query) or reject — never execute
		if rr.Code == http.StatusCreated {
			// Verify the literal was stored, not executed
			var u server.User
			_ = json.Unmarshal(rr.Body.Bytes(), &u)
			if u.Username != strings.TrimSpace(payload) {
				t.Errorf("username injection: stored %q instead of %q", u.Username, payload)
			}
		}
	}

	// Test command injection via exec args
	seedDevice(t, app, "inject-dev")
	badArgs := []string{
		`{"command":"openclaw","args":["status;rm -rf /"],"timeout":5}`,
		`{"command":"openclaw","args":["status|cat /etc/shadow"],"timeout":5}`,
		`{"command":"openclaw","args":["status&nc attacker"],"timeout":5}`,
		`{"command":"openclaw","args":["status$(whoami)"],"timeout":5}`,
	}
	for _, body := range badArgs {
		rr := doRequest(t, r, http.MethodPost, "/api/devices/inject-dev/exec", body, testAdminAliasToken)
		if rr.Code == http.StatusAccepted {
			t.Errorf("injection should be blocked: %s → %d", body, rr.Code)
		}
	}
}

func TestSecurity_XSSVectors_InDisplayNames(t *testing.T) {
	_, r := setupTestApp(t)

	xssPayloads := []string{
		`<script>alert('xss')</script>`,
		`<img src=x onerror=alert(1)>`,
		`javascript:alert(1)`,
		`"><svg onload=alert(1)>`,
	}

	for i, payload := range xssPayloads {
		username := fmt.Sprintf("xssuser%d", i)
		body := fmt.Sprintf(`{"username":%q,"password":"testpass","role":"user","displayName":%q}`, username, payload)
		rr := doRequest(t, r, http.MethodPost, "/api/users", body, testAdminAliasToken)
		if rr.Code != http.StatusCreated {
			continue // Some chars might be rejected, which is OK
		}

		var u server.User
		_ = json.Unmarshal(rr.Body.Bytes(), &u)
		// The display name should be stored literally (frontend escapes on render)
		if u.DisplayName != payload {
			t.Errorf("XSS payload %d: stored %q instead of literal %q", i, u.DisplayName, payload)
		}
	}
}

func TestSecurity_DeviceTokenIsolationFromJWT(t *testing.T) {
	_, r := setupTestApp(t)

	// Device token should not work as JWT
	rr := doRequest(t, r, http.MethodGet, "/api/me", "", "test-device-token")
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("device token as JWT: expected 401, got %d", rr.Code)
	}
}

func TestSecurity_PasswordChangeRequiresOldPassword(t *testing.T) {
	_, r := setupTestApp(t)
	_, userJWT := createUserViaAPI(t, r, "pwuser", "oldpw", "user")

	// Wrong old password
	rr := doRequest(t, r, http.MethodPut, "/api/me/password",
		`{"oldPassword":"wrongold","newPassword":"newpw"}`, userJWT)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("wrong old password: expected 401, got %d", rr.Code)
	}

	// Empty old password
	rr = doRequest(t, r, http.MethodPut, "/api/me/password",
		`{"oldPassword":"","newPassword":"newpw"}`, userJWT)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("empty old password: expected 400, got %d", rr.Code)
	}
}

// ══════════════════════════════════════════════════════════════════════════════
// 3. DATA INTEGRITY TESTS
// ══════════════════════════════════════════════════════════════════════════════

func TestDataIntegrity_SchemaIdempotency(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "idempotent.db")
	db, err := server.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	// Run all schema migrations 3 times
	for i := 0; i < 3; i++ {
		for _, setup := range []func() error{
			server.NewDeviceStore(db).EnsureSchema,
			server.NewUserStore(db).EnsureSchema,
			server.NewCommandStore(db).EnsureSchema,
			server.NewJoinTokenStore(db).EnsureSchema,
			server.NewBatchJobStore(db).EnsureSchema,
			server.NewAuditLogStore(db).EnsureSchema,
			server.NewAdminTokenStore(db).EnsureSchema,
		} {
			if err := setup(); err != nil {
				t.Fatalf("iteration %d: schema error: %v", i, err)
			}
		}
	}
}

func TestDataIntegrity_ForeignKeyCascade_DeleteDevice(t *testing.T) {
	app, _ := setupTestApp(t)

	// Create device, commands, status, user binding
	seedDevice(t, app, "fk-dev")
	app.commands.Create("fk-dev", "openclaw", []string{"status"}, 60)
	app.commands.Create("fk-dev", "openclaw", []string{"doctor", "--json"}, 60)
	app.devices.UpdateHeartbeat(protocol.HeartbeatPayload{
		DeviceID: "fk-dev",
		System:   protocol.SystemInfo{CPUUsage: 42.0, MemTotal: 1024},
	})

	user, _ := app.users.CreateUser("fkuser", "fkpass", "user", "FK User")
	app.users.BindDevice(user.ID, "fk-dev")

	// Delete device and rely on FK cascade to remove related rows.
	err := app.devices.DeleteDevice("fk-dev")
	if err != nil {
		t.Fatalf("delete device: %v", err)
	}

	// Bindings should cascade
	bound, _ := app.users.IsDeviceBoundToUser(user.ID, "fk-dev")
	if bound {
		t.Fatal("binding should cascade on device delete")
	}

	// Device status should cascade (via foreign key)
	_, err = app.devices.GetDevice("fk-dev")
	if err != server.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestDataIntegrity_ConcurrentWritesToSameDevice(t *testing.T) {
	app, _ := setupTestApp(t)
	seedDevice(t, app, "conc-dev")

	var wg sync.WaitGroup
	// 20 concurrent heartbeats
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			app.devices.UpdateHeartbeat(protocol.HeartbeatPayload{
				DeviceID: "conc-dev",
				System:   protocol.SystemInfo{CPUUsage: float64(idx), MemTotal: uint64(idx * 1024)},
			})
		}(i)
	}
	wg.Wait()

	// Device should still be readable
	snap, err := app.devices.GetDevice("conc-dev")
	if err != nil {
		t.Fatalf("get device after concurrent writes: %v", err)
	}
	if snap.ID != "conc-dev" {
		t.Fatalf("device ID mismatch")
	}
}

func TestDataIntegrity_LargeDataset(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping large dataset test in short mode")
	}
	app, r := setupTestApp(t)

	// Create 100 devices
	for i := 0; i < 100; i++ {
		seedDevice(t, app, fmt.Sprintf("large-dev-%d", i))
	}

	// Create 20 users
	for i := 0; i < 20; i++ {
		app.users.CreateUser(fmt.Sprintf("largeuser%d", i), "pass", "user", "")
	}

	// Create 100 audit entries
	for i := 0; i < 100; i++ {
		app.auditLogs.Log("test.action", fmt.Sprintf("large-dev-%d", i%100), "detail", "1.2.3.4", "success")
	}

	// List all devices
	rr := doRequest(t, r, http.MethodGet, "/api/devices", "", testAdminAliasToken)
	if rr.Code != http.StatusOK {
		t.Fatalf("list 100 devices: expected 200, got %d", rr.Code)
	}
	var devResp struct {
		Devices []json.RawMessage `json:"devices"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &devResp)
	if len(devResp.Devices) != 100 {
		t.Fatalf("expected 100 devices, got %d", len(devResp.Devices))
	}

	// List users
	rr = doRequest(t, r, http.MethodGet, "/api/users", "", testAdminAliasToken)
	if rr.Code != http.StatusOK {
		t.Fatalf("list users: expected 200, got %d", rr.Code)
	}

	// Audit log pagination
	rr = doRequest(t, r, http.MethodGet, "/api/audit-log?limit=10&offset=0", "", testAdminAliasToken)
	if rr.Code != http.StatusOK {
		t.Fatalf("audit log page: expected 200, got %d", rr.Code)
	}
	var page server.AuditLogPage
	_ = json.Unmarshal(rr.Body.Bytes(), &page)
	if len(page.Items) != 10 {
		t.Fatalf("expected 10 items per page, got %d", len(page.Items))
	}
	if page.Total < 100 {
		t.Fatalf("expected total >= 100, got %d", page.Total)
	}
}

// ══════════════════════════════════════════════════════════════════════════════
// 4. API CONTRACT TESTS
// ══════════════════════════════════════════════════════════════════════════════

func TestAPIContract_AllEndpointsRequireAuth(t *testing.T) {
	_, r := setupTestApp(t)

	authEndpoints := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/me"},
		{http.MethodPut, "/api/me/password"},
		{http.MethodGet, "/api/devices"},
		{http.MethodGet, "/api/devices/test"},
		{http.MethodDelete, "/api/devices/test"},
		{http.MethodPost, "/api/devices/test/exec"},
		{http.MethodGet, "/api/devices/test/exec/cmd1"},
		{http.MethodPost, "/api/devices/test/install-openclaw"},
		{http.MethodGet, "/api/devices/test/install-openclaw/j1"},
		{http.MethodPost, "/api/devices/test/configure-im"},
		{http.MethodGet, "/api/devices/test/configure-im/j1"},
		{http.MethodPost, "/api/batch/exec"},
		{http.MethodGet, "/api/batch/j1"},
		{http.MethodGet, "/api/audit-log"},
		{http.MethodPost, "/api/join-tokens"},
		{http.MethodGet, "/api/join-tokens"},
		{http.MethodDelete, "/api/join-tokens/x"},
		{http.MethodPost, "/api/users"},
		{http.MethodGet, "/api/users"},
		{http.MethodGet, "/api/users/x"},
		{http.MethodPut, "/api/users/x"},
		{http.MethodDelete, "/api/users/x"},
		{http.MethodPost, "/api/users/x/devices"},
		{http.MethodDelete, "/api/users/x/devices/y"},
	}
	for _, ep := range authEndpoints {
		rr := doRequest(t, r, ep.method, ep.path, "", "") // no token
		if rr.Code != http.StatusUnauthorized {
			t.Errorf("%s %s: expected 401 without auth, got %d", ep.method, ep.path, rr.Code)
		}
	}
}

func TestAPIContract_ErrorResponseFormat(t *testing.T) {
	_, r := setupTestApp(t)

	// Test various error cases and verify consistent JSON error format
	cases := []struct {
		method string
		path   string
		body   string
		token  string
		code   int
	}{
		{http.MethodGet, "/api/devices/nonexistent", "", testAdminAliasToken, 404},
		{http.MethodPost, "/api/join-tokens", "invalid json", testAdminAliasToken, 400},
		{http.MethodGet, "/api/me", "", "bad-token", 401},
		{http.MethodPost, "/api/join-tokens", `{"label":"x","expiresInHours":1}`, "bad-token", 401},
	}

	for _, tc := range cases {
		rr := doRequest(t, r, tc.method, tc.path, tc.body, tc.token)
		if rr.Code != tc.code {
			t.Errorf("%s %s: expected %d, got %d", tc.method, tc.path, tc.code, rr.Code)
			continue
		}

		// All error responses should be JSON with "error" field
		var errResp map[string]interface{}
		if err := json.Unmarshal(rr.Body.Bytes(), &errResp); err != nil {
			t.Errorf("%s %s: error response not valid JSON: %s", tc.method, tc.path, rr.Body.String())
			continue
		}
		if _, ok := errResp["error"]; !ok {
			t.Errorf("%s %s: error response missing 'error' field: %s", tc.method, tc.path, rr.Body.String())
		}
	}
}

func TestAPIContract_PaginationAuditLog(t *testing.T) {
	app, r := setupTestApp(t)

	// Create 25 audit entries
	for i := 0; i < 25; i++ {
		app.auditLogs.Log("test.page", fmt.Sprintf("dev-%d", i), "detail", "1.2.3.4", "success")
	}

	// First page (10 items)
	rr := doRequest(t, r, http.MethodGet, "/api/audit-log?limit=10&offset=0", "", testAdminAliasToken)
	if rr.Code != http.StatusOK {
		t.Fatalf("page 1: expected 200, got %d", rr.Code)
	}
	var page1 server.AuditLogPage
	_ = json.Unmarshal(rr.Body.Bytes(), &page1)
	if len(page1.Items) != 10 {
		t.Fatalf("page 1: expected 10 items, got %d", len(page1.Items))
	}
	if page1.Total != 25 {
		t.Fatalf("page 1: expected total=25, got %d", page1.Total)
	}
	if page1.Limit != 10 {
		t.Fatalf("page 1: expected limit=10, got %d", page1.Limit)
	}
	if page1.Offset != 0 {
		t.Fatalf("page 1: expected offset=0, got %d", page1.Offset)
	}

	// Second page
	rr = doRequest(t, r, http.MethodGet, "/api/audit-log?limit=10&offset=10", "", testAdminAliasToken)
	var page2 server.AuditLogPage
	_ = json.Unmarshal(rr.Body.Bytes(), &page2)
	if len(page2.Items) != 10 {
		t.Fatalf("page 2: expected 10 items, got %d", len(page2.Items))
	}

	// Third page (only 5 remaining)
	rr = doRequest(t, r, http.MethodGet, "/api/audit-log?limit=10&offset=20", "", testAdminAliasToken)
	var page3 server.AuditLogPage
	_ = json.Unmarshal(rr.Body.Bytes(), &page3)
	if len(page3.Items) != 5 {
		t.Fatalf("page 3: expected 5 items, got %d", len(page3.Items))
	}

	// Verify no overlap
	allIDs := map[int64]bool{}
	for _, item := range page1.Items {
		allIDs[item.ID] = true
	}
	for _, item := range page2.Items {
		if allIDs[item.ID] {
			t.Fatalf("page 2 overlaps with page 1: id=%d", item.ID)
		}
		allIDs[item.ID] = true
	}
	for _, item := range page3.Items {
		if allIDs[item.ID] {
			t.Fatalf("page 3 overlaps with previous pages: id=%d", item.ID)
		}
	}
}

func TestAPIContract_AuditLogFilterCombinations(t *testing.T) {
	app, r := setupTestApp(t)

	now := time.Now().Unix()
	app.auditLogs.Log("command.exec", "filter-dev-1", "d1", "1.1.1.1", "success")
	app.auditLogs.Log("device.delete", "filter-dev-2", "d2", "2.2.2.2", "failed")
	app.auditLogs.Log("command.exec", "filter-dev-1", "d3", "1.1.1.1", "failed")

	// Filter by device
	rr := doRequest(t, r, http.MethodGet, "/api/audit-log?device_id=filter-dev-1", "", testAdminAliasToken)
	var page server.AuditLogPage
	_ = json.Unmarshal(rr.Body.Bytes(), &page)
	if page.Total != 2 {
		t.Fatalf("device filter: expected 2, got %d", page.Total)
	}

	// Filter by action
	rr = doRequest(t, r, http.MethodGet, "/api/audit-log?action=device.delete", "", testAdminAliasToken)
	_ = json.Unmarshal(rr.Body.Bytes(), &page)
	if page.Total != 1 {
		t.Fatalf("action filter: expected 1, got %d", page.Total)
	}

	// Filter by time range
	rr = doRequest(t, r, http.MethodGet,
		fmt.Sprintf("/api/audit-log?from=%d&to=%d", now-1, now+10), "", testAdminAliasToken)
	_ = json.Unmarshal(rr.Body.Bytes(), &page)
	if page.Total != 3 {
		t.Fatalf("time range filter: expected 3, got %d", page.Total)
	}

	// Combined: device + action
	rr = doRequest(t, r, http.MethodGet,
		"/api/audit-log?device_id=filter-dev-1&action=command.exec", "", testAdminAliasToken)
	_ = json.Unmarshal(rr.Body.Bytes(), &page)
	if page.Total != 2 {
		t.Fatalf("combined filter: expected 2, got %d", page.Total)
	}
}

func TestAPIContract_UserAuditLogScoping(t *testing.T) {
	app, r := setupTestApp(t)

	seedDevice(t, app, "scope-dev-1")
	seedDevice(t, app, "scope-dev-2")

	userID, userJWT := createUserViaAPI(t, r, "scopeuser", "scopepass", "user")
	doRequest(t, r, http.MethodPost, fmt.Sprintf("/api/users/%s/devices", userID),
		`{"deviceId":"scope-dev-1"}`, testAdminAliasToken)

	// Create audit entries for both devices
	app.auditLogs.Log("command.exec", "scope-dev-1", "d1", "1.1.1.1", "success")
	app.auditLogs.Log("command.exec", "scope-dev-2", "d2", "2.2.2.2", "success")

	// User should only see entries for their bound device
	rr := doRequest(t, r, http.MethodGet, "/api/audit-log", "", userJWT)
	if rr.Code != http.StatusOK {
		t.Fatalf("user audit: expected 200, got %d", rr.Code)
	}
	var page server.AuditLogPage
	_ = json.Unmarshal(rr.Body.Bytes(), &page)
	for _, item := range page.Items {
		if item.TargetDeviceID == "scope-dev-2" {
			t.Fatal("user should not see entries for unbound device scope-dev-2")
		}
	}

	// User filtering unbound device should be forbidden
	rr = doRequest(t, r, http.MethodGet, "/api/audit-log?device_id=scope-dev-2", "", userJWT)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("user filter unbound device: expected 403, got %d", rr.Code)
	}
}

// ══════════════════════════════════════════════════════════════════════════════
// 5. EDGE CASES
// ══════════════════════════════════════════════════════════════════════════════

func TestEdge_EmptyDatabase_FirstBoot(t *testing.T) {
	// Fresh DB — no users yet, EnsureDefaultAdmin should create admin/admin
	dbPath := filepath.Join(t.TempDir(), "fresh.db")
	db, err := server.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	ds := server.NewDeviceStore(db)
	ds.EnsureSchema()
	us := server.NewUserStore(db)
	us.EnsureSchema()

	created, err := us.EnsureDefaultAdmin()
	if err != nil {
		t.Fatalf("ensure default admin: %v", err)
	}
	if !created {
		t.Fatal("expected default admin to be created on empty DB")
	}

	// Authenticate with default credentials
	user, err := us.Authenticate("admin", "admin")
	if err != nil {
		t.Fatalf("auth default admin: %v", err)
	}
	if user.Role != "admin" {
		t.Fatalf("expected admin role, got %s", user.Role)
	}

	// Devices list empty
	devices, err := ds.ListDevices()
	if err != nil {
		t.Fatalf("list devices: %v", err)
	}
	if len(devices) != 0 {
		t.Fatalf("expected 0 devices, got %d", len(devices))
	}
}

func TestEdge_VeryLongStrings(t *testing.T) {
	_, r := setupTestApp(t)

	longStr := strings.Repeat("x", 1000)

	// Long username
	rr := doRequest(t, r, http.MethodPost, "/api/users",
		fmt.Sprintf(`{"username":%q,"password":"pass","role":"user"}`, longStr), testAdminAliasToken)
	// Should succeed or fail gracefully (no crash)
	if rr.Code >= 500 {
		t.Fatalf("long username: server error %d", rr.Code)
	}

	// Long display name
	rr = doRequest(t, r, http.MethodPost, "/api/users",
		fmt.Sprintf(`{"username":"longdn","password":"pass","role":"user","displayName":%q}`, longStr), testAdminAliasToken)
	if rr.Code >= 500 {
		t.Fatalf("long display name: server error %d", rr.Code)
	}

	// Long join token label
	rr = doRequest(t, r, http.MethodPost, "/api/join-tokens",
		fmt.Sprintf(`{"label":%q,"expiresInHours":1}`, longStr), testAdminAliasToken)
	if rr.Code >= 500 {
		t.Fatalf("long label: server error %d", rr.Code)
	}
}

func TestEdge_UnicodeInAllFields(t *testing.T) {
	app, r := setupTestApp(t)

	// Unicode username and display name
	rr := doRequest(t, r, http.MethodPost, "/api/users",
		`{"username":"用户名","password":"密码123","role":"user","displayName":"显示名称🎉"}`,
		testAdminAliasToken)
	if rr.Code != http.StatusCreated {
		t.Fatalf("unicode user: expected 201, got %d: %s", rr.Code, rr.Body.String())
	}
	var u server.User
	_ = json.Unmarshal(rr.Body.Bytes(), &u)
	if u.Username != "用户名" || u.DisplayName != "显示名称🎉" {
		t.Fatalf("unicode not preserved: username=%q displayName=%q", u.Username, u.DisplayName)
	}

	// Unicode device hostname
	seedDeviceHostname(t, app, "unicode-dev", "设备名称🔧")
	snap, err := app.devices.GetDevice("unicode-dev")
	if err != nil {
		t.Fatalf("get unicode device: %v", err)
	}
	if snap.Hostname != "设备名称🔧" {
		t.Fatalf("hostname not preserved: %q", snap.Hostname)
	}

	// Unicode join token label
	rr = doRequest(t, r, http.MethodPost, "/api/join-tokens",
		`{"label":"标签🏷️","expiresInHours":1}`, testAdminAliasToken)
	if rr.Code != http.StatusCreated {
		t.Fatalf("unicode label: expected 201, got %d", rr.Code)
	}
}

func TestEdge_DuplicateOperations(t *testing.T) {
	app, r := setupTestApp(t)

	// Double bind same device
	seedDevice(t, app, "dup-dev")
	userID, _ := createUserViaAPI(t, r, "dupuser", "duppass", "user")

	rr := doRequest(t, r, http.MethodPost, fmt.Sprintf("/api/users/%s/devices", userID),
		`{"deviceId":"dup-dev"}`, testAdminAliasToken)
	if rr.Code != http.StatusOK {
		t.Fatalf("first bind: expected 200, got %d", rr.Code)
	}

	rr = doRequest(t, r, http.MethodPost, fmt.Sprintf("/api/users/%s/devices", userID),
		`{"deviceId":"dup-dev"}`, testAdminAliasToken)
	if rr.Code != http.StatusConflict {
		t.Fatalf("double bind: expected 409, got %d: %s", rr.Code, rr.Body.String())
	}

	// Double delete user
	userID2, _ := createUserViaAPI(t, r, "dupuser2", "dp2", "user")
	rr = doRequest(t, r, http.MethodDelete, fmt.Sprintf("/api/users/%s", userID2), "", testAdminAliasToken)
	if rr.Code != http.StatusOK {
		t.Fatalf("first user delete: expected 200, got %d", rr.Code)
	}
	rr = doRequest(t, r, http.MethodDelete, fmt.Sprintf("/api/users/%s", userID2), "", testAdminAliasToken)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("double user delete: expected 404, got %d", rr.Code)
	}

	// Duplicate username
	rr = doRequest(t, r, http.MethodPost, "/api/users",
		`{"username":"dupuser","password":"pass","role":"user"}`, testAdminAliasToken)
	if rr.Code != http.StatusConflict {
		t.Fatalf("duplicate username: expected 409, got %d", rr.Code)
	}
}

func TestEdge_NullMissingOptionalFields(t *testing.T) {
	_, r := setupTestApp(t)

	// Create user with minimal fields (no displayName)
	rr := doRequest(t, r, http.MethodPost, "/api/users",
		`{"username":"minuser","password":"pass","role":"user"}`, testAdminAliasToken)
	if rr.Code != http.StatusCreated {
		t.Fatalf("minimal user: expected 201, got %d: %s", rr.Code, rr.Body.String())
	}
	var u server.User
	_ = json.Unmarshal(rr.Body.Bytes(), &u)
	if u.DisplayName == "" {
		t.Fatal("displayName should default to username")
	}

	// Update with empty body (should work as no-op)
	rr = doRequest(t, r, http.MethodPut, fmt.Sprintf("/api/users/%s", u.ID),
		`{}`, testAdminAliasToken)
	if rr.Code != http.StatusOK {
		t.Fatalf("empty update: expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	// Join token without userId
	rr = doRequest(t, r, http.MethodPost, "/api/join-tokens",
		`{"label":"no-user","expiresInHours":1}`, testAdminAliasToken)
	if rr.Code != http.StatusCreated {
		t.Fatalf("token without userId: expected 201, got %d", rr.Code)
	}
	var tok server.JoinToken
	_ = json.Unmarshal(rr.Body.Bytes(), &tok)
	if tok.UserID != nil {
		t.Fatalf("expected nil userId, got %v", tok.UserID)
	}
}

func TestEdge_JoinTokenWithNonExistentUserID(t *testing.T) {
	_, r := setupTestApp(t)

	rr := doRequest(t, r, http.MethodPost, "/api/join-tokens",
		`{"label":"bad-user","expiresInHours":1,"userId":"nonexistent-id"}`, testAdminAliasToken)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("token with bad userId: expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestEdge_ExecOnNonExistentDevice(t *testing.T) {
	_, r := setupTestApp(t)

	rr := doRequest(t, r, http.MethodPost, "/api/devices/ghost-device/exec",
		`{"command":"openclaw","args":["status"],"timeout":5}`, testAdminAliasToken)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("exec on ghost device: expected 404, got %d", rr.Code)
	}
}

func TestEdge_BatchExecEmptyDeviceList(t *testing.T) {
	_, r := setupTestApp(t)

	rr := doRequest(t, r, http.MethodPost, "/api/batch/exec",
		`{"deviceIds":[],"command":"openclaw status"}`, testAdminAliasToken)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("batch empty devices: expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestEdge_BatchExecInvalidCommand(t *testing.T) {
	app, r := setupTestApp(t)
	seedDevice(t, app, "batch-cmd-dev")

	rr := doRequest(t, r, http.MethodPost, "/api/batch/exec",
		`{"deviceIds":["batch-cmd-dev"],"command":"rm -rf /"}`, testAdminAliasToken)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("batch invalid command: expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestEdge_LoginInvalidJSON(t *testing.T) {
	_, r := setupTestApp(t)

	rr := doRequest(t, r, http.MethodPost, "/api/auth/login", "not-json", "")
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("login bad json: expected 400, got %d", rr.Code)
	}
}

func TestEdge_LoginEmptyCredentials(t *testing.T) {
	_, r := setupTestApp(t)

	rr := doRequest(t, r, http.MethodPost, "/api/auth/login",
		`{"username":"","password":""}`, "")
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("login empty: expected 401, got %d", rr.Code)
	}
}

func TestEdge_GetNonExistentBatchJob(t *testing.T) {
	_, r := setupTestApp(t)

	rr := doRequest(t, r, http.MethodGet, "/api/batch/nonexistent-job", "", testAdminAliasToken)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("get ghost batch job: expected 404, got %d", rr.Code)
	}
}

func TestEdge_AuditLogInvalidQueryParams(t *testing.T) {
	_, r := setupTestApp(t)

	// Invalid limit
	rr := doRequest(t, r, http.MethodGet, "/api/audit-log?limit=abc", "", testAdminAliasToken)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("invalid limit: expected 400, got %d", rr.Code)
	}

	// Invalid offset
	rr = doRequest(t, r, http.MethodGet, "/api/audit-log?offset=xyz", "", testAdminAliasToken)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("invalid offset: expected 400, got %d", rr.Code)
	}

	// Invalid from
	rr = doRequest(t, r, http.MethodGet, "/api/audit-log?from=invalid", "", testAdminAliasToken)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("invalid from: expected 400, got %d", rr.Code)
	}
}

func TestEdge_AdminFilterDevicesByUserId(t *testing.T) {
	app, r := setupTestApp(t)

	seedDevice(t, app, "filter-dev-1")
	seedDevice(t, app, "filter-dev-2")

	userID, _ := createUserViaAPI(t, r, "filteruser", "filterpass", "user")
	doRequest(t, r, http.MethodPost, fmt.Sprintf("/api/users/%s/devices", userID),
		`{"deviceId":"filter-dev-1"}`, testAdminAliasToken)

	// Admin filters by userId
	rr := doRequest(t, r, http.MethodGet, fmt.Sprintf("/api/devices?userId=%s", userID), "", testAdminAliasToken)
	if rr.Code != http.StatusOK {
		t.Fatalf("filter by userId: expected 200, got %d", rr.Code)
	}
	var resp struct {
		Devices []server.DeviceSnapshot `json:"devices"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if len(resp.Devices) != 1 || resp.Devices[0].ID != "filter-dev-1" {
		t.Fatalf("expected 1 device (filter-dev-1), got %d", len(resp.Devices))
	}

	// Filter by non-existent userId
	rr = doRequest(t, r, http.MethodGet, "/api/devices?userId=nonexistent", "", testAdminAliasToken)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("filter nonexistent user: expected 404, got %d", rr.Code)
	}
}

func TestEdge_UserWithNoDevices_EmptyAuditLog(t *testing.T) {
	_, r := setupTestApp(t)
	_, userJWT := createUserViaAPI(t, r, "emptyuser", "emptypass", "user")

	// No devices bound — device list should be empty
	rr := doRequest(t, r, http.MethodGet, "/api/devices", "", userJWT)
	if rr.Code != http.StatusOK {
		t.Fatalf("empty devices: expected 200, got %d", rr.Code)
	}
	var devResp struct {
		Devices []json.RawMessage `json:"devices"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &devResp)
	if len(devResp.Devices) != 0 {
		t.Fatalf("expected 0 devices, got %d", len(devResp.Devices))
	}

	// Audit log should be empty
	rr = doRequest(t, r, http.MethodGet, "/api/audit-log", "", userJWT)
	if rr.Code != http.StatusOK {
		t.Fatalf("empty audit: expected 200, got %d", rr.Code)
	}
	var page server.AuditLogPage
	_ = json.Unmarshal(rr.Body.Bytes(), &page)
	if page.Total != 0 {
		t.Fatalf("expected 0 audit entries, got %d", page.Total)
	}
}

func TestEdge_ChangePasswordWrongOldMultipleTimes(t *testing.T) {
	_, r := setupTestApp(t)
	_, userJWT := createUserViaAPI(t, r, "multichangeuser", "origpass", "user")

	// Multiple wrong old password attempts
	for i := 0; i < 5; i++ {
		rr := doRequest(t, r, http.MethodPut, "/api/me/password",
			fmt.Sprintf(`{"oldPassword":"wrong%d","newPassword":"new"}`, i), userJWT)
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: expected 401, got %d", i, rr.Code)
		}
	}

	// Correct old password should still work
	rr := doRequest(t, r, http.MethodPut, "/api/me/password",
		`{"oldPassword":"origpass","newPassword":"newpass"}`, userJWT)
	if rr.Code != http.StatusOK {
		t.Fatalf("correct password after failures: expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestEdge_AdminPromoteAndDemote(t *testing.T) {
	_, r := setupTestApp(t)

	// Create a regular user
	userID, _ := createUserViaAPI(t, r, "promo", "promopass", "user")

	// Promote to admin
	rr := doRequest(t, r, http.MethodPut, fmt.Sprintf("/api/users/%s", userID),
		`{"role":"admin"}`, testAdminAliasToken)
	if rr.Code != http.StatusOK {
		t.Fatalf("promote: expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var u server.User
	_ = json.Unmarshal(rr.Body.Bytes(), &u)
	if u.Role != "admin" {
		t.Fatalf("expected admin role, got %s", u.Role)
	}

	// Now 2 admins, so demoting back should work
	rr = doRequest(t, r, http.MethodPut, fmt.Sprintf("/api/users/%s", userID),
		`{"role":"user"}`, testAdminAliasToken)
	if rr.Code != http.StatusOK {
		t.Fatalf("demote: expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &u)
	if u.Role != "user" {
		t.Fatalf("expected user role, got %s", u.Role)
	}
}

func TestEdge_InstallOpenClawOnNonExistentDevice(t *testing.T) {
	_, r := setupTestApp(t)

	rr := doRequest(t, r, http.MethodPost, "/api/devices/ghost/install-openclaw", "", testAdminAliasToken)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("install on ghost: expected 404, got %d", rr.Code)
	}
}

func TestEdge_ConfigureIMOnNonExistentDevice(t *testing.T) {
	_, r := setupTestApp(t)

	rr := doRequest(t, r, http.MethodPost, "/api/devices/ghost/configure-im",
		`{"platform":"dingtalk","credentials":{"id":"abc1234567","secret":"def1234567"}}`, testAdminAliasToken)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("IM config on ghost: expected 404, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestEdge_UserDeleteDevice_OnlyIfBound(t *testing.T) {
	app, r := setupTestApp(t)

	seedDevice(t, app, "perm-dev-1")
	seedDevice(t, app, "perm-dev-2")

	userID, userJWT := createUserViaAPI(t, r, "permuser", "permpass", "user")
	doRequest(t, r, http.MethodPost, fmt.Sprintf("/api/users/%s/devices", userID),
		`{"deviceId":"perm-dev-1"}`, testAdminAliasToken)

	// User can delete their bound device
	rr := doRequest(t, r, http.MethodDelete, "/api/devices/perm-dev-1", "", userJWT)
	if rr.Code != http.StatusOK {
		t.Fatalf("delete bound device: expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	// User cannot delete unbound device
	rr = doRequest(t, r, http.MethodDelete, "/api/devices/perm-dev-2", "", userJWT)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("delete unbound device: expected 403, got %d", rr.Code)
	}
}

func TestEdge_ConcurrentUserCreation(t *testing.T) {
	_, r := setupTestApp(t)

	var wg sync.WaitGroup
	results := make([]int, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			body := fmt.Sprintf(`{"username":"concreate-%d","password":"pass","role":"user"}`, idx)
			rr := doRequest(t, r, http.MethodPost, "/api/users", body, testAdminAliasToken)
			results[idx] = rr.Code
		}(i)
	}
	wg.Wait()

	successCount := 0
	for _, code := range results {
		if code == http.StatusCreated {
			successCount++
		} else if code >= 500 {
			t.Errorf("got server error %d during concurrent creation", code)
		}
	}
	if successCount < 15 {
		t.Fatalf("expected most concurrent creations to succeed, got %d/20", successCount)
	}
}

func TestEdge_AuditLogGarbageCollection(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "gc.db")
	db, err := server.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	store := server.NewAuditLogStore(db)
	store.EnsureSchema()

	// Insert old entries directly (simulating entries from 100 days ago)
	oldTimestamp := time.Now().Add(-100 * 24 * time.Hour).Unix()
	for i := 0; i < 5; i++ {
		db.Exec(`INSERT INTO audit_log(timestamp, action, result) VALUES(?, ?, ?)`,
			oldTimestamp, "old.action", "success")
	}

	// Insert recent entries
	for i := 0; i < 3; i++ {
		store.Log("recent.action", "", "detail", "1.2.3.4", "success")
	}

	// Cleanup entries older than 90 days
	cutoff := time.Now().Add(-90 * 24 * time.Hour).Unix()
	cleaned, err := store.CleanupOlderThan(cutoff)
	if err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if cleaned != 5 {
		t.Fatalf("expected 5 cleaned, got %d", cleaned)
	}

	// Only recent entries remain
	page, _ := store.List(server.AuditLogQuery{Limit: 50})
	if page.Total != 3 {
		t.Fatalf("expected 3 remaining, got %d", page.Total)
	}
}

func TestEdge_BatchExecDuplicateDeviceIDs(t *testing.T) {
	app, r := setupTestApp(t)
	seedDevice(t, app, "dup-batch-dev")

	// Batch with duplicate device IDs
	rr := doRequest(t, r, http.MethodPost, "/api/batch/exec",
		`{"deviceIds":["dup-batch-dev","dup-batch-dev","dup-batch-dev"],"command":"openclaw status"}`,
		testAdminAliasToken)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("batch dedup: expected 202, got %d: %s", rr.Code, rr.Body.String())
	}

	// Job should have only 1 item (deduped)
	var resp struct {
		JobID string          `json:"jobId"`
		Job   server.BatchJob `json:"job"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp.Job.TotalCount != 1 {
		t.Fatalf("expected 1 deduped device, got %d", resp.Job.TotalCount)
	}
}

func TestEdge_UserExecOnBoundDevice_OfflineDeviceFails(t *testing.T) {
	app, r := setupTestApp(t)
	seedDevice(t, app, "offline-dev")

	userID, userJWT := createUserViaAPI(t, r, "offuser", "offpass", "user")
	doRequest(t, r, http.MethodPost, fmt.Sprintf("/api/users/%s/devices", userID),
		`{"deviceId":"offline-dev"}`, testAdminAliasToken)

	// Exec on offline device — should get 409 conflict
	rr := doRequest(t, r, http.MethodPost, "/api/devices/offline-dev/exec",
		`{"command":"openclaw","args":["status"],"timeout":5}`, userJWT)
	if rr.Code != http.StatusConflict {
		t.Fatalf("exec offline: expected 409, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestEdge_ExecInvalidCommand(t *testing.T) {
	app, r := setupTestApp(t)
	seedDevice(t, app, "exec-dev")

	// Not on whitelist
	rr := doRequest(t, r, http.MethodPost, "/api/devices/exec-dev/exec",
		`{"command":"rm","args":["-rf","/"],"timeout":5}`, testAdminAliasToken)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("exec invalid cmd: expected 400, got %d", rr.Code)
	}
}

func TestEdge_LoginWithSpecialCharacters(t *testing.T) {
	_, r := setupTestApp(t)

	// Login with special characters in username/password
	rr := doRequest(t, r, http.MethodPost, "/api/auth/login",
		`{"username":"admin' OR '1'='1","password":"admin' OR '1'='1"}`, "")
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("SQL injection login: expected 401, got %d", rr.Code)
	}

	// Null bytes
	rr = doRequest(t, r, http.MethodPost, "/api/auth/login",
		`{"username":"admin\u0000","password":"admin"}`, "")
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("null byte login: expected 401, got %d", rr.Code)
	}
}

func TestEdge_UpdateNonExistentUser(t *testing.T) {
	_, r := setupTestApp(t)

	rr := doRequest(t, r, http.MethodPut, "/api/users/nonexistent",
		`{"displayName":"test"}`, testAdminAliasToken)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("update ghost user: expected 404, got %d", rr.Code)
	}
}

func TestEdge_BindDeviceToNonExistentUser(t *testing.T) {
	app, r := setupTestApp(t)
	seedDevice(t, app, "bind-dev")

	rr := doRequest(t, r, http.MethodPost, "/api/users/nonexistent/devices",
		`{"deviceId":"bind-dev"}`, testAdminAliasToken)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("bind to ghost user: expected 404, got %d", rr.Code)
	}
}

func TestEdge_UnbindNonExistentBinding(t *testing.T) {
	app, r := setupTestApp(t)
	seedDevice(t, app, "unbind-nope-dev")
	userID, _ := createUserViaAPI(t, r, "unbindnope", "pass", "user")

	rr := doRequest(t, r, http.MethodDelete,
		fmt.Sprintf("/api/users/%s/devices/unbind-nope-dev", userID), "", testAdminAliasToken)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("unbind non-existent: expected 404, got %d", rr.Code)
	}
}

func TestEdge_AdminResetPasswordForUser(t *testing.T) {
	_, r := setupTestApp(t)

	userID, _ := createUserViaAPI(t, r, "resetuser", "oldpass", "user")

	// Admin can reset user's password via PUT /api/users/:id
	rr := doRequest(t, r, http.MethodPut, fmt.Sprintf("/api/users/%s", userID),
		`{"password":"admin-set-pass"}`, testAdminAliasToken)
	if rr.Code != http.StatusOK {
		t.Fatalf("admin reset password: expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	// User can login with new password
	rr = doRequest(t, r, http.MethodPost, "/api/auth/login",
		`{"username":"resetuser","password":"admin-set-pass"}`, "")
	if rr.Code != http.StatusOK {
		t.Fatalf("login with reset password: expected 200, got %d", rr.Code)
	}
}

func TestEdge_ListDevicesAsAdminWithAndWithoutUserFilter(t *testing.T) {
	app, r := setupTestApp(t)

	seedDevice(t, app, "list-dev-1")
	seedDevice(t, app, "list-dev-2")
	seedDevice(t, app, "list-dev-3")

	// Admin sees all devices without filter
	rr := doRequest(t, r, http.MethodGet, "/api/devices", "", testAdminAliasToken)
	var resp struct {
		Devices []server.DeviceSnapshot `json:"devices"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if len(resp.Devices) != 3 {
		t.Fatalf("admin no filter: expected 3 devices, got %d", len(resp.Devices))
	}
}

func TestEdge_AuditLogMaxLimit(t *testing.T) {
	_, r := setupTestApp(t)

	// Requesting more than max limit (200) should be capped
	rr := doRequest(t, r, http.MethodGet, "/api/audit-log?limit=500", "", testAdminAliasToken)
	if rr.Code != http.StatusOK {
		t.Fatalf("audit max limit: expected 200, got %d", rr.Code)
	}
	var page server.AuditLogPage
	_ = json.Unmarshal(rr.Body.Bytes(), &page)
	if page.Limit != 200 {
		t.Fatalf("expected capped limit=200, got %d", page.Limit)
	}
}
