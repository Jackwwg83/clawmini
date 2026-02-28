package server

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"time"

	"github.com/raystone-ai/clawmini/internal/protocol"
)

// CommandRecord stores server command lifecycle and result.
type CommandRecord struct {
	ID         string   `json:"id"`
	DeviceID   string   `json:"deviceId"`
	Command    string   `json:"command"`
	Args       []string `json:"args"`
	Timeout    int      `json:"timeout"`
	Status     string   `json:"status"`
	ExitCode   *int     `json:"exitCode,omitempty"`
	Stdout     string   `json:"stdout,omitempty"`
	Stderr     string   `json:"stderr,omitempty"`
	DurationMs *int64   `json:"durationMs,omitempty"`
	CreatedAt  int64    `json:"createdAt"`
	UpdatedAt  int64    `json:"updatedAt"`
}

type CommandStore struct {
	db *sql.DB
}

func NewCommandStore(db *sql.DB) *CommandStore {
	return &CommandStore{db: db}
}

func (s *CommandStore) EnsureSchema() error {
	return ensureSchemaMigrations(s.db, schemaNameCommands, 1, map[int]string{
		1: `
CREATE TABLE IF NOT EXISTS commands (
	id TEXT PRIMARY KEY,
	device_id TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
	command TEXT NOT NULL,
	args_json TEXT NOT NULL,
	timeout INTEGER NOT NULL,
	status TEXT NOT NULL,
	exit_code INTEGER,
	stdout TEXT,
	stderr TEXT,
	duration_ms INTEGER,
	created_at INTEGER NOT NULL,
	updated_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_commands_device_id ON commands(device_id);
`,
	})
}

func newCommandID() string {
	buf := make([]byte, 10)
	_, err := rand.Read(buf)
	if err != nil {
		return "cmd-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return "cmd-" + hex.EncodeToString(buf)
}

func (s *CommandStore) Create(deviceID, command string, args []string, timeout int) (CommandRecord, error) {
	if timeout <= 0 {
		timeout = 60
	}
	id := newCommandID()
	now := nowUnix()
	argsJSON, _ := json.Marshal(args)
	_, err := s.db.Exec(`
INSERT INTO commands(id, device_id, command, args_json, timeout, status, created_at, updated_at)
VALUES(?, ?, ?, ?, ?, 'queued', ?, ?);
`, id, deviceID, command, string(argsJSON), timeout, now, now)
	if err != nil {
		return CommandRecord{}, err
	}
	return CommandRecord{
		ID:        id,
		DeviceID:  deviceID,
		Command:   command,
		Args:      args,
		Timeout:   timeout,
		Status:    "queued",
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

func (s *CommandStore) MarkSent(id string) error {
	_, err := s.db.Exec(`UPDATE commands SET status='sent', updated_at=? WHERE id=?;`, nowUnix(), id)
	return err
}

func (s *CommandStore) MarkFailed(id string, msg string) error {
	_, err := s.db.Exec(`UPDATE commands SET status='failed', stderr=?, updated_at=? WHERE id=?;`, msg, nowUnix(), id)
	return err
}

// FailExpiredSent marks stale sent commands as failed when they exceed timeout+graceSeconds.
func (s *CommandStore) FailExpiredSent(graceSeconds int) (int64, error) {
	if graceSeconds < 0 {
		graceSeconds = 0
	}
	now := nowUnix()
	res, err := s.db.Exec(`
UPDATE commands
SET status='failed', stderr='timeout', updated_at=?
WHERE status='sent'
  AND (updated_at + timeout + ?) <= ?;
`, now, graceSeconds, now)
	if err != nil {
		return 0, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	return affected, nil
}

func (s *CommandStore) Complete(deviceID string, result protocol.ResultPayload) error {
	_, err := s.db.Exec(`
UPDATE commands
SET status='completed', exit_code=?, stdout=?, stderr=?, duration_ms=?, updated_at=?
WHERE id=? AND device_id=?;
`, result.ExitCode, result.Stdout, result.Stderr, result.DurationMs, nowUnix(), result.CommandID, deviceID)
	return err
}

func (s *CommandStore) GetByDeviceAndID(deviceID, id string) (CommandRecord, error) {
	row := s.db.QueryRow(`
SELECT id, device_id, command, args_json, timeout, status, exit_code, stdout, stderr, duration_ms, created_at, updated_at
FROM commands
WHERE id=? AND device_id=?;
`, id, deviceID)
	return scanCommand(row)
}

func (s *CommandStore) DeleteByDevice(deviceID string) error {
	_, err := s.db.Exec(`DELETE FROM commands WHERE device_id = ?;`, deviceID)
	return err
}

func scanCommand(scanner interface {
	Scan(dest ...interface{}) error
}) (CommandRecord, error) {
	var rec CommandRecord
	var argsJSON string
	var exitCode sql.NullInt64
	var stdout sql.NullString
	var stderr sql.NullString
	var duration sql.NullInt64
	if err := scanner.Scan(
		&rec.ID, &rec.DeviceID, &rec.Command, &argsJSON, &rec.Timeout, &rec.Status,
		&exitCode, &stdout, &stderr, &duration, &rec.CreatedAt, &rec.UpdatedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return CommandRecord{}, ErrNotFound
		}
		return CommandRecord{}, err
	}
	_ = json.Unmarshal([]byte(argsJSON), &rec.Args)
	if stdout.Valid {
		rec.Stdout = stdout.String
	}
	if stderr.Valid {
		rec.Stderr = stderr.String
	}
	if exitCode.Valid {
		v := int(exitCode.Int64)
		rec.ExitCode = &v
	}
	if duration.Valid {
		v := duration.Int64
		rec.DurationMs = &v
	}
	return rec, nil
}
