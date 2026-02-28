# ClawMini QA Report

Date: 2026-02-28
Reviewer: Codex (QA)
Scope: `internal/`, `cmd/`, `web/src/`

## 1. Validation Runs

### Go tests (requested command)
Command:

```bash
PATH=$PATH:/usr/local/go/bin go test ./... -v -race
```

Result: PASS (`-race` enabled), all test suites passed.

### Additional checks run
- `go vet ./...` -> PASS
- `cd web && npm run build` -> PASS (bundle size warning: main JS chunk ~901.68 kB minified)
- `cd web && npm run lint` -> FAIL (5 errors, 2 warnings)

## 2. Executive Summary

Current state is **not enterprise-ready**. The most serious blockers are:
- insecure default credentials,
- token leakage via query strings and logs,
- permissive WebSocket origin policy,
- command whitelist logic that does not enforce allowed arguments,
- unbounded command output buffering (memory exhaustion risk).

No SQL injection vulnerabilities were found in current DB query usage (parameterized placeholders are consistently used).

## 3. Findings (Severity-Ordered)

## Critical

### C1. Hardcoded default admin/device tokens
Evidence:
- `internal/server/auth.go:10`
- `internal/server/auth.go:11`
- `internal/server/auth.go:21`
- `internal/server/auth.go:27`

Issue:
- Server silently falls back to static defaults (`clawmini-admin`, `clawmini-device`) if env vars are missing.

Impact:
- Predictable credentials allow trivial unauthorized access and device impersonation.

Recommendation:
- Fail startup if tokens are unset or weak.
- Enforce minimum entropy/length policy.
- Support rotation with audit logs.

### C2. WebSocket origin check is fully disabled
Evidence:
- `internal/server/hub.go:66`
- `internal/server/hub.go:67`

Issue:
- `CheckOrigin` always returns `true` for browser and device sockets.

Impact:
- Cross-origin WebSocket requests are accepted from any site.
- Combined with token-in-query behavior, this materially increases session hijack/exfiltration risk.

Recommendation:
- Restrict allowed origins using explicit allowlist.
- Separate browser and device WS paths with independent origin/auth policy.

## High

### H1. Token exposure via URL query + server logging
Evidence:
- `internal/server/auth.go:50`
- `internal/server/auth.go:51`
- `web/src/contexts/RealtimeContext.tsx:43`
- `web/src/contexts/RealtimeContext.tsx:44`
- `cmd/server/main.go:56`

Issue:
- Admin token is accepted via query parameter and frontend sends it in `/api/ws?token=...`.
- Request logger middleware logs request URIs including query strings.

Impact:
- Token leaks to logs, browser history, reverse proxies, monitoring systems, and potentially referrer chains.

Recommendation:
- Remove query-token auth path.
- Use `Authorization: Bearer` for HTTP and explicit WS auth message or short-lived signed WS ticket.
- Redact sensitive fields from logs.

### H2. Device token is logged in plaintext
Evidence:
- `internal/server/hub.go:131`

Issue:
- Registration log line prints `token=%q`.

Impact:
- Credential disclosure through logs.

Recommendation:
- Remove token from logs entirely.
- If needed, log token hash prefix only for troubleshooting.

### H3. Command whitelist can be bypassed via unrestricted arguments
Evidence:
- `internal/openclaw/whitelist.go:6`
- `internal/openclaw/whitelist.go:27`
- `internal/openclaw/whitelist.go:28`
- `internal/openclaw/whitelist.go:33`

Issue:
- Validation only checks binary name and first subcommand.
- `AllowedCommands` argument lists are not enforced.
- Extra arguments are effectively unrestricted except for a small shell-character blacklist.

Impact:
- Admin can run subcommands with unexpected/high-risk flags and parameters.
- Security model claims whitelist control but runtime behavior is significantly broader.

Recommendation:
- Implement strict per-subcommand grammar validation (arg count, allowed flags, allowed value patterns).
- Default-deny unknown flags/positionals.
- Add unit tests for rejection of unapproved argument combinations.

### H4. Unbounded stdout/stderr capture enables memory exhaustion
Evidence:
- `internal/client/executor.go:40`
- `internal/client/executor.go:41`
- `internal/client/executor.go:42`
- `internal/client/executor.go:45`
- `internal/client/executor.go:46`

Issue:
- `bytes.Buffer` collects full command output in memory before sending/storing.

Impact:
- Large or infinite-output commands can cause client OOM and downstream DB bloat.

Recommendation:
- Enforce output size caps (e.g., max bytes per stream).
- Truncate with explicit marker and collect tail/head safely.
- Optionally stream output in chunks with bounded buffers.

### H5. Admin token stored in `localStorage`
Evidence:
- `web/src/contexts/AuthContext.tsx:16`
- `web/src/contexts/AuthContext.tsx:17`
- `web/src/contexts/AuthContext.tsx:38`

Issue:
- Long-lived admin token is persisted in browser `localStorage`.

Impact:
- Any XSS event can exfiltrate admin credentials.
- Token persists across tabs/restarts without server-side revocation control.

Recommendation:
- Move to HttpOnly Secure SameSite cookies or short-lived access token + refresh token model.
- Add server-side session invalidation.

## Medium

### M1. WebSocket session lifecycle lacks heartbeat/deadline management (stale-session risk)
Evidence:
- `internal/server/hub.go:104`
- `internal/server/hub.go:105`
- `internal/server/hub.go:150`
- `internal/server/hub.go:151`

