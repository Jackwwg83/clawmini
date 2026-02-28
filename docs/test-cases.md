# ClawMini MVP Comprehensive Test Cases

## Scope and conventions
- Priorities: `P0` = demo-blocking/core reliability/security, `P1` = important robustness, `P2` = nice-to-have/edge behavior.
- Environment baseline for most tests: 1 Manager Server (Go + SQLite), 2-3 client test devices, TLS-enabled WSS endpoint, seeded admin token, at least one valid client join token.
- Unless noted, API calls use Bearer token auth and responses are JSON.

## 1) Server Tests

### TC-SRV-AUTH-001
- ID: `TC-SRV-AUTH-001`
- Category: `Server - Auth`
- Description: Admin API accepts valid admin token.
- Preconditions: Valid admin token exists.
- Steps:
  1. Call `GET /api/devices` with `Authorization: Bearer <valid_admin_token>`.
- Expected Result: HTTP `200`; device list payload returned.
- Priority: `P0`

### TC-SRV-AUTH-002
- ID: `TC-SRV-AUTH-002`
- Category: `Server - Auth`
- Description: Admin API rejects missing token.
- Preconditions: None.
- Steps:
  1. Call `GET /api/devices` without auth header.
- Expected Result: HTTP `401`; deterministic error message; no data leakage.
- Priority: `P0`

### TC-SRV-AUTH-003
- ID: `TC-SRV-AUTH-003`
- Category: `Server - Auth`
- Description: Admin API rejects malformed/invalid token.
- Preconditions: Invalid token string prepared.
- Steps:
  1. Call `GET /api/devices` with `Authorization: Bearer invalid-token`.
- Expected Result: HTTP `401`; request denied; audit/event log contains auth failure reason code.
- Priority: `P0`

### TC-SRV-AUTH-004
- ID: `TC-SRV-AUTH-004`
- Category: `Server - Auth`
- Description: Rotated/revoked token is immediately invalid.
- Preconditions: Token rotation endpoint or config process available.
- Steps:
  1. Verify old token works on `GET /api/devices`.
  2. Rotate/revoke token.
  3. Retry same API using old token.
  4. Retry using new token.
- Expected Result: Old token fails with `401`; new token succeeds with `200`.
- Priority: `P0`

### TC-SRV-AUTH-005
- ID: `TC-SRV-AUTH-005`
- Category: `Server - Auth`
- Description: Client join/device token cannot access admin endpoints.
- Preconditions: Valid client join token exists.
- Steps:
  1. Use client token on `GET /api/devices`.
- Expected Result: HTTP `403` or `401` (as designed); no admin data returned.
- Priority: `P0`

### TC-SRV-REG-001
- ID: `TC-SRV-REG-001`
- Category: `Server - Device Registration`
- Description: New device registers successfully over WSS.
- Preconditions: Valid join token; device has reachable server URL.
- Steps:
  1. Connect client via WSS.
  2. Send registration payload (`deviceId`, hostname, OpenClaw version, system info).
- Expected Result: Registration ack returned; device persisted; device appears `online` in list.
- Priority: `P0`

### TC-SRV-REG-002
- ID: `TC-SRV-REG-002`
- Category: `Server - Device Registration`
- Description: Duplicate registration for same device updates active session safely.
- Preconditions: Device already registered and online.
- Steps:
  1. Start second client session using same `deviceId`.
- Expected Result: Server deterministically handles conflict (replaces old session or rejects per spec); no duplicate device rows.
- Priority: `P1`

### TC-SRV-REG-003
- ID: `TC-SRV-REG-003`
- Category: `Server - Device Registration`
- Description: Registration rejects missing required fields.
- Preconditions: WSS connectivity available.
- Steps:
  1. Send registration payload missing hostname or OpenClaw version.
- Expected Result: Server rejects with validation error; device not marked online.
- Priority: `P0`

### TC-SRV-REG-004
- ID: `TC-SRV-REG-004`
- Category: `Server - Device Registration`
- Description: Expired/invalid join token cannot register device.
- Preconditions: Expired or forged join token.
- Steps:
  1. Attempt WSS registration with invalid token.
- Expected Result: Registration denied; connection closed or marked unauthorized; no DB insert.
- Priority: `P0`

### TC-SRV-REG-005
- ID: `TC-SRV-REG-005`
- Category: `Server - Device Registration`
- Description: One-time join token behavior enforced (if configured one-time use).
- Preconditions: One-time token policy enabled.
- Steps:
  1. Register device A using token.
  2. Attempt register device B with same token.
