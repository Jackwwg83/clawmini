package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSpaHandlerRootReturnsOK(t *testing.T) {
	h := spaHandler()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200 for /, got %d", rr.Code)
	}
}

func TestDecodeJSONBody(t *testing.T) {
	type payload struct {
		Token string `json:"token"`
	}

	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"token":"abc"}`))
	rr := httptest.NewRecorder()

	var got payload
	if err := decodeJSONBody(rr, req, &got); err != nil {
		t.Fatalf("decodeJSONBody returned error: %v", err)
	}
	if got.Token != "abc" {
		t.Fatalf("unexpected decoded token: %q", got.Token)
	}
}

func TestClientIP(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{name: "host and port", in: "192.0.2.10:54321", want: "192.0.2.10"},
		{name: "raw ip", in: "192.0.2.11", want: "192.0.2.11"},
		{name: "empty", in: "", want: "unknown"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := clientIP(tc.in); got != tc.want {
				t.Fatalf("clientIP(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestIsBodyTooLarge(t *testing.T) {
	large := strings.Repeat("a", maxJSONBodyBytes)
	body := `{"token":"` + large + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(body))
	rr := httptest.NewRecorder()

	var got map[string]string
	err := decodeJSONBody(rr, req, &got)
	if err == nil {
		t.Fatalf("expected decodeJSONBody to fail for oversized body")
	}
	if !isBodyTooLarge(err) {
		t.Fatalf("expected isBodyTooLarge to return true, err=%v", err)
	}
}

func TestLoginRateLimiter(t *testing.T) {
	limiter := newLoginRateLimiter(2, time.Minute)
	ip := "198.51.100.9"

	if !limiter.allow(ip) {
		t.Fatalf("first attempt should be allowed")
	}
	if !limiter.allow(ip) {
		t.Fatalf("second attempt should be allowed")
	}
	if limiter.allow(ip) {
		t.Fatalf("third attempt should be blocked")
	}

	hit := false
	handler := limiter.Middleware(func(w http.ResponseWriter, _ *http.Request) {
		hit = true
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	req.RemoteAddr = ip + ":1234"
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 for rate-limited request, got %d", rr.Code)
	}
	if hit {
		t.Fatalf("wrapped handler should not be called when rate-limited")
	}
}
