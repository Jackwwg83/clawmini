package server

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

var (
	ErrJoinTokenExpired = errors.New("join token expired")
	ErrJoinTokenUsed    = errors.New("join token already used")
)

// JoinToken is a one-time onboarding token for device registration.
type JoinToken struct {
	ID           string  `json:"id"`
	Label        string  `json:"label"`
	CreatedAt    int64   `json:"createdAt"`
	ExpiresAt    int64   `json:"expiresAt"`
	UsedAt       *int64  `json:"usedAt,omitempty"`
	UsedByDevice *string `json:"usedByDevice,omitempty"`
}

type JoinTokenStore struct {
	db *sql.DB
}

func NewJoinTokenStore(db *sql.DB) *JoinTokenStore {
	return &JoinTokenStore{db: db}
}

func (s *JoinTokenStore) EnsureSchema() error {
	return ensureSchemaMigrations(s.db, schemaNameJoinTokens, 1, map[int]string{
		1: `
CREATE TABLE IF NOT EXISTS join_tokens (
	id TEXT PRIMARY KEY,
	label TEXT NOT NULL,
	created_at INTEGER NOT NULL,
	expires_at INTEGER NOT NULL,
	used_at INTEGER,
	used_by_device TEXT
);
CREATE INDEX IF NOT EXISTS idx_join_tokens_created_at ON join_tokens(created_at DESC);
`,
	})
}

func (s *JoinTokenStore) CreateToken(label string, expiresIn time.Duration) (JoinToken, error) {
	if expiresIn <= 0 {
		return JoinToken{}, fmt.Errorf("expiresIn must be positive")
	}

	tokenID, err := newJoinTokenID()
	if err != nil {
		return JoinToken{}, err
	}

	now := nowUnix()
	expiresAt := time.Now().UTC().Add(expiresIn).Unix()
	_, err = s.db.Exec(`
INSERT INTO join_tokens(id, label, created_at, expires_at)
VALUES(?, ?, ?, ?);
`, tokenID, label, now, expiresAt)
	if err != nil {
		return JoinToken{}, err
	}

	return JoinToken{
		ID:        tokenID,
		Label:     label,
		CreatedAt: now,
		ExpiresAt: expiresAt,
	}, nil
}

func (s *JoinTokenStore) ValidateAndConsume(tokenID, deviceID string) error {
	if tokenID == "" || deviceID == "" {
		return ErrNotFound
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var expiresAt int64
	var usedAt sql.NullInt64
	if err := tx.QueryRow(`
SELECT expires_at, used_at
FROM join_tokens
WHERE id = ?;
`, tokenID).Scan(&expiresAt, &usedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}

	now := nowUnix()
	if usedAt.Valid {
		return ErrJoinTokenUsed
	}
	if now > expiresAt {
		return ErrJoinTokenExpired
	}

	res, err := tx.Exec(`
UPDATE join_tokens
SET used_at = ?, used_by_device = ?
WHERE id = ? AND used_at IS NULL;
`, now, deviceID, tokenID)
	if err != nil {
		return err
	}

	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrJoinTokenUsed
	}

	return tx.Commit()
}

func (s *JoinTokenStore) ListTokens() ([]JoinToken, error) {
	rows, err := s.db.Query(`
SELECT id, label, created_at, expires_at, used_at, used_by_device
FROM join_tokens
ORDER BY created_at DESC;
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]JoinToken, 0)
	for rows.Next() {
		var token JoinToken
		var usedAt sql.NullInt64
		var usedByDevice sql.NullString
		if err := rows.Scan(&token.ID, &token.Label, &token.CreatedAt, &token.ExpiresAt, &usedAt, &usedByDevice); err != nil {
			return nil, err
		}
		if usedAt.Valid {
			v := usedAt.Int64
			token.UsedAt = &v
		}
		if usedByDevice.Valid {
			v := usedByDevice.String
			token.UsedByDevice = &v
		}
		out = append(out, token)
	}
	return out, rows.Err()
}

func (s *JoinTokenStore) DeleteToken(id string) error {
	res, err := s.db.Exec(`DELETE FROM join_tokens WHERE id = ?;`, id)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func newJoinTokenID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate join token id: %w", err)
	}
	return hex.EncodeToString(buf), nil
}
