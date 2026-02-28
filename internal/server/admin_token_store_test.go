package server

import "testing"

func TestAdminTokenStore_SaveAndLoad(t *testing.T) {
	db := openTestDB(t)
	store := NewAdminTokenStore(db)
	if err := store.EnsureSchema(); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}

	if _, err := store.GetAdminToken(); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound before save, got %v", err)
	}

	if err := store.SaveAdminToken("first-token"); err != nil {
		t.Fatalf("save token: %v", err)
	}
	got, err := store.GetAdminToken()
	if err != nil {
		t.Fatalf("load token: %v", err)
	}
	if got != "first-token" {
		t.Fatalf("got %q, want %q", got, "first-token")
	}

	if err := store.SaveAdminToken("second-token"); err != nil {
		t.Fatalf("update token: %v", err)
	}
	got, err = store.GetAdminToken()
	if err != nil {
		t.Fatalf("load updated token: %v", err)
	}
	if got != "second-token" {
		t.Fatalf("got %q, want %q", got, "second-token")
	}
}
