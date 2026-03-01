package main

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/raystone-ai/clawmini/internal/openclaw"
	"github.com/raystone-ai/clawmini/internal/protocol"
	"github.com/raystone-ai/clawmini/internal/server"
)

const (
	commandPollInterval = 500 * time.Millisecond
)

type configureIMRequest struct {
	Platform    string `json:"platform"`
	Credentials struct {
		ID     string `json:"id"`
		Secret string `json:"secret"`
	} `json:"credentials"`
}

type configureIMResponse = server.IMConfigJob
type configureIMStep = server.IMConfigStep

type configureCommand struct {
	Key            string
	Title          string
	DisplayCommand string
	Command        string
	Args           []string
	Timeout        int
}

type configureIMJobStore struct {
	store *server.IMConfigJobStore
}

func newConfigureIMJobStore(db *sql.DB) *configureIMJobStore {
	return &configureIMJobStore{store: server.NewIMConfigJobStore(db)}
}

func (s *configureIMJobStore) EnsureSchema() error {
	return s.store.EnsureSchema()
}

func (s *configureIMJobStore) Start() {
	s.store.Start()
}

func (s *configureIMJobStore) Stop() {
	s.store.Stop()
}

func (s *configureIMJobStore) create(deviceID, platform string, steps []configureIMStep) (configureIMResponse, error) {
	return s.store.Create(deviceID, platform, steps)
}

func (s *configureIMJobStore) get(deviceID, id string) (configureIMResponse, error) {
	return s.store.Get(deviceID, id)
}

func (s *configureIMJobStore) update(id string, mutate func(job *configureIMResponse)) error {
	return s.store.Update(id, func(job *server.IMConfigJob) {
		mutate((*configureIMResponse)(job))
	})
}

func normalizeIMPlatform(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func isTerminalCommandStatus(status string) bool {
	return status == "completed" || status == "failed"
}

func isCommandFailed(rec server.CommandRecord) bool {
	if rec.Status != "completed" {
		return true
	}
	return rec.ExitCode != nil && *rec.ExitCode != 0
}

// isPluginAlreadyInstalled checks if a "plugin already exists" error is acceptable
func isPluginAlreadyInstalled(rec server.CommandRecord) bool {
	return strings.Contains(rec.Stderr, "plugin already exists") || strings.Contains(rec.Stdout, "plugin already exists")
}
func commandErrorText(rec server.CommandRecord, fallback string) string {
	if strings.TrimSpace(rec.Stderr) != "" {
		return rec.Stderr
	}
	if rec.ExitCode != nil && *rec.ExitCode != 0 {
		return fmt.Sprintf("command exited with code %d", *rec.ExitCode)
	}
	return fallback
}

func (a *serverApp) handleConfigureIM(w http.ResponseWriter, r *http.Request) {
	adminIP := clientIP(r.RemoteAddr)
	deviceID := strings.TrimSpace(chi.URLParam(r, "id"))
	if deviceID == "" {
		server.WriteError(w, http.StatusBadRequest, "invalid device id")
		a.logAudit("im.configure", deviceID, "invalid device id", adminIP, "failed")
		return
	}
	if _, err := a.devices.GetDevice(deviceID); err != nil {
		if err == server.ErrNotFound {
			server.WriteError(w, http.StatusNotFound, "device not found")
			a.logAudit("im.configure", deviceID, "device not found", adminIP, "failed")
			return
		}
		a.writeInternalError(w, "load device before configure im", err)
		a.logAudit("im.configure", deviceID, err.Error(), adminIP, "failed")
		return
	}
	if !a.requireDeviceAccess(w, r, deviceID) {
		a.logAudit("im.configure", deviceID, "forbidden", adminIP, "forbidden")
		return
	}

	var req configureIMRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		if isBodyTooLarge(err) {
			server.WriteError(w, http.StatusRequestEntityTooLarge, "request too large")
			a.logAudit("im.configure", deviceID, "request too large", adminIP, "failed")
			return
		}
		server.WriteError(w, http.StatusBadRequest, "invalid json")
		a.logAudit("im.configure", deviceID, "invalid json", adminIP, "failed")
		return
	}

	platform := normalizeIMPlatform(req.Platform)
	credID := strings.TrimSpace(req.Credentials.ID)
	credSecret := strings.TrimSpace(req.Credentials.Secret)
	if credID == "" || credSecret == "" {
		server.WriteError(w, http.StatusBadRequest, "credentials are required")
		a.logAudit("im.configure", deviceID, "credentials are required", adminIP, "failed")
		return
	}

	steps, err := initialConfigureSteps(platform)
	if err != nil {
		server.WriteError(w, http.StatusBadRequest, err.Error())
		a.logAudit("im.configure", deviceID, err.Error(), adminIP, "failed")
		return
	}

	job, err := a.imConfigs.create(deviceID, platform, steps)
	if err != nil {
		a.writeInternalError(w, "create configure-im job", err)
		a.logAudit("im.configure", deviceID, err.Error(), adminIP, "failed")
		return
	}

	go a.runConfigureIMJob(job.ID, deviceID, platform, credID, credSecret)
	a.logAudit("im.configure", deviceID, "platform="+platform, adminIP, "accepted")
	server.WriteJSON(w, http.StatusAccepted, job)
}

