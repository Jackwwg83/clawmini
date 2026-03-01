package server

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
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
	UserID       *string `json:"userId,omitempty"`
}

type JoinTokenStore struct {
	db *sql.DB
}

func NewJoinTokenStore(db *sql.DB) *JoinTokenStore {
	return &JoinTokenStore{db: db}
}

func (s *JoinTokenStore) EnsureSchema() error {
	return ensureSchemaMigrations(s.db, schemaNameJoinTokens, 2, map[int]string{
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
		2: `
ALTER TABLE join_tokens ADD COLUMN user_id TEXT;
`,
	})
}

func (s *JoinTokenStore) CreateToken(label string, expiresIn time.Duration, userID string) (JoinToken, error) {
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
INSERT INTO join_tokens(id, label, created_at, expires_at, user_id)
VALUES(?, ?, ?, ?, ?);
`, tokenID, label, now, expiresAt, nullableTrim(userID))
	if err != nil {
		return JoinToken{}, err
	}

	var userIDPtr *string
	if trimmed := nullableTrim(userID); trimmed != nil {
		v := *trimmed
		userIDPtr = &v
	}
	return JoinToken{
		ID:        tokenID,
		Label:     label,
		CreatedAt: now,
		ExpiresAt: expiresAt,
		UserID:    userIDPtr,
	}, nil
}

func nullableTrim(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func (s *JoinTokenStore) ValidateAndConsume(tokenID, deviceID string) (JoinToken, error) {
	if tokenID == "" || deviceID == "" {
		return JoinToken{}, ErrNotFound
	}

	tx, err := s.db.Begin()
	if err != nil {
		return JoinToken{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var token JoinToken
	var expiresAt int64
	var usedAt sql.NullInt64
	var usedByDevice sql.NullString
	var userID sql.NullString
	if err := tx.QueryRow(`
SELECT id, label, created_at, expires_at, used_at, used_by_device, user_id
FROM join_tokens
WHERE id = ?;
`, tokenID).Scan(&token.ID, &token.Label, &token.CreatedAt, &expiresAt, &usedAt, &usedByDevice, &userID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return JoinToken{}, ErrNotFound
		}
		return JoinToken{}, err
	}
	token.ExpiresAt = expiresAt
	if usedByDevice.Valid {
		v := usedByDevice.String
		token.UsedByDevice = &v
	}
	if userID.Valid && strings.TrimSpace(userID.String) != "" {
		v := strings.TrimSpace(userID.String)
		token.UserID = &v
	}

	now := nowUnix()
	if usedAt.Valid {
		return JoinToken{}, ErrJoinTokenUsed
	}
	if now > expiresAt {
		return JoinToken{}, ErrJoinTokenExpired
	}

	res, err := tx.Exec(`
UPDATE join_tokens
SET used_at = ?, used_by_device = ?
WHERE id = ? AND used_at IS NULL;
`, now, deviceID, tokenID)
	if err != nil {
		return JoinToken{}, err
	}

	affected, err := res.RowsAffected()
	if err != nil {
		return JoinToken{}, err
	}
	if affected == 0 {
		return JoinToken{}, ErrJoinTokenUsed
	}

	if err := tx.Commit(); err != nil {
		return JoinToken{}, err
	}
	token.UsedAt = &now
	device := strings.TrimSpace(deviceID)
	token.UsedByDevice = &device
	return token, nil
}

func (s *JoinTokenStore) ListTokens() ([]JoinToken, error) {
	rows, err := s.db.Query(`
SELECT id, label, created_at, expires_at, used_at, used_by_device, user_id
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
		var userID sql.NullString
		if err := rows.Scan(&token.ID, &token.Label, &token.CreatedAt, &token.ExpiresAt, &usedAt, &usedByDevice, &userID); err != nil {
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
		if userID.Valid && strings.TrimSpace(userID.String) != "" {
			v := strings.TrimSpace(userID.String)
			token.UserID = &v
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
