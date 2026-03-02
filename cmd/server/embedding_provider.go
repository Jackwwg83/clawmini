package main

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/raystone-ai/clawmini/internal/server"
)

type EmbeddingProviderRequest struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	APIKey   string `json:"apiKey"`
	BaseURL  string `json:"baseUrl"`
}

func (a *serverApp) handleGetEmbeddingProvider(w http.ResponseWriter, r *http.Request) {
	deviceID := chi.URLParam(r, "id")

	script := `python3 -c "
import json
p = '/root/.openclaw/openclaw.json'
try:
    with open(p) as f: c = json.load(f)
    emb = c.get('models', {}).get('embedding', {})
    if emb and emb.get('provider'):
        result = {
            'provider': emb.get('provider', ''),
            'model': emb.get('model', ''),
            'apiKey': '****' + emb.get('apiKey', '')[-4:] if len(emb.get('apiKey', '')) > 4 else '****',
            'baseUrl': emb.get('baseUrl', '')
        }
        print(json.dumps({'embedding': result}))
    else:
        print(json.dumps({'embedding': None}))
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
		output = `{"embedding":null}`
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(output))
}

func (a *serverApp) handleUpsertEmbeddingProvider(w http.ResponseWriter, r *http.Request) {
	deviceID := chi.URLParam(r, "id")

	var req EmbeddingProviderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		server.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Provider == "" {
		server.WriteError(w, http.StatusBadRequest, "provider is required")
		return
	}
	if req.Model == "" {
		server.WriteError(w, http.StatusBadRequest, "model is required")
		return
	}
	if req.BaseURL == "" {
		server.WriteError(w, http.StatusBadRequest, "baseUrl is required")
		return
	}

	provider := strings.ReplaceAll(req.Provider, "'", "")
	model := strings.ReplaceAll(req.Model, "'", "")
	apiKey := strings.ReplaceAll(req.APIKey, "'", "")
	baseURL := strings.ReplaceAll(req.BaseURL, "'", "")

	// If apiKey is empty, preserve the existing one
	apiKeyPython := "'" + apiKey + "'"
	if apiKey == "" {
		apiKeyPython = "existing.get('apiKey', '')"
	}

	script := `python3 -c "
import json
p = '/root/.openclaw/openclaw.json'
try:
    with open(p) as f: c = json.load(f)
except: c = {}
m = c.setdefault('models', {})
existing = m.get('embedding', {})
m['embedding'] = {
    'provider': '` + provider + `',
    'model': '` + model + `',
    'apiKey': ` + apiKeyPython + `,
    'baseUrl': '` + baseURL + `'
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
		server.WriteError(w, http.StatusInternalServerError, "failed to save embedding config")
		return
	}

	a.logAudit("embedding-provider.upsert", deviceID, "provider="+req.Provider, r.RemoteAddr, "ok")
	server.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