- Expected Result: First succeeds; second rejected as token already consumed.
- Priority: `P1`

### TC-SRV-WSS-001
- ID: `TC-SRV-WSS-001`
- Category: `Server - WSS Connection`
- Description: WSS handshake requires TLS and valid protocol upgrade.
- Preconditions: Server WSS endpoint exposed.
- Steps:
  1. Attempt plain WS connection (`ws://`).
  2. Attempt proper WSS (`wss://`) handshake.
- Expected Result: Plain WS denied (if TLS-only mode); WSS accepted.
- Priority: `P0`

### TC-SRV-WSS-002
- ID: `TC-SRV-WSS-002`
- Category: `Server - WSS Connection`
- Description: Heartbeat timeout marks device offline.
- Preconditions: Registered online device.
- Steps:
  1. Stop sending heartbeat/status from client beyond timeout window.
- Expected Result: Server flips device state to `offline` within configured SLA; UI event emitted.
- Priority: `P0`

### TC-SRV-WSS-003
- ID: `TC-SRV-WSS-003`
- Category: `Server - WSS Connection`
- Description: Reconnect restores same device online state.
- Preconditions: Device offline due to dropped socket.
- Steps:
  1. Reconnect client with same device identity.
  2. Send registration + heartbeat.
- Expected Result: Device transitions back to `online`; `lastSeen` updates; stale session cleaned.
- Priority: `P0`

### TC-SRV-WSS-004
- ID: `TC-SRV-WSS-004`
- Category: `Server - WSS Connection`
- Description: Malformed WSS message is safely handled.
- Preconditions: Active client connection.
- Steps:
  1. Send invalid JSON frame.
  2. Send valid frame afterward from same or new connection (based on policy).
- Expected Result: Server returns parse error and closes or quarantines session per spec; server process remains stable.
- Priority: `P0`

### TC-SRV-CMD-001
- ID: `TC-SRV-CMD-001`
- Category: `Server - Command Dispatch`
- Description: Online device receives and executes `gateway restart` command.
- Preconditions: Device online and command-capable.
- Steps:
  1. Call `POST /api/devices/{id}/commands` with restart payload.
  2. Observe command lifecycle events (`queued` -> `running` -> `success`).
- Expected Result: Terminal state `success`; output stored; activity log entry created.
- Priority: `P0`

### TC-SRV-CMD-002
- ID: `TC-SRV-CMD-002`
- Category: `Server - Command Dispatch`
- Description: Command to offline device returns deterministic failure.
- Preconditions: Target device offline.
- Steps:
  1. Send doctor command to offline device.
- Expected Result: API returns clear rejection (`409`/`422` per spec); command not dispatched.
- Priority: `P0`

### TC-SRV-CMD-003
- ID: `TC-SRV-CMD-003`
- Category: `Server - Command Dispatch`
- Description: Command timeout handling is deterministic.
- Preconditions: Client can simulate hanging command.
- Steps:
  1. Dispatch command that exceeds timeout.
- Expected Result: Command state transitions to `timeout`/`failed`; timeout reason persisted; UI receives final state.
- Priority: `P0`

### TC-SRV-CMD-004
- ID: `TC-SRV-CMD-004`
- Category: `Server - Command Dispatch`
- Description: Duplicate command submission with same idempotency key does not execute twice.
- Preconditions: Idempotency key support enabled.
- Steps:
  1. Submit same command request twice with identical idempotency key.
- Expected Result: Single execution on device; second request returns original command reference/result.
- Priority: `P1`

### TC-SRV-CMD-005
- ID: `TC-SRV-CMD-005`
- Category: `Server - Command Dispatch`
- Description: Per-device command concurrency policy enforced.
- Preconditions: Device online.
- Steps:
  1. Dispatch long-running `doctor` command.
  2. Immediately dispatch restart command.
- Expected Result: Second command queued or rejected based on policy; no undefined interleaving.
- Priority: `P1`

### TC-SRV-STAT-001
- ID: `TC-SRV-STAT-001`
- Category: `Server - Status Aggregation`
- Description: Periodic status updates refresh aggregate device state.
- Preconditions: Device online; status reporting active.
- Steps:
  1. Send status payload with CPU/memory/disk, OpenClaw status, IM status.
  2. Query device detail endpoint.
