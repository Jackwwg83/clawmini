package main

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/raystone-ai/clawmini/internal/server"
)

func (a *serverApp) handleGetMe(w http.ResponseWriter, r *http.Request) {
	user, ok := a.currentUserOrUnauthorized(w, r)
	if !ok {
		return
	}
	detail, err := a.users.GetUserDetail(user.ID)
	if err != nil {
		if err == server.ErrNotFound {
			server.WriteError(w, http.StatusNotFound, "user not found")
			return
		}
		a.writeInternalError(w, "get me", err)
		return
	}
	server.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"user":    detail.User,
		"devices": detail.Devices,
	})
}

func (a *serverApp) handleChangeMyPassword(w http.ResponseWriter, r *http.Request) {
	user, ok := a.currentUserOrUnauthorized(w, r)
	if !ok {
		return
	}
	var req struct {
		OldPassword string `json:"oldPassword"`
		NewPassword string `json:"newPassword"`
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		if isBodyTooLarge(err) {
			server.WriteError(w, http.StatusRequestEntityTooLarge, "request too large")
			return
		}
		server.WriteError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if strings.TrimSpace(req.NewPassword) == "" || strings.TrimSpace(req.OldPassword) == "" {
		server.WriteError(w, http.StatusBadRequest, "oldPassword and newPassword are required")
		return
	}
	valid, err := a.users.VerifyPassword(user.ID, req.OldPassword)
	if err != nil {
		a.writeInternalError(w, "verify current password", err)
		return
	}
	if !valid {
		server.WriteError(w, http.StatusUnauthorized, "old password is incorrect")
		return
	}
	if err := a.users.SetPassword(user.ID, req.NewPassword); err != nil {
		a.writeInternalError(w, "update own password", err)
		return
	}
	server.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *serverApp) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username    string `json:"username"`
		Password    string `json:"password"`
		Role        string `json:"role"`
		DisplayName string `json:"displayName"`
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		if isBodyTooLarge(err) {
			server.WriteError(w, http.StatusRequestEntityTooLarge, "request too large")
			return
		}
		server.WriteError(w, http.StatusBadRequest, "invalid json")
		return
	}

	created, err := a.users.CreateUser(req.Username, req.Password, req.Role, req.DisplayName)
	if err != nil {
		if err == server.ErrConflict {
			server.WriteError(w, http.StatusConflict, "username already exists")
			return
		}
		if strings.Contains(err.Error(), "required") || strings.Contains(err.Error(), "role") {
			server.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		a.writeInternalError(w, "create user", err)
		return
	}
	server.WriteJSON(w, http.StatusCreated, created)
}

func (a *serverApp) handleListUsers(w http.ResponseWriter, _ *http.Request) {
	users, err := a.users.ListUsers()
	if err != nil {
		a.writeInternalError(w, "list users", err)
		return
	}
	server.WriteJSON(w, http.StatusOK, map[string]interface{}{"users": users})
}

func (a *serverApp) handleGetUser(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	detail, err := a.users.GetUserDetail(id)
	if err != nil {
		if err == server.ErrNotFound {
			server.WriteError(w, http.StatusNotFound, "user not found")
			return
		}
		a.writeInternalError(w, "get user", err)
		return
	}
	server.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"user":    detail.User,
		"devices": detail.Devices,
	})
}

func (a *serverApp) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		server.WriteError(w, http.StatusBadRequest, "invalid user id")
		return
	}
	var req struct {
		DisplayName *string `json:"displayName"`
		Role        *string `json:"role"`
		Password    *string `json:"password"`
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		if isBodyTooLarge(err) {
			server.WriteError(w, http.StatusRequestEntityTooLarge, "request too large")
			return
		}
		server.WriteError(w, http.StatusBadRequest, "invalid json")
		return
	}

	existing, err := a.users.GetUserByID(id)
	if err != nil {
		if err == server.ErrNotFound {
			server.WriteError(w, http.StatusNotFound, "user not found")
			return
		}
		a.writeInternalError(w, "load user before update", err)
		return
	}
	if req.Role != nil {
		nextRole := strings.ToLower(strings.TrimSpace(*req.Role))
		if existing.Role == server.RoleAdmin && nextRole == server.RoleUser {
			adminCount, err := a.users.CountAdmins()
			if err != nil {
				a.writeInternalError(w, "count admins before role update", err)
				return
			}
			if adminCount <= 1 {
				server.WriteError(w, http.StatusBadRequest, "cannot demote the last admin")
				return
			}
		}
	}

	updated, err := a.users.UpdateUser(id, server.UpdateUserInput{
		DisplayName: req.DisplayName,
		Role:        req.Role,
	})
	if err != nil {
		if err == server.ErrNotFound {
			server.WriteError(w, http.StatusNotFound, "user not found")
			return
		}
		if strings.Contains(err.Error(), "role") {
			server.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		a.writeInternalError(w, "update user", err)
		return
	}

	if req.Password != nil && strings.TrimSpace(*req.Password) != "" {
		if err := a.users.SetPassword(id, *req.Password); err != nil {
			a.writeInternalError(w, "update user password", err)
			return
		}
		updated, err = a.users.GetUserByID(id)
		if err != nil {
			a.writeInternalError(w, "reload user", err)
			return
		}
	}

	server.WriteJSON(w, http.StatusOK, updated)
}

func (a *serverApp) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	currentUser, ok := a.currentUserOrUnauthorized(w, r)
	if !ok {
		return
	}
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		server.WriteError(w, http.StatusBadRequest, "invalid user id")
		return
	}
	if id == currentUser.ID {
		server.WriteError(w, http.StatusBadRequest, "cannot delete current user")
		return
	}
	existing, err := a.users.GetUserByID(id)
	if err != nil {
		if err == server.ErrNotFound {
			server.WriteError(w, http.StatusNotFound, "user not found")
			return
		}
		a.writeInternalError(w, "load user before delete", err)
		return
	}
	if existing.Role == server.RoleAdmin {
		adminCount, err := a.users.CountAdmins()
		if err != nil {
			a.writeInternalError(w, "count admins before delete", err)
			return
		}
		if adminCount <= 1 {
			server.WriteError(w, http.StatusBadRequest, "cannot delete the last admin")
			return
		}
	}
	if err := a.users.DeleteUser(id); err != nil {
		if err == server.ErrNotFound {
			server.WriteError(w, http.StatusNotFound, "user not found")
			return
		}
		a.writeInternalError(w, "delete user", err)
		return
	}
	server.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *serverApp) handleBindUserDevice(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(chi.URLParam(r, "id"))
	var req struct {
		DeviceID string `json:"deviceId"`
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		if isBodyTooLarge(err) {
			server.WriteError(w, http.StatusRequestEntityTooLarge, "request too large")
			return
		}
		server.WriteError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if err := a.users.BindDevice(userID, req.DeviceID); err != nil {
		switch err {
		case server.ErrNotFound:
			server.WriteError(w, http.StatusNotFound, "user or device not found")
			return
		case server.ErrConflict:
			server.WriteError(w, http.StatusConflict, "device already bound")
			return
		default:
			a.writeInternalError(w, "bind user device", err)
			return
		}
	}
	server.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *serverApp) handleUnbindUserDevice(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(chi.URLParam(r, "id"))
	deviceID := strings.TrimSpace(chi.URLParam(r, "deviceId"))
	if err := a.users.UnbindDevice(userID, deviceID); err != nil {
		if err == server.ErrNotFound {
			server.WriteError(w, http.StatusNotFound, "binding not found")
			return
		}
		a.writeInternalError(w, "unbind user device", err)
		return
	}
	server.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
