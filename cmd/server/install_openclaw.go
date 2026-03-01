package main

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/raystone-ai/clawmini/internal/openclaw"
	"github.com/raystone-ai/clawmini/internal/server"
)

func installOpenClawSteps() []server.IMConfigStep {
	return []server.IMConfigStep{
		{
			Key:            "check-existing",
			Title:          "检查 OpenClaw",
			DisplayCommand: "which openclaw",
			Status:         "pending",
		},
		{
			Key:            "run-installer",
			Title:          "执行安装脚本",
			DisplayCommand: openclaw.OfficialInstallScript(),
			Status:         "pending",
		},
		{
			Key:            "verify-version",
			Title:          "验证安装结果",
			DisplayCommand: "bash -lc \"openclaw --version\"",
			Status:         "pending",
		},
	}
}

func (a *serverApp) handleInstallOpenClaw(w http.ResponseWriter, r *http.Request) {
	adminIP := clientIP(r.RemoteAddr)
	deviceID := strings.TrimSpace(chi.URLParam(r, "id"))
	if deviceID == "" {
		server.WriteError(w, http.StatusBadRequest, "invalid device id")
		a.logAudit("openclaw.install", deviceID, "invalid device id", adminIP, "failed")
		return
	}
	if _, err := a.devices.GetDevice(deviceID); err != nil {
		if err == server.ErrNotFound {
			server.WriteError(w, http.StatusNotFound, "device not found")
			a.logAudit("openclaw.install", deviceID, "device not found", adminIP, "failed")
			return
		}
		a.writeInternalError(w, "load device before install", err)
		a.logAudit("openclaw.install", deviceID, err.Error(), adminIP, "failed")
		return
	}
	if !a.requireDeviceAccess(w, r, deviceID) {
		a.logAudit("openclaw.install", deviceID, "forbidden", adminIP, "forbidden")
		return
	}

	job, err := a.imConfigs.create(deviceID, "openclaw-install", installOpenClawSteps())
	if err != nil {
		a.writeInternalError(w, "create install-openclaw job", err)
		a.logAudit("openclaw.install", deviceID, err.Error(), adminIP, "failed")
		return
	}

	go a.runInstallOpenClawJob(job.ID, deviceID, adminIP)
	a.logAudit("openclaw.install", deviceID, "started install job "+job.ID, adminIP, "accepted")
	server.WriteJSON(w, http.StatusAccepted, job)
}