- Expected Result: Latest metrics reflected in DB/API/UI; `lastSeen` timestamp updated.
- Priority: `P0`

### TC-SRV-STAT-002
- ID: `TC-SRV-STAT-002`
- Category: `Server - Status Aggregation`
- Description: Stale status transitions from online to stale/offline indicator.
- Preconditions: Device had prior fresh status.
- Steps:
  1. Pause status updates beyond staleness threshold.
- Expected Result: Aggregator marks stale/offline consistently and emits realtime update.
- Priority: `P0`

### TC-SRV-STAT-003
- ID: `TC-SRV-STAT-003`
- Category: `Server - Status Aggregation`
- Description: Partial status payload does not erase unrelated previous metrics.
- Preconditions: Device has full status stored.
- Steps:
  1. Send payload containing only IM status field updates.
- Expected Result: IM fields update; CPU/memory/disk/openclaw version fields remain from last known values unless explicitly nullified by design.
- Priority: `P1`

### TC-SRV-STAT-004
- ID: `TC-SRV-STAT-004`
- Category: `Server - Status Aggregation`
- Description: Out-of-order status timestamps are handled correctly.
- Preconditions: Ability to send synthetic timestamps.
- Steps:
  1. Send newer status message.
  2. Send older status message.
- Expected Result: Aggregate keeps newest snapshot; older message stored only as history if applicable.
- Priority: `P1`

### TC-SRV-API-001
- ID: `TC-SRV-API-001`
- Category: `Server - API Endpoints`
- Description: Device list endpoint supports pagination/filter/sort.
- Preconditions: >=10 seeded devices with mixed states.
- Steps:
  1. Call list endpoint with status filter `online`, then `offline`.
  2. Apply sort by `lastSeen` desc.
  3. Request page 2.
- Expected Result: Correct subset and ordering returned; pagination metadata accurate.
- Priority: `P1`

### TC-SRV-API-002
- ID: `TC-SRV-API-002`
- Category: `Server - API Endpoints`
- Description: Device detail endpoint returns full view model.
- Preconditions: Device with history and recent commands exists.
- Steps:
  1. Call `GET /api/devices/{id}`.
- Expected Result: Includes identity, health, resources, IM state, last activity list, and last command summary.
- Priority: `P0`

### TC-SRV-API-003
- ID: `TC-SRV-API-003`
- Category: `Server - API Endpoints`
- Description: Command result endpoint returns lifecycle and output.
- Preconditions: Executed command exists.
- Steps:
  1. Call `GET /api/commands/{commandId}`.
- Expected Result: Returns status, start/end timestamps, exit code, sanitized stdout/stderr.
- Priority: `P0`

### TC-SRV-API-004
- ID: `TC-SRV-API-004`
- Category: `Server - API Endpoints`
- Description: Frontend realtime WS subscription emits status and command events.
- Preconditions: UI WS channel connected with valid token.
- Steps:
  1. Trigger device status change.
  2. Dispatch a command.
- Expected Result: Subscriber receives structured events in expected schema/order.
- Priority: `P0`

## 2) Client Tests

### TC-CLI-WSS-001
- ID: `TC-CLI-WSS-001`
- Category: `Client - WSS Reconnection`
- Description: Client retries initial connection with backoff when server unavailable.
- Preconditions: Client configured to point at unavailable server.
- Steps:
  1. Start client.
  2. Observe retry intervals.
  3. Bring server online.
- Expected Result: Exponential (or configured) backoff respected; client eventually connects automatically.
- Priority: `P0`

### TC-CLI-WSS-002
- ID: `TC-CLI-WSS-002`
- Category: `Client - WSS Reconnection`
- Description: Mid-session network drop triggers reconnect and re-register.
- Preconditions: Client online.
- Steps:
  1. Cut network for 1-2 minutes.
  2. Restore network.
- Expected Result: Client reconnects, re-registers once, resumes status/heartbeat without manual intervention.
- Priority: `P0`

### TC-CLI-WSS-003
- ID: `TC-CLI-WSS-003`
- Category: `Client - WSS Reconnection`
- Description: Reconnect jitter avoids synchronized reconnect storm.
- Preconditions: Start 20+ clients simultaneously; force server restart.
- Steps:
  1. Restart server.
  2. Observe reconnect timings.
- Expected Result: Reconnect attempts are distributed over jitter window; server remains stable.
- Priority: `P1`

