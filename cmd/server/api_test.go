package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/raystone-ai/clawmini/internal/protocol"
	"github.com/raystone-ai/clawmini/internal/server"
)

func setupTestApp(t *testing.T) (*serverApp, *chi.Mux) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := server.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	devices := server.NewDeviceStore(db)
	if err := devices.EnsureSchema(); err != nil {
		t.Fatalf("ensure device schema: %v", err)
	}
	commands := server.NewCommandStore(db)
	if err := commands.EnsureSchema(); err != nil {
		t.Fatalf("ensure command schema: %v", err)
	}
	joinTokens := server.NewJoinTokenStore(db)
	if err := joinTokens.EnsureSchema(); err != nil {
		t.Fatalf("ensure join token schema: %v", err)
	}
	auth := &server.TokenAuth{AdminToken: "test-admin-token", DeviceToken: "test-device-token"}
	hub := server.NewHub(devices, commands, joinTokens, auth)
	imConfigs := newConfigureIMJobStore(db)
	if err := imConfigs.EnsureSchema(); err != nil {
		t.Fatalf("ensure im config jobs schema: %v", err)
	}
	imConfigs.Start()
	t.Cleanup(imConfigs.Stop)

	app := &serverApp{
		auth:       auth,
		devices:    devices,
		commands:   commands,
		joinTokens: joinTokens,
		hub:        hub,
		imConfigs:  imConfigs,
	}

	r := chi.NewRouter()
	r.Route("/api", func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Use(auth.AdminMiddleware)
			r.Get("/devices", app.handleListDevices)
			r.Get("/devices/{id}", app.handleGetDevice)
			r.Delete("/devices/{id}", app.handleDeleteDevice)
			r.Post("/devices/{id}/exec", app.handleExec)
			r.Get("/devices/{id}/exec/{cmdId}", app.handleGetCommand)
			r.Post("/devices/{id}/configure-im", app.handleConfigureIM)
			r.Get("/devices/{id}/configure-im/{jobId}", app.handleGetConfigureIM)
			r.Post("/join-tokens", app.handleCreateJoinToken)
			r.Get("/join-tokens", app.handleListJoinTokens)
			r.Delete("/join-tokens/{id}", app.handleDeleteJoinToken)
		})
	})
	r.Get("/install.sh", app.handleInstallScript)

	return app, r
}

func doRequest(t *testing.T, r http.Handler, method, path, body, token string) *httptest.ResponseRecorder {
	t.Helper()
	var bodyReader *strings.Reader
	if body != "" {
		bodyReader = strings.NewReader(body)
	} else {
		bodyReader = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, bodyReader)
	if token != "" {
		req.Header.Set("X-Admin-Token", token)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	return rr
}

// --- Join Token API Tests ---

func TestCreateJoinTokenAPI_Success(t *testing.T) {
	_, r := setupTestApp(t)

	rr := doRequest(t, r, http.MethodPost, "/api/join-tokens",
		`{"label":"测试设备","expiresInHours":24}`, "test-admin-token")

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}

	var tok server.JoinToken
	if err := json.Unmarshal(rr.Body.Bytes(), &tok); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if tok.ID == "" || len(tok.ID) != 32 {
		t.Fatalf("invalid token id: %q", tok.ID)
	}
	if tok.Label != "测试设备" {
		t.Fatalf("label mismatch: %q", tok.Label)
	}
	if tok.ExpiresAt <= tok.CreatedAt {
		t.Fatalf("expiresAt should be > createdAt")
	}
	if tok.UsedAt != nil {
		t.Fatalf("new token should have nil usedAt")
	}
}

func TestCreateJoinTokenAPI_EmptyLabel(t *testing.T) {
	_, r := setupTestApp(t)

	rr := doRequest(t, r, http.MethodPost, "/api/join-tokens",
		`{"label":"","expiresInHours":1}`, "test-admin-token")

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201 for empty label, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestCreateJoinTokenAPI_ZeroExpiry(t *testing.T) {
	_, r := setupTestApp(t)

	rr := doRequest(t, r, http.MethodPost, "/api/join-tokens",
		`{"label":"test","expiresInHours":0}`, "test-admin-token")

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for zero expiresInHours, got %d", rr.Code)
	}
}

