# ClawMini 最终综合 QA 报告

**日期:** 2026-03-01
**审查者:** Codex (full review x2) + Claude Code (deep review x2) — 合并
**范围:** 全项目 — 67 Go files + 21 TS/TSX files (87 total)

---

## 测试结果 (2026-03-01 最新)

| 检查项 | 结果 |
|--------|------|
| `go test -race ./internal/...` | ✅ PASS (all 4 packages) |
| `go test -race ./cmd/server/` | ⚠️ 93/94 PASS, **1 deadlock**: `TestDataIntegrity_ForeignKeyCascade_DeleteDevice` 🆕 |
| `go vet ./...` | ✅ PASS |
| `npm run build` | ✅ PASS (chunk >500kB warning) |
| `npm run lint` | ❌ 6 errors, 4 warnings |

---

## 发现汇总

| 严重度 | 数量 | 状态 |
|--------|------|------|
| **Critical** | 4 | 全部未修 |
| **High** | 8 | 全部未修 |
| **Medium** | 10 | 全部未修 |
| **Low** | 8 | 全部未修 |

---

## CRITICAL (4)

### C1. 硬编码默认 admin/device tokens
**来源:** Codex Phase1 + Claude Phase1 (双确认)
**文件:** `internal/server/auth.go:10-11,21,27`
**问题:** 环境变量缺失时静默回退到 `clawmini-admin`/`clawmini-device`
**影响:** 可预测凭证，未授权访问
**修复:** 启动时缺失 token 则 fatal exit；强制最小熵

### C2. WebSocket Origin 检查完全禁用
**来源:** Codex Phase1 + Claude Phase1 (双确认)
**文件:** `internal/server/hub.go:66-67`
**问题:** `CheckOrigin` 永远返回 `true`
**影响:** 跨站 WebSocket 劫持
**修复:** 显式 Origin 白名单

