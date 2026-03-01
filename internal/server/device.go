package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/raystone-ai/clawmini/internal/protocol"
	_ "modernc.org/sqlite"
)

// DeviceStatus is the latest status reported by the device.
type DeviceStatus struct {
	CPUUsage      float64               `json:"cpuUsage"`
	MemTotal      uint64                `json:"memTotal"`
	MemUsed       uint64                `json:"memUsed"`
	DiskTotal     uint64                `json:"diskTotal"`
	DiskUsed      uint64                `json:"diskUsed"`
	Uptime        uint64                `json:"uptime"`
	OpenClawInfo  protocol.OpenClawInfo `json:"openclaw"`
	UpdatedAtUnix int64                 `json:"updatedAt"`
}

// DeviceSnapshot is returned by list/detail API.
type DeviceSnapshot struct {
	ID              string        `json:"id"`
	Hostname        string        `json:"hostname"`
	OS              string        `json:"os"`
	Arch            string        `json:"arch"`
	HasOpenClaw     bool          `json:"hasOpenClaw"`
	OpenClawVersion string        `json:"openclawVersion,omitempty"`
	ClientVersion   string        `json:"clientVersion"`
	CreatedAtUnix   int64         `json:"createdAt"`
	UpdatedAtUnix   int64         `json:"updatedAt"`
	LastSeenAtUnix  int64         `json:"lastSeenAt"`
	Online          bool          `json:"online"`
	Status          *DeviceStatus `json:"status,omitempty"`
}

type DeviceStore struct {
	db *sql.DB
}