func TestCreateJoinTokenAPI_NegativeExpiry(t *testing.T) {
	_, r := setupTestApp(t)

	rr := doRequest(t, r, http.MethodPost, "/api/join-tokens",
		`{"label":"test","expiresInHours":-5}`, "test-admin-token")

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for negative expiresInHours, got %d", rr.Code)
	}
}

func TestCreateJoinTokenAPI_InvalidJSON(t *testing.T) {
	_, r := setupTestApp(t)

	rr := doRequest(t, r, http.MethodPost, "/api/join-tokens",
		`not valid json`, "test-admin-token")

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid JSON, got %d", rr.Code)
	}
}

func TestCreateJoinTokenAPI_Unauthorized(t *testing.T) {
	_, r := setupTestApp(t)

	rr := doRequest(t, r, http.MethodPost, "/api/join-tokens",
		`{"label":"test","expiresInHours":1}`, "wrong-token")

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestCreateJoinTokenAPI_NoToken(t *testing.T) {
	_, r := setupTestApp(t)

	rr := doRequest(t, r, http.MethodPost, "/api/join-tokens",
		`{"label":"test","expiresInHours":1}`, "")

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without token, got %d", rr.Code)
	}
}

func TestListJoinTokensAPI_Empty(t *testing.T) {
	_, r := setupTestApp(t)

	rr := doRequest(t, r, http.MethodGet, "/api/join-tokens", "", "test-admin-token")

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var resp struct {
		Tokens []server.JoinToken `json:"tokens"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Tokens) != 0 {
		t.Fatalf("expected 0 tokens, got %d", len(resp.Tokens))
	}
}

func TestListJoinTokensAPI_WithTokens(t *testing.T) {
	_, r := setupTestApp(t)

	// Create two tokens
	doRequest(t, r, http.MethodPost, "/api/join-tokens",
		`{"label":"first","expiresInHours":1}`, "test-admin-token")
	doRequest(t, r, http.MethodPost, "/api/join-tokens",
		`{"label":"second","expiresInHours":24}`, "test-admin-token")

	rr := doRequest(t, r, http.MethodGet, "/api/join-tokens", "", "test-admin-token")

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var resp struct {
		Tokens []server.JoinToken `json:"tokens"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Tokens) != 2 {
		t.Fatalf("expected 2 tokens, got %d", len(resp.Tokens))
	}
}