func (a *serverApp) handleGetConfigureIM(w http.ResponseWriter, r *http.Request) {
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
			server.WriteError(w, http.StatusNotFound, "configure-im job not found")
			return
		}
		a.writeInternalError(w, "get configure-im job", err)
		return
	}
	server.WriteJSON(w, http.StatusOK, job)
}

func initialConfigureSteps(platform string) ([]configureIMStep, error) {
	switch platform {
	case "dingtalk":
		return []configureIMStep{
			{Key: "install-plugin", Title: "安装插件", DisplayCommand: "openclaw plugins install clawdbot-dingtalk", Status: "pending"},
			{Key: "set-client-id", Title: "配置 ClientID", DisplayCommand: "openclaw config set plugins.entries.clawdbot-dingtalk.clientId <已填写>", Status: "pending"},
			{Key: "set-client-secret", Title: "配置 ClientSecret", DisplayCommand: "openclaw config set plugins.entries.clawdbot-dingtalk.clientSecret ******", Status: "pending"},
			{Key: "enable-ai-card", Title: "启用 AI Card", DisplayCommand: "openclaw config set plugins.entries.clawdbot-dingtalk.aiCard.enabled true", Status: "pending"},
			{Key: "restart-gateway", Title: "重启 Gateway", DisplayCommand: "openclaw gateway restart", Status: "pending"},
		}, nil
	case "feishu":
		return []configureIMStep{
			{Key: "install-feishu", Title: "安装 Feishu 插件", DisplayCommand: "openclaw plugins install @anthropic-ai/feishu", Status: "pending"},
			{Key: "install-lark", Title: "安装 Lark 插件（回退）", DisplayCommand: "openclaw plugins install @anthropic-ai/lark", Status: "pending"},
			{Key: "set-app-id", Title: "配置 AppID", DisplayCommand: "openclaw config set plugins.entries.@anthropic-ai/feishu.appId <已填写>", Status: "pending"},
			{Key: "set-app-secret", Title: "配置 AppSecret", DisplayCommand: "openclaw config set plugins.entries.@anthropic-ai/feishu.appSecret ******", Status: "pending"},
			{Key: "restart-gateway", Title: "重启 Gateway", DisplayCommand: "openclaw gateway restart", Status: "pending"},
		}, nil
	default:
		return nil, errors.New("unsupported platform")
	}
}

func (a *serverApp) runConfigureIMJob(jobID, deviceID, platform, credentialID, credentialSecret string) {
	_ = a.imConfigs.update(jobID, func(job *configureIMResponse) {
		job.Status = "running"
		job.Error = ""
	})

	var runErr error
	switch platform {
	case "dingtalk":
		runErr = a.runDingTalkConfigure(jobID, deviceID, credentialID, credentialSecret)
	case "feishu":
		runErr = a.runFeishuConfigure(jobID, deviceID, credentialID, credentialSecret)
	default:
		runErr = errors.New("unsupported platform")
	}

	if runErr != nil {
		_ = a.imConfigs.update(jobID, func(job *configureIMResponse) {
			job.Status = "failed"
			job.Error = runErr.Error()
		})
		return
	}

	_ = a.imConfigs.update(jobID, func(job *configureIMResponse) {
		job.Status = "success"
		job.Error = ""
	})
}

