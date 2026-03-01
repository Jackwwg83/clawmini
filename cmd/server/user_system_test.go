package main

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/raystone-ai/clawmini/internal/protocol"
	"github.com/raystone-ai/clawmini/internal/server"
)

func issueUserTokenForTest(t *testing.T, app *serverApp, username, password string) string {
	t.Helper()
	user, err := app.users.Authenticate(username, password)
	if err != nil {
		t.Fatalf("authenticate %s: %v", username, err)
	}
	token, err := app.auth.GenerateToken(user)
	if err != nil {
		t.Fatalf("generate token for %s: %v", username, err)
	}
	return token
}

func TestUserRBAC_DeviceFilteringAndAccess(t *testing.T) {
	app, r := setupTestApp(t)

	if _, err := app.users.CreateUser("viewer", "viewer-pass", server.RoleUser, "Viewer"); err != nil {
		t.Fatalf("create viewer user: %v", err)
	}
	viewerToken := issueUserTokenForTest(t, app, "viewer", "viewer-pass")

	for _, deviceID := range []string{"dev-owned", "dev-other"} {
		if err := app.devices.UpsertDevice(protocol.RegisterPayload{
			DeviceID:      deviceID,
			Hostname:      deviceID,
			OS:            "linux",
			Arch:          "amd64",
			ClientVersion: "0.1.0",
		}); err != nil {
			t.Fatalf("seed device %s: %v", deviceID, err)
		}
	}
	viewer, err := app.users.Authenticate("viewer", "viewer-pass")
	if err != nil {
		t.Fatalf("authenticate viewer: %v", err)
	}
	if err := app.users.BindDevice(viewer.ID, "dev-owned"); err != nil {
		t.Fatalf("bind viewer device: %v", err)
	}

	rr := doRequest(t, r, http.MethodGet, "/api/devices", "", viewerToken)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for user list devices, got %d: %s", rr.Code, rr.Body.String())
	}
	var listResp struct {
		Devices []server.DeviceSnapshot `json:"devices"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("decode devices list: %v", err)
	}
	if len(listResp.Devices) != 1 || listResp.Devices[0].ID != "dev-owned" {
		t.Fatalf("expected only owned device, got %+v", listResp.Devices)
	}

	rr = doRequest(t, r, http.MethodGet, "/api/devices/dev-other", "", viewerToken)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for unbound device detail, got %d", rr.Code)
	}

	rr = doRequest(t, r, http.MethodPost, "/api/devices/dev-owned/exec", `{"command":"openclaw","args":["status"]}`, viewerToken)
	if rr.Code != http.StatusConflict {
		t.Fatalf("expected 409 for owned offline exec, got %d: %s", rr.Code, rr.Body.String())
	}

	rr = doRequest(t, r, http.MethodPost, "/api/devices/dev-other/exec", `{"command":"openclaw","args":["status"]}`, viewerToken)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for unbound exec, got %d", rr.Code)
	}
}

func TestAdminUserCRUDAndBindingAPI(t *testing.T) {
	app, r := setupTestApp(t)

	if err := app.devices.UpsertDevice(protocol.RegisterPayload{
		DeviceID:      "dev-bind-api",
		Hostname:      "bind-host",
		OS:            "linux",
		Arch:          "amd64",
		ClientVersion: "0.1.0",
	}); err != nil {
		t.Fatalf("seed device: %v", err)
	}

	rr := doRequest(t, r, http.MethodPost, "/api/users", `{"username":"ops","password":"ops-pass","role":"user","displayName":"Ops"}`, testAdminAliasToken)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create user expected 201, got %d: %s", rr.Code, rr.Body.String())
	}
	var created server.User
	if err := json.Unmarshal(rr.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created user: %v", err)
	}
	if created.Username != "ops" {
		t.Fatalf("expected username ops, got %q", created.Username)
	}

	rr = doRequest(t, r, http.MethodPut, "/api/users/"+created.ID, `{"displayName":"Ops Team","role":"user","password":"ops-pass-2"}`, testAdminAliasToken)
	if rr.Code != http.StatusOK {
		t.Fatalf("update user expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	rr = doRequest(t, r, http.MethodPost, "/api/users/"+created.ID+"/devices", `{"deviceId":"dev-bind-api"}`, testAdminAliasToken)
	if rr.Code != http.StatusOK {
		t.Fatalf("bind device expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	rr = doRequest(t, r, http.MethodGet, "/api/users/"+created.ID, "", testAdminAliasToken)
	if rr.Code != http.StatusOK {
		t.Fatalf("get user expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var detail struct {
		User    server.User             `json:"user"`
		Devices []server.DeviceSnapshot `json:"devices"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &detail); err != nil {
		t.Fatalf("decode user detail: %v", err)
	}
	if len(detail.Devices) != 1 || detail.Devices[0].ID != "dev-bind-api" {
		t.Fatalf("expected bound device in detail, got %+v", detail.Devices)
	}

	rr = doRequest(t, r, http.MethodDelete, "/api/users/"+created.ID+"/devices/dev-bind-api", "", testAdminAliasToken)
	if rr.Code != http.StatusOK {
		t.Fatalf("unbind device expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	rr = doRequest(t, r, http.MethodDelete, "/api/users/"+created.ID, "", testAdminAliasToken)
	if rr.Code != http.StatusOK {
		t.Fatalf("delete user expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestNonAdminCannotAccessAdminUserAPIs(t *testing.T) {
	app, r := setupTestApp(t)
	if _, err := app.users.CreateUser("member", "member-pass", server.RoleUser, "Member"); err != nil {
		t.Fatalf("create member user: %v", err)
	}
	memberToken := issueUserTokenForTest(t, app, "member", "member-pass")

	rr := doRequest(t, r, http.MethodGet, "/api/users", "", memberToken)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-admin users endpoint, got %d", rr.Code)
	}
}

func TestJoinTokenAPI_CreateWithUserID(t *testing.T) {
	app, r := setupTestApp(t)
	created, err := app.users.CreateUser("join-user", "join-pass", server.RoleUser, "Join User")
	if err != nil {
		t.Fatalf("create join user: %v", err)
	}

	body := `{"label":"bound-token","expiresInHours":1,"userId":"` + created.ID + `"}`
	rr := doRequest(t, r, http.MethodPost, "/api/join-tokens", body, testAdminAliasToken)
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}
	var token server.JoinToken
	if err := json.Unmarshal(rr.Body.Bytes(), &token); err != nil {
		t.Fatalf("decode join token: %v", err)
	}
	if token.UserID == nil || *token.UserID != created.ID {
		t.Fatalf("expected userId on token, got %+v", token.UserID)
	}
}
