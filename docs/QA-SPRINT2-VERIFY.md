# Sprint 2 QA Fix Verification Report

**Date:** 2026-02-28
**Reviewer:** Claude Opus 4.6
**Scope:** Post-fix verification of 7 QA issues (C1, C2, H1-H5)

## Validation Suite Results

| Check | Result |
|-------|--------|
| `go test -race ./...` | PASS (all packages) |
| `go vet ./...` | PASS |
| `npm run lint` | PASS |
| `npm run build` | PASS |

---

## C1: Allow `$` and `\` in Positional Args

**File:** `internal/openclaw/whitelist.go:42`
**Status:** VERIFIED CORRECT

The shell metacharacter blocklist is now `";|&\``" only:
```go
if strings.ContainsAny(arg, ";|&`") {
    return false
}
```

`$` and `\` are NOT in the blocklist, allowing secrets like `abc$def\ghi` to pass validation. Test `TestValidateCommand_IMWizardCommands` explicitly confirms this with values containing both characters.

---

## C2: Redact `config set` Values in DB and WS Broadcast

**Files:** `internal/server/redact.go`, `cmd/server/main.go:234-246`, `internal/server/hub.go:375-377`
**Status:** VERIFIED CORRECT

Three-layer redaction verified:

1. **`redact.go`** — `RedactSensitiveArgs` copies the slice (`append([]string(nil), args...)`) then replaces `args[3]` with `"******"` when command is `openclaw` and args match `config set <key> <value>`. Non-mutating to original.

2. **`main.go:234-246`** — DB storage uses `redactedArgs`; device dispatch uses `req.Args` (original):
   ```go
   redactedArgs := server.RedactSensitiveArgs(req.Command, req.Args)
   rec, err := a.commands.Create(deviceID, req.Command, redactedArgs, req.Timeout)
   // ...
   msg := protocol.CommandPayload{ Args: req.Args, ... }
   ```

3. **`hub.go:375-377`** — WS broadcast to browsers uses redacted copy:
   ```go
   cmdForBroadcast := cmd
   cmdForBroadcast.Args = redactSensitiveArgs(cmd.Command, cmd.Args)
   h.broadcast("command_dispatched", cmdForBroadcast)
   ```

4. **`redact_test.go`** — Tests cover config set redaction, non-config passthrough, non-openclaw passthrough, and verifies slice copy safety.

---

## H1: `runCommand` Throws Error on Timeout

**File:** `web/src/pages/DeviceDetailPage.tsx:930-981`
**Status:** VERIFIED CORRECT

After the polling loop exits at deadline, the function throws:
```tsx
if (!isTerminalStatus(latest.status)) {
    throw new Error(lastPollError || `命令执行超时：openclaw ${args.join(' ')}`)
}
```

No code path returns a non-terminal `CommandRecord`. All callers (`GatewayControlCard`, `DoctorDiagnosticsCard`, etc.) catch the thrown error via try/catch and display via `message.error()`.

---

## H2: `cancelledRef` with Inter-Step Checks and Unmount Cleanup

**File:** `web/src/pages/IMConfigPage.tsx:413, 423-428, 539-633`
**Status:** VERIFIED CORRECT

- **Declaration:** `const cancelledRef = useRef(false)` (line 413)
- **Unmount cleanup:** `useEffect` with `return () => { cancelledRef.current = true }` (lines 423-428)
- **Inter-step checks in `runConfigureFlow`:**
  - Before flow starts (line 545)
  - Before each loop iteration (line 564)
  - After each `runCommand` resolves (line 579)
  - In each catch block (line 607)
  - After loop completes, before setting success (line 625)

All async boundaries are guarded.

---

## H3: Max Positional Args Per Subcommand

**File:** `internal/openclaw/whitelist.go:81-89`
**Status:** VERIFIED CORRECT

```go
func maxPositionalArgs(subCmd, parent string) int {
    if subCmd == "config" && parent == "set" { return 2 }
    if subCmd == "plugins" && parent == "install" { return 1 }
    return 0
}
```

Enforcement at lines 56-59 tracks per-parent counts and rejects when exceeded. Verified behavior:
- `config set key value` (2 positional) — allowed
- `config set key value extra` (3 positional) — rejected
- `plugins install name` (1 positional) — allowed
- `plugins install name extra` (2 positional) — rejected
- `gateway restart now` (1 positional, max 0) — rejected

---

## H4: Test Coverage for IM Wizard Commands

**File:** `internal/openclaw/whitelist_test.go:48-90`
**Status:** VERIFIED CORRECT

Three dedicated test functions:

1. **`TestValidateCommand_IMWizardCommands`** (lines 48-61) — Tests plugin install for both platforms, `config set` with `$` and `\` in secret values.

2. **`TestValidateCommand_RejectsExtraPositionalArgs`** (lines 63-75) — Tests rejection of excess positionals for `config set` (3 > 2), `plugins install` (2 > 1), and `gateway restart` (1 > 0).

3. **`TestValidateCommand_RejectsShellInjection`** (lines 77-90) — Tests rejection of `;`, `|`, `&`, and `` ` `` characters.

---

## H5: No `window.location.replace` in `client.ts`

**File:** `web/src/api/client.ts`
**Status:** VERIFIED CORRECT

`handleUnauthorized()` (lines 31-43) uses only `unauthorizedHandler?.()` — a callback set externally by the React app (via `onUnauthorized()`). No `window.location.replace`, `window.location.href`, or any direct navigation. Only `window.location` references in the entire `web/src/` are in `RealtimeContext.tsx` for WebSocket protocol/host detection (not redirects).

---

## Summary

| Issue | Verdict |
|-------|---------|
| C1 | PASS |
| C2 | PASS |
| H1 | PASS |
| H2 | PASS |
| H3 | PASS |
| H4 | PASS |
| H5 | PASS |

**All 7 fixes verified correct. No issues found.**
