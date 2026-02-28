# ClawMini QA Report

**Date:** 2026-02-28
**Reviewer:** Claude (automated + manual code review)
**Scope:** Full-stack review — Go backend, React/TS frontend, security, architecture, build

---

## Automated Check Results

| Check | Result |
|-------|--------|
| `go test -race ./...` | **PASS** — all packages pass, no race conditions detected |
| `go vet ./...` | **PASS** — no issues |
| `go build ./...` | **PASS** — compiles cleanly |
| `tsc -b` (TypeScript) | **PASS** — no type errors |
| `eslint .` (Frontend) | **FAIL** — 5 errors, 2 warnings |

---

## Findings

### CRITICAL

#### C1. Hardcoded Default Admin/Device Tokens
**File:** `internal/server/auth.go:10-11`
```go
const (
    defaultAdminToken  = "clawmini-admin"
    defaultDeviceToken = "clawmini-device"
)
```
If `CLAWMINI_ADMIN_TOKEN` / `CLAWMINI_DEVICE_TOKEN` env vars are unset, the server silently falls back to well-known defaults. Any attacker who reads the source (or this report) can authenticate as admin.

**Recommendation:** Refuse to start if env vars are empty in production. At minimum, log a loud warning. Consider generating a random token on first run and persisting it.

---

#### C2. No TLS — All Tokens Transmitted in Cleartext
**File:** `cmd/server/main.go:84`
```go
if err := http.ListenAndServe(addr, r); err != nil {
```
The server only supports plain HTTP. Admin tokens, device tokens, and all WebSocket traffic flow unencrypted. Combined with C1, this means default credentials travel in plaintext.

**Recommendation:** Add `ListenAndServeTLS` option or document that a reverse proxy (nginx/caddy) with TLS termination is required. Add a startup warning when TLS is not configured.

---

#### C3. WebSocket Origin Check Disabled (CSWSH)
**File:** `internal/server/hub.go:66-68`
```go
CheckOrigin: func(r *http.Request) bool {
    return true
},
```
This allows Cross-Site WebSocket Hijacking. A malicious webpage can open a WebSocket to the ClawMini server and issue commands if the browser has a valid token cookie/URL.

**Recommendation:** Validate the `Origin` header against a configurable allowlist, or at minimum check it matches the `Host` header.

---

