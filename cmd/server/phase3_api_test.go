package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/raystone-ai/clawmini/internal/server"
)

func TestInstallOpenClawAPI_CreatesAndUpdatesJob(t *testing.T) {
	app, r := setupTestApp(t)
	seedDevice(t, app, "dev-install")

	rr := doRequest(t, r, http.MethodPost, "/api/devices/dev-install/install-openclaw", "", "test-admin-token")
	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rr.Code, rr.Body.String())
	}

	var job server.IMConfigJob
	if err := json.Unmarshal(rr.Body.Bytes(), &job); err != nil {
		t.Fatalf("decode install job: %v", err)
	}
	if job.ID == "" {
		t.Fatalf("expected install job id")
	}

	waitForCondition(t, 2*time.Second, func() bool {
		resp := doRequest(t, r, http.MethodGet, "/api/devices/dev-install/install-openclaw/"+job.ID, "", "test-admin-token")
		return resp.Code == http.StatusOK && (strings.Contains(resp.Body.String(), "\"failed\"") || strings.Contains(resp.Body.String(), "\"success\""))
	}, "install job should reach terminal state")
}

func TestBatchExecAPI_CreateAndGetJob(t *testing.T) {
	app, r := setupTestApp(t)
	seedDevice(t, app, "dev-batch-a")
	seedDevice(t, app, "dev-batch-b")

	body := `{"deviceIds":["dev-batch-a","dev-batch-b"],"command":"openclaw gateway restart"}`
	rr := doRequest(t, r, http.MethodPost, "/api/batch/exec", body, "test-admin-token")
	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rr.Code, rr.Body.String())
	}

	var created struct {
		JobID string `json:"jobId"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode batch response: %v", err)
	}
	if created.JobID == "" {
		t.Fatalf("expected batch job id")
	}

	waitForCondition(t, 2*time.Second, func() bool {
		resp := doRequest(t, r, http.MethodGet, "/api/batch/"+created.JobID, "", "test-admin-token")
		if resp.Code != http.StatusOK {
			return false
		}
		var job server.BatchJob
		if err := json.Unmarshal(resp.Body.Bytes(), &job); err != nil {
			return false
		}
		return job.Status == "failed" || job.Status == "success"
	}, "batch job should reach terminal state")
}

func TestAuditLogAPI_List(t *testing.T) {
	app, r := setupTestApp(t)
	if err := app.auditLogs.Log("command.exec", "dev-1", "openclaw status", "127.0.0.1", "success"); err != nil {
		t.Fatalf("seed audit log: %v", err)
	}

	rr := doRequest(t, r, http.MethodGet, "/api/audit-log?limit=10&offset=0", "", "test-admin-token")
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var page server.AuditLogPage
	if err := json.Unmarshal(rr.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode audit log page: %v", err)
	}
	if page.Total == 0 || len(page.Items) == 0 {
		t.Fatalf("expected non-empty audit log page")
	}
}
