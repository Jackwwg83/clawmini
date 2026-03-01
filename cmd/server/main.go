package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/raystone-ai/clawmini/internal/openclaw"
	"github.com/raystone-ai/clawmini/internal/protocol"
	"github.com/raystone-ai/clawmini/internal/server"
	webassets "github.com/raystone-ai/clawmini/web"
)

type serverApp struct {
	auth       *server.TokenAuth
	users      *server.UserStore
	devices    *server.DeviceStore
	commands   *server.CommandStore
	joinTokens *server.JoinTokenStore
	batchJobs  *server.BatchJobStore
	auditLogs  *server.AuditLogStore
	hub        *server.Hub
	imConfigs  *configureIMJobStore
}

const maxJSONBodyBytes = 1 << 20

func main() {
	addr := flag.String("addr", ":18790", "server listen address")
	tlsCert := flag.String("tls-cert", "", "path to TLS certificate PEM file")
	tlsKey := flag.String("tls-key", "", "path to TLS private key PEM file")
	allowedOriginsFlag := flag.String("allowed-origins", "", "comma-separated allowed websocket origins; default is same-origin only")
	flag.Parse()

	deviceToken := strings.TrimSpace(os.Getenv("CLAWMINI_DEVICE_TOKEN"))
	if deviceToken == "" {
		log.Fatal("CLAWMINI_DEVICE_TOKEN is required")
	}

	dbPath := os.Getenv("CLAWMINI_DB_PATH")
	if dbPath == "" {
		dbPath = "./clawmini.db"
	}

	db, err := server.OpenSQLite(dbPath)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()

	adminTokenStore := server.NewAdminTokenStore(db)
	if err := adminTokenStore.EnsureSchema(); err != nil {
		log.Fatalf("ensure admin settings schema: %v", err)
	}

	jwtSecret, generatedSecret, err := resolveJWTSecret(adminTokenStore, os.Getenv("CLAWMINI_JWT_SECRET"))
	if err != nil {
		log.Fatalf("resolve JWT secret: %v", err)
	}
	if err := server.ValidateAuthConfig(deviceToken, jwtSecret); err != nil {
		log.Fatal(err)
	}
	if generatedSecret {
		log.Printf("WARNING: generated CLAWMINI JWT secret in SQLite app_settings; set CLAWMINI_JWT_SECRET in production")
	}

	deviceStore := server.NewDeviceStore(db)
	if err := deviceStore.EnsureSchema(); err != nil {
		log.Fatalf("ensure device schema: %v", err)
	}
	userStore := server.NewUserStore(db)
	if err := userStore.EnsureSchema(); err != nil {
		log.Fatalf("ensure user schema: %v", err)
	}
	createdDefaultAdmin, err := userStore.EnsureDefaultAdmin()
	if err != nil {
		log.Fatalf("ensure default admin user: %v", err)
	}
	if createdDefaultAdmin {
		log.Printf("WARNING: created default admin user admin/admin. Change this password immediately.")
	}
	auth := server.NewTokenAuth(deviceToken, jwtSecret, userStore)

	commandStore := server.NewCommandStore(db)
	if err := commandStore.EnsureSchema(); err != nil {
		log.Fatalf("ensure command schema: %v", err)
	}

	joinTokenStore := server.NewJoinTokenStore(db)
	if err := joinTokenStore.EnsureSchema(); err != nil {
		log.Fatalf("ensure join token schema: %v", err)
	}

	batchJobStore := server.NewBatchJobStore(db)
	if err := batchJobStore.EnsureSchema(); err != nil {
		log.Fatalf("ensure batch jobs schema: %v", err)
	}

	auditLogStore := server.NewAuditLogStore(db)
	if err := auditLogStore.EnsureSchema(); err != nil {
		log.Fatalf("ensure audit log schema: %v", err)
	}
	auditLogStore.Start()
	defer auditLogStore.Stop()

	hub := server.NewHub(deviceStore, commandStore, joinTokenStore, userStore, auth)
	if err := hub.SetAllowedOrigins(splitCSV(*allowedOriginsFlag)); err != nil {
		log.Fatalf("configure websocket allowed origins: %v", err)
	}
	hub.Start()
	defer hub.Stop()

	imConfigStore := newConfigureIMJobStore(db)
	if err := imConfigStore.EnsureSchema(); err != nil {
		log.Fatalf("ensure im config jobs schema: %v", err)
	}
	imConfigStore.Start()
	defer imConfigStore.Stop()

	app := &serverApp{
		auth:       auth,
		users:      userStore,
		devices:    deviceStore,
		commands:   commandStore,
		joinTokens: joinTokenStore,
		batchJobs:  batchJobStore,
		auditLogs:  auditLogStore,
		hub:        hub,
		imConfigs:  imConfigStore,
	}
	loginLimiter := newLoginRateLimiter(5, time.Minute)

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Post("/api/auth/login", loginLimiter.Middleware(app.handleLogin))
	r.Get("/ws", hub.HandleDeviceWS)
	r.Get("/api/ws", hub.HandleBrowserWS)
	r.Get("/install.sh", app.handleInstallScript)
	r.Get("/downloads/{file}", func(w http.ResponseWriter, r *http.Request) {
		fileName := chi.URLParam(r, "file")
		if fileName == "" || strings.Contains(fileName, "..") || strings.Contains(fileName, "/") {
			http.Error(w, "invalid", 400)
			return
		}
		path := "downloads/" + fileName
		http.ServeFile(w, r, path)
	})

	r.Route("/api", func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Use(auth.AuthMiddleware)
			r.Get("/me", app.handleGetMe)
			r.Put("/me/password", app.handleChangeMyPassword)
			r.Get("/devices", app.handleListDevices)
			r.Get("/devices/{id}", app.handleGetDevice)
			r.Delete("/devices/{id}", app.handleDeleteDevice)
			r.Post("/devices/{id}/exec", app.handleExec)
			r.Get("/devices/{id}/exec/{cmdId}", app.handleGetCommand)
			r.Post("/devices/{id}/install-openclaw", app.handleInstallOpenClaw)
			r.Get("/devices/{id}/install-openclaw/{jobId}", app.handleGetInstallOpenClaw)
			r.Post("/devices/{id}/configure-im", app.handleConfigureIM)
			r.Get("/devices/{id}/configure-im/{jobId}", app.handleGetConfigureIM)
			r.Post("/batch/exec", app.handleBatchExec)
			r.Get("/batch/{jobId}", app.handleGetBatchJob)
			r.Get("/audit-log", app.handleGetAuditLog)
		})
		r.Group(func(r chi.Router) {
			r.Use(auth.AuthMiddleware)
			r.Use(auth.AdminOnly)
			r.Post("/join-tokens", app.handleCreateJoinToken)
			r.Get("/join-tokens", app.handleListJoinTokens)
			r.Delete("/join-tokens/{id}", app.handleDeleteJoinToken)
			r.Post("/users", app.handleCreateUser)
			r.Get("/users", app.handleListUsers)
			r.Get("/users/{id}", app.handleGetUser)
			r.Put("/users/{id}", app.handleUpdateUser)
			r.Delete("/users/{id}", app.handleDeleteUser)
			r.Post("/users/{id}/devices", app.handleBindUserDevice)
			r.Delete("/users/{id}/devices/{deviceId}", app.handleUnbindUserDevice)
		})
	})

	spa := spaHandler()
	r.NotFound(func(w http.ResponseWriter, req *http.Request) {
		if strings.HasPrefix(req.URL.Path, "/api/") || req.URL.Path == "/ws" {
			http.NotFound(w, req)
			return
		}
		spa.ServeHTTP(w, req)
	})

	if (*tlsCert == "") != (*tlsKey == "") {
		log.Fatal("both --tls-cert and --tls-key must be set together")
	}

	if *tlsCert == "" && *tlsKey == "" {
		log.Printf("WARNING: TLS is not configured; traffic and tokens will be sent in plaintext HTTP")
	}

	httpServer := &http.Server{
		Addr:    *addr,
		Handler: r,
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	errCh := make(chan error, 1)
	go func() {
		if *tlsCert == "" && *tlsKey == "" {
			log.Printf("server listening on http://%s", *addr)
			errCh <- httpServer.ListenAndServe()
			return
		}
		log.Printf("server listening on https://%s", *addr)
		errCh <- httpServer.ListenAndServeTLS(*tlsCert, *tlsKey)
	}()

	select {
	case sig := <-sigCh:
		log.Printf("received signal %s, shutting down", sig)
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(ctx); err != nil {
		log.Printf("graceful shutdown failed: %v", err)
		if closeErr := httpServer.Close(); closeErr != nil {
			log.Printf("force close failed: %v", closeErr)
		}
	}
	app.imConfigs.Stop()
	hub.Stop()

	if err := <-errCh; err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

func (a *serverApp) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		if isBodyTooLarge(err) {
			server.WriteError(w, http.StatusRequestEntityTooLarge, "request too large")
			return
		}
		server.WriteError(w, http.StatusBadRequest, "invalid json")
		return
	}

	user, err := a.users.Authenticate(req.Username, req.Password)
	if err != nil {
		server.WriteError(w, http.StatusUnauthorized, "invalid username or password")
		return
	}
	token, err := a.auth.GenerateToken(user)
	if err != nil {
		a.writeInternalError(w, "generate login token", err)
		return
	}
	server.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"token": token,
		"user":  user,
	})
}