func TestListJoinTokensAPI_Unauthorized(t *testing.T) {
	_, r := setupTestApp(t)

	rr := doRequest(t, r, http.MethodGet, "/api/join-tokens", "", "bad-token")

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestDeleteJoinTokenAPI_Success(t *testing.T) {
	_, r := setupTestApp(t)

	// Create a token
	rr := doRequest(t, r, http.MethodPost, "/api/join-tokens",
		`{"label":"to-delete","expiresInHours":1}`, "test-admin-token")
	var tok server.JoinToken
	if err := json.Unmarshal(rr.Body.Bytes(), &tok); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// Delete it
	rr = doRequest(t, r, http.MethodDelete, "/api/join-tokens/"+tok.ID, "", "test-admin-token")
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	// Verify deleted
	rr = doRequest(t, r, http.MethodGet, "/api/join-tokens", "", "test-admin-token")
	var resp struct {
		Tokens []server.JoinToken `json:"tokens"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Tokens) != 0 {
		t.Fatalf("expected 0 tokens after delete, got %d", len(resp.Tokens))
	}
}

func TestDeleteJoinTokenAPI_NotFound(t *testing.T) {
	_, r := setupTestApp(t)

	rr := doRequest(t, r, http.MethodDelete, "/api/join-tokens/non-existent-id", "", "test-admin-token")
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestDeleteJoinTokenAPI_Unauthorized(t *testing.T) {
	_, r := setupTestApp(t)

	rr := doRequest(t, r, http.MethodDelete, "/api/join-tokens/any-id", "", "wrong")
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

// --- Device Deletion API Tests ---

func TestDeleteDeviceAPI_Success(t *testing.T) {
	app, r := setupTestApp(t)

	// Create a device directly
	if err := app.devices.UpsertDevice(protocol.RegisterPayload{
		DeviceID:      "dev-del-1",
		Hostname:      "host-a",
		OS:            "linux",
		Arch:          "amd64",
		ClientVersion: "0.1.0",
	}); err != nil {
		t.Fatalf("seed device: %v", err)
	}

	rr := doRequest(t, r, http.MethodDelete, "/api/devices/dev-del-1", "", "test-admin-token")
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp map[string]bool
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp["ok"] {
		t.Fatalf("expected ok=true")
	}

	// Verify device is gone
	rr = doRequest(t, r, http.MethodGet, "/api/devices/dev-del-1", "", "test-admin-token")
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404 after delete, got %d", rr.Code)
	}
}

func TestDeleteDeviceAPI_NotFound(t *testing.T) {
	_, r := setupTestApp(t)

	rr := doRequest(t, r, http.MethodDelete, "/api/devices/non-existent", "", "test-admin-token")
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestDeleteDeviceAPI_Unauthorized(t *testing.T) {
	_, r := setupTestApp(t)

	rr := doRequest(t, r, http.MethodDelete, "/api/devices/any", "", "wrong-token")
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestDeleteDeviceAPI_CleansUpCommands(t *testing.T) {
	app, r := setupTestApp(t)

	// Create device and command
	if err := app.devices.UpsertDevice(protocol.RegisterPayload{
		DeviceID:      "dev-cmd-clean",
		Hostname:      "host-a",
		OS:            "linux",
		Arch:          "amd64",
		ClientVersion: "0.1.0",
	}); err != nil {
		t.Fatalf("seed device: %v", err)
	}

	cmd, err := app.commands.Create("dev-cmd-clean", "openclaw", []string{"status"}, 60)
	if err != nil {
		t.Fatalf("create command: %v", err)
	}

	// Delete device
	rr := doRequest(t, r, http.MethodDelete, "/api/devices/dev-cmd-clean", "", "test-admin-token")
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	// Command should be gone
	_, err = app.commands.GetByDeviceAndID("dev-cmd-clean", cmd.ID)
	if err == nil {
		t.Fatalf("expected command to be deleted with device")
	}
}

// --- Install Script Tests ---

func TestInstallScript_MissingToken(t *testing.T) {
	_, r := setupTestApp(t)

	rr := doRequest(t, r, http.MethodGet, "/install.sh", "", "")
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing token, got %d", rr.Code)
	}
}

func TestInstallScript_EmptyToken(t *testing.T) {
	_, r := setupTestApp(t)

	rr := doRequest(t, r, http.MethodGet, "/install.sh?token=", "", "")
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty token, got %d", rr.Code)
	}
}

func TestInstallScript_ValidToken(t *testing.T) {
	_, r := setupTestApp(t)

	req := httptest.NewRequest(http.MethodGet, "/install.sh?token=abc123def456", nil)
	req.Host = "example.com:18790"
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	contentType := rr.Header().Get("Content-Type")
	if !strings.Contains(contentType, "text/plain") {
		t.Fatalf("expected text/plain content type, got %q", contentType)
	}

	body := rr.Body.String()
	if !strings.Contains(body, "#!/usr/bin/env bash") {
		t.Fatalf("script should start with bash shebang")
	}
	if !strings.Contains(body, "abc123def456") {
		t.Fatalf("script should contain the token")
	}
	if !strings.Contains(body, "ws://example.com:18790/ws") {
		t.Fatalf("script should contain websocket server URL, got: %s", body)
	}
	if !strings.Contains(body, "clawmini-client") {
		t.Fatalf("script should reference clawmini-client")
	}
	if !strings.Contains(body, "systemctl") {
		t.Fatalf("script should use systemctl")
	}
}

func TestInstallScript_HTTPSForwarded(t *testing.T) {
	_, r := setupTestApp(t)

	req := httptest.NewRequest(http.MethodGet, "/install.sh?token=mytoken", nil)
	req.Host = "secure.example.com"
	req.Header.Set("X-Forwarded-Proto", "https")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	body := rr.Body.String()
	if !strings.Contains(body, "wss://secure.example.com/ws") {
		t.Fatalf("expected wss:// scheme with X-Forwarded-Proto: https, got: %s", body)
	}
}

// --- Build Install Script Unit Tests ---

func TestBuildInstallScript_Content(t *testing.T) {
	script := buildInstallScript("ws://localhost:18790/ws", "test-token-123")

	checks := []string{
		"#!/usr/bin/env bash",
		"set -euo pipefail",
		"detect_os()",
		"detect_arch()",
		"test-token-123",
		"ws://localhost:18790/ws",
		"clawmini-client",
		"systemctl daemon-reload",
		"systemctl enable",
		"systemctl restart",
	}
	for _, check := range checks {
		if !strings.Contains(script, check) {
			t.Errorf("script missing %q", check)
		}
	}
}

func TestShellSingleQuote(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"hello", "hello"},
		{"it's", `it'"'"'s`},
		{"a'b'c", `a'"'"'b'"'"'c`},
		{"no-quotes", "no-quotes"},
	}
	for _, tc := range cases {
		got := shellSingleQuote(tc.in)
		if got != tc.want {
			t.Errorf("shellSingleQuote(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestWebsocketServerURL(t *testing.T) {
	cases := []struct {
		name    string
		host    string
		proto   string
		wantURL string
	}{
		{"plain http", "myhost:18790", "", "ws://myhost:18790/ws"},
		{"https forwarded", "myhost:443", "https", "wss://myhost:443/ws"},
		{"http forwarded", "myhost:80", "http", "ws://myhost:80/ws"},
		{"empty host", "", "", "ws://localhost:18790/ws"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/install.sh", nil)
			req.Host = tc.host
			if tc.proto != "" {
				req.Header.Set("X-Forwarded-Proto", tc.proto)
			}
			got := websocketServerURL(req)
			if got != tc.wantURL {
				t.Fatalf("websocketServerURL() = %q, want %q", got, tc.wantURL)
			}
		})
	}
}

func TestRequestScheme(t *testing.T) {
	cases := []struct {
		name  string
		proto string
		want  string
	}{
		{"no header", "", "http"},
		{"https forwarded", "https", "https"},
		{"http forwarded", "http", "http"},
		{"invalid forwarded", "ftp", "http"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tc.proto != "" {
				req.Header.Set("X-Forwarded-Proto", tc.proto)
			}
			got := requestScheme(req)
			if got != tc.want {
				t.Fatalf("requestScheme() = %q, want %q", got, tc.want)
			}
		})
	}
}

// --- Device List API Tests ---

func TestListDevicesAPI_Empty(t *testing.T) {
	_, r := setupTestApp(t)

	rr := doRequest(t, r, http.MethodGet, "/api/devices", "", "test-admin-token")
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var resp struct {
		Devices []json.RawMessage `json:"devices"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Devices) != 0 {
		t.Fatalf("expected 0 devices, got %d", len(resp.Devices))
	}
}

func TestGetDeviceAPI_NotFound(t *testing.T) {
	_, r := setupTestApp(t)

	rr := doRequest(t, r, http.MethodGet, "/api/devices/missing", "", "test-admin-token")
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

// --- Login API Test ---

func TestLoginHandler_ValidToken(t *testing.T) {
	app, _ := setupTestApp(t)
	loginR := chi.NewRouter()
	loginR.Post("/api/auth/login", app.handleLogin)

	rr := doRequest(t, loginR, http.MethodPost, "/api/auth/login",
		`{"token":"test-admin-token"}`, "")
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestLoginHandler_InvalidToken(t *testing.T) {
	app, _ := setupTestApp(t)
	loginR := chi.NewRouter()
	loginR.Post("/api/auth/login", app.handleLogin)

	rr := doRequest(t, loginR, http.MethodPost, "/api/auth/login",
		`{"token":"wrong-token"}`, "")
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

// --- Install Script Token Injection Prevention ---

func TestInstallScript_SpecialCharsInToken(t *testing.T) {
	script := buildInstallScript("ws://localhost/ws", "token'with;special$(chars)")

	// The shellSingleQuote function should prevent injection
	if strings.Contains(script, "token'with") {
		t.Fatalf("unescaped single quote in token — potential injection")
	}
}

// --- Double Delete Token ---

func TestDeleteJoinTokenAPI_DoubleDelete(t *testing.T) {
	_, r := setupTestApp(t)

	// Create a token
	rr := doRequest(t, r, http.MethodPost, "/api/join-tokens",
		`{"label":"double-del","expiresInHours":1}`, "test-admin-token")
	var tok server.JoinToken
	if err := json.Unmarshal(rr.Body.Bytes(), &tok); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// First delete — success
	rr = doRequest(t, r, http.MethodDelete, "/api/join-tokens/"+tok.ID, "", "test-admin-token")
	if rr.Code != http.StatusOK {
		t.Fatalf("first delete expected 200, got %d", rr.Code)
	}

	// Second delete — not found
	rr = doRequest(t, r, http.MethodDelete, "/api/join-tokens/"+tok.ID, "", "test-admin-token")
	if rr.Code != http.StatusNotFound {
		t.Fatalf("second delete expected 404, got %d", rr.Code)
	}
}

// --- Double Delete Device ---

func TestDeleteDeviceAPI_DoubleDelete(t *testing.T) {
	app, r := setupTestApp(t)

	if err := app.devices.UpsertDevice(protocol.RegisterPayload{
		DeviceID:      "dev-double-del",
		Hostname:      "host-a",
		OS:            "linux",
		Arch:          "amd64",
		ClientVersion: "0.1.0",
	}); err != nil {
		t.Fatalf("seed device: %v", err)
	}

	// First delete — success
	rr := doRequest(t, r, http.MethodDelete, "/api/devices/dev-double-del", "", "test-admin-token")
	if rr.Code != http.StatusOK {
		t.Fatalf("first delete expected 200, got %d", rr.Code)
	}

	// Second delete — not found
	rr = doRequest(t, r, http.MethodDelete, "/api/devices/dev-double-del", "", "test-admin-token")
	if rr.Code != http.StatusNotFound {
		t.Fatalf("second delete expected 404, got %d", rr.Code)
	}
}

// --- Create Multiple Tokens Via API ---

func TestCreateMultipleTokensAPI(t *testing.T) {
	_, r := setupTestApp(t)

	ids := make(map[string]bool)
	for i := 0; i < 5; i++ {
		rr := doRequest(t, r, http.MethodPost, "/api/join-tokens",
			fmt.Sprintf(`{"label":"tok-%d","expiresInHours":%d}`, i, i+1), "test-admin-token")
		if rr.Code != http.StatusCreated {
			t.Fatalf("token %d: expected 201, got %d", i, rr.Code)
		}

		var tok server.JoinToken
		if err := json.Unmarshal(rr.Body.Bytes(), &tok); err != nil {
			t.Fatalf("decode token %d: %v", i, err)
		}
		if ids[tok.ID] {
			t.Fatalf("duplicate token ID from API: %s", tok.ID)
		}
		ids[tok.ID] = true
	}

	rr := doRequest(t, r, http.MethodGet, "/api/join-tokens", "", "test-admin-token")
	var resp struct {
		Tokens []server.JoinToken `json:"tokens"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(resp.Tokens) != 5 {
		t.Fatalf("expected 5 tokens, got %d", len(resp.Tokens))
	}
}

// --- Various expiry durations ---

func TestCreateJoinToken_VariousExpiryDurations(t *testing.T) {
	_, r := setupTestApp(t)

	durations := []int{1, 6, 24, 168} // 1h, 6h, 24h, 7d
	for _, h := range durations {
		rr := doRequest(t, r, http.MethodPost, "/api/join-tokens",
			fmt.Sprintf(`{"label":"exp-%dh","expiresInHours":%d}`, h, h), "test-admin-token")
		if rr.Code != http.StatusCreated {
			t.Fatalf("expiry %dh: expected 201, got %d", h, rr.Code)
		}

		var tok server.JoinToken
		if err := json.Unmarshal(rr.Body.Bytes(), &tok); err != nil {
			t.Fatalf("decode: %v", err)
		}

		expectedDelta := int64(h) * 3600
		actualDelta := tok.ExpiresAt - tok.CreatedAt
		if actualDelta < expectedDelta-2 || actualDelta > expectedDelta+2 {
			t.Fatalf("expiry %dh: expiresAt-createdAt = %d, expected ~%d", h, actualDelta, expectedDelta)
		}
	}
}

// --- Token label trimming ---

func TestCreateJoinTokenAPI_LabelTrimmed(t *testing.T) {
	_, r := setupTestApp(t)

	rr := doRequest(t, r, http.MethodPost, "/api/join-tokens",
		`{"label":"  spaces  ","expiresInHours":1}`, "test-admin-token")

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", rr.Code)
	}

	var tok server.JoinToken
	if err := json.Unmarshal(rr.Body.Bytes(), &tok); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if tok.Label != "spaces" {
		t.Fatalf("expected trimmed label 'spaces', got %q", tok.Label)
	}
}

// --- Missing body for create token ---

func TestCreateJoinTokenAPI_EmptyBody(t *testing.T) {
	_, r := setupTestApp(t)

	rr := doRequest(t, r, http.MethodPost, "/api/join-tokens", "", "test-admin-token")
	// Empty body should be treated as bad request (EOF on decode)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty body, got %d", rr.Code)
	}
}

// --- Install Script: whitespace-only token ---

func TestInstallScript_WhitespaceToken(t *testing.T) {
	_, r := setupTestApp(t)

	rr := doRequest(t, r, http.MethodGet, "/install.sh?token=+", "", "")
	// After trimming " " is empty
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for whitespace-only token, got %d", rr.Code)
	}
}