Issue:
- Server read loops have no read deadlines or ping/pong handlers.

Impact:
- Half-open sockets may remain tracked too long; resource growth risk under network churn.

Recommendation:
- Add `SetReadDeadline`, pong handlers, and periodic server pings.
- Drop idle clients deterministically.

### M2. Frontend API refresh path has unhandled promise rejection behavior
Evidence:
- `web/src/contexts/RealtimeContext.tsx:55`
- `web/src/contexts/RealtimeContext.tsx:62`
- `web/src/contexts/RealtimeContext.tsx:69`
- `web/src/pages/DashboardPage.tsx:59`

Issue:
- `refreshDevices()` throws on fetch errors; callers often invoke it with `void` and no catch.

Impact:
- Silent failures and unhandled promise rejections; poor operator visibility during outage/auth expiry.

Recommendation:
- Centralize API error boundary in context and expose `error` state.
- Ensure every fire-and-forget call catches and reports failures.

### M3. WS reconnect strategy is fixed-interval infinite retry with no auth/backoff controls
Evidence:
- `web/src/contexts/RealtimeContext.tsx:116`
- `web/src/contexts/RealtimeContext.tsx:121`

Issue:
- Constant 2.5s retry forever, no jitter, no max backoff, no stop condition for 401/403 scenarios.

Impact:
- Retry storms and noisy logs; poor behavior under outages or invalid token states.

Recommendation:
- Implement exponential backoff with jitter and max cap.
- Stop reconnect on auth failures until re-login.

### M4. Missing error logging in backend message processing paths
Evidence:
- `internal/server/hub.go:164`
- `internal/server/hub.go:170`
- `internal/server/hub.go:179`
- `internal/server/hub.go:193`
- `cmd/server/main.go:174`

Issue:
- Several parsing/DB failures are swallowed or ignored.

Impact:
- Operational debugging and incident response are significantly harder.

Recommendation:
- Log structured errors with request/device/command identifiers.
- Track failure counters/metrics.

### M5. Command timeout accepts unbounded client-provided values
Evidence:
- `cmd/server/main.go:153`
- `cmd/server/main.go:154`

Issue:
- Timeout defaults to 60s only when <=0; no upper bound enforcement.

Impact:
- Long-running commands can tie up agents/resources for excessive durations.

Recommendation:
- Enforce max timeout (e.g., 300s) and reject out-of-policy values.

## Low

### L1. `commandRecords` map grows unbounded in frontend state
Evidence:
- `web/src/contexts/RealtimeContext.tsx:52`
- `web/src/contexts/RealtimeContext.tsx:109`

Issue:
- Every command result is retained indefinitely.

Impact:
- Long-lived sessions accumulate memory and slow renders.

Recommendation:
- Keep bounded LRU cache or per-device recent N records.

### L2. UX resilience gaps for degraded realtime/API states
Evidence:
- `web/src/components/AppLayout.tsx:67`
- `web/src/pages/DeviceDetailPage.tsx:130`

Issue:
- UI indicates disconnected WS via badge only; polling/API errors are often silently ignored.

Impact:
- Operators lack clear guidance during incidents.

Recommendation:
- Show persistent warning banner/toast on prolonged disconnection.
- Add retry action and last-successful-sync timestamp.

## 4. Category-Specific Review Conclusions

### Race conditions
- Dynamic runtime race check (`go test -race`) passed.
- No clear shared-memory data race found in reviewed code paths.
- Residual risk remains around websocket lifecycle behavior under churn (see M1).

### Security holes
- Multiple high/critical issues confirmed (C1, C2, H1, H2, H3, H5).

### XSS
- No direct DOM XSS sink was found (no `dangerouslySetInnerHTML` in reviewed UI paths).
- Residual account-takeover risk remains high because admin token is in `localStorage` (H5) if any future XSS is introduced.

### SQL injection
- No SQL injection pattern found; SQL uses parameterized placeholders (`?`) across CRUD paths.

### Command whitelist bypass
- Confirmed (H3): argument-level policy not enforced.

### WS memory/resource leaks
- Stale-session/resource leak risk identified due to missing heartbeat/deadline controls (M1).

### Missing error handling
- Confirmed in multiple backend and frontend flows (M2, M4, L2).

## 5. Lint/Build Observations (Frontend)

`npm run lint` findings to address before enterprise rollout:
- `react-refresh/only-export-components` errors in context files.
- `react-hooks/exhaustive-deps` warnings around `refreshDevices` dependency management.
- `react-hooks/set-state-in-effect` errors in `DeviceDetailPage`.

These are not just style concerns; they indicate maintainability and state-flow reliability issues.

## 6. Recommended Remediation Plan

1. Security blockers first (C1, C2, H1, H2, H5).
2. Enforce strict command argument policy and add negative tests (H3).
3. Bound command output and timeout controls (H4, M5).
4. Harden websocket lifecycle (M1, M3).
5. Improve observability and UI error transparency (M2, M4, L2).
6. Clear all lint errors/warnings and add CI gates (`go test -race`, `go vet`, `npm run lint`, `npm run build`).

## 7. Enterprise Deployment Gate

Status: **FAIL (blocker issues present)**

Blocking findings:
- C1, C2, H1, H2, H3, H4, H5

Re-evaluation criteria:
- All blockers fixed,
- regression tests added for auth/whitelist/ws lifecycle,
- lint clean,
- security retest completed.