func (a *serverApp) handleListDevices(w http.ResponseWriter, r *http.Request) {
	user, ok := server.UserFromRequest(r)
	if !ok {
		server.WriteError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var devices []server.DeviceSnapshot
	var err error
	if user.Role == server.RoleAdmin {
		filterUserID := strings.TrimSpace(r.URL.Query().Get("userId"))
		if filterUserID != "" {
			if _, err := a.users.GetUserByID(filterUserID); err != nil {
				if err == server.ErrNotFound {
					server.WriteError(w, http.StatusNotFound, "user not found")
					return
				}
				a.writeInternalError(w, "load filter user", err)
				return
			}
			devices, err = a.users.ListDevicesByUser(filterUserID)
		} else {
			devices, err = a.devices.ListDevices()
		}
	} else {
		devices, err = a.users.ListDevicesByUser(user.ID)
	}
	if err != nil {
		a.writeInternalError(w, "list devices", err)
		return
	}
	server.WriteJSON(w, http.StatusOK, map[string]interface{}{"devices": devices})
}

func (a *serverApp) handleGetDevice(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	device, err := a.devices.GetDevice(id)
	if err != nil {
		if err == server.ErrNotFound {
			server.WriteError(w, http.StatusNotFound, "device not found")
			return
		}
		a.writeInternalError(w, "get device", err)
		return
	}
	if !a.requireDeviceAccess(w, r, id) {
		return
	}
	server.WriteJSON(w, http.StatusOK, device)
}

func (a *serverApp) handleDeleteDevice(w http.ResponseWriter, r *http.Request) {
	adminIP := clientIP(r.RemoteAddr)
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		server.WriteError(w, http.StatusBadRequest, "invalid device id")
		a.logAudit("device.delete", id, "invalid device id", adminIP, "failed")
		return
	}

	if _, err := a.devices.GetDevice(id); err != nil {
		if err == server.ErrNotFound {
			server.WriteError(w, http.StatusNotFound, "device not found")
			a.logAudit("device.delete", id, "device not found", adminIP, "failed")
			return
		}
		a.writeInternalError(w, "load device before delete", err)
		a.logAudit("device.delete", id, err.Error(), adminIP, "failed")
		return
	}
	if !a.requireDeviceAccess(w, r, id) {
		a.logAudit("device.delete", id, "forbidden", adminIP, "forbidden")
		return
	}

	a.hub.DisconnectDevice(id)
	if err := a.devices.DeleteDevice(id); err != nil {
		if err == server.ErrNotFound {
			server.WriteError(w, http.StatusNotFound, "device not found")
			a.logAudit("device.delete", id, "device not found", adminIP, "failed")
			return
		}
		a.writeInternalError(w, "delete device", err)
		a.logAudit("device.delete", id, err.Error(), adminIP, "failed")
		return
	}

	a.logAudit("device.delete", id, "deleted device", adminIP, "success")
	server.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *serverApp) handleCreateJoinToken(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Label          string `json:"label"`
		ExpiresInHours int    `json:"expiresInHours"`
		UserID         string `json:"userId"`
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		if isBodyTooLarge(err) {
			server.WriteError(w, http.StatusRequestEntityTooLarge, "request too large")
			return
		}
		server.WriteError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.ExpiresInHours <= 0 {
		server.WriteError(w, http.StatusBadRequest, "expiresInHours must be positive")
		return
	}
	userID := strings.TrimSpace(req.UserID)
	if userID != "" {
		if _, err := a.users.GetUserByID(userID); err != nil {
			if err == server.ErrNotFound {
				server.WriteError(w, http.StatusBadRequest, "user not found")
				return
			}
			a.writeInternalError(w, "validate join token user", err)
			return
		}
	}

	token, err := a.joinTokens.CreateToken(strings.TrimSpace(req.Label), time.Duration(req.ExpiresInHours)*time.Hour, userID)
	if err != nil {
		a.writeInternalError(w, "create join token", err)
		return
	}
	server.WriteJSON(w, http.StatusCreated, token)
}