### TC-CLI-HB-001
- ID: `TC-CLI-HB-001`
- Category: `Client - Heartbeat`
- Description: Client sends heartbeat/status at configured 30s cadence.
- Preconditions: Client online, sampling interval default.
- Steps:
  1. Run client for >=3 intervals.
  2. Check sent timestamps.
- Expected Result: Interval drift within acceptable tolerance; no missed heartbeats.
- Priority: `P0`

### TC-CLI-HB-002
- ID: `TC-CLI-HB-002`
- Category: `Client - Heartbeat`
- Description: Long-running command does not block heartbeat loop.
- Preconditions: Command that runs >30s available.
- Steps:
  1. Dispatch long-running command.
  2. Monitor heartbeat frames during execution.
- Expected Result: Heartbeats continue on schedule while command runs.
- Priority: `P0`

### TC-CLI-HB-003
- ID: `TC-CLI-HB-003`
- Category: `Client - Heartbeat`
- Description: Client responds to server ping/pong keepalive.
- Preconditions: Ping/pong enabled.
- Steps:
  1. Observe server-initiated ping frames.
- Expected Result: Timely pong responses; connection remains healthy.
- Priority: `P1`

### TC-CLI-WL-001
- ID: `TC-CLI-WL-001`
- Category: `Client - Command Whitelist`
- Description: Allowed OpenClaw command executes successfully.
- Preconditions: Client online.
- Steps:
  1. Dispatch `openclaw doctor --json`.
- Expected Result: Command accepted and executed; result returned to server.
- Priority: `P0`

### TC-CLI-WL-002
- ID: `TC-CLI-WL-002`
- Category: `Client - Command Whitelist`
- Description: Non-whitelisted command is rejected.
- Preconditions: Client online.
- Steps:
  1. Dispatch command `uname -a` or `rm -rf /tmp/x`.
- Expected Result: Client rejects with `not allowed`; command never reaches shell/process execution.
- Priority: `P0`

### TC-CLI-WL-003
- ID: `TC-CLI-WL-003`
- Category: `Client - Command Whitelist`
- Description: Whitelisted base command with injected metacharacters is rejected.
- Preconditions: Client online.
- Steps:
  1. Dispatch `openclaw status --json; cat /etc/passwd`.
  2. Dispatch `openclaw config get key && whoami`.
- Expected Result: Parser rejects payload as invalid; no injected command execution.
- Priority: `P0`

### TC-CLI-WRAP-001
- ID: `TC-CLI-WRAP-001`
- Category: `Client - OpenClaw CLI Wrapper`
- Description: Wrapper captures stdout, stderr, exit code, and duration.
- Preconditions: Command generating mixed output available.
- Steps:
  1. Execute command via client wrapper.
- Expected Result: Structured result includes separate streams, exit code, timing metadata.
- Priority: `P0`

### TC-CLI-WRAP-002
- ID: `TC-CLI-WRAP-002`
- Category: `Client - OpenClaw CLI Wrapper`
- Description: Wrapper enforces execution timeout and kills process tree.
- Preconditions: Hanging command fixture available.
- Steps:
  1. Run hanging command with timeout configured.
- Expected Result: Process terminated at timeout; response marked timeout with deterministic error text.
- Priority: `P0`

### TC-CLI-WRAP-003
- ID: `TC-CLI-WRAP-003`
- Category: `Client - OpenClaw CLI Wrapper`
- Description: Missing `openclaw` binary is surfaced clearly.
- Preconditions: Temporarily rename/remove `openclaw` binary from PATH.
- Steps:
  1. Execute any command.
- Expected Result: Error indicates binary not found; client remains running and recoverable.
- Priority: `P1`

### TC-CLI-STAT-001
- ID: `TC-CLI-STAT-001`
- Category: `Client - Status Collection`
- Description: Client parses `openclaw status --json` and sends normalized payload.
- Preconditions: OpenClaw installed and healthy.
- Steps:
  1. Trigger status collection cycle.
- Expected Result: Payload includes gateway status, version, channel/plugin/IM health fields.
- Priority: `P0`

### TC-CLI-STAT-002
- ID: `TC-CLI-STAT-002`
- Category: `Client - Status Collection`
- Description: Invalid JSON from OpenClaw status handled without crash.
- Preconditions: Stub command returns malformed JSON.
- Steps:
  1. Run collection cycle against malformed output.
- Expected Result: Parse error reported in status/error channel; client keeps running and retries next cycle.
- Priority: `P0`

