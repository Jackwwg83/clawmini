package main

import (
	"path/filepath"
	"testing"

	"github.com/raystone-ai/clawmini/internal/server"
)

func openAdminTokenStoreForTest(t *testing.T) *server.AdminTokenStore {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "security-config.db")
	db, err := server.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	store := server.NewAdminTokenStore(db)
	if err := store.EnsureSchema(); err != nil {
		t.Fatalf("ensure admin token schema: %v", err)
	}
	return store
}

func TestResolveAdminToken_Precedence(t *testing.T) {
	store := openAdminTokenStoreForTest(t)

	token, generated, err := resolveAdminToken(store, "env-token", "flag-token", "config-token")
	if err != nil {
		t.Fatalf("resolve token: %v", err)
	}
	if generated {
		t.Fatalf("expected non-generated token")
	}
	if token != "env-token" {
		t.Fatalf("got %q, want env-token", token)
	}
}

func TestResolveAdminToken_UsesFlagThenConfigThenDB(t *testing.T) {
	store := openAdminTokenStoreForTest(t)

	token, generated, err := resolveAdminToken(store, "", "flag-token", "config-token")
	if err != nil {
		t.Fatalf("resolve flag token: %v", err)
	}
	if generated || token != "flag-token" {
		t.Fatalf("expected flag-token non-generated, got token=%q generated=%v", token, generated)
	}

	token, generated, err = resolveAdminToken(store, "", "", "config-token")
	if err != nil {
		t.Fatalf("resolve config token: %v", err)
	}
	if generated || token != "config-token" {
		t.Fatalf("expected config-token non-generated, got token=%q generated=%v", token, generated)
	}

	token, generated, err = resolveAdminToken(store, "", "", "")
	if err != nil {
		t.Fatalf("resolve db token: %v", err)
	}
	if generated || token != "config-token" {
		t.Fatalf("expected db token=config-token, got token=%q generated=%v", token, generated)
	}
}

func TestResolveAdminToken_GeneratesAndPersists(t *testing.T) {
	store := openAdminTokenStoreForTest(t)

	token, generated, err := resolveAdminToken(store, "", "", "")
	if err != nil {
		t.Fatalf("resolve generated token: %v", err)
	}
	if !generated {
		t.Fatalf("expected generated token")
	}
	if len(token) != 64 {
		t.Fatalf("generated token len=%d, want 64", len(token))
	}

	loaded, err := store.GetAdminToken()
	if err != nil {
		t.Fatalf("load persisted token: %v", err)
	}
	if loaded != token {
		t.Fatalf("persisted token mismatch: got %q want %q", loaded, token)
	}
}

func TestSplitCSV(t *testing.T) {
	got := splitCSV(" https://a.example.com, ,http://b.example.com  ")
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0] != "https://a.example.com" || got[1] != "http://b.example.com" {
		t.Fatalf("unexpected parsed list: %#v", got)
	}
}
