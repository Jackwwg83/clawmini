# QA Report — Sprint 2A (Device Detail Page) + Sprint 2B (IM Config Wizard)

**Date:** 2026-02-28
**Reviewer:** Claude (deep code review)
**Scope:** DeviceDetailPage, IMConfigPage, App routing, API client, types, RealtimeContext, AuthContext, format utils, command whitelist + whitelist tests

---

## Automated Check Results

| Check | Result |
|-------|--------|
| `go test -race ./...` | **PASS** — all packages pass, no race conditions |
| `go vet ./...` | **PASS** — no issues |
| `npm run lint` | **PASS** — no errors or warnings |
| `npm run build` (tsc + vite) | **PASS** — builds cleanly (993 KB JS bundle, 314 KB gzip) |

---

## Findings

### CRITICAL

#### C1. Credentials with `$` or `\` silently rejected by whitelist — wizard fails with no useful error

**Files:** `internal/openclaw/whitelist.go:38-41`, `web/src/pages/IMConfigPage.tsx:297-306`

The whitelist's shell injection guard rejects ANY argument containing `;|&$`\`:

```go
for _, arg := range args {
    if strings.ContainsAny(arg, ";|&$`\\") {
        return false
    }
}
```

The IM wizard passes user-entered credential values (ClientID, ClientSecret, AppID, AppSecret) as positional args to `config set`:

```ts
args: ['config', 'set', 'plugins.entries.clawdbot-dingtalk.clientSecret', credential.secret],
```

If an OAuth credential contains `$` (common in base64-encoded secrets, e.g., `aB3$xK9...`) or `\`, the `ValidateCommand` call on the device silently returns `false`. The command is rejected before execution, surfacing as a generic "command failed" error. The user has no indication that the credential format is the problem and no path to fix it.

**Impact:** Users with certain valid credentials cannot complete the IM wizard at all.

**Recommendation:**
1. Credential values passed to `config set` should bypass the shell-metacharacter check (they are not interpreted by a shell — they are positional args to the `openclaw` binary).
2. Alternatively, restrict the blocklist to only characters that are actually dangerous for `exec.Command` (which passes args directly, not through a shell): none. The `exec.Command` call does not invoke a shell, so `;|&$` are not dangerous.
3. At minimum, add frontend validation warning the user if their credential contains these characters, with a clear explanation.

---

### HIGH

#### H1. `runCommand` in DeviceDetailPage returns non-terminal record on timeout — caller shows false success

**File:** `web/src/pages/DeviceDetailPage.tsx:930-975`

When the polling loop in `runCommand` exceeds the deadline, it returns the latest `CommandRecord` even if the command is still in `sent` or `queued` status:

```ts
// Line 972 — after the while loop exits
return latest  // status could be 'sent' or 'queued'
```

The callers then check `isCommandFailed(record)` which returns `false` for non-terminal statuses, causing `message.success(...)` to display:

```ts
// GatewayControlCard, line 448-451
if (isCommandFailed(record)) {
  message.error(record.stderr || `${action} 执行失败`)
} else {
  message.success(`网关启动完成`)  // ← shown even if command is still 'sent'
}
```

**Impact:** User sees "网关启动完成" (gateway started) when the command is actually still pending or timed out.

**Contrast:** `IMConfigPage.tsx:508` correctly throws on timeout:
```ts
throw new Error(lastPollError || `命令执行超时：openclaw ${args.join(' ')}`)
```

**Recommendation:** Change `DeviceDetailPage.runCommand` to throw on timeout (matching `IMConfigPage`), or add a `!isTerminalStatus(latest.status)` check in callers to show a "timeout" message instead of success.

---

#### H2. `runConfigureFlow` is not cancellable — continues executing commands after navigation away

**File:** `web/src/pages/IMConfigPage.tsx:531-610`

The configure flow runs 4–5 sequential `await runCommand(...)` calls in a `for` loop. There is no cancellation mechanism. If the user navigates away (e.g., clicks "返回设备详情") mid-flow:

1. The `for` loop continues running in the background.
2. Each `runCommand` call creates a new HTTP request + polling loop.
3. State setters (`setConfigureSteps`, `setConfigureState`, etc.) fire on an unmounted component.
4. Commands continue executing on the device with no UI to observe or abort them.

The verify step (line 623-672) correctly uses a `cancelled` flag via `useEffect` cleanup, but the configure flow does not.

**Impact:** Resource leak, wasted device commands, potential for confusing device state if the user navigates away and back.

**Recommendation:** Add an `AbortController` or `cancelled` ref to `runConfigureFlow`. Check for cancellation between each step. Wire the cleanup to the component's unmount or a "cancel" button.

---

#### H3. `hasValidatedParent` is overly permissive — allows arbitrary extra positional args

**File:** `internal/openclaw/whitelist.go:61-75`

The function checks if ANY validated verb (non-flag) appears anywhere before the current position:

```go
for i := 1; i < idx; i++ {
    if _, ok := allowedSet[args[i]]; ok && !strings.HasPrefix(args[i], "-") {
        return true
    }
}
```

This allows appending arbitrary positional arguments after any validated verb. For example:

```
openclaw plugins install clawdbot-dingtalk malicious-plugin
```

`malicious-plugin` at index 4 passes because `install` at index 2 is a validated verb. Whether `openclaw plugins install` accepts multiple arguments depends on the CLI, but the whitelist does not enforce arity.

Similarly:
```
openclaw config set key value extra1 extra2 extra3
```
All extra positional args pass validation.

**Impact:** The whitelist is more permissive than intended. A compromised server or MITM could inject extra args into whitelisted commands.

**Recommendation:** Enforce maximum arg counts per subcommand+verb combination. For example, `config set` should allow exactly 2 positional args (key + value). `plugins install` should allow exactly 1 positional arg (plugin name).

---

#### H4. Whitelist test coverage does not cover actual IM wizard commands

**File:** `internal/openclaw/whitelist_test.go`

The tests cover generic cases (`config set gateway.mode prod`) but do not test the actual command sequences generated by the IM wizard:

- `plugins install clawdbot-dingtalk`
- `config set plugins.entries.clawdbot-dingtalk.clientId somevalue`
- `config set plugins.entries.clawdbot-dingtalk.clientSecret somevalue`
- `config set plugins.entries.clawdbot-dingtalk.aiCard.enabled true`
- `plugins install @openclaw/feishu`
- `config set plugins.entries.@openclaw/feishu.appId somevalue`
- `channels status`
- `gateway restart`

Missing test scenarios:
- Credentials containing `$`, `\`, or other shell metacharacters (expected: rejected by current code, but should ideally pass — see C1)
- Plugin names with `@` and `/` (e.g., `@openclaw/feishu`)
- Deep dotted config keys (`plugins.entries.clawdbot-dingtalk.aiCard.enabled`)

**Recommendation:** Add test cases for every command the wizard generates, including edge-case credential values.

---

#### H5. `handleUnauthorized` does redundant full-page reload racing with React Router

**File:** `web/src/api/client.ts:31-46`

On 401, `handleUnauthorized` does two things:
1. Calls `unauthorizedHandler()` which sets `token` to `null`, triggering React Router's `<Navigate to="/login">`.
2. Calls `window.location.replace('/login')` which does a full page reload.

These race against each other. The React Router redirect starts, then `window.location.replace` fires and triggers a full page reload, discarding any in-flight React state transitions.

```ts
isHandlingUnauthorized = true
localStorage.removeItem(AUTH_TOKEN_STORAGE_KEY)
unauthorizedHandler?.()              // triggers React Router redirect
if (window.location.pathname !== '/login') {
    window.location.replace('/login') // full page reload — races with above
    return                            // isHandlingUnauthorized stays true forever
}
isHandlingUnauthorized = false        // only reached if already on /login
```

**Impact:** Flash of blank screen during login redirect. The `isHandlingUnauthorized` flag stays `true` in the module scope, but this is effectively reset by the full page reload. Still, the double-redirect is wasteful.

**Recommendation:** Remove `window.location.replace('/login')`. The React Router redirect via `unauthorizedHandler` is sufficient. If React Router redirect doesn't work (edge case), add it as a fallback with a small delay.

---

### MEDIUM

#### M1. Duplicate utility functions across DeviceDetailPage and IMConfigPage

**Files:** `web/src/pages/DeviceDetailPage.tsx:81-99,127-132,220-252,247-252,301-312`, `web/src/pages/IMConfigPage.tsx:97-125,127-129,152-167`

The following functions are defined identically (or near-identically) in both files:

| Function | DeviceDetailPage | IMConfigPage |
|----------|-----------------|--------------|
| `waitFor` | Line 88 | Line 97 |
| `isTerminalStatus` | Line 81 | Line 103 |
| `isCommandFailed` | Line 220 | Line 110 |
| `getErrorMessage` | Line 94 | Line 120 |
| `toLowerText` | Line 130 | Line 127 |
| `toObject` | Line 247 | Line 152 |
| `firstString` | Line 301 | Line 159 |

**Impact:** Maintenance burden. A bug fix in one copy won't be applied to the other.

**Recommendation:** Extract shared utilities into a common module (e.g., `utils/command.ts`).

---

#### M2. `runCommand` polling loops lack `AbortController` for in-flight fetch requests

**Files:** `web/src/pages/DeviceDetailPage.tsx:960-963`, `web/src/pages/IMConfigPage.tsx:498-505`

Both polling loops call `fetchCommandById` repeatedly but never use `AbortController`:

```ts
try {
  latest = await fetchCommandById(token, id, submitted.id)
} catch {
  // ignore
}
```

When the component unmounts or the command reaches terminal status via WebSocket, in-flight HTTP requests continue to completion. The `requestJson` function in `client.ts` also does not accept an `AbortSignal`.

**Impact:** Wasted network requests after unmount or after the result arrives via WebSocket. Minor resource waste, not a correctness issue.

**Recommendation:** Add optional `signal` parameter to `requestJson` and `fetchCommandById`. Pass an `AbortController.signal` that is aborted on cleanup.

---

#### M3. No confirmation dialog before destructive gateway actions

**File:** `web/src/pages/DeviceDetailPage.tsx:480-487`

The "停止" (stop) and "重启" (restart) buttons execute immediately on click without confirmation:

```tsx
<Button danger onClick={() => void runAction('stop')}>停止</Button>
<Button onClick={() => void runAction('restart')}>重启</Button>
```

Stopping the gateway kills all active IM channels and disconnects all connected users.

**Impact:** Accidental gateway stop could cause service disruption.

**Recommendation:** Add `Modal.confirm` for `stop` and `restart` actions with a clear warning about the impact.

---

#### M4. `eslint-disable react-hooks/set-state-in-effect` disables lint at file level

**Files:** `web/src/pages/DeviceDetailPage.tsx:1`, `web/src/pages/IMConfigPage.tsx:1`

Both files use a file-level disable:
```ts
/* eslint-disable react-hooks/set-state-in-effect */
```

This blanket suppression hides any future violations that may be introduced. The existing patterns (setState in async callbacks within effects) are valid and wouldn't trigger this rule — the disable may be unnecessary or could be scoped to specific lines.

**Recommendation:** Remove the file-level disable. If specific lines trigger the rule, use inline `// eslint-disable-next-line` comments instead.

