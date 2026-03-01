package server

import "testing"

func TestEnsureSchemaVersionTable(t *testing.T) {
	db := openTestDB(t)

	devices := NewDeviceStore(db)
	commands := NewCommandStore(db)
	joinTokens := NewJoinTokenStore(db)
	users := NewUserStore(db)
	adminSettings := NewAdminTokenStore(db)
	imJobs := NewIMConfigJobStore(db)
	batchJobs := NewBatchJobStore(db)
	auditLogs := NewAuditLogStore(db)

	if err := devices.EnsureSchema(); err != nil {
		t.Fatalf("ensure device schema: %v", err)
	}
	if err := commands.EnsureSchema(); err != nil {
		t.Fatalf("ensure command schema: %v", err)
	}
	if err := joinTokens.EnsureSchema(); err != nil {
		t.Fatalf("ensure join token schema: %v", err)
	}
	if err := users.EnsureSchema(); err != nil {
		t.Fatalf("ensure user schema: %v", err)
	}
	if err := adminSettings.EnsureSchema(); err != nil {
		t.Fatalf("ensure admin settings schema: %v", err)
	}
	if err := imJobs.EnsureSchema(); err != nil {
		t.Fatalf("ensure im config jobs schema: %v", err)
	}
	if err := batchJobs.EnsureSchema(); err != nil {
		t.Fatalf("ensure batch jobs schema: %v", err)
	}
	if err := auditLogs.EnsureSchema(); err != nil {
		t.Fatalf("ensure audit logs schema: %v", err)
	}

	if err := devices.EnsureSchema(); err != nil {
		t.Fatalf("ensure device schema second run: %v", err)
	}
	if err := commands.EnsureSchema(); err != nil {
		t.Fatalf("ensure command schema second run: %v", err)
	}
	if err := joinTokens.EnsureSchema(); err != nil {
		t.Fatalf("ensure join token schema second run: %v", err)
	}
	if err := users.EnsureSchema(); err != nil {
		t.Fatalf("ensure user schema second run: %v", err)
	}
	if err := adminSettings.EnsureSchema(); err != nil {
		t.Fatalf("ensure admin settings schema second run: %v", err)
	}
	if err := imJobs.EnsureSchema(); err != nil {
		t.Fatalf("ensure im config jobs schema second run: %v", err)
	}
	if err := batchJobs.EnsureSchema(); err != nil {
		t.Fatalf("ensure batch jobs schema second run: %v", err)
	}
	if err := auditLogs.EnsureSchema(); err != nil {
		t.Fatalf("ensure audit logs schema second run: %v", err)
	}

	assertSchemaVersion := func(name string, want int) {
		t.Helper()
		var got int
		if err := db.QueryRow(`SELECT version FROM schema_version WHERE name = ?;`, name).Scan(&got); err != nil {
			t.Fatalf("load schema version for %s: %v", name, err)
		}
		if got != want {
			t.Fatalf("schema version for %s = %d, want %d", name, got, want)
		}
	}

	assertSchemaVersion(schemaNameDevices, 2)
	assertSchemaVersion(schemaNameCommands, 1)
	assertSchemaVersion(schemaNameJoinTokens, 2)
	assertSchemaVersion(schemaNameUsers, 1)
	assertSchemaVersion(schemaNameAdminSettings, 1)
	assertSchemaVersion(schemaNameIMConfigJobs, 1)
	assertSchemaVersion(schemaNameBatchJobs, 1)
	assertSchemaVersion(schemaNameAuditLog, 1)
}
