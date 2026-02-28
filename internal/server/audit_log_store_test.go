package server

import "testing"

func TestAuditLogStoreListAndFilter(t *testing.T) {
	db := openTestDB(t)
	store := NewAuditLogStore(db)
	if err := store.EnsureSchema(); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}

	if err := store.Log("command.exec", "dev-a", "openclaw status", "127.0.0.1", "success"); err != nil {
		t.Fatalf("log 1: %v", err)
	}
	if err := store.Log("device.delete", "dev-b", "deleted", "127.0.0.2", "success"); err != nil {
		t.Fatalf("log 2: %v", err)
	}

	page, err := store.List(AuditLogQuery{Limit: 20, Offset: 0})
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if page.Total != 2 || len(page.Items) != 2 {
		t.Fatalf("expected 2 audit records, got total=%d len=%d", page.Total, len(page.Items))
	}

	filtered, err := store.List(AuditLogQuery{DeviceID: "dev-a", Limit: 20})
	if err != nil {
		t.Fatalf("list filtered: %v", err)
	}
	if filtered.Total != 1 || len(filtered.Items) != 1 {
		t.Fatalf("expected one filtered record, got total=%d len=%d", filtered.Total, len(filtered.Items))
	}
	if filtered.Items[0].Action != "command.exec" {
		t.Fatalf("unexpected filtered action: %q", filtered.Items[0].Action)
	}
}

func TestAuditLogStoreCleanupOlderThan(t *testing.T) {
	db := openTestDB(t)
	store := NewAuditLogStore(db)
	if err := store.EnsureSchema(); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}

	if _, err := db.Exec(`
INSERT INTO audit_log(timestamp, action, target_device_id, detail, admin_ip, result)
VALUES(?, 'old.action', 'dev-old', 'old', '127.0.0.1', 'success');
`, nowUnix()-1000); err != nil {
		t.Fatalf("insert old record: %v", err)
	}
	if err := store.Log("new.action", "dev-new", "new", "127.0.0.1", "success"); err != nil {
		t.Fatalf("insert new record: %v", err)
	}

	affected, err := store.CleanupOlderThan(nowUnix() - 100)
	if err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if affected != 1 {
		t.Fatalf("expected 1 deleted row, got %d", affected)
	}

	page, err := store.List(AuditLogQuery{Limit: 10})
	if err != nil {
		t.Fatalf("list after cleanup: %v", err)
	}
	if page.Total != 1 || len(page.Items) != 1 {
		t.Fatalf("expected one remaining row after cleanup, got total=%d len=%d", page.Total, len(page.Items))
	}
	if page.Items[0].Action != "new.action" {
		t.Fatalf("unexpected remaining action: %q", page.Items[0].Action)
	}
}