func OpenSQLite(path string) (*sql.DB, error) {
	if path == "" {
		path = "clawmini.db"
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil && filepath.Dir(path) != "." {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`PRAGMA journal_mode = WAL;`); err != nil {
		_ = db.Close()
		return nil, err
	}
	if _, err := db.Exec(`PRAGMA busy_timeout = 5000;`); err != nil {
		_ = db.Close()
		return nil, err
	}
	if _, err := db.Exec(`PRAGMA foreign_keys = ON;`); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func NewDeviceStore(db *sql.DB) *DeviceStore {
	return &DeviceStore{db: db}
}

func (s *DeviceStore) EnsureSchema() error {
	return ensureSchemaMigrations(s.db, schemaNameDevices, 2, map[int]string{
		1: `
CREATE TABLE IF NOT EXISTS devices (
	id TEXT PRIMARY KEY,
	hostname TEXT NOT NULL,
	os TEXT NOT NULL,
	arch TEXT NOT NULL,
	openclaw_version TEXT,
	client_version TEXT NOT NULL,
	created_at INTEGER NOT NULL,
	updated_at INTEGER NOT NULL,
	last_seen_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS device_status (
	device_id TEXT PRIMARY KEY REFERENCES devices(id) ON DELETE CASCADE,
	cpu_usage REAL NOT NULL DEFAULT 0,
	mem_total INTEGER NOT NULL DEFAULT 0,
	mem_used INTEGER NOT NULL DEFAULT 0,
	disk_total INTEGER NOT NULL DEFAULT 0,
	disk_used INTEGER NOT NULL DEFAULT 0,
	uptime INTEGER NOT NULL DEFAULT 0,
	openclaw_installed INTEGER NOT NULL DEFAULT 0,
	openclaw_version TEXT,
	gateway_status TEXT,
	update_available TEXT,
	channels_json TEXT,
	updated_at INTEGER NOT NULL
);
`,
		2: `
ALTER TABLE devices ADD COLUMN has_openclaw INTEGER NOT NULL DEFAULT 0;
`,
	})
}

func nowUnix() int64 {
	return time.Now().UTC().Unix()
}

func (s *DeviceStore) UpsertDevice(reg protocol.RegisterPayload) error {
	now := nowUnix()
	hasOpenClaw := 0
	if reg.HasOpenClaw {
		hasOpenClaw = 1
	}
	_, err := s.db.Exec(`
INSERT INTO devices(id, hostname, os, arch, has_openclaw, openclaw_version, client_version, created_at, updated_at, last_seen_at)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
	hostname=excluded.hostname,
	os=excluded.os,
	arch=excluded.arch,
	has_openclaw=excluded.has_openclaw,
	openclaw_version=excluded.openclaw_version,
	client_version=excluded.client_version,
	updated_at=excluded.updated_at,
	last_seen_at=excluded.last_seen_at;
`, reg.DeviceID, reg.Hostname, reg.OS, reg.Arch, hasOpenClaw, reg.OpenClawVersion, reg.ClientVersion, now, now, now)
	return err
}

func (s *DeviceStore) UpdateHeartbeat(hb protocol.HeartbeatPayload) error {
	now := nowUnix()
	channels, _ := json.Marshal(hb.OpenClaw.Channels)
	installed := 0
	if hb.OpenClaw.Installed {
		installed = 1
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(`
UPDATE devices
SET updated_at=?, last_seen_at=?, has_openclaw=?, openclaw_version=CASE WHEN ? = '' THEN openclaw_version ELSE ? END
WHERE id=?;
`, now, now, installed, hb.OpenClaw.Version, hb.OpenClaw.Version, hb.DeviceID); err != nil {
		return err
	}

	if _, err := tx.Exec(`
INSERT INTO device_status(device_id, cpu_usage, mem_total, mem_used, disk_total, disk_used, uptime,
	openclaw_installed, openclaw_version, gateway_status, update_available, channels_json, updated_at)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(device_id) DO UPDATE SET
	cpu_usage=excluded.cpu_usage,
	mem_total=excluded.mem_total,
	mem_used=excluded.mem_used,
	disk_total=excluded.disk_total,
	disk_used=excluded.disk_used,
	uptime=excluded.uptime,
	openclaw_installed=excluded.openclaw_installed,
	openclaw_version=excluded.openclaw_version,
	gateway_status=excluded.gateway_status,
	update_available=excluded.update_available,
	channels_json=excluded.channels_json,
	updated_at=excluded.updated_at;
`, hb.DeviceID, hb.System.CPUUsage, hb.System.MemTotal, hb.System.MemUsed, hb.System.DiskTotal, hb.System.DiskUsed,
		hb.System.Uptime, installed, hb.OpenClaw.Version, hb.OpenClaw.GatewayStatus, hb.OpenClaw.UpdateAvailable, string(channels), now); err != nil {
		return err
	}

	return tx.Commit()
}

func (s *DeviceStore) ListDevices() ([]DeviceSnapshot, error) {
	rows, err := s.db.Query(`
SELECT d.id, d.hostname, d.os, d.arch, d.openclaw_version, d.client_version,
	d.has_openclaw,
	d.created_at, d.updated_at, d.last_seen_at,
	s.cpu_usage, s.mem_total, s.mem_used, s.disk_total, s.disk_used, s.uptime,
	s.openclaw_installed, s.openclaw_version, s.gateway_status, s.update_available, s.channels_json, s.updated_at
FROM devices d
LEFT JOIN device_status s ON s.device_id = d.id
ORDER BY d.updated_at DESC;
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]DeviceSnapshot, 0)
	for rows.Next() {
		snap, err := scanDeviceSnapshot(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, snap)
	}
	return out, rows.Err()
}

func (s *DeviceStore) GetDevice(id string) (DeviceSnapshot, error) {
	row := s.db.QueryRow(`
SELECT d.id, d.hostname, d.os, d.arch, d.openclaw_version, d.client_version,
	d.has_openclaw,
	d.created_at, d.updated_at, d.last_seen_at,
	s.cpu_usage, s.mem_total, s.mem_used, s.disk_total, s.disk_used, s.uptime,
	s.openclaw_installed, s.openclaw_version, s.gateway_status, s.update_available, s.channels_json, s.updated_at
FROM devices d
LEFT JOIN device_status s ON s.device_id = d.id
WHERE d.id = ?;
`, id)

	snap, err := scanDeviceSnapshot(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return DeviceSnapshot{}, ErrNotFound
		}
		return DeviceSnapshot{}, err
	}
	return snap, nil
}

func (s *DeviceStore) DeleteDevice(id string) error {
	res, err := s.db.Exec(`DELETE FROM devices WHERE id = ?;`, id)
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

func scanDeviceSnapshot(scanner interface {
	Scan(dest ...interface{}) error
}) (DeviceSnapshot, error) {
	var snap DeviceSnapshot
	var hasOpenClaw int64
	var statusUpdated sql.NullInt64
	var statusCPU sql.NullFloat64
	var statusMemTotal, statusMemUsed sql.NullInt64
	var statusDiskTotal, statusDiskUsed sql.NullInt64
	var statusUptime sql.NullInt64
	var statusInstalled sql.NullInt64
	var statusVersion, statusGateway, statusUpdate, channels sql.NullString
	if err := scanner.Scan(
		&snap.ID, &snap.Hostname, &snap.OS, &snap.Arch, &snap.OpenClawVersion, &snap.ClientVersion,
		&hasOpenClaw,
		&snap.CreatedAtUnix, &snap.UpdatedAtUnix, &snap.LastSeenAtUnix,
		&statusCPU, &statusMemTotal, &statusMemUsed, &statusDiskTotal, &statusDiskUsed, &statusUptime,
		&statusInstalled, &statusVersion, &statusGateway, &statusUpdate, &channels, &statusUpdated,
	); err != nil {
		return DeviceSnapshot{}, err
	}
	snap.HasOpenClaw = hasOpenClaw == 1

	snap.Online = (nowUnix() - snap.LastSeenAtUnix) <= 90
	if statusUpdated.Valid {
		st := &DeviceStatus{
			CPUUsage:      statusCPU.Float64,
			MemTotal:      uint64(statusMemTotal.Int64),
			MemUsed:       uint64(statusMemUsed.Int64),
			DiskTotal:     uint64(statusDiskTotal.Int64),
			DiskUsed:      uint64(statusDiskUsed.Int64),
			Uptime:        uint64(statusUptime.Int64),
			UpdatedAtUnix: statusUpdated.Int64,
			OpenClawInfo: protocol.OpenClawInfo{
				Installed:       statusInstalled.Int64 == 1,
				Version:         statusVersion.String,
				GatewayStatus:   statusGateway.String,
				UpdateAvailable: statusUpdate.String,
			},
		}
		if channels.Valid && channels.String != "" {
			_ = json.Unmarshal([]byte(channels.String), &st.OpenClawInfo.Channels)
		}
		snap.Status = st
	}
	return snap, nil
}

func (s *DeviceStore) UpdateOpenClawState(deviceID string, installed bool, version string) error {
	installedInt := 0
	if installed {
		installedInt = 1
	}
	now := nowUnix()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.Exec(`
UPDATE devices
SET has_openclaw=?, openclaw_version=?, updated_at=?, last_seen_at=CASE WHEN last_seen_at < ? THEN ? ELSE last_seen_at END
WHERE id=?;
`, installedInt, version, now, now, now, deviceID)
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

	if _, err := tx.Exec(`
INSERT INTO device_status(device_id, openclaw_installed, openclaw_version, gateway_status, updated_at)
VALUES(?, ?, ?, COALESCE((SELECT gateway_status FROM device_status WHERE device_id=?), 'unknown'), ?)
ON CONFLICT(device_id) DO UPDATE SET
	openclaw_installed=excluded.openclaw_installed,
	openclaw_version=excluded.openclaw_version,
	updated_at=excluded.updated_at;
`, deviceID, installedInt, version, deviceID, now); err != nil {
		return err
	}

	return tx.Commit()
}

var ErrNotFound = errors.New("not found")
