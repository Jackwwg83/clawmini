package server

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// --- JWT Validation Edge Cases ---

func TestParseUserToken_Expired(t *testing.T) {
	db := openTestDB(t)
	users := NewUserStore(db)
	if err := users.EnsureSchema(); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	user, err := users.CreateUser("expiry-test", "pass", RoleUser, "Expiry")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	auth := NewTokenAuth("device-token", []byte("test-secret"), nil)

	// Manually create an expired token
	headerJSON, _ := json.Marshal(map[string]string{"alg": "HS256", "typ": "JWT"})
	claimsJSON, _ := json.Marshal(map[string]interface{}{
		"user_id":  user.ID,
		"username": user.Username,
		"role":     user.Role,
		"exp":      time.Now().UTC().Add(-1 * time.Hour).Unix(), // expired 1 hour ago
	})
	head := base64.RawURLEncoding.EncodeToString(headerJSON)
	payload := base64.RawURLEncoding.EncodeToString(claimsJSON)
	signingInput := head + "." + payload
	sig := auth.jwtSign(signingInput)
	expiredToken := signingInput + "." + sig

	_, err = auth.ParseUserToken(expiredToken)
	if err == nil {
		t.Fatalf("expected error for expired token")
	}
	if !strings.Contains(err.Error(), "expired") {
		t.Fatalf("expected 'expired' error, got: %v", err)
	}
}

func TestParseUserToken_MalformedFormat(t *testing.T) {
	auth := NewTokenAuth("device-token", []byte("test-secret"), nil)

	testCases := []struct {
		name  string
		token string
		errIn string
	}{
		{"empty", "", "missing token"},
		{"single part", "justonepart", "invalid token format"},
		{"two parts", "part1.part2", "invalid token format"},
		{"four parts", "a.b.c.d", "invalid token format"},
		{"whitespace only", "   ", "missing token"},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := auth.ParseUserToken(tc.token)
			if err == nil {
				t.Fatalf("expected error for token %q", tc.token)
			}
			if !strings.Contains(err.Error(), tc.errIn) {
				t.Fatalf("expected error containing %q, got: %v", tc.errIn, err)
			}
		})
	}
}

func TestParseUserToken_WrongSecret(t *testing.T) {
	db := openTestDB(t)
	users := NewUserStore(db)
	if err := users.EnsureSchema(); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	user, err := users.CreateUser("wrong-secret", "pass", RoleUser, "WS")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	auth1 := NewTokenAuth("dev", []byte("secret-one"), nil)
	auth2 := NewTokenAuth("dev", []byte("secret-two"), nil)

	token, err := auth1.GenerateToken(user)
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}

	// Parse with different secret
	_, err = auth2.ParseUserToken(token)
	if err == nil {
		t.Fatalf("expected error for wrong secret")
	}
	if !strings.Contains(err.Error(), "invalid signature") {
		t.Fatalf("expected 'invalid signature' error, got: %v", err)
	}
}

func TestParseUserToken_MissingClaims(t *testing.T) {
	auth := NewTokenAuth("dev", []byte("test-secret"), nil)

	// Create token with missing user_id
	headerJSON, _ := json.Marshal(map[string]string{"alg": "HS256", "typ": "JWT"})
	claimsJSON, _ := json.Marshal(map[string]interface{}{
		"user_id":  "",
		"username": "test",
		"role":     "user",
		"exp":      time.Now().UTC().Add(time.Hour).Unix(),
	})
	head := base64.RawURLEncoding.EncodeToString(headerJSON)
	payload := base64.RawURLEncoding.EncodeToString(claimsJSON)
	signingInput := head + "." + payload
	sig := auth.jwtSign(signingInput)
	token := signingInput + "." + sig

	_, err := auth.ParseUserToken(token)
	if err == nil {
		t.Fatalf("expected error for missing claims")
	}
	if !strings.Contains(err.Error(), "invalid claims") {
		t.Fatalf("expected 'invalid claims' error, got: %v", err)
	}
}

func TestParseUserToken_TamperedPayload(t *testing.T) {
	db := openTestDB(t)
	users := NewUserStore(db)
	if err := users.EnsureSchema(); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	user, err := users.CreateUser("tamper-test", "pass", RoleUser, "Tamper")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	auth := NewTokenAuth("dev", []byte("test-secret"), nil)
	token, err := auth.GenerateToken(user)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	// Tamper with the payload to escalate role
	parts := strings.Split(token, ".")
	payloadBytes, _ := base64.RawURLEncoding.DecodeString(parts[1])
	var claims map[string]interface{}
	_ = json.Unmarshal(payloadBytes, &claims)
	claims["role"] = "admin" // privilege escalation attempt
	tamperedPayload, _ := json.Marshal(claims)
	parts[1] = base64.RawURLEncoding.EncodeToString(tamperedPayload)
	tamperedToken := strings.Join(parts, ".")

	_, err = auth.ParseUserToken(tamperedToken)
	if err == nil {
		t.Fatalf("expected error for tampered token")
	}
	if !strings.Contains(err.Error(), "invalid signature") {
		t.Fatalf("expected 'invalid signature' error, got: %v", err)
	}
}