### TC-CLI-STAT-003
- ID: `TC-CLI-STAT-003`
- Category: `Client - Status Collection`
- Description: System resource metrics collection tolerates partial probe failure.
- Preconditions: Simulate one probe failure (e.g., disk metric unavailable).
- Steps:
  1. Run status collection.
- Expected Result: Available metrics still reported; failed probe flagged; no full-report drop.
- Priority: `P1`

## 3) Frontend Tests

### TC-FE-LIST-001
- ID: `TC-FE-LIST-001`
- Category: `Frontend - Device List Rendering`
- Description: Device list renders rows with core fields and status badges.
- Preconditions: API returns multiple devices with mixed states.
- Steps:
  1. Open fleet page.
- Expected Result: Each row shows hostname/device ID, online/offline, OpenClaw version, resource summary.
- Priority: `P0`

### TC-FE-LIST-002
- ID: `TC-FE-LIST-002`
- Category: `Frontend - Device List Rendering`
- Description: Loading, empty, and API-error states are user-readable.
- Preconditions: Ability to simulate delayed success, empty response, and server error.
- Steps:
  1. Open page under each scenario.
- Expected Result: Skeleton/spinner during load, clear empty-state message, deterministic retry-capable error state.
- Priority: `P1`

### TC-FE-LIST-003
- ID: `TC-FE-LIST-003`
- Category: `Frontend - Device List Rendering`
- Description: Search/filter/sort controls produce expected dataset.
- Preconditions: Seed devices with varied names/states/lastSeen.
- Steps:
  1. Search by hostname fragment.
  2. Filter by online status.
  3. Sort by last seen descending.
- Expected Result: UI shows correct subset/order and preserves control states.
- Priority: `P1`

### TC-FE-RT-001
- ID: `TC-FE-RT-001`
- Category: `Frontend - Realtime Status Updates`
- Description: Realtime connection indicator reflects websocket state.
- Preconditions: Frontend WS connected.
- Steps:
  1. Observe indicator while connected.
  2. Drop WS temporarily.
  3. Restore connection.
- Expected Result: Indicator transitions `connected -> reconnecting -> connected` correctly.
- Priority: `P0`

### TC-FE-RT-002
- ID: `TC-FE-RT-002`
- Category: `Frontend - Realtime Status Updates`
- Description: Device row updates in place without manual refresh.
- Preconditions: Fleet page open; device online.
- Steps:
  1. Trigger status change on device (e.g., CPU spike or IM disconnected).
- Expected Result: Corresponding row updates within realtime SLA; no full page reload.
- Priority: `P0`

### TC-FE-RT-003
- ID: `TC-FE-RT-003`
- Category: `Frontend - Realtime Status Updates`
- Description: Offline transition appears after heartbeat timeout event.
- Preconditions: Device currently online in UI.
- Steps:
  1. Stop client heartbeat.
- Expected Result: Row state changes to offline with updated last-seen timestamp.
- Priority: `P0`

### TC-FE-RT-004
- ID: `TC-FE-RT-004`
- Category: `Frontend - Realtime Status Updates`
- Description: Out-of-order websocket events do not regress visible state.
- Preconditions: Event replay/simulation tooling available.
- Steps:
  1. Emit newer status event.
  2. Emit older status event.
- Expected Result: UI retains newest state and ignores stale regression.
- Priority: `P1`

### TC-FE-CMD-001
- ID: `TC-FE-CMD-001`
- Category: `Frontend - Remote Command UI`
- Description: Command action buttons trigger correct API payload.
- Preconditions: Device detail page open for online device.
- Steps:
  1. Click `Restart Gateway`.
  2. Click `Run Doctor`.
- Expected Result: Matching command requests sent with correct device ID and command type.
- Priority: `P0`

### TC-FE-CMD-002
- ID: `TC-FE-CMD-002`
- Category: `Frontend - Remote Command UI`
- Description: UI shows command progress and terminal result.
- Preconditions: Long-running command available.
- Steps:
  1. Trigger command.
  2. Observe running state and completion.
- Expected Result: Button disabled while running; progress/result panel updates to success/failure with output snippet.
- Priority: `P0`

### TC-FE-CMD-003
- ID: `TC-FE-CMD-003`
- Category: `Frontend - Remote Command UI`
- Description: Command failure surfaces deterministic remediation message.
- Preconditions: Simulate command timeout/failure.
- Steps:
  1. Trigger failing command.
