# ClawMini 综合 QA 报告

**日期:** 2026-02-28
**审查者:** Codex (QA) + Claude Code (automated + manual review)
**范围:** 全栈审查 — Go 后端、React/TS 前端、安全、架构、构建部署

---

## 自动化检查结果

| 检查项 | 结果 |
|--------|------|
| `go test -race ./...` | **PASS** — 全部通过，无竞态条件 |
| `go vet ./...` | **PASS** — 无问题 |
| `go build ./...` | **PASS** — 编译正常 |
| `tsc -b` (TypeScript) | **PASS** — 无类型错误 |
| `npm run build` | **PASS** — 主 JS chunk 901.68 kB（需代码分割） |
| `eslint .` | **FAIL** — 5 errors, 2 warnings |

**SQL 注入:** 未发现 — 全部参数化占位符
**XSS:** 未发现 — 无 dangerouslySetInnerHTML

---

## 发现汇总

| 严重度 | 数量 | 关键问题 |
|--------|------|----------|
| **Critical** | 4 | 默认令牌、无 TLS、CSWSH、令牌在 URL 中 |
| **High** | 8 | 无限流、无请求体限制、输出无上限、错误泄露、令牌明文日志、白名单绕过、localStorage 存令牌 |
| **Medium** | 9 | ESLint 错误、重复类型、无命令过期、无 ping/pong、无 401 全局处理、重连无退避 |
| **Low** | 10 | 无优雅关闭、无 DB 迁移、缺少测试、gitignore |

**总体评估: ❌ FAIL** — 需修复 Critical + High 后才可生产部署

---

## CRITICAL (4)

### C1. 硬编码默认管理员/设备令牌 ⚠️ 双报告确认
**文件:** `internal/server/auth.go:10-11`

服务器在环境变量缺失时静默回退到已知默认值 (`clawmini-admin`, `clawmini-device`)。

**建议:** 环境变量未设置时拒绝启动。强制最小熵/长度策略。

### C2. 无 TLS — 所有令牌明文传输
**文件:** `cmd/server/main.go:84`

服务器仅支持 HTTP，令牌和 WebSocket 流量均未加密。

**建议:** 添加 ListenAndServeTLS 选项或文档要求反向代理 TLS 终端。

### C3. WebSocket Origin 检查禁用 (CSWSH) ⚠️ 双报告确认
**文件:** `internal/server/hub.go:66-68`

CheckOrigin 始终返回 true，允许跨站 WebSocket 劫持。

**建议:** 校验 Origin 头，使用可配置白名单。

### C4. 令牌通过 URL 查询字符串传递 ⚠️ 双报告确认
**文件:** `internal/server/auth.go:50-52`, `web/src/contexts/RealtimeContext.tsx:43-44`

令牌在 URL 中被日志、代理、浏览器历史记录。

**建议:** 移除 query-string 令牌，使用 WebSocket 子协议头或短期票据。

---

## HIGH (8)

### H1. 无登录端点限流
**文件:** `cmd/server/main.go:59`

无限流、无账户锁定，令牌可被暴力破解。

### H2. 无 HTTP 请求体大小限制
**文件:** `cmd/server/main.go:93, :146`

JSON body 无大小限制，可耗尽服务器内存。建议 http.MaxBytesReader 1MB。

### H3. 命令执行 stdout/stderr 无大小限制 ⚠️ 双报告确认
**文件:** `internal/client/executor.go:40-42`

bytes.Buffer 无上限捕获输出，大输出可导致 OOM。

### H4. 内部错误详情泄露给 API 客户端
**文件:** `cmd/server/main.go:110, :124, :137, :163`

原始数据库错误返回客户端。建议返回通用 "internal error"。

### H5. 设备令牌明文写入日志 ⚠️ 双报告确认
**文件:** `internal/server/hub.go:131`

### H6. 登录占位符暴露默认令牌
**文件:** `web/src/pages/LoginPage.tsx:52`

placeholder="例如：clawmini-admin" 即为实际默认令牌。