func TestParseUserToken_InvalidBase64Payload(t *testing.T) {
	auth := NewTokenAuth("dev", []byte("test-secret"), nil)

	// Create valid header
	headerJSON, _ := json.Marshal(map[string]string{"alg": "HS256", "typ": "JWT"})
	head := base64.RawURLEncoding.EncodeToString(headerJSON)

	// Use garbage as payload
	payload := "!!!not-valid-base64!!!"
	signingInput := head + "." + payload
	sig := auth.jwtSign(signingInput)
	token := signingInput + "." + sig

	_, err := auth.ParseUserToken(token)
	if err == nil {
		t.Fatalf("expected error for invalid base64 payload")
	}
}

// --- JWT Generation / Round Trip ---

func TestGenerateAndParseToken_RoundTrip(t *testing.T) {
	db := openTestDB(t)
	users := NewUserStore(db)
	if err := users.EnsureSchema(); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	user, err := users.CreateUser("roundtrip", "pass", RoleAdmin, "Round Trip")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	auth := NewTokenAuth("dev", []byte("roundtrip-secret"), users)
	token, err := auth.GenerateToken(user)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	parsed, err := auth.ParseUserToken(token)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if parsed.ID != user.ID {
		t.Fatalf("ID mismatch: %q vs %q", parsed.ID, user.ID)
	}
	if parsed.Username != user.Username {
		t.Fatalf("username mismatch: %q vs %q", parsed.Username, user.Username)
	}
	if parsed.Role != user.Role {
		t.Fatalf("role mismatch: %q vs %q", parsed.Role, user.Role)
	}
	if parsed.DisplayName != user.DisplayName {
		t.Fatalf("displayName mismatch: %q vs %q", parsed.DisplayName, user.DisplayName)
	}
}

// --- Token Refreshes from DB ---

func TestParseUserToken_RefreshesRoleFromDB(t *testing.T) {
	db := openTestDB(t)
	users := NewUserStore(db)
	if err := users.EnsureSchema(); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	user, err := users.CreateUser("role-refresh", "pass", RoleUser, "Refresh Test")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	auth := NewTokenAuth("dev", []byte("refresh-secret"), users)
	token, err := auth.GenerateToken(user)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	// Promote user to admin in DB
	newRole := RoleAdmin
	if _, err := users.UpdateUser(user.ID, UpdateUserInput{Role: &newRole}); err != nil {
		t.Fatalf("update role: %v", err)
	}

	// Token was issued as "user" but DB now says "admin"
	parsed, err := auth.ParseUserToken(token)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if parsed.Role != RoleAdmin {
		t.Fatalf("expected refreshed role 'admin', got %q", parsed.Role)
	}
}

func TestParseUserToken_DeletedUserFailsRefresh(t *testing.T) {
	db := openTestDB(t)
	devices := NewDeviceStore(db)
	if err := devices.EnsureSchema(); err != nil {
		t.Fatalf("ensure device schema: %v", err)
	}
	users := NewUserStore(db)
	if err := users.EnsureSchema(); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	user, err := users.CreateUser("delete-refresh", "pass", RoleUser, "Del Refresh")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	auth := NewTokenAuth("dev", []byte("del-secret"), users)
	token, err := auth.GenerateToken(user)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	if err := users.DeleteUser(user.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	// Should fail because user no longer in DB
	_, err = auth.ParseUserToken(token)
	if err == nil {
		t.Fatalf("expected error for deleted user token refresh")
	}
}

// --- ValidateDeviceToken ---

func TestValidateDeviceToken(t *testing.T) {
	auth := NewTokenAuth("my-device-token", []byte("secret"), nil)

	if !auth.ValidateDeviceToken("my-device-token") {
		t.Fatalf("expected valid device token")
	}
	if !auth.ValidateDeviceToken(" my-device-token ") {
		t.Fatalf("expected trimmed token to be valid")
	}
	if auth.ValidateDeviceToken("wrong-token") {
		t.Fatalf("expected invalid device token")
	}
	if auth.ValidateDeviceToken("") {
		t.Fatalf("expected empty string to be invalid")
	}
}

// --- AuthMiddleware ---

func TestAuthMiddleware_ValidToken(t *testing.T) {
	db := openTestDB(t)
	users := NewUserStore(db)
	if err := users.EnsureSchema(); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	user, err := users.CreateUser("mw-valid", "pass", RoleUser, "MW Valid")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	auth := NewTokenAuth("dev", []byte("mw-secret"), users)
	token, err := auth.GenerateToken(user)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	var capturedUser AuthUser
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, ok := UserFromRequest(r)
		if !ok {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		capturedUser = u
		w.WriteHeader(http.StatusOK)
	})

	h := auth.AuthMiddleware(next)
	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if capturedUser.ID != user.ID {
		t.Fatalf("expected user ID %q, got %q", user.ID, capturedUser.ID)
	}
}

