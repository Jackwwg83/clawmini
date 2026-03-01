package server

import (
	"errors"
	"testing"
	"time"
)

func TestJoinTokenCreateValidateConsume(t *testing.T) {
	db := openTestDB(t)
	store := NewJoinTokenStore(db)
	if err := store.EnsureSchema(); err != nil {
		t.Fatalf("ensure join token schema: %v", err)
	}

	tok, err := store.CreateToken("测试设备", 2*time.Hour, "")
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	if tok.ID == "" {
		t.Fatalf("expected token id")
	}
	if len(tok.ID) != 32 {
		t.Fatalf("token id length = %d, want 32", len(tok.ID))
	}
	if tok.Label != "测试设备" {
		t.Fatalf("token label mismatch: %q", tok.Label)
	}
	if tok.ExpiresAt <= tok.CreatedAt {
		t.Fatalf("expiresAt should be greater than createdAt: %+v", tok)
	}
	if tok.UsedAt != nil {
		t.Fatalf("expected token usedAt nil")
	}

	if _, err := store.ValidateAndConsume(tok.ID, "dev-join-1"); err != nil {
		t.Fatalf("validate and consume: %v", err)
	}

	listed, err := store.ListTokens()
	if err != nil {
		t.Fatalf("list tokens: %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("expected one token in list, got %d", len(listed))
	}
	if listed[0].UsedAt == nil {
		t.Fatalf("expected usedAt to be persisted")
	}
	if listed[0].UsedByDevice == nil || *listed[0].UsedByDevice != "dev-join-1" {
		t.Fatalf("unexpected usedByDevice: %+v", listed[0].UsedByDevice)
	}

	_, err = store.ValidateAndConsume(tok.ID, "dev-join-2")
	if !errors.Is(err, ErrJoinTokenUsed) {
		t.Fatalf("expected ErrJoinTokenUsed, got %v", err)
	}
}

func TestJoinTokenExpiryAndDelete(t *testing.T) {
	db := openTestDB(t)
	store := NewJoinTokenStore(db)
	if err := store.EnsureSchema(); err != nil {
		t.Fatalf("ensure join token schema: %v", err)
	}

	tok, err := store.CreateToken("即将过期", time.Hour, "")
	if err != nil {
		t.Fatalf("create token: %v", err)
	}

	if _, err := db.Exec(`UPDATE join_tokens SET expires_at = ? WHERE id = ?;`, nowUnix()-1, tok.ID); err != nil {
		t.Fatalf("force token expired: %v", err)
	}

	_, err = store.ValidateAndConsume(tok.ID, "dev-expired")
	if !errors.Is(err, ErrJoinTokenExpired) {
		t.Fatalf("expected ErrJoinTokenExpired, got %v", err)
	}

	if err := store.DeleteToken(tok.ID); err != nil {
		t.Fatalf("delete token: %v", err)
	}
	if err := store.DeleteToken(tok.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for second delete, got %v", err)
	}
}
