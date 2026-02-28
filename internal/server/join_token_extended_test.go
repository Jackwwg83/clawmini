package server

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func TestJoinTokenCreateNegativeExpiry(t *testing.T) {
	db := openTestDB(t)
	store := NewJoinTokenStore(db)
	if err := store.EnsureSchema(); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}

	_, err := store.CreateToken("bad-expiry", -time.Hour)
	if err == nil {
		t.Fatalf("expected error for negative expiresIn")
	}

	_, err = store.CreateToken("zero-expiry", 0)
	if err == nil {
		t.Fatalf("expected error for zero expiresIn")
	}
}

func TestJoinTokenValidateEmptyInputs(t *testing.T) {
	db := openTestDB(t)
	store := NewJoinTokenStore(db)
	if err := store.EnsureSchema(); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}

	tok, err := store.CreateToken("valid", time.Hour)
	if err != nil {
		t.Fatalf("create token: %v", err)
	}

	// Empty tokenID
	err = store.ValidateAndConsume("", "device-1")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for empty tokenID, got %v", err)
	}

	// Empty deviceID
	err = store.ValidateAndConsume(tok.ID, "")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for empty deviceID, got %v", err)
	}

	// Both empty
	err = store.ValidateAndConsume("", "")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for both empty, got %v", err)
	}
}

func TestJoinTokenValidateNonExistent(t *testing.T) {
	db := openTestDB(t)
	store := NewJoinTokenStore(db)
	if err := store.EnsureSchema(); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}

	err := store.ValidateAndConsume("does-not-exist-at-all", "device-1")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for non-existent token, got %v", err)
	}
}

func TestJoinTokenMultipleCreation(t *testing.T) {
	db := openTestDB(t)
	store := NewJoinTokenStore(db)
	if err := store.EnsureSchema(); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}

	ids := make(map[string]bool)
	for i := 0; i < 10; i++ {
		tok, err := store.CreateToken("token", time.Hour)
		if err != nil {
			t.Fatalf("create token %d: %v", i, err)
		}
		if ids[tok.ID] {
			t.Fatalf("duplicate token ID generated: %s", tok.ID)
		}
		ids[tok.ID] = true
		if len(tok.ID) != 32 {
			t.Fatalf("token id length = %d, want 32", len(tok.ID))
		}
	}

	listed, err := store.ListTokens()
	if err != nil {
		t.Fatalf("list tokens: %v", err)
	}
	if len(listed) != 10 {
		t.Fatalf("expected 10 tokens, got %d", len(listed))
	}
}

func TestJoinTokenListEmpty(t *testing.T) {
	db := openTestDB(t)
	store := NewJoinTokenStore(db)
	if err := store.EnsureSchema(); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}

	listed, err := store.ListTokens()
	if err != nil {
		t.Fatalf("list tokens: %v", err)
	}
	if listed == nil {
		t.Fatalf("expected non-nil empty slice, got nil")
	}
	if len(listed) != 0 {
		t.Fatalf("expected 0 tokens, got %d", len(listed))
	}
}

func TestJoinTokenListOrder(t *testing.T) {
	db := openTestDB(t)
	store := NewJoinTokenStore(db)
	if err := store.EnsureSchema(); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}

	tok1, err := store.CreateToken("first", time.Hour)
	if err != nil {
		t.Fatalf("create first: %v", err)
	}
	tok2, err := store.CreateToken("second", time.Hour)
	if err != nil {
		t.Fatalf("create second: %v", err)
	}

	listed, err := store.ListTokens()
	if err != nil {
		t.Fatalf("list tokens: %v", err)
	}
	if len(listed) != 2 {
		t.Fatalf("expected 2 tokens, got %d", len(listed))
	}
	// Both have same created_at (same second) — just verify both present
	foundIDs := map[string]bool{listed[0].ID: true, listed[1].ID: true}
	if !foundIDs[tok1.ID] || !foundIDs[tok2.ID] {
		t.Fatalf("listed tokens don't match created ones: %+v", listed)
	}
}

func TestJoinTokenCreateEmptyLabel(t *testing.T) {
	db := openTestDB(t)
	store := NewJoinTokenStore(db)
	if err := store.EnsureSchema(); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}

	tok, err := store.CreateToken("", time.Hour)
	if err != nil {
		t.Fatalf("create token with empty label: %v", err)
	}
	if tok.Label != "" {
		t.Fatalf("expected empty label, got %q", tok.Label)
	}

	listed, err := store.ListTokens()
	if err != nil {
		t.Fatalf("list tokens: %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("expected 1 token, got %d", len(listed))
	}
	if listed[0].Label != "" {
		t.Fatalf("expected empty label in list, got %q", listed[0].Label)
	}
}

func TestJoinTokenConsumedTokenShowsUsage(t *testing.T) {
	db := openTestDB(t)
	store := NewJoinTokenStore(db)
	if err := store.EnsureSchema(); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}

	tok, err := store.CreateToken("test", time.Hour)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Before consumption: UsedAt and UsedByDevice should be nil
	listed, _ := store.ListTokens()
	if listed[0].UsedAt != nil || listed[0].UsedByDevice != nil {
		t.Fatalf("expected nil usedAt/usedByDevice before consumption")
	}

	// Consume
	if err := store.ValidateAndConsume(tok.ID, "dev-test"); err != nil {
		t.Fatalf("consume: %v", err)
	}

	// After consumption: UsedAt and UsedByDevice should be set
	listed, _ = store.ListTokens()
	if listed[0].UsedAt == nil {
		t.Fatalf("expected usedAt set after consumption")
	}
	if listed[0].UsedByDevice == nil || *listed[0].UsedByDevice != "dev-test" {
		t.Fatalf("expected usedByDevice='dev-test', got %+v", listed[0].UsedByDevice)
	}
}

