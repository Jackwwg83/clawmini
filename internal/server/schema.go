package server

import (
	"database/sql"
	"fmt"
)

const (
	schemaNameDevices    = "devices"
	schemaNameCommands   = "commands"
	schemaNameJoinTokens = "join_tokens"
	schemaNameBatchJobs  = "batch_jobs"
	schemaNameAuditLog   = "audit_log"
)

func ensureSchemaMigrations(db *sql.DB, schemaName string, targetVersion int, migrations map[int]string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(`
CREATE TABLE IF NOT EXISTS schema_version (
	name TEXT PRIMARY KEY,
	version INTEGER NOT NULL,
	updated_at INTEGER NOT NULL
);
`); err != nil {
		return fmt.Errorf("create schema_version table: %w", err)
	}

	var currentVersion int
	err = tx.QueryRow(`SELECT version FROM schema_version WHERE name = ?;`, schemaName).Scan(&currentVersion)
	if err != nil {
		if err != sql.ErrNoRows {
			return fmt.Errorf("load %s schema version: %w", schemaName, err)
		}
		currentVersion = 0
	}

	if currentVersion > targetVersion {
		return fmt.Errorf("%s schema version %d is newer than supported %d", schemaName, currentVersion, targetVersion)
	}

	for version := currentVersion + 1; version <= targetVersion; version++ {
		stmt, ok := migrations[version]
		if !ok {
			return fmt.Errorf("missing migration for %s schema version %d", schemaName, version)
		}
		if _, err := tx.Exec(stmt); err != nil {
			return fmt.Errorf("apply %s schema migration v%d: %w", schemaName, version, err)
		}
		if _, err := tx.Exec(`
INSERT INTO schema_version(name, version, updated_at)
VALUES(?, ?, ?)
ON CONFLICT(name) DO UPDATE SET
	version=excluded.version,
	updated_at=excluded.updated_at;
`, schemaName, version, nowUnix()); err != nil {
			return fmt.Errorf("save %s schema version %d: %w", schemaName, version, err)
		}
	}

	return tx.Commit()
}