### C3. 凭证含 `$`/`\` 被白名单静默拒绝
**来源:** Claude Sprint2 (首发)
**文件:** `internal/openclaw/whitelist.go:38-41`, `web/src/pages/IMConfigPage.tsx:297-306`
**问题:** Shell 注入防护过度，`exec.Command` 不经 shell 这些字符无害；OAuth Secret 常含 `$`
**影响:** IM 配置向导无法设置含特殊字符的凭证
**修复:** `config set` 的值参数绕过 `$\` 检查

### C4. IM 凭证在命令记录中明文泄露
**来源:** Codex Sprint2 (首发)
**文件:** `cmd/server/main.go:234`, `internal/server/hub.go:375`
**问题:** 凭证作为命令参数被持久化到 DB 并通过 WS 广播
**影响:** 凭证泄露给所有浏览器客户端
**修复:** 持久化/广播前脱敏 `config set` 值

---

## HIGH (8)

### H1. Token 通过 URL query + 日志泄露
**来源:** Codex Phase1 + Claude Phase1 (双确认)
**文件:** `internal/server/auth.go:50-51`, `web/src/contexts/RealtimeContext.tsx:43-44`
**修复:** 移除 query-token，改用 Bearer header + WS auth message

### H2. Device token 明文日志
**来源:** Codex Phase1 (首发)
**文件:** `internal/server/hub.go:131`
**修复:** 日志中只打 token hash 前缀

### H3. 命令白名单参数不验证
**来源:** Codex Phase1 + Claude Phase1 (双确认)
**文件:** `internal/openclaw/whitelist.go:6,27-28,33`
**修复:** 严格 per-subcommand 参数语法验证

### H4. stdout/stderr 无限缓冲 (OOM 风险)
**来源:** Codex Phase1 + Claude Phase1 (双确认)
**文件:** `internal/client/executor.go:40-46`
**修复:** 强制输出大小上限，截断+标记

### H5. Admin token 存 localStorage
**来源:** Codex Phase1 + Claude Phase1 (双确认)
**文件:** `web/src/contexts/AuthContext.tsx:16-17,38`
**修复:** 改用 HttpOnly cookie 或短期 token + refresh

### H6. DeviceDetailPage 命令超时返回假成功
**来源:** Claude Sprint2 + Codex Sprint2 (双确认)
**文件:** `web/src/pages/DeviceDetailPage.tsx:930-975`
**修复:** 超时时抛异常（与 IMConfigPage 一致）

### H7. 配置流不可取消 — 导航离开后继续执行
**来源:** Claude Sprint2 (首发)
**文件:** `web/src/pages/IMConfigPage.tsx:531-610`
**修复:** 添加 AbortController 取消机制

### H8. `hasValidatedParent` 过于宽松
**来源:** Claude Sprint2 + Codex Sprint2 (双确认)
**文件:** `internal/openclaw/whitelist.go:61-75`
**修复:** 限制参数数量，default-deny

---

## MEDIUM (10)

| # | 问题 | 文件 | 来源 |
|---|------|------|------|
| M1 | WS 无心跳/deadline (僵尸连接) | hub.go:104,150 | Codex P1 |
| M2 | 前端 API 错误未处理 (unhandled rejection) | RealtimeContext.tsx:55 | Codex P1 |
| M3 | WS 重连无退避/无 401 停止 | RealtimeContext.tsx:116 | Codex P1 |
| M4 | 后端消息处理错误被吞 | hub.go:164-193 | Codex P1 |
| M5 | 命令超时无上限 | main.go:153 | Codex P1 |
| M6 | 两页面 7 个重复工具函数 | DeviceDetailPage/IMConfigPage | Claude S2 |
| M7 | Gateway 停止/重启无确认对话框 | DeviceDetailPage.tsx:480 | Claude S2 |
| M8 | 升级进度条硬编码 70% | DeviceDetailPage.tsx:587 | Claude S2 |
| M9 | 离线设备可进入 wizard 全流程 | IMConfigPage.tsx:965 | Claude S2 |
| M10 | 配置流无重入保护 | IMConfigPage.tsx:612 | Claude S2 |

---

## LOW (8)

| # | 问题 | 来源 |
|---|------|------|
| L1 | commandRecords 无限增长 | Codex P1 |
| L2 | 断连仅 badge 提示，无全局警告 | Codex P1 |
| L3 | JS Bundle 993KB (需代码分割) | 双确认 |
| L4 | COMMAND_POLL_INTERVAL_MS 不一致 (1200 vs 2000) | Claude S2 |
| L5 | 无 React 错误边界 | Claude S2 |
| L6 | 验证等待固定 10s 不自适应 | Claude S2 |
| L7 | 401 处理双重定向竞态 | Codex S2 |
| L8 | commandRecords 登出不清除 | Codex S2 |

---

## 🆕 新发现 (2026-03-01)

### N1. `TestDataIntegrity_ForeignKeyCascade_DeleteDevice` deadlock
**严重度:** HIGH
**文件:** `cmd/server/` 测试
**问题:** 该测试无限挂起，导致整个 `cmd/server` 测试套件超时 (>180s)
**影响:** CI 不可靠，可能掩盖其他回归
**修复:** 排查外键级联删除的数据库锁/事务问题

### N2. ESLint 新增 4 个 set-state-in-effect 错误
**严重度:** MEDIUM
**文件:** `DeviceDetailPage.tsx`, `IMConfigPage.tsx`
**问题:** useEffect 内直接 setState 触发级联渲染
**影响:** 性能问题 + 潜在无限循环

### N3. ESLint 新增 exhaustive-deps 警告
**严重度:** LOW
**文件:** `UserDetailPage.tsx`, `UserManagementPage.tsx`

---

## 企业部署评估

### 🔴 结论: **NOT READY** — 需修复 blockers

**阻塞项 (必须修复):**
1. C1-C4: 凭证安全（硬编码、泄露、白名单）
2. H1-H2: Token 泄露（URL/日志）
3. H3-H4: 命令执行安全（白名单/OOM）
4. N1: 测试 deadlock

**发布前修复:**
5. H5-H8: 前端安全+可靠性
6. M1-M5: 后端健壮性
7. ESLint 全部清零

**后续优化:**
8. M6-M10 + L1-L8: 代码质量 + UX

### 修复优先级路线图

| 阶段 | 工作量 | 内容 |
|------|--------|------|
| **Sprint 3A** (2-3天) | 安全 blockers | C1-C4, H1-H3, N1 |
| **Sprint 3B** (1-2天) | 可靠性 | H4-H8, M1-M5 |
| **Sprint 3C** (1天) | 质量 | ESLint clean, M6-M10, L* |

---

*报告合并自: QA-REPORT.md (Codex P1) + QA-CLAUDE.md (Claude P1) + QA-SPRINT2-CODEX.md + QA-SPRINT2-CLAUDE.md + QA-SPRINT2-MERGED.md + 2026-03-01 Codex review + 新测试结果*
