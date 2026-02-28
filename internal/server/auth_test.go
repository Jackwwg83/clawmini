package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewTokenAuthFromEnv_DefaultsAndOverrides(t *testing.T) {
	t.Setenv("CLAWMINI_ADMIN_TOKEN", "")
	t.Setenv("CLAWMINI_DEVICE_TOKEN", "")
	if _, err := NewTokenAuthFromEnv(); err == nil {
		t.Fatalf("expected missing env tokens to fail")
	}

	t.Setenv("CLAWMINI_ADMIN_TOKEN", "admin-override")
	t.Setenv("CLAWMINI_DEVICE_TOKEN", "device-override")
	a, err := NewTokenAuthFromEnv()
	if err != nil {
		t.Fatalf("unexpected env parse error: %v", err)
	}
	if a.AdminToken != "admin-override" || a.DeviceToken != "device-override" {
		t.Fatalf("override tokens not applied: %+v", a)
	}
}

func TestExtractToken_Precedence(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/devices?token=query-token", nil)
	req.Header.Set("Authorization", "Bearer bearer-token")
	req.Header.Set("X-Admin-Token", "header-token")
	if got := ExtractToken(req); got != "bearer-token" {
		t.Fatalf("unexpected token precedence result: %q", got)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/devices?token=query-token", nil)
	req.Header.Set("X-Admin-Token", "header-token")
	if got := ExtractToken(req); got != "header-token" {
		t.Fatalf("expected X-Admin-Token, got %q", got)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/devices?token=query-token", nil)
	if got := ExtractToken(req); got != "" {
		t.Fatalf("expected no query token support, got %q", got)
	}
}

func TestAdminMiddleware_ValidatesToken(t *testing.T) {
	auth := &TokenAuth{AdminToken: "admin-secret", DeviceToken: "device-secret"}

	t.Run("unauthorized request", func(t *testing.T) {
		called := false
		next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			called = true
		})
		h := auth.AdminMiddleware(next)

		req := httptest.NewRequest(http.MethodGet, "/api/devices", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status mismatch: got %d want %d", rec.Code, http.StatusUnauthorized)
		}
		if called {
			t.Fatalf("next handler should not be called")
		}

		var body map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if body["error"] != "unauthorized" {
			t.Fatalf("unexpected error body: %+v", body)
		}
	})

	t.Run("authorized request", func(t *testing.T) {
		called := false
		next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			called = true
			w.WriteHeader(http.StatusNoContent)
		})
		h := auth.AdminMiddleware(next)

		req := httptest.NewRequest(http.MethodGet, "/api/devices", nil)
		req.Header.Set("Authorization", "Bearer admin-secret")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusNoContent {
			t.Fatalf("status mismatch: got %d want %d", rec.Code, http.StatusNoContent)
		}
		if !called {
			t.Fatalf("next handler should be called")
		}
	})
}