func (a *serverApp) handleGetInstallOpenClaw(w http.ResponseWriter, r *http.Request) {
	deviceID := strings.TrimSpace(chi.URLParam(r, "id"))
	jobID := strings.TrimSpace(chi.URLParam(r, "jobId"))
	if deviceID == "" || jobID == "" {
		server.WriteError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if !a.requireDeviceAccess(w, r, deviceID) {
		return
	}

	job, err := a.imConfigs.get(deviceID, jobID)
	if err != nil {
		if err == server.ErrNotFound {
			server.WriteError(w, http.StatusNotFound, "install job not found")
			return
		}
		a.writeInternalError(w, "get install-openclaw job", err)
		return
	}
	server.WriteJSON(w, http.StatusOK, job)
}

func (a *serverApp) runInstallOpenClawJob(jobID, deviceID, adminIP string) {
	_ = a.imConfigs.update(jobID, func(job *configureIMResponse) {
		job.Status = "running"
		job.Error = ""
	})

	checkRec, err := a.dispatchAndWaitCommand(deviceID, "which", []string{"openclaw"}, 20)
	if err != nil {
		a.failInstallStep(jobID, "check-existing", "检查 OpenClaw 失败", &checkRec, err)
		a.logAudit("openclaw.install", deviceID, err.Error(), adminIP, "failed")
		return
	}

	alreadyInstalled := !isCommandFailed(checkRec)
	_ = a.completeInstallStep(jobID, "check-existing", &checkRec, "")

	if alreadyInstalled {
		_ = a.imConfigs.update(jobID, func(job *configureIMResponse) {
			for i := range job.Steps {
				if job.Steps[i].Key == "run-installer" {
					job.Steps[i].Status = "skipped"
					job.Steps[i].Error = "检测到 OpenClaw 已安装，跳过安装脚本"
				}
			}
		})
	} else {
		installRec, installErr := a.dispatchAndWaitCommand(deviceID, "bash", []string{"-lc", openclaw.OfficialInstallScript()}, 240)
		if installErr != nil {
			a.failInstallStep(jobID, "run-installer", "执行安装脚本失败", &installRec, installErr)
			a.logAudit("openclaw.install", deviceID, installErr.Error(), adminIP, "failed")
			return
		}
		if isCommandFailed(installRec) {
			errText := commandErrorText(installRec, "执行安装脚本失败")
			a.failInstallStep(jobID, "run-installer", errText, &installRec, errors.New(errText))
			a.logAudit("openclaw.install", deviceID, errText, adminIP, "failed")
			return
		}
		_ = a.completeInstallStep(jobID, "run-installer", &installRec, "")
		time.Sleep(2 * time.Second)
	}

	verifyRec, verifyErr := a.dispatchAndWaitCommand(deviceID, "bash", []string{"-lc", "openclaw --version"}, 20)
	if verifyErr != nil {
		a.failInstallStep(jobID, "verify-version", "验证版本失败", &verifyRec, verifyErr)
		a.logAudit("openclaw.install", deviceID, verifyErr.Error(), adminIP, "failed")
		return
	}
	if isCommandFailed(verifyRec) {
		errText := commandErrorText(verifyRec, "验证版本失败")
		a.failInstallStep(jobID, "verify-version", errText, &verifyRec, errors.New(errText))
		a.logAudit("openclaw.install", deviceID, errText, adminIP, "failed")
		return
	}
	version := parseVersionFromCommandOutput(verifyRec)
	_ = a.completeInstallStep(jobID, "verify-version", &verifyRec, "")

	if err := a.devices.UpdateOpenClawState(deviceID, true, version); err != nil {
		a.failInstallJob(jobID, "更新设备 OpenClaw 状态失败: "+err.Error())
		a.logAudit("openclaw.install", deviceID, err.Error(), adminIP, "failed")
		return
	}

	_ = a.imConfigs.update(jobID, func(job *configureIMResponse) {
		job.Status = "success"
		job.Error = ""
	})
	a.logAudit("openclaw.install", deviceID, "installed "+version, adminIP, "success")
}

func parseVersionFromCommandOutput(rec server.CommandRecord) string {
	text := strings.TrimSpace(rec.Stdout)
	if text == "" {
		text = strings.TrimSpace(rec.Stderr)
	}
	lines := strings.Split(text, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func (a *serverApp) completeInstallStep(jobID, stepKey string, rec *server.CommandRecord, stepError string) error {
	return a.imConfigs.update(jobID, func(job *configureIMResponse) {
		for i := range job.Steps {
			if job.Steps[i].Key != stepKey {
				continue
			}
			job.Steps[i].Status = "success"
			job.Steps[i].Error = stepError
			if rec != nil {
				job.Steps[i].CommandID = rec.ID
				job.Steps[i].Record = copyCommandRecordPtr(rec)
			}
			return
		}
	})
}

func (a *serverApp) failInstallStep(jobID, stepKey, stepError string, rec *server.CommandRecord, err error) {
	_ = a.imConfigs.update(jobID, func(job *configureIMResponse) {
		for i := range job.Steps {
			if job.Steps[i].Key != stepKey {
				continue
			}
			job.Steps[i].Status = "failed"
			job.Steps[i].Error = strings.TrimSpace(stepError)
			if rec != nil {
				job.Steps[i].CommandID = rec.ID
				job.Steps[i].Record = copyCommandRecordPtr(rec)
			}
		}
		job.Status = "failed"
		if err != nil {
			job.Error = err.Error()
		} else {
			job.Error = stepError
		}
	})
}

func (a *serverApp) failInstallJob(jobID, msg string) {
	_ = a.imConfigs.update(jobID, func(job *configureIMResponse) {
		job.Status = "failed"
		job.Error = strings.TrimSpace(msg)
	})
}
