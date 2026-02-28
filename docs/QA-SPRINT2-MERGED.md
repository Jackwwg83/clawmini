# Sprint 2A+2B 综合 QA 报告

**日期:** 2026-02-28
**审查者:** Codex + Claude Code (双审查)
**自动化:** go test ✅ | go vet ✅ | eslint ✅ | build ✅

---

## 发现汇总

| 严重度 | 数量 | 关键问题 |
|--------|------|----------|
| Critical | 2 | 凭证含 `$\` 被白名单拒绝; 凭证在命令记录中泄露 |
| High | 5 | 超时假成功、配置流不可取消、白名单过宽、缺 wizard 测试、401 双重定向 |
| Medium | 8 | 重复代码、无 AbortController、无破坏性操作确认、a11y、离线未拦截、无重入保护 |
| Low | 7 | Bundle 大小、常量不一致、无错误边界等 |

---

## CRITICAL (2)

### C1. 凭证含 `$` 或 `\` 被白名单静默拒绝 ⚠️ Claude 发现
**文件:** `whitelist.go:38-41`, `IMConfigPage.tsx:297-306`

白名单 shell 注入防护拒绝包含 `;|&$\`` 的参数。但 `exec.Command` 不经过 shell，这些字符无害。OAuth Secret 常含 `$`。

**建议:** 移除 `$\` 检查，或为 `config set` 的值参数绕过此检查。

### C2. IM 凭证在命令记录中泄露 ⚠️ Codex 发现
**文件:** `cmd/server/main.go:234`, `hub.go:375`

凭证作为命令参数被持久化到 DB 并通过 WebSocket 广播给所有浏览器。

**建议:** 在持久化和广播前对 `config set` 的值参数脱敏。

---

## HIGH (5)

### H1. DeviceDetailPage 命令超时返回假成功 ⚠️ 双报告确认
**文件:** `DeviceDetailPage.tsx:930-975`

轮询超时后返回 `latest`（可能仍是 `sent`），调用方显示"成功"。IMConfigPage 正确抛异常。

### H2. 配置流不可取消 — 导航离开后继续执行
**文件:** `IMConfigPage.tsx:531-610`

5 步 `await` 循环无取消机制。用户导航离开后命令继续在设备上执行。

### H3. `hasValidatedParent` 过于宽松 ⚠️ 双报告确认
**文件:** `whitelist.go:61-75`

只要前面有一个合法 verb，后续所有位置参数都放行。未限制参数数量。

### H4. 白名单测试不覆盖 IM wizard 命令
**文件:** `whitelist_test.go`

缺少 wizard 实际命令测试：`plugins install clawdbot-dingtalk`、深层 dotted key、含 `@/` 的插件名。

### H5. 401 处理双重定向竞态
**文件:** `api/client.ts:31-46`

React Router 重定向 + `window.location.replace` 同时触发。

---

## MEDIUM (8)

| # | 问题 | 文件 | 来源 |
|---|------|------|------|
| M1 | 两页面 7 个重复工具函数 | DeviceDetailPage/IMConfigPage | Claude |
| M2 | fetch 无 AbortController/超时 | api/client.ts:61 | 双报告 |
| M3 | Gateway 停止/重启无确认对话框 | DeviceDetailPage.tsx:480 | Claude |
| M4 | 平台选择卡片键盘不可访问 | IMConfigPage.tsx:715 | 双报告 |
| M5 | 升级进度条硬编码 70% | DeviceDetailPage.tsx:587 | Claude |
| M6 | 离线设备可进入 wizard 全流程 | IMConfigPage.tsx:965 | Claude |
| M7 | 配置流无重入保护 | IMConfigPage.tsx:612 | Claude |
| M8 | commandRecords 登出不清除 | RealtimeContext.tsx:76 | Codex |

---

## LOW (7)

| # | 问题 | 来源 |
|---|------|------|
| L1 | JS Bundle 993KB (应代码分割) | 双报告 |
| L2 | COMMAND_POLL_INTERVAL_MS 不一致 (1200 vs 2000) | Claude |
| L3 | TERMINAL_STATUS 重复定义 | Claude |
| L4 | firstString 返回类型不一致 | Claude |
| L5 | 无 React 错误边界 | Claude |
| L6 | 验证等待固定 10s 不自适应 | Claude |
| L7 | emoji 与 Ant Design 图标风格不一致 | Claude |

---

## 修复优先级

### 立即修 (阻塞发布)
1. **C1**: 白名单允许 `$\` 用于 `config set` 值参数
2. **C2**: 命令记录中脱敏凭证
3. **H1**: DeviceDetailPage 超时抛异常（与 IMConfigPage 一致）
4. **H4**: 补充 IM wizard 命令白名单测试

### 发布前修
5. **H2**: 配置流添加取消机制
6. **H3**: 限制 `hasValidatedParent` 参数数量
7. **M3**: 破坏性操作确认对话框
8. **M7**: 防止配置流重入

### 后续优化
9. M1 代码去重、M2 AbortController、M4 a11y、L1 代码分割
