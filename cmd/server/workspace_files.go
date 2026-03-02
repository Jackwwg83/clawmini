package main

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/raystone-ai/clawmini/internal/server"
)

type WorkspaceFileWriteRequest struct {
	Content string `json:"content"`
}

func (a *serverApp) handleListWorkspaceFiles(w http.ResponseWriter, r *http.Request) {
	deviceID := chi.URLParam(r, "id")

	script := `python3 -c "
import os, json
workspace = os.path.expanduser('~/.openclaw/workspace')
files = []
if os.path.isdir(workspace):
    for root, dirs, fnames in os.walk(workspace):
        for f in fnames:
            if f.endswith('.md'):
                files.append(os.path.relpath(os.path.join(root, f), workspace))
print(json.dumps({'files': sorted(files)}))
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
		output = `{"files":[]}`
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(output))
}

func (a *serverApp) handleReadWorkspaceFile(w http.ResponseWriter, r *http.Request) {
	deviceID := chi.URLParam(r, "id")
	filename := chi.URLParam(r, "filename")

	if filename == "" || strings.Contains(filename, "..") {
		server.WriteError(w, http.StatusBadRequest, "invalid filename")
		return
	}

	safeName := strings.ReplaceAll(filename, "'", "")

	script := `python3 -c "
import json, os, base64
workspace = os.path.expanduser('~/.openclaw/workspace')
fpath = os.path.join(workspace, '` + safeName + `')
fpath = os.path.realpath(fpath)
if not fpath.startswith(os.path.realpath(workspace)):
    print(json.dumps({'error': 'path traversal'}))
else:
    try:
        with open(fpath, 'r') as f:
            content = f.read()
        print(json.dumps({'content': content}))
    except FileNotFoundError:
        print(json.dumps({'error': 'not found'}))
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
		output = `{"error":"empty response"}`
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(output))
}

func (a *serverApp) handleWriteWorkspaceFile(w http.ResponseWriter, r *http.Request) {
	deviceID := chi.URLParam(r, "id")
	filename := chi.URLParam(r, "filename")

	if filename == "" || strings.Contains(filename, "..") {
		server.WriteError(w, http.StatusBadRequest, "invalid filename")
		return
	}

	var req WorkspaceFileWriteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		server.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	safeName := strings.ReplaceAll(filename, "'", "")
	b64Content := base64.StdEncoding.EncodeToString([]byte(req.Content))

	script := `python3 -c "
import base64, os, json
workspace = os.path.expanduser('~/.openclaw/workspace')
fpath = os.path.join(workspace, '` + safeName + `')
fpath = os.path.realpath(fpath)
if not fpath.startswith(os.path.realpath(workspace)):
    print(json.dumps({'error': 'path traversal'}))
else:
    os.makedirs(os.path.dirname(fpath), exist_ok=True)
    content = base64.b64decode('` + b64Content + `').decode('utf-8')
    with open(fpath, 'w') as f:
        f.write(content)
    print(json.dumps({'status': 'ok'}))
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
		server.WriteError(w, http.StatusInternalServerError, "failed to write file")
		return
	}

	a.logAudit("workspace-file.write", deviceID, "file="+filename, r.RemoteAddr, "ok")
	server.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *serverApp) handleDeleteWorkspaceFile(w http.ResponseWriter, r *http.Request) {
	deviceID := chi.URLParam(r, "id")
	filename := chi.URLParam(r, "filename")

	if filename == "" || strings.Contains(filename, "..") {
		server.WriteError(w, http.StatusBadRequest, "invalid filename")
		return
	}

	safeName := strings.ReplaceAll(filename, "'", "")

	script := `python3 -c "
import os, json
workspace = os.path.expanduser('~/.openclaw/workspace')
fpath = os.path.join(workspace, '` + safeName + `')
fpath = os.path.realpath(fpath)
if not fpath.startswith(os.path.realpath(workspace)):
    print(json.dumps({'error': 'path traversal'}))
elif os.path.exists(fpath):
    os.remove(fpath)
    print(json.dumps({'status': 'ok'}))
else:
    print(json.dumps({'error': 'not found'}))
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
	if strings.Contains(output, "not found") {
		server.WriteError(w, http.StatusNotFound, "file not found")
		return
	}

	a.logAudit("workspace-file.delete", deviceID, "file="+filename, r.RemoteAddr, "ok")
	server.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