func (a *serverApp) handleListJoinTokens(w http.ResponseWriter, _ *http.Request) {
	tokens, err := a.joinTokens.ListTokens()
	if err != nil {
		a.writeInternalError(w, "list join tokens", err)
		return
	}
	server.WriteJSON(w, http.StatusOK, map[string]interface{}{"tokens": tokens})
}

func (a *serverApp) handleDeleteJoinToken(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		server.WriteError(w, http.StatusBadRequest, "invalid token id")
		return
	}
	if err := a.joinTokens.DeleteToken(id); err != nil {
		if err == server.ErrNotFound {
			server.WriteError(w, http.StatusNotFound, "join token not found")
			return
		}
		a.writeInternalError(w, "delete join token", err)
		return
	}
	server.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *serverApp) handleExec(w http.ResponseWriter, r *http.Request) {
	adminIP := clientIP(r.RemoteAddr)
	deviceID := chi.URLParam(r, "id")
	if _, err := a.devices.GetDevice(deviceID); err != nil {
		if err == server.ErrNotFound {
			server.WriteError(w, http.StatusNotFound, "device not found")
			a.logAudit("command.exec", deviceID, "device not found", adminIP, "failed")
			return
		}
		a.writeInternalError(w, "load device before exec", err)
		a.logAudit("command.exec", deviceID, err.Error(), adminIP, "failed")
		return
	}
	if !a.requireDeviceAccess(w, r, deviceID) {
		a.logAudit("command.exec", deviceID, "forbidden", adminIP, "forbidden")
		return
	}

	var req struct {
		Command string   `json:"command"`
		Args    []string `json:"args"`
		Timeout int      `json:"timeout"`
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		if isBodyTooLarge(err) {
			server.WriteError(w, http.StatusRequestEntityTooLarge, "request too large")
			a.logAudit("command.exec", deviceID, "request too large", adminIP, "failed")
			return
		}
		server.WriteError(w, http.StatusBadRequest, "invalid json")
		a.logAudit("command.exec", deviceID, "invalid json", adminIP, "failed")
		return
	}
	if req.Command == "" {
		req.Command = "openclaw"
	}
	if req.Timeout <= 0 {
		req.Timeout = 60
	}
	if !openclaw.ValidateCommand(req.Command, req.Args) {
		server.WriteError(w, http.StatusBadRequest, "command not allowed")
		detail := strings.TrimSpace(req.Command + " " + strings.Join(req.Args, " "))
		a.logAudit("command.exec", deviceID, detail, adminIP, "rejected")
		return
	}

	redactedArgs := server.RedactSensitiveArgs(req.Command, req.Args)
	commandText := strings.TrimSpace(req.Command + " " + strings.Join(redactedArgs, " "))
	rec, err := a.commands.Create(deviceID, req.Command, redactedArgs, req.Timeout)
	if err != nil {
		a.writeInternalError(w, "create command", err)
		a.logAudit("command.exec", deviceID, commandText, adminIP, "failed")
		return
	}

	msg := protocol.CommandPayload{
		CommandID: rec.ID,
		Command:   rec.Command,
		Args:      req.Args,
		Timeout:   rec.Timeout,
	}
	if err := a.hub.DispatchCommand(deviceID, msg); err != nil {
		_ = a.commands.MarkFailed(rec.ID, err.Error())
		server.WriteError(w, http.StatusConflict, "device offline")
		a.logAudit("command.exec", deviceID, commandText, adminIP, "failed")
		return
	}

	updated, err := a.commands.GetByDeviceAndID(deviceID, rec.ID)
	if err != nil {
		log.Printf("reload command %s for device %s: %v", rec.ID, deviceID, err)
		server.WriteJSON(w, http.StatusAccepted, rec)
		a.logAudit("command.exec", deviceID, commandText, adminIP, "accepted")
		return
	}
	a.logAudit("command.exec", deviceID, commandText, adminIP, "accepted")
	server.WriteJSON(w, http.StatusAccepted, updated)
}