func (a *serverApp) runDingTalkConfigure(jobID, deviceID, clientID, clientSecret string) error {
	commands := []configureCommand{
		{
			Key:            "install-plugin",
			Title:          "安装插件",
			DisplayCommand: "openclaw plugins install clawdbot-dingtalk",
			Command:        "openclaw",
			Args:           []string{"plugins", "install", "clawdbot-dingtalk"},
			Timeout:        600,
		},
		{
			Key:            "configure-plugin",
			Title:          "配置钉钉凭证",
			DisplayCommand: "写入 clientId / clientSecret / aiCard 到 openclaw.json",
			Command:        "bash",
			Args:           []string{"-lc", `python3 -c "
import json, sys
p = '/root/.openclaw/openclaw.json'
try:
    with open(p) as f: c = json.load(f)
except: c = {}
e = c.setdefault('plugins',{}).setdefault('entries',{}).setdefault('clawdbot-dingtalk',{})
e['enabled'] = True
e['clientId'] = '` + clientID + `'
e['clientSecret'] = '` + clientSecret + `'
e['aiCard'] = {'enabled': True}
with open(p,'w') as f: json.dump(c, f, indent=2)
print('OK')
"`},
			Timeout:        15,
		},
		{
			Key:            "restart-gateway",
			Title:          "重启 Gateway",
			DisplayCommand: "openclaw gateway restart",
			Command:        "openclaw",
			Args:           []string{"gateway", "restart"},
			Timeout:        30,
		},
	}

	for _, step := range commands {
		if err := a.runConfigureStep(jobID, deviceID, step); err != nil {
			return err
		}
	}

	return nil
}

func (a *serverApp) runFeishuConfigure(jobID, deviceID, appID, appSecret string) error {
	pluginName := "@anthropic-ai/feishu"
	if err := a.runConfigureStep(jobID, deviceID, configureCommand{
		Key:            "install-feishu",
		Title:          "安装 Feishu 插件",
		DisplayCommand: "openclaw plugins install @anthropic-ai/feishu",
		Command:        "openclaw",
		Args:           []string{"plugins", "install", "@anthropic-ai/feishu"},
		Timeout:        600,
	}); err != nil {
		pluginName = "@anthropic-ai/lark"
		if fallbackErr := a.runConfigureStep(jobID, deviceID, configureCommand{
			Key:            "install-lark",
			Title:          "安装 Lark 插件（回退）",
			DisplayCommand: "openclaw plugins install @anthropic-ai/lark",
			Command:        "openclaw",
			Args:           []string{"plugins", "install", "@anthropic-ai/lark"},
			Timeout:        600,
		}); fallbackErr != nil {
			return fallbackErr
		}
		_ = a.imConfigs.update(jobID, func(job *configureIMResponse) {
			for i := range job.Steps {
				if job.Steps[i].Key == "install-feishu" {
					job.Steps[i].Status = "skipped"
					job.Steps[i].Error = "未安装到 @anthropic-ai/feishu，已回退到 @anthropic-ai/lark"
					break
				}
			}
		})
	} else {
		_ = a.imConfigs.update(jobID, func(job *configureIMResponse) {
			for i := range job.Steps {
				if job.Steps[i].Key == "install-lark" {
					job.Steps[i].Status = "skipped"
					job.Steps[i].Error = ""
					break
				}
			}
		})
	}

	_ = a.imConfigs.update(jobID, func(job *configureIMResponse) {
		job.Plugin = pluginName
		for i := range job.Steps {
			switch job.Steps[i].Key {
			case "set-app-id":
				job.Steps[i].DisplayCommand = fmt.Sprintf("openclaw config set plugins.entries.%s.appId <已填写>", pluginName)
			case "set-app-secret":
				job.Steps[i].DisplayCommand = fmt.Sprintf("openclaw config set plugins.entries.%s.appSecret ******", pluginName)
			}
		}
	})

	if err := a.runConfigureStep(jobID, deviceID, configureCommand{
		Key:            "set-app-id",
		Title:          "配置 AppID",
		DisplayCommand: fmt.Sprintf("openclaw config set plugins.entries.%s.appId <已填写>", pluginName),
		Command:        "openclaw",
		Args:           []string{"config", "set", fmt.Sprintf("plugins.entries.%s.appId", pluginName), appID},
		Timeout:        15,
	}); err != nil {
		return err
	}

	if err := a.runConfigureStep(jobID, deviceID, configureCommand{
		Key:            "set-app-secret",
		Title:          "配置 AppSecret",
		DisplayCommand: fmt.Sprintf("openclaw config set plugins.entries.%s.appSecret ******", pluginName),
		Command:        "openclaw",
		Args:           []string{"config", "set", fmt.Sprintf("plugins.entries.%s.appSecret", pluginName), appSecret},
		Timeout:        15,
	}); err != nil {
		return err
	}

	if err := a.runConfigureStep(jobID, deviceID, configureCommand{
		Key:            "restart-gateway",
		Title:          "重启 Gateway",
		DisplayCommand: "openclaw gateway restart",
		Command:        "openclaw",
		Args:           []string{"gateway", "restart"},
		Timeout:        30,
	}); err != nil {
		return err
	}

	return nil
}

