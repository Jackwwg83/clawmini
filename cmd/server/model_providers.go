package main

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/raystone-ai/clawmini/internal/server"
)

type ModelProviderRequest struct {
	Name       string `json:"name"`
	BaseURL    string `json:"baseUrl"`
	APIKey     string `json:"apiKey"`
	APIType    string `json:"apiType"`
	AuthHeader *bool  `json:"authHeader,omitempty"`
}

var providerNameRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

func (a *serverApp) handleGetModelProviders(w http.ResponseWriter, r *http.Request) {
	deviceID := chi.URLParam(r, "id")

	script := `python3 -c "
import json
p = '/root/.openclaw/openclaw.json'
try:
    with open(p) as f: c = json.load(f)
    providers = c.get('models', {}).get('providers', {})
    result = {}
    for name, cfg in providers.items():
        result[name] = {
            'baseUrl': cfg.get('baseUrl', ''),
            'apiKey': '****' + cfg.get('apiKey', '')[-4:] if len(cfg.get('apiKey', '')) > 4 else '****',
            'apiType': cfg.get('api', ''),
            'models': len(cfg.get('models', [])),
            'authHeader': cfg.get('authHeader', True),
            'hasHeaders': bool(cfg.get('headers', {}))
        }
    print(json.dumps(result))
except Exception as e:
    print(json.dumps({'error': str(e)}))
"`

	rec, err := a.dispatchAndWaitCommand(deviceID, "bash", []string{"-lc", script}, 15)
	if err != nil {
		if isLikelyDeviceOfflineError(err) {
			server.WriteError(w, http.StatusBadGateway, "device offline")
			return
		}
		server.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	output := strings.TrimSpace(rec.Stdout)
	if output == "" {
		output = "{}"
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(output))
}

func (a *serverApp) handleUpsertModelProvider(w http.ResponseWriter, r *http.Request) {
	deviceID := chi.URLParam(r, "id")

	var req ModelProviderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		server.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name == "" || !providerNameRegex.MatchString(req.Name) {
		server.WriteError(w, http.StatusBadRequest, "invalid provider name (alphanumeric, dash, underscore only)")
		return
	}
	if req.BaseURL == "" {
		server.WriteError(w, http.StatusBadRequest, "baseUrl is required")
		return
	}
	if req.APIKey == "" {
		server.WriteError(w, http.StatusBadRequest, "apiKey is required")
		return
	}
	if req.APIType == "" {
		req.APIType = "openai-chat"
	}

	// Escape single quotes in values for safe python string embedding
	name := strings.ReplaceAll(req.Name, "'", "")
	baseURL := strings.ReplaceAll(req.BaseURL, "'", "")
	apiKey := strings.ReplaceAll(req.APIKey, "'", "")
	apiType := strings.ReplaceAll(req.APIType, "'", "")

	// Determine authHeader behavior
	authHeaderPython := "True"
	headersPython := "{}"
	if req.AuthHeader != nil && !*req.AuthHeader {
		authHeaderPython = "False"
		// When authHeader is disabled, pass API key via x-api-key header
		headersPython = "{'x-api-key': '" + apiKey + "'}"
	}

	script := `python3 -c "
import json
p = '/root/.openclaw/openclaw.json'
try:
    with open(p) as f: c = json.load(f)
except: c = {}
m = c.setdefault('models', {})
m['mode'] = 'merge'
providers = m.setdefault('providers', {})
existing = providers.get('` + name + `', {})
providers['` + name + `'] = {
    'baseUrl': '` + baseURL + `',
    'apiKey': '` + apiKey + `',
    'api': '` + apiType + `',
    'authHeader': ` + authHeaderPython + `,
    'headers': ` + headersPython + `,
    'models': existing.get('models', [])
}
with open(p, 'w') as f: json.dump(c, f, indent=2)
print('OK')
"`

	rec, err := a.dispatchAndWaitCommand(deviceID, "bash", []string{"-lc", script}, 15)
	if err != nil {
		if isLikelyDeviceOfflineError(err) {
			server.WriteError(w, http.StatusBadGateway, "device offline")
			return
		}
		server.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if rec.ExitCode != nil && *rec.ExitCode != 0 {
		server.WriteError(w, http.StatusInternalServerError, "failed to save provider config")
		return
	}

	a.logAudit("model-provider.upsert", deviceID, "provider="+req.Name, r.RemoteAddr, "ok")
	server.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *serverApp) handleDeleteModelProvider(w http.ResponseWriter, r *http.Request) {
	deviceID := chi.URLParam(r, "id")
	name := chi.URLParam(r, "name")

	if name == "" || !providerNameRegex.MatchString(name) {
		server.WriteError(w, http.StatusBadRequest, "invalid provider name")
		return
	}

	script := `python3 -c "
import json
p = '/root/.openclaw/openclaw.json'
with open(p) as f: c = json.load(f)
providers = c.get('models', {}).get('providers', {})
if '` + name + `' in providers:
    del providers['` + name + `']
    with open(p, 'w') as f: json.dump(c, f, indent=2)
    print('OK')
else:
    print('NOT_FOUND')
"`

	rec, err := a.dispatchAndWaitCommand(deviceID, "bash", []string{"-lc", script}, 15)
	if err != nil {
		if isLikelyDeviceOfflineError(err) {
			server.WriteError(w, http.StatusBadGateway, "device offline")
			return
		}
		server.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if strings.TrimSpace(rec.Stdout) == "NOT_FOUND" {
		server.WriteError(w, http.StatusNotFound, "provider not found")
		return
	}

	a.logAudit("model-provider.delete", deviceID, "provider="+name, r.RemoteAddr, "ok")
	server.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