func (a *serverApp) handleGetCommand(w http.ResponseWriter, r *http.Request) {
	deviceID := chi.URLParam(r, "id")
	cmdID := chi.URLParam(r, "cmdId")
	if !a.requireDeviceAccess(w, r, deviceID) {
		return
	}
	rec, err := a.commands.GetByDeviceAndID(deviceID, cmdID)
	if err != nil {
		if err == server.ErrNotFound {
			server.WriteError(w, http.StatusNotFound, "command not found")
			return
		}
		a.writeInternalError(w, "get command", err)
		return
	}
	server.WriteJSON(w, http.StatusOK, rec)
}

func (a *serverApp) handleInstallScript(w http.ResponseWriter, r *http.Request) {
	joinToken := strings.TrimSpace(r.URL.Query().Get("token"))
	if joinToken == "" {
		http.Error(w, "missing token", http.StatusBadRequest)
		return
	}

	serverURL := websocketServerURL(r)
	script := buildInstallScript(serverURL, joinToken)

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(script))
}

func spaHandler() http.Handler {
	sub, err := fs.Sub(webassets.Dist, "dist")
	if err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("missing embedded SPA"))
		})
	}
	fileServer := http.FileServer(http.FS(sub))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}
		if _, err := fs.Stat(sub, path); err == nil {
			fileServer.ServeHTTP(w, r)
			return
		}
		r2 := new(http.Request)
		*r2 = *r
		r2.URL = cloneURL(r.URL)
		r2.URL.Path = "/index.html"
		fileServer.ServeHTTP(w, r2)
	})
}