- Expected Result: Error text is actionable (timeout, offline, permission, etc.) and not raw stack trace.
- Priority: `P1`

### TC-FE-IM-001
- ID: `TC-FE-IM-001`
- Category: `Frontend - IM Config Wizard`
- Description: DingTalk wizard step validation blocks incomplete input.
- Preconditions: Device detail page and IM wizard accessible.
- Steps:
  1. Choose DingTalk.
  2. Attempt continue with missing `ClientID`/`ClientSecret`.
- Expected Result: Inline validation errors; cannot proceed until required fields valid.
- Priority: `P0`

### TC-FE-IM-002
- ID: `TC-FE-IM-002`
- Category: `Frontend - IM Config Wizard`
- Description: Feishu wizard path uses correct provider-specific fields/help text.
- Preconditions: IM wizard open.
- Steps:
  1. Choose Feishu provider.
- Expected Result: Feishu-specific instructions and form keys shown, not DingTalk fields.
- Priority: `P1`

### TC-FE-IM-003
- ID: `TC-FE-IM-003`
- Category: `Frontend - IM Config Wizard`
- Description: Wizard progress reflects backend command sequence status.
- Preconditions: Prepared device with plugin installed (demo-safe path).
- Steps:
  1. Submit valid credentials.
  2. Observe sequence: config set -> gateway restart -> verify status.
- Expected Result: Stepper marks each stage success/failure with timestamps and retry action on failure.
- Priority: `P0`

### TC-FE-IM-004
- ID: `TC-FE-IM-004`
- Category: `Frontend - IM Config Wizard`
- Description: Wizard ends in success only after status confirms connected.
- Preconditions: Valid credentials; status reporting active.
- Steps:
  1. Complete wizard submission.
  2. Wait for realtime status confirmation.
- Expected Result: Final success state only when IM status is `connected`; otherwise shows pending/failed.
- Priority: `P0`

## 4) Integration Tests

### TC-INT-JOIN-001
- ID: `TC-INT-JOIN-001`
- Category: `Integration - Full Device Join Flow`
- Description: End-to-end join command adds a new device into fleet view.
- Preconditions: Fresh device not yet registered; valid join token generated.
- Steps:
  1. Run install/join command on device: `openclaw-manager-client join --server ... --token ...`.
  2. Open fleet page.
- Expected Result: New device appears within SLA as online with initial status snapshot.
- Priority: `P0`

### TC-INT-JOIN-002
- ID: `TC-INT-JOIN-002`
- Category: `Integration - Full Device Join Flow`
- Description: Invalid token join fails with clear operator guidance.
- Preconditions: Invalid token prepared.
- Steps:
  1. Run join command with invalid token.
- Expected Result: Join fails with explicit auth error and next-step hint; server fleet unchanged.
- Priority: `P0`

### TC-INT-RST-001
- ID: `TC-INT-RST-001`
- Category: `Integration - Remote Restart`
- Description: Remote restart command shows expected transient and recovered states.
- Preconditions: Healthy online device in detail page.
- Steps:
  1. Trigger `gateway restart` from UI.
  2. Observe status transitions.
- Expected Result: Device/OpenClaw status transitions (restarting/degraded -> healthy) and returns online without manual refresh.
- Priority: `P0`

### TC-INT-RST-002
- ID: `TC-INT-RST-002`
- Category: `Integration - Remote Restart`
- Description: Restart on unstable network still converges to correct final state.
- Preconditions: Network shaping tool can inject packet loss.
- Steps:
  1. Trigger restart while introducing temporary packet loss.
- Expected Result: Command may be delayed but final state and command outcome remain consistent; no stuck `running` forever.
- Priority: `P1`

### TC-INT-DR-001
- ID: `TC-INT-DR-001`
- Category: `Integration - Remote Doctor`
- Description: `doctor --json` returns structured diagnostics to UI.
- Preconditions: Device online.
- Steps:
  1. Trigger doctor command.
- Expected Result: UI shows parsed diagnostic summary and raw output snippet.
- Priority: `P0`

### TC-INT-DR-002
- ID: `TC-INT-DR-002`
- Category: `Integration - Remote Doctor`
- Description: `doctor --repair` remediation changes health signal.
- Preconditions: Device intentionally in recoverable unhealthy state.
- Steps:
  1. Trigger repair doctor command.
  2. Wait for post-command status cycle.