func (a *serverApp) runConfigureStep(jobID, deviceID string, step configureCommand) error {
	if !openclaw.ValidateDispatchCommand(step.Command, step.Args) {
		errText := "command not allowed"
		_ = a.imConfigs.update(jobID, func(job *configureIMResponse) {
			for i := range job.Steps {
				if job.Steps[i].Key != step.Key {
					continue
				}
				job.Steps[i].Status = "failed"
				job.Steps[i].Error = errText
				return
			}
		})
		return errors.New(errText)
	}

	_ = a.imConfigs.update(jobID, func(job *configureIMResponse) {
		for i := range job.Steps {
			if job.Steps[i].Key != step.Key {
				continue
			}
			job.Steps[i].Status = "running"
			job.Steps[i].Error = ""
			return
		}
	})

	rec, err := a.dispatchAndWaitCommand(deviceID, step.Command, step.Args, step.Timeout)
	if err != nil {
		errText := strings.TrimSpace(err.Error())
		if errText == "" {
			errText = step.Title + "执行失败"
		}
		_ = a.imConfigs.update(jobID, func(job *configureIMResponse) {
			for i := range job.Steps {
				if job.Steps[i].Key != step.Key {
					continue
				}
				job.Steps[i].Status = "failed"
				job.Steps[i].CommandID = rec.ID
				job.Steps[i].Record = copyCommandRecordPtr(&rec)
				job.Steps[i].Error = errText
				return
			}
		})
		return errors.New(errText)
	}

	if isCommandFailed(rec) && !isPluginAlreadyInstalled(rec) {
		errText := commandErrorText(rec, step.Title+"执行失败")
		_ = a.imConfigs.update(jobID, func(job *configureIMResponse) {
			for i := range job.Steps {
				if job.Steps[i].Key != step.Key {
					continue
				}
				job.Steps[i].Status = "failed"
				job.Steps[i].CommandID = rec.ID
				job.Steps[i].Record = copyCommandRecordPtr(&rec)
				job.Steps[i].Error = errText
				return
			}
		})
		return errors.New(errText)
	}

	_ = a.imConfigs.update(jobID, func(job *configureIMResponse) {
		for i := range job.Steps {
			if job.Steps[i].Key != step.Key {
				continue
			}
			job.Steps[i].Status = "success"
			job.Steps[i].CommandID = rec.ID
			job.Steps[i].Record = copyCommandRecordPtr(&rec)
			job.Steps[i].Error = ""
			return
		}
	})

	return nil
}

func copyCommandRecordPtr(in *server.CommandRecord) *server.CommandRecord {
	if in == nil {
		return nil
	}
	copied := *in
	copied.Args = append([]string(nil), in.Args...)
	return &copied
}

func (a *serverApp) dispatchAndWaitCommand(deviceID, command string, args []string, timeout int) (server.CommandRecord, error) {
	if timeout <= 0 {
		timeout = 60
	}
	if requiresGatewayLinger(command, args) {
		username := a.lookupDeviceUsername(deviceID)
		lingerRec, lingerErr := a.ensureLingerEnabled("", deviceID, "", "", username)
		if lingerErr != nil {
			return lingerRec, lingerErr
		}
	}

	redactedArgs := server.RedactSensitiveArgs(command, args)
	rec, err := a.commands.Create(deviceID, command, redactedArgs, timeout)
	if err != nil {
		return server.CommandRecord{}, err
	}

	msg := protocol.CommandPayload{
		CommandID: rec.ID,
		Command:   command,
		Args:      args,
		Timeout:   timeout,
	}
	if err := a.hub.DispatchCommand(deviceID, msg); err != nil {
		_ = a.commands.MarkFailed(rec.ID, err.Error())
		updated, reloadErr := a.commands.GetByDeviceAndID(deviceID, rec.ID)
		if reloadErr == nil {
			return updated, err
		}
		return rec, err
	}

	deadline := time.Now().Add(time.Duration(max(timeout, 30)+30) * time.Second)
	for {
		updated, getErr := a.commands.GetByDeviceAndID(deviceID, rec.ID)
		if getErr != nil {
			return rec, getErr
		}
		if isTerminalCommandStatus(updated.Status) {
			return updated, nil
		}
		if time.Now().After(deadline) {
			timeoutErr := fmt.Errorf("command timed out: %s %s", command, strings.Join(args, " "))
			_ = a.commands.MarkFailed(rec.ID, timeoutErr.Error())
			finalRecord, finalErr := a.commands.GetByDeviceAndID(deviceID, rec.ID)
			if finalErr == nil {
				return finalRecord, timeoutErr
			}
			return updated, timeoutErr
		}
		time.Sleep(commandPollInterval)
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