---

#### M5. Hardcoded progress bar at 70% during OpenClaw update

**File:** `web/src/pages/DeviceDetailPage.tsx:587`

```tsx
<Progress percent={70} status="active" showInfo={false} />
```

This shows a progress bar stuck at 70% during the entire update process. It doesn't reflect actual progress.

**Impact:** Misleading UX — users may think the update is almost done when it just started, or may think it's stuck.

**Recommendation:** Either use an indeterminate spinner (remove `percent`), or track actual progress from command status updates.

---

#### M6. Platform selection cards are not keyboard accessible

**File:** `web/src/pages/IMConfigPage.tsx:714-748`

Platform cards use `<Card hoverable onClick={...}>` which renders as a `<div>`. There is no `tabIndex`, `role="button"`, or `onKeyDown` handler:

```tsx
<Card hoverable onClick={() => handleSelectPlatform(item)}>
```

**Impact:** Users navigating with keyboard (Tab + Enter) cannot select a platform. Accessibility (a11y) issue.

**Recommendation:** Add `tabIndex={0}`, `role="button"`, and `onKeyDown` handler for Enter/Space, or use Ant Design's `Radio.Group` with card-style radio buttons.

---

#### M7. No device-offline guard at wizard start

**File:** `web/src/pages/IMConfigPage.tsx:965`