- Expected Result: Health indicator improves from unhealthy/warning to healthy (or clearly reports unrecoverable issue).
- Priority: `P1`

### TC-INT-IM-001
- ID: `TC-INT-IM-001`
- Category: `Integration - IM Config End-to-End`
- Description: Prepared-plugin IM flow succeeds end to end (demo-safe path).
- Preconditions: DingTalk/Feishu plugin preinstalled; valid credentials available.
- Steps:
  1. Use wizard to apply credentials.
  2. Restart gateway via automated sequence.
  3. Observe IM status and test message event.
- Expected Result: IM state reaches `connected`; test interaction reflected in status/activity feed.
- Priority: `P0`

### TC-INT-IM-002
- ID: `TC-INT-IM-002`
- Category: `Integration - IM Config End-to-End`
- Description: Invalid IM credentials produce deterministic failure and recover on retry.
- Preconditions: Invalid then valid credential sets prepared.
- Steps:
  1. Submit invalid credentials.
  2. Confirm failure state and error.
  3. Resubmit valid credentials.
- Expected Result: First attempt fails with actionable reason; second succeeds without manual backend cleanup.
- Priority: `P0`

## 5) Demo-Specific Tests

### TC-DEMO-FIX-001
- ID: `TC-DEMO-FIX-001`
- Category: `Demo - Demo Mode Fixtures`
- Description: Demo mode seeds deterministic fleet (e.g., 3 devices) on startup.
- Preconditions: Demo mode enabled.
- Steps:
  1. Start server in demo mode.
  2. Open fleet overview.
- Expected Result: Predefined devices and statuses appear consistently across runs.
- Priority: `P0`

### TC-DEMO-FIX-002
- ID: `TC-DEMO-FIX-002`
- Category: `Demo - Demo Mode Fixtures`
- Description: Fixture scenario toggles (healthy/unhealthy/recovering) work via control panel or API.
- Preconditions: Demo fixture controls enabled.
- Steps:
  1. Toggle device to unhealthy scenario.
  2. Run doctor/restart demo action.
- Expected Result: Narrative-friendly before/after transitions are deterministic.
- Priority: `P1`

### TC-DEMO-FIX-003
- ID: `TC-DEMO-FIX-003`
- Category: `Demo - Demo Mode Fixtures`
- Description: Demo fixtures isolate from real production device data.
- Preconditions: Non-demo devices also exist in backend.
- Steps:
  1. Start demo mode.
  2. Query fleet data.
- Expected Result: Only demo-scoped fixtures shown (or clearly segmented), avoiding accidental real-data leakage.
- Priority: `P1`

### TC-DEMO-PF-001
- ID: `TC-DEMO-PF-001`
- Category: `Demo - Preflight Checker`
- Description: Preflight pass verifies all critical demo dependencies.
- Preconditions: All dependencies healthy.
- Steps:
  1. Run preflight check command/script.
- Expected Result: Pass report includes WSS reachability, token validity, plugin presence, OpenClaw health, and frontend-server connectivity.
- Priority: `P0`

### TC-DEMO-PF-002
- ID: `TC-DEMO-PF-002`
- Category: `Demo - Preflight Checker`
- Description: Preflight fail surfaces actionable blockers before demo.
- Preconditions: Intentionally break one dependency (e.g., invalid token or missing plugin).
- Steps:
  1. Run preflight check.
- Expected Result: Fails with concise remediation guidance and non-zero exit code.
- Priority: `P0`

### TC-DEMO-RST-001
- ID: `TC-DEMO-RST-001`
- Category: `Demo - One-Click Reset`
- Description: One-click reset restores known-good demo state.
- Preconditions: Demo run modified credentials/command history/status states.
- Steps:
  1. Execute reset script/button once.
  2. Reload UI.
- Expected Result: Devices, IM config state, command queue/history, and fixture scenario return to baseline snapshot.
- Priority: `P0`

### TC-DEMO-RST-002
- ID: `TC-DEMO-RST-002`
- Category: `Demo - One-Click Reset`
- Description: Reset operation is idempotent and quick enough for live rerun.
- Preconditions: Baseline already restored.
- Steps:
  1. Execute reset repeatedly (3 times).
  2. Measure duration.
- Expected Result: No errors on repeated runs; each run completes within defined demo SLA (e.g., <60s).
- Priority: `P1`

## 6) Security Tests

