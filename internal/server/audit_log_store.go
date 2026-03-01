package server

import (
	"database/sql"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

const (
	defaultAuditGCInterval   = 12 * time.Hour
	defaultAuditLogRetention = 90 * 24 * time.Hour
)

type AuditLogEntry struct {
	ID             int64  `json:"id"`
	Timestamp      int64  `json:"timestamp"`
	Action         string `json:"action"`
	TargetDeviceID string `json:"targetDeviceId,omitempty"`
	Detail         string `json:"detail,omitempty"`
	AdminIP        string `json:"adminIp,omitempty"`
	Result         string `json:"result"`
}

type AuditLogQuery struct {
	Limit     int
	Offset    int
	DeviceID  string
	DeviceIDs []string
	Action    string
	FromUnix  int64
	ToUnix    int64
}

type AuditLogPage struct {
	Items  []AuditLogEntry `json:"items"`
	Total  int             `json:"total"`
	Limit  int             `json:"limit"`
	Offset int             `json:"offset"`
}

type AuditLogStore struct {
	db *sql.DB

	gcInterval time.Duration
	retention  time.Duration

	lifecycleMu sync.Mutex
	running     bool
	stopCh      chan struct{}
	doneCh      chan struct{}
}

func NewAuditLogStore(db *sql.DB) *AuditLogStore {
	return &AuditLogStore{
		db:         db,
		gcInterval: defaultAuditGCInterval,
		retention:  defaultAuditLogRetention,
	}
}

func (s *AuditLogStore) EnsureSchema() error {
	return ensureSchemaMigrations(s.db, schemaNameAuditLog, 1, map[int]string{
		1: `
CREATE TABLE IF NOT EXISTS audit_log (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	timestamp INTEGER NOT NULL,
	action TEXT NOT NULL,
	target_device_id TEXT,
	detail TEXT,
	admin_ip TEXT,
	result TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_audit_log_timestamp ON audit_log(timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_audit_log_device ON audit_log(target_device_id);
CREATE INDEX IF NOT EXISTS idx_audit_log_action ON audit_log(action);
`,
	})
}

func (s *AuditLogStore) Start() {
	s.lifecycleMu.Lock()
	if s.running {
		s.lifecycleMu.Unlock()
		return
	}
	s.running = true
	s.stopCh = make(chan struct{})
	s.doneCh = make(chan struct{})
	stopCh := s.stopCh
	doneCh := s.doneCh
	interval := s.gcInterval
	s.lifecycleMu.Unlock()

	go s.runGC(stopCh, doneCh, interval)
}

func (s *AuditLogStore) Stop() {
	s.lifecycleMu.Lock()
	if !s.running {
		s.lifecycleMu.Unlock()
		return
	}
	stopCh := s.stopCh
	doneCh := s.doneCh
	s.running = false
	s.stopCh = nil
	s.doneCh = nil
	s.lifecycleMu.Unlock()

	close(stopCh)
	<-doneCh
}

func (s *AuditLogStore) runGC(stopCh <-chan struct{}, doneCh chan<- struct{}, interval time.Duration) {
	defer close(doneCh)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-stopCh:
			return
		case <-ticker.C:
			cutoff := nowUnix() - int64(s.retention/time.Second)
			if _, err := s.CleanupOlderThan(cutoff); err != nil {
				log.Printf("audit log cleanup error: %v", err)
			}
		}
	}
}

func (s *AuditLogStore) Log(action, targetDeviceID, detail, adminIP, result string) error {
	action = strings.TrimSpace(action)
	if action == "" {
		action = "unknown"
	}
	result = strings.TrimSpace(result)
	if result == "" {
		result = "unknown"
	}
	_, err := s.db.Exec(`
INSERT INTO audit_log(timestamp, action, target_device_id, detail, admin_ip, result)
VALUES(?, ?, ?, ?, ?, ?);
`, nowUnix(), action, strings.TrimSpace(targetDeviceID), strings.TrimSpace(detail), strings.TrimSpace(adminIP), result)
	return err
}

func (s *AuditLogStore) List(query AuditLogQuery) (AuditLogPage, error) {
	limit := query.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	offset := query.Offset
	if offset < 0 {
		offset = 0
	}

	whereSQL, args := buildAuditWhere(query)

	var total int
	countSQL := "SELECT COUNT(*) FROM audit_log" + whereSQL + ";"
	if err := s.db.QueryRow(countSQL, args...).Scan(&total); err != nil {
		return AuditLogPage{}, err
	}

	listArgs := append([]interface{}{}, args...)
	listArgs = append(listArgs, limit, offset)
	rows, err := s.db.Query(`
SELECT id, timestamp, action, target_device_id, detail, admin_ip, result
FROM audit_log`+whereSQL+`
ORDER BY timestamp DESC, id DESC
LIMIT ? OFFSET ?;
`, listArgs...)
	if err != nil {
		return AuditLogPage{}, err
	}
	defer rows.Close()

	out := make([]AuditLogEntry, 0, limit)
	for rows.Next() {
		var item AuditLogEntry
		if err := rows.Scan(&item.ID, &item.Timestamp, &item.Action, &item.TargetDeviceID, &item.Detail, &item.AdminIP, &item.Result); err != nil {
			return AuditLogPage{}, err
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return AuditLogPage{}, err
	}

	return AuditLogPage{
		Items:  out,
		Total:  total,
		Limit:  limit,
		Offset: offset,
	}, nil
}

func buildAuditWhere(query AuditLogQuery) (string, []interface{}) {
	clauses := make([]string, 0, 4)
	args := make([]interface{}, 0, 4)

	if deviceID := strings.TrimSpace(query.DeviceID); deviceID != "" {
		clauses = append(clauses, "target_device_id = ?")
		args = append(args, deviceID)
	}
	if len(query.DeviceIDs) > 0 {
		placeholders := strings.Repeat("?,", len(query.DeviceIDs))
		placeholders = strings.TrimSuffix(placeholders, ",")
		clauses = append(clauses, "target_device_id IN ("+placeholders+")")
		for _, deviceID := range query.DeviceIDs {
			args = append(args, strings.TrimSpace(deviceID))
		}
	}
	if action := strings.TrimSpace(query.Action); action != "" {
		clauses = append(clauses, "action = ?")
		args = append(args, action)
	}
	if query.FromUnix > 0 {
		clauses = append(clauses, "timestamp >= ?")
		args = append(args, query.FromUnix)
	}
	if query.ToUnix > 0 {
		clauses = append(clauses, "timestamp <= ?")
		args = append(args, query.ToUnix)
	}

	if len(clauses) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(clauses, " AND "), args
}

func (s *AuditLogStore) CleanupOlderThan(cutoffUnix int64) (int64, error) {
	res, err := s.db.Exec(`DELETE FROM audit_log WHERE timestamp < ?;`, cutoffUnix)
	if err != nil {
		return 0, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("audit log cleanup rows affected: %w", err)
	}
	return affected, nil
}
