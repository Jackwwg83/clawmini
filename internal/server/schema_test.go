package server

import "testing"

func TestEnsureSchemaVersionTable(t *testing.T) {
	db := openTestDB(t)

	devices := NewDeviceStore(db)
	commands := NewCommandStore(db)
	joinTokens := NewJoinTokenStore(db)

	if err := devices.EnsureSchema(); err != nil {
		t.Fatalf("ensure device schema: %v", err)
	}
	if err := commands.EnsureSchema(); err != nil {
		t.Fatalf("ensure command schema: %v", err)
	}
	if err := joinTokens.EnsureSchema(); err != nil {
		t.Fatalf("ensure join token schema: %v", err)
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

	assertSchemaVersion(schemaNameDevices, 1)
	assertSchemaVersion(schemaNameCommands, 1)
	assertSchemaVersion(schemaNameJoinTokens, 1)
}