While there's an offline warning banner (`!device.online ? <Alert ...>`), the wizard allows proceeding through all steps even when the device is offline. The configure flow will then fail at each command execution.

**Impact:** User wastes time filling in credentials and watching commands fail one by one.

**Recommendation:** Disable the "下一步" / "我已创建" buttons when `device.online` is false. Show a clear message that the device must be online to proceed.

---

#### M8. Wizard doesn't prevent re-entry during configure flow

**File:** `web/src/pages/IMConfigPage.tsx:612-621`

`handleSubmitCredential` calls `void runConfigureFlow(values)` without checking if a flow is already running:

```ts
const handleSubmitCredential = async () => {
  const values = await form.validateFields()
  setCredentials(values)
  setCurrentStep(3)
  void runConfigureFlow(values)  // no guard against double-invocation
}
```

If the user rapidly clicks "下一步" or navigates back to step 2 and submits again while a flow is running, two concurrent configure flows would execute, interleaving commands on the device.

**Impact:** Corrupted device configuration from interleaved commands.

**Recommendation:** Check `configureState === 'running'` before starting a new flow, or disable the submit button while running.

---

### LOW

#### L1. Bundle size exceeds recommended limit

**Build output:**
```
dist/assets/index-BZXC3rUB.js   993.03 kB │ gzip: 313.76 kB
```

