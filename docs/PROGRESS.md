# ClawMini 项目进度

> 最后更新: 2026-02-28 14:10 UTC

## 总览

ClawMini = OpenClaw Appliance 设备管理平台。管理员在浏览器上管理所有 iMini 设备的 OpenClaw 安装、配置、IM 接入。

**Server**: `13.229.214.34:18790` | **真实设备**: iMini (`b5e4d540`) 已在线

---

## Phase 1: 基础设施 ✅ 完成

| 模块 | 状态 |
|------|------|
| Go Server (chi + gorilla/ws + SQLite) | ✅ 单二进制，嵌入前端 |
| Go Client (WSS + 心跳 + 命令执行) | ✅ systemd 部署在 iMini |
| React 前端 (Ant Design 5 + TypeScript) | ✅ 登录、Dashboard、设备详情 |
| 认证 + 安全加固 | ✅ Origin 校验、限流、TLS 选项、错误脱敏 |
| 命令白名单 (10 个 openclaw 子命令) | ✅ 参数严格验证 |
| WebSocket 实时推送 + Ping/Pong | ✅ 指数退避重连 |
| QA 31/31 修复 | ✅ Codex + Claude Code 双审查 |
| DB 迁移框架 + 优雅关闭 | ✅ |

---

## Phase 2: 设备管理功能 🔄 进行中

### Sprint 2A: 设备详情页增强 (预计 3-4h)

| 功能 | 优先级 | 状态 |
|------|--------|------|
| Gateway 控制面板 (启动/停止/重启) | P0 | ⏳ |
| Doctor 诊断卡片 (结果可视化) | P0 | ⏳ |
| OpenClaw 升级 (版本+一键更新) | P0 | ⏳ |
| 系统资源图表 (CPU/内存/磁盘) | P1 | ⏳ |
| IM 渠道状态卡片 | P1 | ⏳ |
| 日志查看器 | P1 | ⏳ |
| 插件管理 | P2 | ⏳ |

### Sprint 2B: IM 配置向导 (预计 4-6h)

| 功能 | 优先级 | 状态 |
|------|--------|------|
| 钉钉配置向导 (分步表单) | P0 | ⏳ |
| 飞书配置向导 | P0 | ⏳ |
| 配置状态轮询+进度展示 | P0 | ⏳ |
| 教程引导 (截图+说明) | P1 | ⏳ |

后台执行序列 (钉钉):
1. `openclaw plugins install clawdbot-dingtalk`
2. `openclaw config set plugins.entries.clawdbot-dingtalk.clientId <value>`
3. `openclaw config set plugins.entries.clawdbot-dingtalk.clientSecret <value>`
4. `openclaw config set plugins.entries.clawdbot-dingtalk.aiCard.enabled true`
5. `openclaw gateway restart`
6. 轮询 `openclaw channels status` 确认连接

### Sprint 2C: 设备接入优化 (预计 2-3h)

| 功能 | 优先级 | 状态 |
|------|--------|------|
| 加入令牌生成 | P1 | ⏳ |
| 一行安装脚本 | P1 | ⏳ |
| 设备移除 | P2 | ⏳ |

---

## Phase 3: 演示打磨 (Week 3)

- 远程安装 OpenClaw (检测+引导安装)
- 批量操作 (多设备升级/重启)
- 操作审计日志
- 5 分钟演示脚本

---

## 部署信息

| 项目 | 值 |
|------|-----|
| Server | `13.229.214.34:18790` |
| systemd | `clawmini-server` / `clawmini-client` |
| 设备 ID | `b5e4d540b1434046a88d546d3608196a` |
| 项目根目录 | `~/clawmini` |
