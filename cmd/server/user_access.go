package main

import (
	"net/http"
	"strings"

	"github.com/raystone-ai/clawmini/internal/server"
)

func (a *serverApp) currentUserOrUnauthorized(w http.ResponseWriter, r *http.Request) (server.AuthUser, bool) {
	user, ok := server.UserFromRequest(r)
	if !ok {
		server.WriteError(w, http.StatusUnauthorized, "unauthorized")
		return server.AuthUser{}, false
	}
	return user, true
}

func (a *serverApp) requireDeviceAccess(w http.ResponseWriter, r *http.Request, deviceID string) bool {
	user, ok := a.currentUserOrUnauthorized(w, r)
	if !ok {
		return false
	}
	if user.Role == server.RoleAdmin {
		return true
	}
	ok, err := a.users.IsDeviceBoundToUser(user.ID, strings.TrimSpace(deviceID))
	if err != nil {
		a.writeInternalError(w, "check device access", err)
		return false
	}
	if !ok {
		server.WriteError(w, http.StatusForbidden, "forbidden")
		return false
	}
	return true
}

func (a *serverApp) filterAccessibleDeviceIDs(user server.AuthUser, deviceIDs []string) (map[string]bool, error) {
	if user.Role == server.RoleAdmin {
		out := make(map[string]bool, len(deviceIDs))
		for _, id := range deviceIDs {
			trimmed := strings.TrimSpace(id)
			if trimmed != "" {
				out[trimmed] = true
			}
		}
		return out, nil
	}
	return a.users.FilterBoundDeviceIDs(user.ID, deviceIDs)
}
