# OpenClaw Manager — MVP 方案（展示版）

## 目标
管理员在浏览器上看到所有 imini 设备状态，能远程操作 OpenClaw，能配置钉钉/飞书 IM 连接。
用于公司内部演示（5-10 分钟），受众是非技术高管。

## 架构
```
浏览器 ──► Manager Server (Cloud/VPS) ──WSS──► imini Client
                                                    ↓
                                               OpenClaw CLI
```
- Server: Go 单二进制，嵌入 Web UI，SQLite
- Client: Go 单二进制，WSS 连接 Server，执行 OpenClaw CLI
- 通信: Client 主动出站 WSS，不需要 NAT 穿透

## MVP 范围

### ✅ 包含
1. **设备列表页** — 所有 imini 在线/离线状态、OpenClaw 版本、系统资源
2. **设备详情页** — OpenClaw 状态、IM 连接状态、最近活动
3. **远程操作** — 重启 Gateway���运行诊断（doctor）、检查升级、查看日志
4. **IM 配置向导** — 页面上配置钉钉和飞书（使用第三方插件），分步引导
5. **设备加入** — imini 上一行命令加入 Server
6. **状态实时推送** — WebSocket 实时更新设备状态

### ❌ 不包含（Phase 2+）
- 多用户/多租户（MVP 单管理员）
- 企业微信支持
- 本地管理页面（只做云端）
- 批量操作
- 告警/通知系统
- 证书认证（MVP 用简单 token）
- 审计日志
- 自升级

## 技术选型
| 组件 | 选型 | 理由 |
|------|------|------|
| Server 语言 | Go | 单二进制、低内存 |
| Server 框架 | chi v5 + gorilla/ws | 轻量成熟 |
| 数据库 | SQLite (modernc) | 纯 Go，零依赖 |
| 前端 | React + Ant Design 或 Svelte + shadcn | 待定 |
| Client 语言 | Go | 与 Server 共享代码 |
| 通信 | WSS + JSON | 简单、防火墙友好 |
| 认证 | Server: 固定 token / Client: 预共享 token | MVP 简化 |

## Client 核心逻辑
1. 启动 → WSS 连接 Server → 注册（hostname + OpenClaw 版本 + 系统信息）
2. 每 30s 执行 `openclaw status --json` + 系统资源采集 → 上报 Server
3. 收到 Server 命令 → 白名单检查 → 执行 OpenClaw CLI → 返回结果

命令白名单：
- `openclaw status [--json|--all|--deep]`
- `openclaw doctor [--repair|--json]`
- `openclaw gateway start|stop|restart|status`
- `openclaw update status --json`
- `openclaw update --json`
- `openclaw config get <key>`
- `openclaw config set <key> <value>`
- `openclaw channels list|status`
- `openclaw channels add --channel <ch> [flags]`
- `openclaw plugins list|install|enable|disable`
- `openclaw logs`

## IM 配置流程（以钉钉为例）
Web UI 分步向导：
1. 用户选择"钉钉" → 显示教程（去开放平台创建应用）
2. 填入 ClientID + ClientSecret
3. 后台依次执行：
   - `openclaw plugins install clawdbot-dingtalk`（如未安装）
   - `openclaw config set plugins.entries.clawdbot-dingtalk.clientId <value>`
   - `openclaw config set plugins.entries.clawdbot-dingtalk.clientSecret <value>`
   - `openclaw config set plugins.entries.clawdbot-dingtalk.aiCard.enabled true`
   - `openclaw gateway restart`
4. 等待状态上报确认连接成功

飞书同理，使用 @openclaw/feishu 官方插件或社区插件。

## 设备加入流程
```bash
# 管理员在 Server 生成 token
# 在 imini 上执行：
curl -fsSL https://manager-server/install.sh | bash
openclaw-manager-client join --server wss://manager-server --token <token>
```

## 开发计划
- **Week 1**: Server + Client 骨架、WSS 通信、设备注册、状态上报
- **Week 2**: Web UI（设备列表/详情）、远程命令执行、日志查看
- **Week 3**: IM 配置向导（钉钉+飞书）、演示流程打磨

## 演示脚本（5 分钟）
1. 打开管理台 → 看到 2 台 imini 在线（30s）
2. 点一台 → 看状态详情、IM 连接正常（30s）
3. 现场添加一台新 imini → 一行命令加入（1min）
4. 给新设备配置钉钉 → 分步向导（2min）
5. 钉钉群里 @AI → AI 回复 → 管理台实时看到消息（1min）