### TC-SEC-INJ-001
- ID: `TC-SEC-INJ-001`
- Category: `Security - Command Injection Prevention`
- Description: Reject shell metacharacters in command payload.
- Preconditions: Online device and command endpoint access.
- Steps:
  1. Submit payloads containing `;`, `&&`, `||`, `` ` ``, `$()`.
- Expected Result: Request rejected at validation/whitelist layer; no execution occurs on client.
- Priority: `P0`

### TC-SEC-INJ-002
- ID: `TC-SEC-INJ-002`
- Category: `Security - Command Injection Prevention`
- Description: Config set command sanitizes/escapes arbitrary value content safely.
- Preconditions: Ability to send special characters in config value.
- Steps:
  1. Submit `openclaw config set` with value containing spaces/quotes/shell-like symbols.
- Expected Result: Value passed as literal argument (not interpreted by shell); command either succeeds safely or fails validation deterministically.
- Priority: `P0`

### TC-SEC-INJ-003
- ID: `TC-SEC-INJ-003`
- Category: `Security - Command Injection Prevention`
- Description: Sensitive secrets are redacted in command output logs.
- Preconditions: IM credential set commands executed.
- Steps:
  1. Execute config set for client secret.
  2. Retrieve command logs/result via API.
- Expected Result: Secret values masked/redacted in persisted output and UI.
- Priority: `P0`

### TC-SEC-TOK-001
- ID: `TC-SEC-TOK-001`
- Category: `Security - Token Validation`
- Description: Token format and signature validation rejects forged tokens.
- Preconditions: Crafted forged token samples.
- Steps:
  1. Use forged tokens for API and WSS auth.
- Expected Result: Authentication fails consistently; no partial access.
- Priority: `P0`

### TC-SEC-TOK-002
- ID: `TC-SEC-TOK-002`
- Category: `Security - Token Validation`
- Description: Expired token denied across API and websocket channels.
- Preconditions: Expired token generated.
- Steps:
  1. Call admin API with expired token.
  2. Attempt WS subscription with expired token.
- Expected Result: Both rejected with auth error; client prompted to refresh/re-auth as applicable.
- Priority: `P0`

### TC-SEC-TOK-003
- ID: `TC-SEC-TOK-003`
- Category: `Security - Token Validation`
- Description: Repeated invalid token attempts trigger throttling/rate-limit.
- Preconditions: Rate limiting enabled.
- Steps:
  1. Send burst of invalid-auth requests from same IP/client.
- Expected Result: Requests throttled or temporarily blocked; service remains responsive for valid traffic.
- Priority: `P1`

### TC-SEC-UNA-001
- ID: `TC-SEC-UNA-001`
- Category: `Security - Unauthorized Access`
- Description: Unauthenticated users cannot access device APIs, command APIs, or realtime feed.
- Preconditions: No credentials.
- Steps:
  1. Attempt `GET /api/devices`.
  2. Attempt `POST /api/devices/{id}/commands`.
  3. Attempt frontend websocket subscribe.
- Expected Result: All requests denied (`401`/`403`); no payload leakage.
- Priority: `P0`

### TC-SEC-UNA-002
- ID: `TC-SEC-UNA-002`
- Category: `Security - Unauthorized Access`
- Description: Device identity spoofing attempt is blocked.
- Preconditions: One legitimate device online.
- Steps:
  1. Start rogue client using copied `deviceId` but invalid token.
  2. Attempt registration and status push.
- Expected Result: Rogue registration denied; legitimate device session unaffected.
- Priority: `P0`

### TC-SEC-UNA-003
- ID: `TC-SEC-UNA-003`
- Category: `Security - Unauthorized Access`
- Description: Cross-device command targeting is validated.
- Preconditions: At least two devices online.
- Steps:
  1. Submit command with mismatched path/body device IDs (or tampered device target field).
- Expected Result: Server validates target identity and rejects mismatched/tampered command request.
- Priority: `P1`

---

## Traceability matrix (quick coverage)
- Server coverage: Auth, registration, WSS lifecycle, dispatch, status aggregation, API endpoint behavior.
- Client coverage: Reconnect, heartbeat continuity, whitelist enforcement, CLI wrapper robustness, status collection resilience.
- Frontend coverage: Fleet rendering, realtime behavior, command UX, IM wizard flow.
- Integration coverage: Join, restart, doctor, IM E2E.
- Demo readiness coverage: Fixtures, preflight, reset reliability.
- Security coverage: Injection prevention, token validation, unauthorized access controls.
