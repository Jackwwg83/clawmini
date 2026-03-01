package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func setupAuthForTest(t *testing.T) (*TokenAuth, string, string) {
	t.Helper()
	db := openTestDB(t)
	users := NewUserStore(db)
	if err := users.EnsureSchema(); err != nil {
		t.Fatalf("ensure user schema: %v", err)
	}
	if _, err := users.EnsureDefaultAdmin(); err != nil {
		t.Fatalf("ensure default admin: %v", err)
	}
	normalUser, err := users.CreateUser("alice", "alice-pass", RoleUser, "Alice")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	adminUser, err := users.Authenticate("admin", "admin")
	if err != nil {
		t.Fatalf("authenticate admin: %v", err)
	}
	auth := NewTokenAuth("device-secret", []byte("jwt-test-secret"), users)
	adminToken, err := auth.GenerateToken(adminUser)
	if err != nil {
		t.Fatalf("generate admin token: %v", err)
	}
	userToken, err := auth.GenerateToken(normalUser)
	if err != nil {
		t.Fatalf("generate user token: %v", err)
	}
	return auth, adminToken, userToken
}

func TestNewTokenAuthFromEnv_DefaultsAndOverrides(t *testing.T) {
	t.Setenv("CLAWMINI_JWT_SECRET", "")
	t.Setenv("CLAWMINI_ADMIN_TOKEN", "")
	t.Setenv("CLAWMINI_DEVICE_TOKEN", "")
	if _, err := NewTokenAuthFromEnv(); err == nil {
		t.Fatalf("expected missing env tokens to fail")
	}

	t.Setenv("CLAWMINI_JWT_SECRET", "jwt-override-1234")
	t.Setenv("CLAWMINI_DEVICE_TOKEN", "device-override-1234")
	a, err := NewTokenAuthFromEnv()
	if err != nil {
		t.Fatalf("unexpected env parse error: %v", err)
	}
	if string(a.JWTSecret) != "jwt-override-1234" || a.DeviceToken != "device-override-1234" {
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
	if got := ExtractToken(req); got != "" {
		t.Fatalf("expected X-Admin-Token to be ignored, got %q", got)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/devices?token=query-token", nil)
	if got := ExtractToken(req); got != "" {
		t.Fatalf("expected no query token support, got %q", got)
	}
}

func TestAuthAndAdminMiddleware(t *testing.T) {
	auth, adminToken, userToken := setupAuthForTest(t)

	t.Run("auth middleware rejects missing token", func(t *testing.T) {
		called := false
		next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			called = true
		})
		h := auth.AuthMiddleware(next)

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

	t.Run("admin middleware accepts admin token", func(t *testing.T) {
		called := false
		next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			called = true
			w.WriteHeader(http.StatusNoContent)
		})
		h := auth.AuthMiddleware(auth.AdminOnly(next))

		req := httptest.NewRequest(http.MethodGet, "/api/devices", nil)
		req.Header.Set("Authorization", "Bearer "+adminToken)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusNoContent {
			t.Fatalf("status mismatch: got %d want %d", rec.Code, http.StatusNoContent)
		}
		if !called {
			t.Fatalf("next handler should be called")
		}
	})

	t.Run("admin middleware rejects normal user", func(t *testing.T) {
		next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		})
		h := auth.AuthMiddleware(auth.AdminOnly(next))

		req := httptest.NewRequest(http.MethodGet, "/api/devices", nil)
		req.Header.Set("Authorization", "Bearer "+userToken)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("status mismatch: got %d want %d", rec.Code, http.StatusForbidden)
		}
	})
}
