# QA Review Report: Sprint 2A + Sprint 2B

Date: 2026-02-28
Reviewer: Codex

## Scope Reviewed
Frontend:
- `web/src/pages/DeviceDetailPage.tsx`
- `web/src/pages/IMConfigPage.tsx`
- `web/src/App.tsx`
- `web/src/api/client.ts`
- `web/src/types.ts`
- `web/src/contexts/RealtimeContext.tsx`
- `web/src/utils/format.ts`

Backend:
- `internal/openclaw/whitelist.go`
- `internal/server/hub.go`
- `cmd/server/main.go`

## Validation Commands
Executed exactly as requested:
- `go test -race ./...`
- `go vet ./...`
- `cd web && npm run lint`
- `cd web && npm run build`

Result:
- Go tests: PASS
- Go vet: PASS
- ESLint: PASS
- Frontend build: PASS
- Build warning: single JS chunk is ~993 kB (Rollup chunk-size warning)

## Executive Summary
- Critical: 1
- High: 1
- Medium: 4
- Low: 2

Most serious issue: IM credentials are sent/stored/broadcast as raw command arguments.

---

## Findings

### C-01 (Critical) - IM credentials exposed in command payload lifecycle
Area: Security (credentials handling)

Evidence:
- Wizard includes raw secrets in command args:
  - `web/src/pages/IMConfigPage.tsx:304`
  - `web/src/pages/IMConfigPage.tsx:343`
- Args are sent to backend exec API:
  - `web/src/pages/IMConfigPage.tsx:472`
- Backend persists command record with args:
  - `cmd/server/main.go:234`
- Command payload (including args) is broadcast to browsers:
  - `internal/server/hub.go:375`

Impact:
- Client secrets/App secrets can be persisted in DB and exposed over admin websocket/API command records.
- This is a direct secret-leak risk.

Recommendation:
- Do not pass secrets through generic command args.
- Add dedicated secret-setting API flow (server-side redaction + no broadcast of sensitive fields).
- At minimum, redact sensitive args before persistence and before websocket/API responses.

### H-01 (High) - Device Detail command flow can report success after timeout/non-terminal state
Area: Command execution flow, timeout handling

Evidence:
- Device detail polling returns `latest` even when terminal status was never reached:
  - `web/src/pages/DeviceDetailPage.tsx:950`
  - `web/src/pages/DeviceDetailPage.tsx:972`
- Callers treat non-failed result as success (no explicit terminal-state validation):
  - `web/src/pages/DeviceDetailPage.tsx:448`
  - `web/src/pages/DeviceDetailPage.tsx:539`

Impact:
- User can see success toast while command is still `queued/sent` or effectively timed out.
- Operationally unsafe for gateway restart/stop/update actions.

Recommendation:
- Match IM page behavior: throw timeout error if no terminal state before deadline.
- Require terminal status (`completed`/`failed`) before success UI.

### M-01 (Medium) - API requests have no timeout/cancelation
Area: API error handling, resilience

Evidence:
- `fetch` is used without `AbortController` timeout:
  - `web/src/api/client.ts:61`

Impact:
- Under network hangs, UI may remain indefinitely loading/waiting.
- Affects detail fetches and command polling fetches.

Recommendation:
- Add request timeout (e.g., 15-30s) with `AbortController`.
- Surface timeout-specific user error.

### M-02 (Medium) - `commandRecords` not cleared on logout/token loss
Area: Security/privacy + state hygiene

Evidence:
- On no token, devices/ws state is reset but command records are retained:
  - `web/src/contexts/RealtimeContext.tsx:76`
  - `web/src/contexts/RealtimeContext.tsx:53`

Impact:
- Cross-session stale command metadata may remain visible in-memory.
- Potential information leakage between admin sessions in same browser context.

Recommendation:
- Clear `commandRecords` when token becomes empty and during auth reset.

### M-03 (Medium) - IM wizard retry strategy is mostly manual and single-shot verify is brittle
Area: IM wizard reliability, retry logic

Evidence:
- Configure loop stops on first failure (no transient retry/backoff):
  - `web/src/pages/IMConfigPage.tsx:552`
- Verify waits fixed 10s then performs one status check:
  - `web/src/pages/IMConfigPage.tsx:635`
  - `web/src/pages/IMConfigPage.tsx:641`

Impact:
- Temporary plugin registry/network/startup latency causes avoidable failures.
- User is forced into manual retries.

Recommendation:
- Add bounded automatic retries with backoff for command fetch/verify steps.
- Consider multi-attempt verification window (e.g., 3 checks over 30-60s).

### M-04 (Medium) - Platform selection cards are mouse-only (keyboard inaccessible)
Area: Accessibility

Evidence:
- Interactive card uses `onClick` without keyboard handlers/role/tab focus:
  - `web/src/pages/IMConfigPage.tsx:715`
  - `web/src/pages/IMConfigPage.tsx:717`

Impact:
- Keyboard and assistive-tech users cannot reliably select platform.

Recommendation:
- Use semantic buttons or add `role="button"`, `tabIndex={0}`, Enter/Space handlers, and clear aria labels.

### L-01 (Low) - WebSocket payloads are trust-cast without runtime schema checks
Area: Type safety/runtime robustness

Evidence:
- Direct casts to app models after JSON parse:
  - `web/src/contexts/RealtimeContext.tsx:117`
  - `web/src/contexts/RealtimeContext.tsx:126`
  - `web/src/contexts/RealtimeContext.tsx:131`

Impact:
- Malformed payloads may still mutate state shape unexpectedly.

Recommendation:
- Add lightweight runtime guards/schema validation for inbound websocket events.

### L-02 (Low) - Whitelist logic is broad for positional arguments
Area: Command allowlist strictness

Evidence:
- After one validated verb, additional positional arguments are broadly allowed:
  - `internal/openclaw/whitelist.go:69`

Impact:
- Allowlist is not tightly scoped per subcommand argument structure.
- Increases blast radius if admin token is compromised.

Recommendation:
- Define stricter per-subcommand argument schemas (expected arity and key/value patterns).

---

## IM Wizard vs Whitelist Coverage
Manual mapping against `ValidateCommand` and current `AllowedCommands`:

- `plugins install clawdbot-dingtalk`: PASS
- `config set plugins.entries.clawdbot-dingtalk.clientId <value>`: PASS
- `config set plugins.entries.clawdbot-dingtalk.clientSecret <value>`: PASS
- `config set plugins.entries.clawdbot-dingtalk.aiCard.enabled true`: PASS
- `plugins install @openclaw/feishu`: PASS
- `config set plugins.entries.@openclaw/feishu.appId <value>`: PASS
- `config set plugins.entries.@openclaw/feishu.appSecret <value>`: PASS
- `gateway restart`: PASS
- `channels status`: PASS

Conclusion: all current IM wizard commands pass `ValidateCommand`.

## Additional Observations
- React hook dependency arrays are generally correct and include key values used in effects/callbacks.
- Command record growth is bounded (`maxCommandRecords = 100`), so unbounded memory growth risk is mitigated.
- Command output rendering uses React text rendering (`<pre>{...}</pre>` / Antd text fields), so direct script injection via output is not observed in current code.

## Recommended Fix Priority
1. Fix credential exposure path (C-01).
2. Fix command timeout semantics in Device Detail (H-01).
3. Add request timeout/cancelation and improve IM retry automation (M-01, M-03).
4. Address accessibility + session state hygiene (M-04, M-02).
