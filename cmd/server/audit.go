package main

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/raystone-ai/clawmini/internal/server"
)

func (a *serverApp) logAudit(action, targetDeviceID, detail, adminIP, result string) {
	if a.auditLogs == nil {
		return
	}
	if err := a.auditLogs.Log(action, targetDeviceID, detail, adminIP, result); err != nil {
		// Keep request path non-fatal when audit insert fails.
	}
}

func (a *serverApp) handleGetAuditLog(w http.ResponseWriter, r *http.Request) {
	limit, err := parseQueryInt(r, "limit", 50)
	if err != nil {
		server.WriteError(w, http.StatusBadRequest, "invalid limit")
		return
	}
	offset, err := parseQueryInt(r, "offset", 0)
	if err != nil {
		server.WriteError(w, http.StatusBadRequest, "invalid offset")
		return
	}
	fromUnix, err := parseQueryInt64(r, "from", 0)
	if err != nil {
		server.WriteError(w, http.StatusBadRequest, "invalid from")
		return
	}
	toUnix, err := parseQueryInt64(r, "to", 0)
	if err != nil {
		server.WriteError(w, http.StatusBadRequest, "invalid to")
		return
	}

	page, err := a.auditLogs.List(server.AuditLogQuery{
		Limit:    limit,
		Offset:   offset,
		DeviceID: strings.TrimSpace(r.URL.Query().Get("device_id")),
		Action:   strings.TrimSpace(r.URL.Query().Get("action")),
		FromUnix: fromUnix,
		ToUnix:   toUnix,
	})
	if err != nil {
		a.writeInternalError(w, "list audit log", err)
		return
	}
	server.WriteJSON(w, http.StatusOK, page)
}

func parseQueryInt(r *http.Request, key string, fallback int) (int, error) {
	value := strings.TrimSpace(r.URL.Query().Get(key))
	if value == "" {
		return fallback, nil
	}
	return strconv.Atoi(value)
}

func parseQueryInt64(r *http.Request, key string, fallback int64) (int64, error) {
	value := strings.TrimSpace(r.URL.Query().Get(key))
	if value == "" {
		return fallback, nil
	}
	return strconv.ParseInt(value, 10, 64)
}
