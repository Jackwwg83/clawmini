package server

import (
	"fmt"
	"net/http"
	"os"
	"strings"
)

// TokenAuth validates admin and device tokens.
type TokenAuth struct {
	AdminToken  string
	DeviceToken string
}

func NewTokenAuthFromEnv() (*TokenAuth, error) {
	admin := strings.TrimSpace(os.Getenv("CLAWMINI_ADMIN_TOKEN"))
	if admin == "" {
		return nil, fmt.Errorf("CLAWMINI_ADMIN_TOKEN is required")
	}
	device := strings.TrimSpace(os.Getenv("CLAWMINI_DEVICE_TOKEN"))
	if device == "" {
		return nil, fmt.Errorf("CLAWMINI_DEVICE_TOKEN is required")
	}
	return &TokenAuth{AdminToken: admin, DeviceToken: device}, nil
}

func (a *TokenAuth) ValidateAdminToken(token string) bool {
	return token != "" && token == a.AdminToken
}

func (a *TokenAuth) ValidateDeviceToken(token string) bool {
	return token != "" && token == a.DeviceToken
}

func ExtractToken(r *http.Request) string {
	if auth := r.Header.Get("Authorization"); auth != "" {
		parts := strings.SplitN(auth, " ", 2)
		if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
			return strings.TrimSpace(parts[1])
		}
	}
	if token := strings.TrimSpace(r.Header.Get("X-Admin-Token")); token != "" {
		return token
	}
	return ""
}

func (a *TokenAuth) AdminMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !a.ValidateAdminToken(ExtractToken(r)) {
			WriteError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next.ServeHTTP(w, r)
	})
}
