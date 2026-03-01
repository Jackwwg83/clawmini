package server

import "database/sql"

const schemaNameAdminSettings = "admin_settings"

// AdminTokenStore persists the server admin token in SQLite.
type AdminTokenStore struct {
	db *sql.DB
}

func NewAdminTokenStore(db *sql.DB) *AdminTokenStore {
	return &AdminTokenStore{db: db}
}

func (s *AdminTokenStore) EnsureSchema() error {
	return ensureSchemaMigrations(s.db, schemaNameAdminSettings, 1, map[int]string{
		1: `
CREATE TABLE IF NOT EXISTS app_settings (
	key TEXT PRIMARY KEY,
	value TEXT NOT NULL,
	updated_at INTEGER NOT NULL
);
`,
	})
}

func (s *AdminTokenStore) GetSetting(key string) (string, error) {
	var value string
	if err := s.db.QueryRow(`SELECT value FROM app_settings WHERE key = ?;`, key).Scan(&value); err != nil {
		if err == sql.ErrNoRows {
			return "", ErrNotFound
		}
		return "", err
	}
	return value, nil
}

func (s *AdminTokenStore) SaveSetting(key, value string) error {
	_, err := s.db.Exec(`
INSERT INTO app_settings(key, value, updated_at)
VALUES(?, ?, ?)
ON CONFLICT(key) DO UPDATE SET
	value=excluded.value,
	updated_at=excluded.updated_at;
`, key, value, nowUnix())
	return err
}

func (s *AdminTokenStore) GetAdminToken() (string, error) {
	return s.GetSetting("admin_token")
}

func (s *AdminTokenStore) SaveAdminToken(token string) error {
	return s.SaveSetting("admin_token", token)
}