func cloneURL(u *url.URL) *url.URL {
	v := *u
	return &v
}

func websocketServerURL(r *http.Request) string {
	host := strings.TrimSpace(r.Host)
	if host == "" {
		host = "localhost:18790"
	}

	wsScheme := "ws"
	if requestScheme(r) == "https" {
		wsScheme = "wss"
	}
	return fmt.Sprintf("%s://%s/ws", wsScheme, host)
}

func requestScheme(r *http.Request) string {
	if forwarded := strings.ToLower(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto"))); forwarded != "" {
		if forwarded == "http" || forwarded == "https" {
			return forwarded
		}
	}
	if r.TLS != nil {
		return "https"
	}
	return "http"
}

func shellSingleQuote(value string) string {
	return strings.ReplaceAll(value, "'", `'"'"'`)
}

func buildInstallScript(serverURL, joinToken string) string {
	return fmt.Sprintf(`#!/usr/bin/env bash
set -euo pipefail

if [ "$(id -u)" -ne 0 ]; then
  echo "请使用 root 权限运行（例如：curl ... | sudo bash）"
  exit 1
fi

detect_os() {
  local os
  os="$(uname -s | tr '[:upper:]' '[:lower:]')"
  case "$os" in
    linux|darwin) echo "$os" ;;
    *)
      echo "不支持的操作系统: $os"
      exit 1
      ;;
  esac
}

detect_arch() {
  local arch
  arch="$(uname -m)"
  case "$arch" in
    x86_64|amd64) echo "amd64" ;;
    aarch64|arm64) echo "arm64" ;;
    armv7l) echo "armv7" ;;
    *)
      echo "不支持的架构: $arch"
      exit 1
      ;;
  esac
}

OS="$(detect_os)"
ARCH="$(detect_arch)"
BINARY_URL="https://github.com/raystone-ai/clawmini/releases/latest/download/clawmini-client-${OS}-${ARCH}"
INSTALL_PATH="/usr/local/bin/clawmini-client"
SERVICE_PATH="/etc/systemd/system/clawmini-client.service"
SERVER_URL='%s'
JOIN_TOKEN='%s'

echo "下载客户端: ${BINARY_URL}"
curl -fsSL "${BINARY_URL}" -o "${INSTALL_PATH}"
chmod +x "${INSTALL_PATH}"

cat > "${SERVICE_PATH}" <<SERVICE
[Unit]
Description=ClawMini Client
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
Restart=always
RestartSec=5
ExecStart=${INSTALL_PATH} join --server ${SERVER_URL} --token ${JOIN_TOKEN}

[Install]
WantedBy=multi-user.target
SERVICE

systemctl daemon-reload
systemctl enable clawmini-client
systemctl restart clawmini-client
systemctl status clawmini-client --no-pager

echo "安装完成，服务已启动。"
`, shellSingleQuote(serverURL), shellSingleQuote(joinToken))
}