The JS bundle is ~993 KB (314 KB gzip), exceeding Vite's 500 KB warning threshold. Ant Design and its icons are likely the main contributors.

**Recommendation:** Use dynamic `import()` for page-level code splitting (React.lazy + Suspense). Consider Ant Design's tree-shaking setup or modular imports for icons.

---

#### L2. `COMMAND_POLL_INTERVAL_MS` differs between the two pages

**Files:** `web/src/pages/DeviceDetailPage.tsx:37` (1200ms), `web/src/pages/IMConfigPage.tsx:35` (2000ms)

```ts
// DeviceDetailPage
const COMMAND_POLL_INTERVAL_MS = 1200

// IMConfigPage
const COMMAND_POLL_INTERVAL_MS = 2000
```

**Impact:** Inconsistent polling behavior. Not a bug, but confusing for maintainers.

**Recommendation:** Extract to a shared constant if the intent is the same.

---

#### L3. `TERMINAL_STATUS` set duplicated

**Files:** `web/src/pages/DeviceDetailPage.tsx:36`, `web/src/pages/IMConfigPage.tsx:37`

Both files define `const TERMINAL_STATUS = new Set(['completed', 'failed'])`.

**Recommendation:** Extract to shared module alongside the other duplicate utilities (see M1).

---

#### L4. `firstString` return type inconsistency

**Files:** `web/src/pages/DeviceDetailPage.tsx:301` returns `string | undefined`, `web/src/pages/IMConfigPage.tsx:159` returns `string`

```ts
// DeviceDetailPage
function firstString(...): string | undefined { ... return undefined }

// IMConfigPage
function firstString(...): string { ... return '' }
```

**Impact:** Different fallback behavior between the two copies. Could cause subtle bugs if consumers are copied between files.

---

#### L5. No error boundary for device detail or IM config pages

**Files:** `web/src/pages/DeviceDetailPage.tsx`, `web/src/pages/IMConfigPage.tsx`

Neither page wraps its content in a React error boundary. A runtime error in any child component (e.g., malformed device data causing a render crash) would propagate up and potentially crash the entire app.

**Recommendation:** Add an error boundary wrapper, at minimum around the card sub-components.

---

#### L6. `VERIFY_WAIT_MS = 10000` is a fixed delay, not adaptive

**File:** `web/src/pages/IMConfigPage.tsx:36`