### H7. 命令白名单不验证参数 ⚠️ 双报告确认
**文件:** `internal/openclaw/whitelist.go:20-38`

AllowedCommands 定义了允许标志但验证代码从未检查。

### H8. 管理员令牌存储在 localStorage
**文件:** `web/src/contexts/AuthContext.tsx:16-17, :38`

任何 XSS 可窃取。建议迁移到 HttpOnly cookies。

---

## MEDIUM (9)

| # | 问题 | 文件 | 来源 |
|---|------|------|------|
| M1 | useEffect 中 setState（级联渲染） | DeviceDetailPage.tsx:79,:88,:103 | 双报告 |
| M2 | 缺少 Hook 依赖 | RealtimeContext.tsx:71,:150 | 双报告 |
| M3 | react-refresh/only-export-components | AuthContext/RealtimeContext | 双报告 |
| M4 | 重复 wsEnvelope 类型（3种信封表示） | hub.go / connection.go / protocol | Claude |
| M5 | 重复 writeJSON 函数 | main.go / http.go | Claude |
| M6 | 无命令过期/清理机制 | command.go | Claude |
| M7 | 无 WebSocket Ping/Pong 心跳 | hub.go / connection.go | 双报告 |
| M8 | 前端未全局处理 401 响应 | api/client.ts | Claude |
| M9 | WS 重连无退避（固定 2.5s） | RealtimeContext.tsx:116 | Codex |

---

## LOW (10)

| # | 问题 | 文件 | 来源 |
|---|------|------|------|
| L1 | 无优雅服务器关闭 | cmd/server/main.go:84 | Claude |
| L2 | 无数据库迁移策略 | device.go:69 | Claude |
| L3 | cmd/ 包无测试 | cmd/server/, cmd/client/ | Claude |
| L4 | Makefile test 缺 -race | Makefile:6 | Claude |
| L5 | 构建产物和 DB 在版本控制中 | bin/, clawmini.db | Claude |
| L6 | 空脚手架目录 | scripts/, configs/ | Claude |
| L7 | Embed 指令冗余 | web/embed.go:7 | Claude |
| L8 | ErrNotFound 非哨兵错误 | device.go:262 | Claude |
| L9 | 前端重连定时器竞态 | RealtimeContext.tsx:121 | Claude |
| L10 | commandRecords 无限增长 | RealtimeContext.tsx:52 | Codex |

---

## 架构优点

- ✅ 清晰分层：protocol/ 共享类型、openclaw/ 白名单、server//client/ 各自逻辑
- ✅ 核心包测试覆盖良好（4个包全部通过含 race 检测）
- ✅ SQLite 单二进制部署适合 MVP
- ✅ WebSocket Hub 模式干净，mutex 使用正确
- ✅ 前端架构组织良好（contexts, API client, typed interfaces）
- ✅ 客户端指数退避重连
- ✅ SQL 全部参数化，无注入风险

---

## 修复优先级建议

### Phase 1: 安全阻塞项 (1-2天)
1. C1: 环境变量未设置时拒绝启动
2. C3: WebSocket Origin 白名单校验
3. C4 + H5: 移除 query-string 令牌，改 Bearer header
4. H3 + H7: 命令输出上限 + 白名单参数验证
5. H4 + H6: 错误信息脱敏 + 登录占位符修改

### Phase 2: 前端质量 (0.5天)
6. M1-M3: 修复 ESLint 5 errors + 2 warnings
7. M8-M9: 全局 401 处理 + 指数退避重连

### Phase 3: 运维就绪 (1天)
8. M7: WebSocket ping/pong
9. M6: 命令超时清理
10. L1-L5: 优雅关闭、DB 迁移、gitignore

---

## 企业部署门控

**状态: ❌ FAIL**

**阻塞项:** C1-C4, H1-H8

**重新评估条件:**
- 所有 Critical + High 修复
- 回归测试覆盖 auth/whitelist/ws lifecycle
- ESLint clean
- 安全复测通过