func TestConfigureIMAPI_InvalidPlatform(t *testing.T) {
	app, r := setupTestApp(t)
	if err := app.devices.UpsertDevice(protocol.RegisterPayload{
		DeviceID:      "dev-im-invalid",
		Hostname:      "host-im",
		OS:            "linux",
		Arch:          "amd64",
		ClientVersion: "0.1.0",
	}); err != nil {
		t.Fatalf("seed device: %v", err)
	}

	rr := doRequest(
		t,
		r,
		http.MethodPost,
		"/api/devices/dev-im-invalid/configure-im",
		`{"platform":"wechat","credentials":{"id":"abc1234567","secret":"def1234567"}}`,
		"test-admin-token",
	)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestConfigureIMAPI_StartAndPollJob(t *testing.T) {
	app, r := setupTestApp(t)
	if err := app.devices.UpsertDevice(protocol.RegisterPayload{
		DeviceID:      "dev-im-job",
		Hostname:      "host-im",
		OS:            "linux",
		Arch:          "amd64",
		ClientVersion: "0.1.0",
	}); err != nil {
		t.Fatalf("seed device: %v", err)
	}

	rr := doRequest(
		t,
		r,
		http.MethodPost,
		"/api/devices/dev-im-job/configure-im",
		`{"platform":"dingtalk","credentials":{"id":"abc1234567","secret":"def1234567"}}`,
		"test-admin-token",
	)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rr.Code, rr.Body.String())
	}

	var created configureIMResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if created.ID == "" {
		t.Fatalf("expected job id")
	}
	if len(created.Steps) != 5 {
		t.Fatalf("expected 5 steps for dingtalk, got %d", len(created.Steps))
	}

	var latest configureIMResponse
	deadline := time.Now().Add(4 * time.Second)
	for {
		getResp := doRequest(
			t,
			r,
			http.MethodGet,
			"/api/devices/dev-im-job/configure-im/"+created.ID,
			"",
			"test-admin-token",
		)
		if getResp.Code != http.StatusOK {
			t.Fatalf("expected 200 when polling, got %d: %s", getResp.Code, getResp.Body.String())
		}
		if err := json.Unmarshal(getResp.Body.Bytes(), &latest); err != nil {
			t.Fatalf("decode poll response: %v", err)
		}
		if latest.Status == "success" || latest.Status == "failed" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("configure-im job did not complete in time, latest status=%q", latest.Status)
		}
		time.Sleep(100 * time.Millisecond)
	}

	if latest.Status != "failed" {
		t.Fatalf("expected offline job to fail, got status=%q", latest.Status)
	}
	if latest.Steps[0].Status != "failed" {
		t.Fatalf("expected first step to fail for offline device, got %q", latest.Steps[0].Status)
	}
}

// --- Rate limiter expiry test ---

func TestLoginRateLimiter_WindowExpiry(t *testing.T) {
	limiter := newLoginRateLimiter(1, 50*time.Millisecond)
	ip := "10.0.0.1"

	if !limiter.allow(ip) {
		t.Fatalf("first attempt should pass")
	}
	if limiter.allow(ip) {
		t.Fatalf("second attempt should be blocked")
	}

	time.Sleep(60 * time.Millisecond)

	if !limiter.allow(ip) {
		t.Fatalf("attempt after window expiry should pass")
	}
}
