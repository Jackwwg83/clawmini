package main

import (
	"net/http"
	"strings"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/raystone-ai/clawmini/internal/openclaw"
	"github.com/raystone-ai/clawmini/internal/server"
)

type batchExecRequest struct {
	DeviceIDs []string `json:"deviceIds"`
	Command   string   `json:"command"`
}

func (a *serverApp) handleBatchExec(w http.ResponseWriter, r *http.Request) {
	adminIP := clientIP(r.RemoteAddr)
	user, ok := a.currentUserOrUnauthorized(w, r)
	if !ok {
		return
	}
	var req batchExecRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		if isBodyTooLarge(err) {
			server.WriteError(w, http.StatusRequestEntityTooLarge, "request too large")
		} else {
			server.WriteError(w, http.StatusBadRequest, "invalid json")
		}
		a.logAudit("batch.exec", "", "invalid request", adminIP, "failed")
		return
	}

	deviceIDs := normalizeDeviceIDs(req.DeviceIDs)
	if len(deviceIDs) == 0 {
		server.WriteError(w, http.StatusBadRequest, "deviceIds is required")
		a.logAudit("batch.exec", "", "deviceIds is required", adminIP, "failed")
		return
	}
	allowedDeviceIDs, err := a.filterAccessibleDeviceIDs(user, deviceIDs)
	if err != nil {
		a.writeInternalError(w, "filter batch device access", err)
		a.logAudit("batch.exec", "", err.Error(), adminIP, "failed")
		return
	}
	for _, deviceID := range deviceIDs {
		if !allowedDeviceIDs[deviceID] {
			server.WriteError(w, http.StatusForbidden, "forbidden")
			a.logAudit("batch.exec", deviceID, "forbidden", adminIP, "forbidden")
			return
		}
	}

	commandText := strings.TrimSpace(req.Command)
	command, args, ok := splitBatchCommand(commandText)
	if !ok || !openclaw.ValidateCommand(command, args) {
		server.WriteError(w, http.StatusBadRequest, "command not allowed")
		a.logAudit("batch.exec", "", commandText, adminIP, "rejected")
		return
	}

	for _, deviceID := range deviceIDs {
		if _, err := a.devices.GetDevice(deviceID); err != nil {
			if err == server.ErrNotFound {
				server.WriteError(w, http.StatusNotFound, "device not found: "+deviceID)
				a.logAudit("batch.exec", deviceID, commandText, adminIP, "failed")
				return
			}
			a.writeInternalError(w, "load device before batch exec", err)
			a.logAudit("batch.exec", deviceID, err.Error(), adminIP, "failed")
			return
		}
	}

	job, err := a.batchJobs.Create(commandText, deviceIDs)
	if err != nil {
		a.writeInternalError(w, "create batch job", err)
		a.logAudit("batch.exec", "", err.Error(), adminIP, "failed")
		return
	}

	go a.runBatchJob(job.ID, command, args, deviceIDs, adminIP)
	a.logAudit("batch.exec", "", commandText, adminIP, "accepted")
	server.WriteJSON(w, http.StatusAccepted, map[string]interface{}{
		"jobId": job.ID,
		"job":   job,
	})
}

func (a *serverApp) handleGetBatchJob(w http.ResponseWriter, r *http.Request) {
	jobID := strings.TrimSpace(chi.URLParam(r, "jobId"))
	if jobID == "" {
		server.WriteError(w, http.StatusBadRequest, "invalid job id")
		return
	}
	user, ok := a.currentUserOrUnauthorized(w, r)
	if !ok {
		return
	}
	job, err := a.batchJobs.Get(jobID)
	if err != nil {
		if err == server.ErrNotFound {
			server.WriteError(w, http.StatusNotFound, "batch job not found")
			return
		}
		a.writeInternalError(w, "get batch job", err)
		return
	}
	if user.Role != server.RoleAdmin {
		itemDeviceIDs := make([]string, 0, len(job.Items))
		for _, item := range job.Items {
			itemDeviceIDs = append(itemDeviceIDs, item.DeviceID)
		}
		allowedDeviceIDs, err := a.filterAccessibleDeviceIDs(user, itemDeviceIDs)
		if err != nil {
			a.writeInternalError(w, "filter batch job access", err)
			return
		}
		for _, item := range job.Items {
			if !allowedDeviceIDs[item.DeviceID] {
				server.WriteError(w, http.StatusForbidden, "forbidden")
				return
			}
		}
	}
	server.WriteJSON(w, http.StatusOK, job)
}

func (a *serverApp) runBatchJob(jobID, command string, args, deviceIDs []string, adminIP string) {
	_ = a.batchJobs.SetStatus(jobID, "running")

	timeout := estimateBatchTimeout(command, args)
	var wg sync.WaitGroup

	for _, deviceID := range deviceIDs {
		deviceID := deviceID
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = a.batchJobs.UpdateItem(jobID, deviceID, "running", "", "")
			rec, err := a.dispatchAndWaitCommand(deviceID, command, args, timeout)
			if err != nil {
				_ = a.batchJobs.UpdateItem(jobID, deviceID, "error", rec.ID, err.Error())
				a.logAudit("command.exec", deviceID, strings.TrimSpace(command+" "+strings.Join(args, " ")), adminIP, "failed")
				return
			}
			if isCommandFailed(rec) {
				errText := commandErrorText(rec, "batch command failed")
				_ = a.batchJobs.UpdateItem(jobID, deviceID, "error", rec.ID, errText)
				a.logAudit("command.exec", deviceID, strings.TrimSpace(command+" "+strings.Join(args, " ")), adminIP, "failed")
				return
			}
			_ = a.batchJobs.UpdateItem(jobID, deviceID, "success", rec.ID, "")
			a.logAudit("command.exec", deviceID, strings.TrimSpace(command+" "+strings.Join(args, " ")), adminIP, "success")
		}()
	}

	wg.Wait()
	finalJob, err := a.batchJobs.Get(jobID)
	if err != nil {
		return
	}
	if finalJob.ErrorCount > 0 {
		_ = a.batchJobs.SetStatus(jobID, "failed")
		a.logAudit("batch.exec", "", finalJob.Command, adminIP, "failed")
		return
	}
	_ = a.batchJobs.SetStatus(jobID, "success")
	a.logAudit("batch.exec", "", finalJob.Command, adminIP, "success")
}

func splitBatchCommand(input string) (string, []string, bool) {
	parts := strings.Fields(strings.TrimSpace(input))
	if len(parts) == 0 {
		return "", nil, false
	}
	return parts[0], parts[1:], true
}

func normalizeDeviceIDs(raw []string) []string {
	out := make([]string, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))
	for _, value := range raw {
		deviceID := strings.TrimSpace(value)
		if deviceID == "" {
			continue
		}
		if _, exists := seen[deviceID]; exists {
			continue
		}
		seen[deviceID] = struct{}{}
		out = append(out, deviceID)
	}
	return out
}

func estimateBatchTimeout(command string, args []string) int {
	if command != "openclaw" || len(args) == 0 {
		return 90
	}
	switch args[0] {
	case "gateway":
		return 45
	case "doctor":
		return 120
	case "update":
		if len(args) >= 2 && args[1] == "status" {
			return 60
		}
		return 180
	default:
		return 90
	}
}