#### C4. Token Accepted via URL Query String
**File:** `internal/server/auth.go:50-52`
```go
if token := strings.TrimSpace(r.URL.Query().Get("token")); token != "" {
    return token
}
```
**Also:** `web/src/contexts/RealtimeContext.tsx:43-44`
```ts
const query = new URLSearchParams({ token })
return `${protocol}://${host}/api/ws?${query.toString()}`
```
Tokens in URLs are logged by web servers, proxies, browser history, and `Referer` headers. The browser WebSocket connection actively uses this pattern.

**Recommendation:** Remove query-string token extraction. Pass the token via a WebSocket subprotocol header, or use a short-lived ticket exchanged via POST before the WS upgrade.

---

### HIGH

#### H1. No Rate Limiting on Login Endpoint
**File:** `cmd/server/main.go:59`
```go
r.Post("/api/auth/login", app.handleLogin)
```
No rate limiting, no account lockout, no CAPTCHA. Combined with simple token auth, the admin token can be brute-forced trivially.

**Recommendation:** Add per-IP rate limiting middleware (e.g., `go-chi/httprate` or a token-bucket in middleware). Consider exponential backoff on failed attempts.

---

#### H2. No HTTP Request Body Size Limit
**Files:** `cmd/server/main.go:93`, `cmd/server/main.go:146`
```go
if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
```
Request bodies are decoded without size limits. An attacker can send a multi-gigabyte JSON body to exhaust server memory.

**Recommendation:** Wrap `r.Body` with `http.MaxBytesReader(w, r.Body, maxSize)` before decoding. A 1 MB limit is more than sufficient for these endpoints.

---

#### H3. No Stdout/Stderr Size Limits in Command Executor
**File:** `internal/client/executor.go:40-41`
```go
var stdout, stderr bytes.Buffer
execCmd.Stdout = &stdout
execCmd.Stderr = &stderr
```
Command output is captured into unbounded buffers. A command producing large output (e.g., `openclaw logs`) could exhaust device memory. The output is then transmitted over WebSocket and stored in the database without size limits.

**Recommendation:** Use `io.LimitedReader` or a custom writer that truncates after a maximum size (e.g., 1 MB). Store truncation metadata so the UI can indicate partial output.

---

#### H4. Internal Error Details Leaked to API Clients
**Files:** `cmd/server/main.go:110`, `cmd/server/main.go:124`, `cmd/server/main.go:137`, `cmd/server/main.go:163`
```go
writeError(w, http.StatusInternalServerError, err.Error())
```
Raw database error messages are returned to the client. These can reveal table structure, SQL queries, file paths, and driver versions.

**Recommendation:** Log the full error server-side. Return a generic `"internal error"` message to the client.

---

#### H5. Device Token Logged in Plaintext
**File:** `internal/server/hub.go:131`
```go
log.Printf("[ws] register: deviceID=%s token=%q", reg.DeviceID, reg.Token)
```
Device tokens are written to logs in cleartext, making them accessible to anyone with log access.

**Recommendation:** Mask the token in logs (e.g., `token=***`). Or remove it from the log line entirely.

---

#### H6. Login Placeholder Reveals Default Admin Token
**File:** `web/src/pages/LoginPage.tsx:52`
```tsx
placeholder="例如：clawmini-admin"
```
The login form's placeholder text is the actual default admin token. This makes it trivially guessable even without reading source code.

**Recommendation:** Use a generic placeholder like `"请输入管理员令牌"`.

---

#### H7. Whitelist Does Not Validate Args Against Allowed List
**File:** `internal/openclaw/whitelist.go:20-38`
```go
func ValidateCommand(cmd string, args []string) bool {
    if cmd != "openclaw" { return false }
    if len(args) == 0 { return false }
    subCmd := args[0]
    _, ok := AllowedCommands[subCmd]
    if !ok { return false }
    for _, arg := range args {
        if strings.ContainsAny(arg, ";|&$`\\") { return false }
    }
    return true
}
```
The `AllowedCommands` map defines permitted flags per subcommand, but the validation code **never checks args against that list**. Any argument (not containing shell metacharacters) is accepted. For example `openclaw config set dangerous_option value` or `openclaw gateway --unknown-flag` would pass.

**Recommendation:** Validate that `args[1:]` are present in `AllowedCommands[subCmd]`, or document that the allowed flags list is informational only and shell-metacharacter blocking is the sole protection.

---

#### H8. Login Response Echoes Raw Token Back to Client
**File:** `cmd/server/main.go:101-104`
```go
writeJSON(w, http.StatusOK, map[string]interface{}{
    "ok":    true,
    "token": req.Token,
})
```
The login endpoint echoes the submitted token directly back. This is unnecessary (the client already has it) and violates the principle of not reflecting secrets. If combined with XSS, the token is trivially extractable from the response.

**Recommendation:** Return only `{"ok": true}` or issue a separate session token.

---

### MEDIUM

#### M1. Frontend ESLint Errors — setState Inside useEffect
**File:** `web/src/pages/DeviceDetailPage.tsx:79`, `:88`, `:103`
```
error: Calling setState synchronously within an effect can trigger cascading renders
```
Three separate instances of calling `setState` inside `useEffect` bodies, causing unnecessary re-render cascades. This degrades performance and can cause stale-state bugs.

**Recommendation:**
- Line 79: Replace with derived state using `useMemo` or just use `storeDevice` directly.
- Line 88: Wrap in a data-fetching pattern (abort controller + isMounted check).
- Line 103: Use derived state from `commandRecords` rather than syncing into local state.

---

#### M2. Frontend ESLint Errors — Missing Hook Dependencies
**File:** `web/src/contexts/RealtimeContext.tsx:71`, `:150`
```
warning: React Hook useEffect has a missing dependency: 'refreshDevices'
warning: React Hook useMemo has a missing dependency: 'refreshDevices'
```
Missing dependencies can cause stale closures. The `refreshDevices` function is created fresh each render (not wrapped in `useCallback`) and is missing from the dependency arrays.

**Recommendation:** Wrap `refreshDevices` in `useCallback` and add it to dependency arrays, or use a ref to avoid the dependency.

---

#### M3. Frontend ESLint Errors — react-refresh/only-export-components
**Files:** `web/src/contexts/AuthContext.tsx:55`, `web/src/contexts/RealtimeContext.tsx:156`

Exporting both the Provider component and the `useAuth`/`useRealtime` hooks from the same file breaks React Fast Refresh during development.

**Recommendation:** Move the hooks to a separate file (e.g., `useAuth.ts`) or suppress the rule for context files if fast refresh isn't a concern.

---

#### M4. Duplicate `wsEnvelope` Type Definition
**Files:** `internal/server/hub.go:15-19` and `internal/client/connection.go:17-21`
```go
type wsEnvelope struct {
    Type string          `json:"type"`
    ID   string          `json:"id,omitempty"`
    Data json.RawMessage `json:"data,omitempty"`
}
```
Identical struct defined in two packages, diverging from the `protocol.Envelope` type which uses `interface{}` for Data. Three different envelope representations is confusing and error-prone.

**Recommendation:** Add a `RawEnvelope` (with `json.RawMessage`) to the `protocol` package. Remove the duplicates.

---

#### M5. Duplicate `writeJSON` Function
**Files:** `cmd/server/main.go:234-238` and `internal/server/http.go:8-12`

Both define an identical `writeJSON` function. The one in `main.go` shadows the package-level one.

**Recommendation:** Remove the duplicate from `main.go` and import from the `server` package, or make one version canonical.

---

#### M6. No Command Expiry / Stale Command Cleanup
**File:** `internal/server/command.go`

Commands in `sent` status that never receive a result (device crashes, network loss) remain in `sent` state forever. No TTL, no background cleanup, no timeout promotion to `failed`.

**Recommendation:** Add a background goroutine that marks commands as `failed` (with reason "timeout") if they've been in `sent` state longer than their timeout value.

---

#### M7. No WebSocket Ping/Pong Keepalive
**Files:** `internal/server/hub.go`, `internal/client/connection.go`

Neither side implements WebSocket ping/pong frames. Connection liveness relies solely on the 30-second heartbeat cycle. Half-open TCP connections (e.g., from network changes) won't be detected for up to 90 seconds (the `Online` threshold in `device.go:237`).

**Recommendation:** Implement ping/pong handlers. The gorilla/websocket library supports `SetPingHandler`/`SetPongHandler`. Set a read deadline that resets on pong receipt.

---

#### M8. Frontend Does Not Handle 401 Responses Globally
**File:** `web/src/api/client.ts:44-48`
```ts
if (!res.ok) {
    throw new Error(data.error || `请求失败（${res.status}）`)
}
```
When a stored token becomes invalid (server restart, token change), API calls return 401 but the frontend only shows an error message. The stale token remains in localStorage and the user is not redirected to login.

**Recommendation:** Add a global response interceptor that calls `logout()` and redirects to `/login` on 401 responses.

---

#### M9. No CORS Configuration
**File:** `cmd/server/main.go` (missing)

No CORS headers are set. While the SPA is served from the same origin in production, development setups (Vite proxy) and any future API consumers will face CORS issues.

**Recommendation:** Add CORS middleware for at least the API routes, or document the same-origin requirement.

---

### LOW

#### L1. No Graceful Server Shutdown
**File:** `cmd/server/main.go:84-86`
```go
if err := http.ListenAndServe(addr, r); err != nil {
    log.Fatal(err)
}
```
No signal handling, no graceful shutdown. Active WebSocket connections and in-flight requests are terminated abruptly on SIGTERM.

**Recommendation:** Use `http.Server` with `Shutdown()` and handle `os.Interrupt`/`SIGTERM`.

---

#### L2. No Database Migration Strategy
**File:** `internal/server/device.go:69-100`
```go
func (s *DeviceStore) EnsureSchema() error {
    _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS ...`)
    return err
}
```
`CREATE TABLE IF NOT EXISTS` works for the initial schema but cannot handle column additions, renames, or type changes in future versions.

**Recommendation:** Adopt a migration approach (numbered SQL files or a library like `golang-migrate`) before any schema changes are needed.

---

#### L3. No Tests for `cmd/` Packages
**Files:** `cmd/server/main.go`, `cmd/client/main.go`
```
?   github.com/raystone-ai/clawmini/cmd/client  [no test files]
?   github.com/raystone-ai/clawmini/cmd/server  [no test files]
```
The main entry points — including URL normalization, CLI flag parsing, routing setup, and handler wiring — have no test coverage.

**Recommendation:** Add integration tests: stand up the HTTP server, make authenticated API calls, verify routing and error responses.

---

#### L4. Makefile `test` Target Missing `-race` Flag
**File:** `Makefile:6`
```makefile
test:
    go test ./... -v
```
The `-race` flag is absent. Race conditions could go undetected in CI.

**Recommendation:** Change to `go test -race ./... -v`.

---

#### L5. Build Artifacts and Database in Version Control
**Git status shows:**
```
?? bin/
M  clawmini.db
```
Binary build artifacts (`bin/`) and the SQLite database (`clawmini.db`) are tracked/present. These shouldn't be in version control.

**Recommendation:** Add to `.gitignore`:
```
bin/
*.db
```

---

#### L6. Empty Scaffolding Directories
**Directories:** `scripts/`, `configs/`

Both directories exist but are completely empty. They appear to be scaffolding stubs.

**Recommendation:** Either populate with deployment scripts/config templates or remove to reduce confusion.

---

#### L7. Embed Directive Redundancy
**File:** `web/embed.go:7`
```go
//go:embed dist dist/*
```
`dist` embeds the directory recursively (excluding dotfiles). Adding `dist/*` is redundant unless dotfiles in the root of `dist/` need inclusion.

**Recommendation:** Simplify to `//go:embed dist` unless dotfiles are needed.

---

#### L8. `ErrNotFound` Is Not a Sentinel Error
**File:** `internal/server/device.go:262`
```go
var ErrNotFound = fmt.Errorf("not found")
```
Using `fmt.Errorf` creates a new error value. While this works with `==` comparison today, it won't survive wrapping. Using `errors.New` is more idiomatic and makes the intent clearer.

**Recommendation:** Change to `var ErrNotFound = errors.New("not found")`.

---

#### L9. Reconnect Timer Race Condition in Frontend
**File:** `web/src/contexts/RealtimeContext.tsx:121`, `:134`
```ts
reconnectTimerRef.current = window.setTimeout(connect, 2500)
// ...
if (reconnectTimerRef.current) {
    window.clearTimeout(reconnectTimerRef.current)
}
```
If `onclose` fires multiple times rapidly, the ref is overwritten and the previous timer leaks.

**Recommendation:** Clear the previous timer before setting a new one in the `onclose` handler.

---

#### L10. Collector's `ctx` Parameter Partially Unused
**File:** `internal/client/collector.go:29-44`
```go
func (c *Collector) Collect(ctx context.Context, deviceID string) protocol.HeartbeatPayload {
    memTotal, memUsed := readMemInfo()
    diskTotal, diskUsed := readDisk()
    // ...
}
```
The `ctx` is passed through to `collectOpenClaw` but not used by `readMemInfo`, `readDisk`, `readCPUStat`, or `uptimeSeconds`. If the context is cancelled, the procfs reads still block.

**Recommendation:** Minor concern for now — procfs reads are fast. Document the limitation or add context support if needed later.

---

## Architecture Notes

### Strengths
- Clean separation: `protocol/` for shared types, `openclaw/` for whitelist, `server/` and `client/` for their respective logic
- Good test coverage for core packages (server, client, protocol, openclaw)
- SQLite single-binary deployment is well-suited for MVP
- WebSocket hub pattern is clean with proper mutex usage
- Frontend architecture (contexts, API client, typed interfaces) is well-organized
- Graceful reconnect with exponential backoff in the client

### Areas for Improvement
- No structured logging (uses stdlib `log` with ad-hoc formats)
- No configuration file support — all config via env vars
- No health-check endpoint for the server itself (e.g., `/healthz`)
- No API versioning (all endpoints at `/api/...`)
- No OpenAPI/Swagger documentation for the API
- The `Online` calculation (`device.go:237`) uses a hardcoded 90-second threshold that should be configurable
- Consider adding context propagation through the HTTP handlers for cancellation

---

## Summary by Severity

| Severity | Count | Key Concerns |
|----------|-------|-------------|
| **Critical** | 4 | Default tokens, no TLS, CSWSH, token in URL |
| **High** | 8 | No rate limiting, no body size limits, unbounded output capture, error leakage, token logging |
| **Medium** | 9 | ESLint errors, duplicate types, no command expiry, no ping/pong, no 401 handling |
| **Low** | 10 | No graceful shutdown, no migrations, missing tests, gitignore |

**Overall Assessment:** The core logic is well-structured and tested. The primary risks are **security-related** — authentication hardening, transport encryption, and input validation need attention before any production deployment. The frontend has several React anti-patterns flagged by ESLint that should be resolved to prevent render performance issues.