func decodeJSONBody(w http.ResponseWriter, r *http.Request, dst interface{}) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(dst)
}

func isBodyTooLarge(err error) bool {
	var target *http.MaxBytesError
	return errors.As(err, &target)
}

func (a *serverApp) writeInternalError(w http.ResponseWriter, context string, err error) {
	log.Printf("%s: %v", context, err)
	server.WriteError(w, http.StatusInternalServerError, "internal error")
}

type loginRateLimiter struct {
	mu       sync.Mutex
	limit    int
	window   time.Duration
	attempts map[string][]time.Time
}

func newLoginRateLimiter(limit int, window time.Duration) *loginRateLimiter {
	return &loginRateLimiter{
		limit:    limit,
		window:   window,
		attempts: make(map[string][]time.Time),
	}
}

func (l *loginRateLimiter) Middleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ip := clientIP(r.RemoteAddr)
		if !l.allow(ip) {
			server.WriteError(w, http.StatusTooManyRequests, "too many attempts")
			return
		}
		next(w, r)
	}
}

func (l *loginRateLimiter) allow(ip string) bool {
	now := time.Now()
	cutoff := now.Add(-l.window)

	l.mu.Lock()
	defer l.mu.Unlock()

	existing := l.attempts[ip]
	filtered := existing[:0]
	for _, at := range existing {
		if at.After(cutoff) {
			filtered = append(filtered, at)
		}
	}
	if len(filtered) >= l.limit {
		l.attempts[ip] = filtered
		return false
	}
	filtered = append(filtered, now)
	l.attempts[ip] = filtered
	return true
}

func clientIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(strings.TrimSpace(remoteAddr))
	if err == nil && host != "" {
		return host
	}
	if remoteAddr == "" {
		return "unknown"
	}
	return remoteAddr
}