func TestJoinTokenDeleteNonExistent(t *testing.T) {
	db := openTestDB(t)
	store := NewJoinTokenStore(db)
	if err := store.EnsureSchema(); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}

	err := store.DeleteToken("non-existent-token-id")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestJoinTokenDeleteEmptyID(t *testing.T) {
	db := openTestDB(t)
	store := NewJoinTokenStore(db)
	if err := store.EnsureSchema(); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}

	err := store.DeleteToken("")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for empty id, got %v", err)
	}
}

func TestJoinTokenConcurrentConsumption(t *testing.T) {
	db := openTestDB(t)
	store := NewJoinTokenStore(db)
	if err := store.EnsureSchema(); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}

	tok, err := store.CreateToken("concurrent", time.Hour)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	const goroutines = 5
	var wg sync.WaitGroup
	results := make(chan error, goroutines)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(deviceNum int) {
			defer wg.Done()
			deviceID := "dev-concurrent-" + string(rune('A'+deviceNum))
			results <- store.ValidateAndConsume(tok.ID, deviceID)
		}(i)
	}
	wg.Wait()
	close(results)

	successes := 0
	failures := 0
	for err := range results {
		if err == nil {
			successes++
		} else {
			failures++
		}
	}

	if successes != 1 {
		t.Fatalf("expected exactly 1 success, got %d successes and %d failures", successes, failures)
	}
	if failures != goroutines-1 {
		t.Fatalf("expected %d failures, got %d", goroutines-1, failures)
	}
}

func TestJoinTokenConsumeAlreadyExpired(t *testing.T) {
	db := openTestDB(t)
	store := NewJoinTokenStore(db)
	if err := store.EnsureSchema(); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}

	tok, err := store.CreateToken("will-expire", time.Hour)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Force expiry to 1 second in the past
	if _, err := db.Exec(`UPDATE join_tokens SET expires_at = ? WHERE id = ?;`, nowUnix()-1, tok.ID); err != nil {
		t.Fatalf("force expire: %v", err)
	}

	err = store.ValidateAndConsume(tok.ID, "dev-late")
	if !errors.Is(err, ErrJoinTokenExpired) {
		t.Fatalf("expected ErrJoinTokenExpired, got %v", err)
	}
}

func TestJoinTokenConsumeUsedThenExpire(t *testing.T) {
	db := openTestDB(t)
	store := NewJoinTokenStore(db)
	if err := store.EnsureSchema(); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}

	tok, err := store.CreateToken("use-then-expire", time.Hour)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Consume first
	if err := store.ValidateAndConsume(tok.ID, "dev-first"); err != nil {
		t.Fatalf("first consume: %v", err)
	}

	// Force expiry
	if _, err := db.Exec(`UPDATE join_tokens SET expires_at = ? WHERE id = ?;`, nowUnix()-1, tok.ID); err != nil {
		t.Fatalf("force expire: %v", err)
	}

	// Try to consume again — should get ErrJoinTokenUsed (checked before expiry)
	err = store.ValidateAndConsume(tok.ID, "dev-second")
	if !errors.Is(err, ErrJoinTokenUsed) {
		t.Fatalf("expected ErrJoinTokenUsed for already-used token even if expired, got %v", err)
	}
}

func TestJoinTokenDeleteConsumedToken(t *testing.T) {
	db := openTestDB(t)
	store := NewJoinTokenStore(db)
	if err := store.EnsureSchema(); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}

	tok, err := store.CreateToken("to-delete", time.Hour)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := store.ValidateAndConsume(tok.ID, "dev-1"); err != nil {
		t.Fatalf("consume: %v", err)
	}

	// Should be able to delete consumed token
	if err := store.DeleteToken(tok.ID); err != nil {
		t.Fatalf("delete consumed token: %v", err)
	}

	listed, _ := store.ListTokens()
	if len(listed) != 0 {
		t.Fatalf("expected 0 tokens after delete, got %d", len(listed))
	}
}

func TestJoinTokenExpiresAtFieldConsistency(t *testing.T) {
	db := openTestDB(t)
	store := NewJoinTokenStore(db)
	if err := store.EnsureSchema(); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}

	before := time.Now().UTC().Unix()
	tok, err := store.CreateToken("check-times", 24*time.Hour)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	after := time.Now().UTC().Unix()

	// createdAt should be between before and after
	if tok.CreatedAt < before || tok.CreatedAt > after {
		t.Fatalf("createdAt %d not between %d and %d", tok.CreatedAt, before, after)
	}

	// expiresAt should be ~24h after createdAt
	expectedExpiry := before + 24*3600
	if tok.ExpiresAt < expectedExpiry-1 || tok.ExpiresAt > expectedExpiry+2 {
		t.Fatalf("expiresAt %d not ~24h after creation (%d)", tok.ExpiresAt, expectedExpiry)
	}
}