func TestAuthMiddleware_InvalidToken(t *testing.T) {
	auth := NewTokenAuth("dev", []byte("mw-secret"), nil)
	called := false
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	})

	h := auth.AuthMiddleware(next)
	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	req.Header.Set("Authorization", "Bearer totally-invalid-token")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
	if called {
		t.Fatalf("next handler should not be called with invalid token")
	}
}

// --- AdminOnly Middleware ---

func TestAdminOnly_NoUserInContext(t *testing.T) {
	auth := NewTokenAuth("dev", []byte("secret"), nil)
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := auth.AdminOnly(next)

	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 with no user, got %d", rec.Code)
	}
}

func TestAdminOnly_UserRoleInContext(t *testing.T) {
	auth := NewTokenAuth("dev", []byte("secret"), nil)
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := auth.AdminOnly(next)

	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	ctx := WithUserContext(req.Context(), AuthUser{
		ID:       "user-1",
		Username: "nonAdmin",
		Role:     RoleUser,
	})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-admin, got %d", rec.Code)
	}
}

func TestAdminOnly_AdminRoleInContext(t *testing.T) {
	auth := NewTokenAuth("dev", []byte("secret"), nil)
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	h := auth.AdminOnly(next)

	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	ctx := WithUserContext(req.Context(), AuthUser{
		ID:       "admin-1",
		Username: "admin",
		Role:     RoleAdmin,
	})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for admin, got %d", rec.Code)
	}
	if !called {
		t.Fatalf("next handler should be called for admin")
	}
}

// --- UserFromContext / UserFromRequest ---

func TestUserFromContext_NoUser(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	_, ok := UserFromRequest(req)
	if ok {
		t.Fatalf("expected no user from empty context")
	}
}

func TestUserFromContext_WithUser(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	expectedUser := AuthUser{ID: "test-id", Username: "tester", Role: RoleAdmin}
	ctx := WithUserContext(req.Context(), expectedUser)
	req = req.WithContext(ctx)

	user, ok := UserFromRequest(req)
	if !ok {
		t.Fatalf("expected user from context")
	}
	if user.ID != expectedUser.ID {
		t.Fatalf("ID mismatch")
	}
}

// --- ExtractToken Edge Cases ---

func TestExtractToken_BearerOnly(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer")
	got := ExtractToken(req)
	// "Bearer" without a space and token should not extract
	if got != "" {
		t.Fatalf("expected empty for 'Bearer' without token, got %q", got)
	}
}

func TestExtractToken_BasicAuth(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Basic dXNlcjpwYXNz")
	got := ExtractToken(req)
	if got != "" {
		t.Fatalf("expected empty for Basic auth, got %q", got)
	}
}

func TestNewTokenAuthFromEnv_MissingAll(t *testing.T) {
	t.Setenv("CLAWMINI_DEVICE_TOKEN", "")
	t.Setenv("CLAWMINI_JWT_SECRET", "")
	t.Setenv("CLAWMINI_ADMIN_TOKEN", "")

	_, err := NewTokenAuthFromEnv()
	if err == nil {
		t.Fatalf("expected error when all tokens missing")
	}
}

func TestNewTokenAuthFromEnv_RejectsShortTokens(t *testing.T) {
	t.Setenv("CLAWMINI_DEVICE_TOKEN", "short-token")
	t.Setenv("CLAWMINI_JWT_SECRET", "short-secret")
	t.Setenv("CLAWMINI_ADMIN_TOKEN", "")

	_, err := NewTokenAuthFromEnv()
	if err == nil {
		t.Fatalf("expected short token validation error")
	}
}

func TestValidateAuthConfig(t *testing.T) {
	if err := ValidateAuthConfig("device-token-1234", []byte("jwt-secret-123456")); err != nil {
		t.Fatalf("expected valid auth config, got %v", err)
	}
	if err := ValidateAuthConfig("short", []byte("jwt-secret-123456")); err == nil {
		t.Fatalf("expected short device token to fail")
	}
	if err := ValidateAuthConfig("device-token-1234", []byte("short")); err == nil {
		t.Fatalf("expected short jwt secret to fail")
	}
}