The verify step always waits exactly 10 seconds before checking channel status:

```ts
await waitFor(VERIFY_WAIT_MS)
```

If the gateway restarts quickly (< 3s), the user waits unnecessarily. If it restarts slowly (> 10s), the check may fail prematurely.

**Recommendation:** Poll for gateway status in a loop with a timeout, rather than using a fixed delay.

---

#### L7. Emoji in Result component titles

**File:** `web/src/pages/IMConfigPage.tsx:889,903`

```tsx
title={`✅ ${verifyChannelName}已连接`}
title="❌ 连接失败"
```

These use text emoji while the rest of the UI uses Ant Design icons for status indicators. Minor inconsistency.

---

## Security Checklist

| Check | Status | Notes |
|-------|--------|-------|
| Credentials logged in console/URLs? | **PASS** | `displayCommand` masks secrets with `******` and `<已填写>` |
| Credentials in browser history/URLs? | **PASS** | Credentials are sent via POST body, not URL params |
| Command output sanitized before display? | **PASS** | All output rendered as text content in `<pre>` or `<Alert>`, no `dangerouslySetInnerHTML` |
| XSS via command output? | **PASS** | React's default escaping handles this |
| Token stored securely? | **WARN** | Token in `localStorage` (vulnerable to XSS); acceptable for internal tool |
| Shell injection via credential values? | **FAIL** | See C1 — whitelist rejects `$`/`\` in credential values |
| Credential exposure in stderr? | **WARN** | If `openclaw config set` echoes args in error output, credentials appear in stderr displayed to user (user already knows the value, but screen-sharing risk) |

---

## IM Wizard Command Validation Matrix

Tracing each wizard command through `ValidateCommand`:

| Step | Command Args | Passes Whitelist? | Notes |
|------|-------------|-------------------|-------|
| 1. Install plugin (dingtalk) | `['plugins', 'install', 'clawdbot-dingtalk']` | **YES** | `install` in allowedSet; `clawdbot-dingtalk` passes `hasValidatedParent` |
| 2. Set clientId | `['config', 'set', 'plugins.entries...clientId', '<value>']` | **YES**\* | \*Only if value has no `$\;|&\`` chars |
| 3. Set clientSecret | `['config', 'set', 'plugins.entries...clientSecret', '<value>']` | **YES**\* | \*Same restriction |
| 4. Enable aiCard | `['config', 'set', 'plugins.entries...aiCard.enabled', 'true']` | **YES** | No special chars |
| 5. Restart gateway | `['gateway', 'restart']` | **YES** | `restart` in allowedSet |
| 6. Verify channels | `['channels', 'status']` | **YES** | `status` in allowedSet |
| Install plugin (feishu) | `['plugins', 'install', '@openclaw/feishu']` | **YES** | `@` and `/` not in blocklist |
| Set feishu appId | `['config', 'set', 'plugins.entries.@openclaw/feishu.appId', '<value>']` | **YES**\* | \*Same restriction |

---

## Summary

| Severity | Count | Key Concerns |
|----------|-------|-------------|
| **Critical** | 1 | Credentials with `$`/`\` silently rejected by whitelist |
| **High** | 5 | False success on timeout, no configure-flow cancellation, overly permissive `hasValidatedParent`, missing wizard command tests, double-redirect race |
| **Medium** | 8 | Duplicate utilities, no AbortController, no destructive-action confirmation, blanket eslint-disable, fake progress bar, a11y, no offline guard, no re-entry guard |
| **Low** | 7 | Bundle size, inconsistent constants, no error boundary, fixed verify delay, emoji inconsistency |

**Overall Assessment:** The Sprint 2A Device Detail Page is well-structured with clean component decomposition and proper loading/empty/error states. All text is correctly in Chinese. The Sprint 2B IM Config Wizard implements a clear 5-step flow with proper state management and a verify-on-complete pattern. The most significant issue is **C1** — the whitelist's shell-metacharacter guard will silently reject credentials containing `$` or `\`, making the wizard unusable for some users. The timeout handling discrepancy (**H1**) between the two pages should also be addressed before release.
