# OpenClaw Manager 架构设计文档

**版本**: v1.0  
**日期**: 2026-02-28  
**作者**: 架构组  
**状态**: 初稿 - 待评审

---

## 目录

1. [产品需求概述](#1-产品需求概述)
2. [OpenClaw 能力总结](#2-openclaw-能力总结)
3. [整体架构图](#3-整体架构图)
4. [本地 Agent 设计](#4-本地-agent-设计)
5. [云端 Controller 设计](#5-云端-controller-设计)
6. [通信协议](#6-通信协议)
7. [API 设计](#7-api-设计)
8. [前端设计](#8-前端设计)
9. [IM 适配器配置流程](#9-im-适配器配置流程)
10. [技术选型](#10-技术选型)
11. [开发计划](#11-开发计划)
12. [风险评估](#12-风险评估)

---

## 1. 产品需求概述

### 1.1 产品定位

OpenClaw Manager 是面向非技术用户（"张姐"）的 AI Agent 管理平台，让企业用户能通过图形界面完成 OpenClaw 的安装、配置、监控和维护，无需接触命令行。

### 1.2 核心需求

| 需求 | 描述 |
|------|------|
| **混合模式** | 每台 imini（AMD mini PC + Ubuntu + OpenClaw）本机有管理页面 + 云端有中央管控台 |
| **多租户** | 普通用户看自己的设备，管理员看所有设备 |
| **初始化向导** | 开箱即用，引导完成 OpenClaw 全部配置 |
| **配置管理** | 可视化编辑 openclaw.json，无需理解 JSON |
| **监控** | 实时查看 Gateway 状态、Channel 健康、Session 活跃度 |
| **升级检测** | 自动检测新版本，一键升级 |
| **排错** | 集成 doctor，可视化展示问题和修复建议 |
| **IM 连接** | 支持所有官方 IM + 飞书 + 钉钉 + 企业微信 |
| **认证** | 本地：Ubuntu PAM；云端：独立用户系统 |
| **目标用户** | 非技术人员为主 |

### 1.3 用户画像

**张姐**（典型用户）：
- 40 岁，企业行政主管
- 会用电脑和手机，但不会用终端
- 需要：连上钉钉 → Agent 帮她处理日常事务
- 期望：像配 WiFi 路由器一样简单

---

## 2. OpenClaw 能力总结

### 2.1 CLI 命令 → API 映射

OpenClaw Gateway 在端口 18789 上提供 WebSocket RPC + HTTP API，几乎所有 CLI 功能都可通过 API 调用。

| 能力 | CLI 命令 | Gateway RPC/API | Manager 可用性 |
|------|----------|-----------------|----------------|
| **状态概览** | `openclaw status [--all|--deep]` | WS: `status`, `health` | ✅ 直接用 |
| **Gateway 生命周期** | `openclaw gateway start/stop/restart` | systemd: `systemctl --user` | ✅ 通过 systemd D-Bus |
| **Gateway 健康** | `openclaw health [--json]` | WS: `health` | ✅ 直接用 |
| **日志查看** | `openclaw logs [--follow]` | WS: `logs.tail` | ✅ 直接用 |
| **配置读取** | `openclaw config get <path>` | WS: `config.get` | ✅ 直接用 |
| **配置写入** | `openclaw config set <path> <val>` | WS: `config.patch` / `config.apply` | ✅ 直接用 |
| **配置向导** | `openclaw configure` | 需封装（交互式） | ⚠️ 需自行实现向导 UI |
| **诊断修复** | `openclaw doctor [--repair]` | 需 CLI 调用 | ⚠️ 通过 exec 调用 CLI |
| **频道管理** | `openclaw channels list/add/remove/login/logout` | WS: `channels.*` | ✅ 大部分直接用 |
| **频道状态** | `openclaw channels status/capabilities` | WS: `channels.status` | ✅ 直接用 |
| **模型管理** | `openclaw models status/list/set/scan` | WS: `models.*` | ✅ 直接用 |
| **模型认证** | `openclaw models auth add/login/paste-token` | WS: `models.auth.*` | ⚠️ OAuth 需浏览器跳转 |
| **插件管理** | `openclaw plugins list/install/enable/disable` | 需 CLI 调用 | ⚠️ 通过 exec 调用 CLI |
| **节点管理** | `openclaw nodes list/approve/invoke` | WS: `node.*` | ✅ 直接用 |
| **设备配对** | `openclaw devices list/approve/reject/rotate` | WS: `device.*` | ✅ 直接用 |
| **会话管理** | `openclaw sessions list` | WS: `sessions.*` | ✅ 直接用 |
| **Cron 管理** | `openclaw cron *` | WS: `cron.*` | ✅ 直接用 |
| **升级** | `openclaw update [--channel]` | 需 CLI 调用 | ⚠️ 通过 exec + 进度流 |
| **安全审计** | `openclaw security *` | 部分 WS | ⚠️ 部分需 CLI |
| **发送消息** | `openclaw message send` | WS: `send` | ✅ 直接用 |

### 2.2 关键发现

1. **Gateway 已内置 Control UI**：`http://127.0.0.1:18789/` 提供基础 Web 管理界面（Config tab 支持表单编辑 + Raw JSON）
2. **WebSocket 是主要 API 通道**：所有 RPC 走 WS，协议已有完整的 TypeBox Schema 定义
3. **config.patch RPC 支持热更新**：大部分配置修改无需重启 Gateway
4. **config.apply 有速率限制**：3 次/60 秒/设备，需注意 UI 防抖
5. **认证体系完善**：Token/Password/Tailscale/TrustedProxy 四种模式
6. **插件系统成熟**：钉钉已作为插件运行（clawdbot-dingtalk v0.3.24）
7. **飞书(Feishu)已有官方支持**：内置 Channel，WebSocket 连接
8. **IRC 已内置支持**：可直接使用

---

## 3. 整体架构图

### 3.1 系统全景

```
┌─────────────────────────────────────────────────────────────────────┐
│                        Cloud Controller                             │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌───────────────────┐  │
│  │ Web App  │  │ Auth Svc │  │ Device   │  │ Status Aggregator │  │
│  │ (React)  │  │ (JWT)    │  │ Registry │  │ (WebSocket Hub)   │  │
│  └────┬─────┘  └────┬─────┘  └────┬─────┘  └────────┬──────────┘  │
│       │              │             │                  │             │
│       └──────────────┴─────────────┴──────────────────┘             │
│                              │                                      │
│                    ┌─────────┴─────────┐                           │
│                    │   API Gateway     │                           │
│                    │  (HTTPS + WSS)    │                           │
│                    └─────────┬─────────┘                           │
└──────────────────────────────┼──────────────────────────────────────┘
                               │
                    ┌──────────┴──────────┐
          ┌────────┤   Internet / VPN     ├────────┐
          │        └─────────────────────┘         │
          │                                        │
┌─────────┴───────────────┐          ┌─────────────┴──────────────┐
│     imini-A (张姐办公室)  │          │     imini-B (李总会议室)    │
│                          │          │                             │
│  ┌────────────────────┐  │          │  ┌────────────────────┐   │
│  │  Local Manager     │  │          │  │  Local Manager     │   │
│  │  Agent (Go)        │  │          │  │  Agent (Go)        │   │
│  │  ┌──────────────┐  │  │          │  │  ┌──────────────┐  │   │
│  │  │ Local Web UI │  │  │          │  │  │ Local Web UI │  │   │
│  │  │ :8080        │  │  │          │  │  │ :8080        │  │   │
│  │  └──────────────┘  │  │          │  │  └──────────────┘  │   │
│  │  ┌──────────────┐  │  │          │  │  ┌──────────────┐  │   │
│  │  │ Cloud Sync   │  │  │          │  │  │ Cloud Sync   │  │   │
│  │  │ (WSS Client) │  │  │          │  │  │ (WSS Client) │  │   │
│  │  └──────┬───────┘  │  │          │  │  └──────┬───────┘  │   │
│  └─────────┼──────────┘  │          │  └─────────┼──────────┘   │
│            │              │          │            │               │
│  ┌─────────┴──────────┐  │          │  ┌─────────┴──────────┐   │
│  │  OpenClaw Gateway  │  │          │  │  OpenClaw Gateway  │   │
│  │  :18789 (WS+HTTP)  │  │          │  │  :18789 (WS+HTTP)  │   │
│  │  ┌──────────────┐  │  │          │  │  ┌──────────────┐  │   │
│  │  │ Channels:    │  │  │          │  │  │ Channels:    │  │   │
│  │  │ - DingTalk   │  │  │          │  │  │ - WeChat Work│  │   │
│  │  │ - Feishu     │  │  │          │  │  │ - Slack      │  │   │
│  │  │ - WhatsApp   │  │  │          │  │  │ - Telegram   │  │   │
│  │  └──────────────┘  │  │          │  │  └──────────────┘  │   │
│  └─────────────────────┘  │          │  └─────────────────────┘   │
└───────────────────────────┘          └────────────────────────────┘
```

### 3.2 数据流

```
张姐浏览器 ──HTTP──▶ imini 本地 Manager ──WS──▶ OpenClaw Gateway
                                │
                                ├──WS──▶ 钉钉 (DingTalk Plugin)
                                ├──WS──▶ 飞书 (Feishu Channel)
                                └──API──▶ LLM Provider (Anthropic/OpenAI/...)

管理员浏览器 ──HTTPS──▶ Cloud Controller ──WSS──▶ 各 imini 本地 Agent
```

---

## 4. 本地 Agent 设计

### 4.1 职责

本地 Manager Agent 跑在每台 imini 上，是 OpenClaw Gateway 和管理界面之间的桥梁。

```
┌──────────────────────────────────────────────────┐
│              Local Manager Agent                  │
│                                                   │
│  ┌─────────────┐  ┌──────────────┐  ┌─────────┐ │
│  │ HTTP Server │  │ WS Client    │  │ CLI     │ │
│  │ (Local UI)  │  │ (→ OpenClaw) │  │ Wrapper │ │
│  │ :8080       │  │ :18789       │  │         │ │
│  └──────┬──────┘  └──────┬───────┘  └────┬────┘ │
│         │                │               │       │
│  ┌──────┴────────────────┴───────────────┴────┐  │
│  │           Core Logic Layer                  │  │
│  │                                             │  │
│  │  ┌──────────┐ ┌──────────┐ ┌─────────────┐ │  │
│  │  │ Config   │ │ Monitor  │ │ Cloud Sync  │ │  │
│  │  │ Manager  │ │ Service  │ │ Client      │ │  │
│  │  └──────────┘ └──────────┘ └─────────────┘ │  │
│  │  ┌──────────┐ ┌──────────┐ ┌─────────────┐ │  │
│  │  │ Wizard   │ │ Update   │ │ Auth        │ │  │
│  │  │ Engine   │ │ Checker  │ │ (PAM)       │ │  │
│  │  └──────────┘ └──────────┘ └─────────────┘ │  │
│  └─────────────────────────────────────────────┘  │
└──────────────────────────────────────────────────┘
```

### 4.2 与 OpenClaw Gateway 的交互方式

| 操作类型 | 交互方式 | 说明 |
|----------|----------|------|
| 状态查询 | WS RPC `health`, `status` | 实时，低延迟 |
| 配置读写 | WS RPC `config.get`, `config.patch` | 热更新，3次/60秒限流 |
| 日志流 | WS RPC `logs.tail` | 实时流式 |
| 频道管理 | WS RPC `channels.*` | 大部分操作 |
| WhatsApp 登录 | CLI `openclaw channels login` | 需要 QR 码交互 |
| 诊断修复 | CLI `openclaw doctor --repair --yes` | 非交互模式 |
| 系统升级 | CLI `openclaw update --json` | JSON 进度输出 |
| 插件管理 | CLI `openclaw plugins install/enable` | 需要 npm |
| 服务控制 | systemd `systemctl --user start/stop openclaw-gateway` | 直接控制 |

### 4.3 本地认证：PAM 集成

```
认证流程:
  浏览器 → POST /api/v1/auth/login {username, password}
         → Manager Agent 调用 PAM authenticate()
         → 成功 → 返回 JWT (httpOnly cookie)
         → 失败 → 401

默认策略:
  - 允许 sudo 组和 openclaw 组的用户登录
  - Session TTL: 24 小时
  - Cookie: httpOnly + SameSite=Strict
```

### 4.4 本地 Agent 作为 systemd 服务

```ini
# /etc/systemd/system/openclaw-manager.service
[Unit]
Description=OpenClaw Manager Agent
After=network-online.target openclaw-gateway.service
Wants=network-online.target

[Service]
Type=simple
User=ubuntu
ExecStart=/usr/local/bin/openclaw-manager
Restart=always
RestartSec=5
Environment=OCM_PORT=8080
Environment=OCM_GATEWAY_URL=ws://127.0.0.1:18789

[Install]
WantedBy=multi-user.target
```

---

## 5. 云端 Controller 设计

### 5.1 架构

```
┌──────────────────────────────────────────────────────────────┐
│                     Cloud Controller                          │
│                                                               │
│  ┌─────────────┐     ┌──────────────────────────────────┐   │
│  │ Caddy       │────▶│        API Service (Go)           │   │
│  │ (TLS term.) │     │                                    │   │
│  └─────────────┘     │  ┌────────┐  ┌─────────────────┐ │   │
│                       │  │ Auth   │  │ Device Registry │ │   │
│                       │  │ Module │  │ (CRUD + status) │ │   │
│                       │  └────────┘  └─────────────────┘ │   │
│                       │  ┌────────────────┐  ┌─────────┐ │   │
│                       │  │ WSS Hub        │  │ Tenant  │ │   │
│                       │  │ (device relay) │  │ Manager │ │   │
│                       │  └────────────────┘  └─────────┘ │   │
│                       └──────────────┬───────────────────┘   │
│                                      │                        │
│  ┌──────────────┐  ┌────────────────┴───────┐               │
│  │ PostgreSQL   │  │ Redis                   │               │
│  │ (users,      │  │ (sessions, device       │               │
│  │  devices,    │  │  status cache,          │               │
│  │  tenants)    │  │  pub/sub)               │               │
│  └──────────────┘  └────────────────────────┘               │
└──────────────────────────────────────────────────────────────┘
```

### 5.2 多租户模型

```sql
-- 核心表结构
CREATE TABLE tenants (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        VARCHAR(255) NOT NULL,
    plan        VARCHAR(50) DEFAULT 'free', -- free | pro | enterprise
    created_at  TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE users (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID REFERENCES tenants(id),
    email       VARCHAR(255) UNIQUE NOT NULL,
    phone       VARCHAR(50),
    role        VARCHAR(50) DEFAULT 'member', -- member | admin | super_admin
    password_hash VARCHAR(255),
    created_at  TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE devices (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID REFERENCES tenants(id),
    name            VARCHAR(255),        -- "张姐办公室"
    hardware_id     VARCHAR(255) UNIQUE, -- 机器指纹
    openclaw_version VARCHAR(50),
    last_seen_at    TIMESTAMPTZ,
    status          VARCHAR(50) DEFAULT 'offline', -- online | offline | error
    config_snapshot JSONB,               -- 最近一次配置快照（脱敏）
    ip_address      INET,
    created_at      TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE device_channels (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    device_id   UUID REFERENCES devices(id),
    channel     VARCHAR(50), -- dingtalk | feishu | wechat_work | whatsapp | ...
    status      VARCHAR(50), -- connected | disconnected | error
    config      JSONB,       -- 脱敏后的配置
    updated_at  TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE audit_logs (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID REFERENCES tenants(id),
    user_id     UUID REFERENCES users(id),
    device_id   UUID REFERENCES devices(id),
    action      VARCHAR(100),
    details     JSONB,
    created_at  TIMESTAMPTZ DEFAULT NOW()
);
```

### 5.3 权限模型

| 角色 | 权限 |
|------|------|
| `super_admin` | 全部设备管理、用户管理、租户管理 |
| `admin` | 本租户内全部设备管理、用户管理 |
| `member` | 只看自己绑定的设备，只能执行受限操作 |

---

## 6. 通信协议

### 6.1 本地 Agent ↔ 云端 Controller

**核心问题**：imini 通常在 NAT 后面，云端无法主动连接。

**解决方案**：本地 Agent 主动发起 WebSocket 长连接到云端。

```
┌─────────┐                              ┌───────────────┐
│  imini  │ ──── WSS (outbound) ────▶    │  Cloud        │
│  Agent  │ ◀─── WSS (bidirectional) ─── │  Controller   │
└─────────┘                              └───────────────┘
    NAT                                    Public IP

连接流程:
1. Agent 启动 → WSS 连接 wss://api.manager.agentsphere.run/ws/device
2. 发送 device_hello { hardware_id, token, openclaw_version, ... }
3. 云端验证 token → 返回 hello_ok { device_id, config_hash }
4. 进入双向通信:
   - Agent → Cloud: heartbeat, status_report, event
   - Cloud → Agent: command (config_change, upgrade, restart, ...)
```

**协议消息格式**（JSON over WSS）：

```
Agent → Cloud (上报):
  { type: "heartbeat", deviceId, timestamp, payload: {
      gatewayStatus, uptime, cpuPercent, memoryPercent, diskPercent,
      openclawVersion, channels: [...], sessions: { active, total }
  }}

  { type: "status", deviceId, timestamp, payload: { ... full status ... }}

  { type: "event", deviceId, timestamp, payload: {
      event: "channel_disconnected" | "update_available" | "error",
      details: { ... }
  }}

Cloud → Agent (命令):
  { type: "command", id: "uuid", action: "config_patch" | "config_apply" |
    "restart_gateway" | "update_openclaw" | "run_doctor" | "add_channel" |
    "remove_channel" | "get_logs" | "get_qr", params: { ... }}

Agent → Cloud (命令响应):
  { type: "command_response", id: "uuid",
    status: "ok" | "error" | "progress", payload: { ... }}
```

### 6.2 NAT 穿透策略

| 方案 | 说明 | 优先级 |
|------|------|--------|
| **WSS 长连接（首选）** | Agent 主动连接云端，保持心跳，双向通信 | P0 |
| **Tailscale VPN** | 如果已部署 Tailscale，直接穿透 | P1 |
| **WireGuard** | 轻量 VPN，备选方案 | P2 |
| **SSH 反向隧道** | 应急方案，运维友好 | P3 |

**推荐**：P0 WSS 长连接即可满足 99% 场景，无需额外基础设施。

### 6.3 安全

```
安全层级:

  1. 传输加密: TLS 1.3 (WSS)
  2. 设备认证: 预分配 device_token (首次注册时生成)
  3. 消息签名: HMAC-SHA256 (防篡改)
  4. 命令授权: 云端验证操作者权限后才下发命令
  5. 敏感数据: API Key 等永远不离开 imini
```

**关键原则**：
- **API Key 永远不离开 imini**：云端只存储 channel 类型和连接状态，不存储凭证
- **配置同步**：云端存配置快照（脱敏后），用于展示和审计
- **命令执行**：云端下发意图，本地 Agent 负责执行和权限校验

---

## 7. API 设计

### 7.1 本地 API（Manager Agent → 浏览器）

基础路径：`http://imini-ip:8080/api/v1`

```
# 认证
POST   /auth/login          # PAM 认证，返回 JWT
POST   /auth/logout
GET    /auth/me

# 系统状态
GET    /status               # OpenClaw 状态概览
GET    /status/health        # Gateway 健康检查
GET    /status/system        # CPU/内存/磁盘

# 配置管理
GET    /config               # 获取完整配置（脱敏）
PATCH  /config               # 部分更新配置 (→ config.patch RPC)
PUT    /config               # 全量替换配置 (→ config.apply RPC)
GET    /config/schema        # JSON Schema（用于前端表单生成）

# 频道管理
GET    /channels              # 列出所有频道
POST   /channels/:provider    # 添加频道
DELETE /channels/:provider    # 删除频道
GET    /channels/:provider/status  # 频道状态
POST   /channels/:provider/login   # 触发登录（返回 QR 等）
POST   /channels/:provider/logout

# 模型管理
GET    /models                # 列出模型
PUT    /models/default        # 设置默认模型
GET    /models/auth           # 认证状态

# 日志
GET    /logs                  # 最近日志
WS     /logs/stream           # 实时日志流 (→ logs.tail RPC)

# 操作
POST   /actions/doctor        # 运行诊断
POST   /actions/update        # 触发升级
POST   /actions/restart       # 重启 Gateway

# 向导
GET    /wizard/status         # 向导进度
POST   /wizard/step           # 执行向导步骤
```

### 7.2 云端 API（Cloud Controller → 浏览器）

基础路径：`https://api.manager.agentsphere.run/api/v1`

```
# 认证
POST   /auth/register
POST   /auth/login
POST   /auth/refresh
GET    /auth/me

# 租户管理 (super_admin)
GET    /tenants
POST   /tenants
GET    /tenants/:id

# 设备管理
GET    /devices                # 列出我的设备
POST   /devices/register       # 注册新设备（返回 device_token）
GET    /devices/:id            # 设备详情
GET    /devices/:id/status     # 实时状态
DELETE /devices/:id            # 删除设备

# 设备远程操作（通过 WSS 下发到 Agent）
POST   /devices/:id/command    # 通用命令下发
GET    /devices/:id/config     # 远程获取配置
PATCH  /devices/:id/config     # 远程修改配置
POST   /devices/:id/restart    # 远程重启
POST   /devices/:id/update     # 远程升级
GET    /devices/:id/logs       # 远程获取日志

# 设备频道管理
GET    /devices/:id/channels
POST   /devices/:id/channels/:provider
DELETE /devices/:id/channels/:provider

# 用户管理 (admin+)
GET    /users
POST   /users
PUT    /users/:id
DELETE /users/:id

# 仪表板（聚合视图）
GET    /dashboard/overview     # 设备总览（在线/离线/异常）
GET    /dashboard/alerts       # 告警列表
```

### 7.3 WebSocket API

```
# 云端 WSS（设备连接）
WSS    /ws/device              # 设备 Agent 连接端点

# 云端 WSS（管理员实时推送）
WSS    /ws/admin               # 管理员控制台实时推送
  events:
    - device_online / device_offline
    - device_status_update
    - device_alert
    - command_progress
    - command_result
```

---

## 8. 前端设计

### 8.1 本地管理页面

**设计原则**：极简、中文优先、大按钮、少输入

#### 页面结构

```
┌───────────────────────────────────────────────────┐
│  🤖 OpenClaw 管理 v1.0         [张姐] [退出]     │
├───────────┬───────────────────────────────────────┤
│           │                                       │
│  📊 概览  │   ┌─────────────────────────────────┐ │
│           │   │        系统状态                   │ │
│  💬 频道  │   │   🟢 运行中  |  最新版本 ✅      │ │
│           │   │   CPU: 12%  |  内存: 45%        │ │
│  ⚙️ 设置  │   │   磁盘: 23% |  运行: 5天3小时   │ │
│           │   └─────────────────────────────────┘ │
│  🔧 诊断  │   ┌─────────────────────────────────┐ │
│           │   │        已连接频道                 │ │
│  📋 日志  │   │   ✅ 钉钉  |  ✅ 飞书           │ │
│           │   │   ❌ 企业微信（未配置）           │ │
│           │   └─────────────────────────────────┘ │
│           │   ┌─────────────────────────────────┐ │
│           │   │        最近活动                   │ │
│           │   │   今天 32 条消息 | 5 个会话      │ │
│           │   └─────────────────────────────────┘ │
└───────────┴───────────────────────────────────────┘
```

#### 初始化向导（首次使用）

```
步骤 1/5: 欢迎
  "欢迎使用 AI 助手！接下来几步帮您连接常用的聊天软件。"
  [开始设置 →]

步骤 2/5: 选择 AI 模型
  "选择 AI 大脑（推荐使用默认设置）"
  ○ Claude（推荐，最智能）
  ○ GPT-4
  ○ 本地模型（高级）
  [填写 API Key: ___________]  [什么是 API Key？]

步骤 3/5: 连接聊天软件
  "选择要连接的聊天软件（可以之后再添加）"
  ☑ 钉钉    ☐ 飞书    ☐ 企业微信
  ☐ 微信    ☐ WhatsApp ☐ Slack

步骤 4/5: 配置钉钉
  "按照以下步骤在钉钉开发者后台创建机器人"
  [图文教程]
  Client ID: ___________
  Client Secret: ___________
  [测试连接]  ✅ 连接成功！

步骤 5/5: 完成
  "🎉 设置完成！您的 AI 助手已经准备好了。"
  "现在可以在钉钉里给机器人发消息试试。"
  [进入管理面板]
```

### 8.2 云端管理台

**设计原则**：仪表板风格、多设备管理、实时状态

```
┌──────────────────────────────────────────────────────────────┐
│  AgentSphere Manager    [搜索设备...]    [通知🔔3] [管理员▼] │
├────────────┬─────────────────────────────────────────────────┤
│            │                                                  │
│  📊 总览   │  ┌──────┐ ┌──────┐ ┌──────┐ ┌──────┐          │
│            │  │  12  │ │  10  │ │   1  │ │   1  │          │
│  🖥 设备   │  │ 设备 │ │ 在线 │ │ 异常 │ │ 离线 │          │
│            │  └──────┘ └──────┘ └──────┘ └──────┘          │
│  👥 用户   │                                                  │
│            │  设备列表                                        │
│  ⚙️ 设置   │  ┌────────────────────────────────────────────┐ │
│            │  │ 🟢 张姐办公室    钉钉✅ 飞书✅   v2026.2.26│ │
│  📋 审计   │  │ 🟢 李总会议室    企微✅ Slack✅   v2026.2.26│ │
│            │  │ 🟡 前台          钉钉✅ 飞书❌   v2026.2.25│ │
│            │  │ 🔴 仓库          --离线--       v2026.2.24 │ │
│            │  └────────────────────────────────────────────┘ │
│            │                                                  │
│            │  最近告警                                        │
│            │  ⚠️ 前台 - 飞书频道断开 (2小时前)               │
│            │  ⚠️ 仓库 - 设备离线 (6小时前)                   │
│            │  ℹ️ 张姐办公室 - 新版本可用 (1天前)             │
└────────────┴─────────────────────────────────────────────────┘
```

### 8.3 技术方案

| 组件 | 技术 | 理由 |
|------|------|------|
| 本地+云端前端 | React 19 + TypeScript | 生态成熟，复用组件 |
| UI 库 | Ant Design 5 中文版 | 企业级、中文友好 |
| 构建 | Vite 6 | 快速构建，适合嵌入 Go 二进制 |
| 状态管理 | Zustand | 轻量、适合实时数据 |
| 实时通信 | 原生 WebSocket | 避免额外依赖 |
| 国际化 | i18next | 中文优先，预留英文 |
| 图表 | Recharts | 监控图表 |

---

## 9. IM 适配器配置流程

### 9.1 IM 支持矩阵

| IM | OpenClaw 支持方式 | 配置复杂度 | 中国企业需求 |
|----|-------------------|-----------|-------------|
| **钉钉** | 插件 (clawdbot-dingtalk v0.3.24) | 中 | ⭐⭐⭐⭐⭐ |
| **飞书** | 内置 Channel (Feishu/Lark) | 中 | ⭐⭐⭐⭐ |
| **企业微信** | 需开发插件 | 高 | ⭐⭐⭐⭐⭐ |
| WhatsApp | 内置 (Baileys Web) | 低 | ⭐⭐ |
| Telegram | 内置 (grammY) | 低 | ⭐⭐ |
| Discord | 内置 | 低 | ⭐ |
| Slack | 内置 (Bolt SDK) | 中 | ⭐⭐⭐ |
| IRC | 内置 | 低 | ⭐ |
| Signal | 内置 (signal-cli) | 中 | ⭐ |
| Google Chat | 内置 (Webhook) | 中 | ⭐⭐ |
| MS Teams | 插件 (Bot Framework) | 高 | ⭐⭐⭐ |
| Matrix | 插件 | 中 | ⭐ |
| LINE | 插件 | 中 | ⭐⭐ (日本) |
| Zalo | 插件 | 中 | ⭐⭐ (越南) |
| 微信公众号 | 需开发插件 | 高 | ⭐⭐⭐ |

### 9.2 钉钉配置完整流程（以 Manager 向导为例）

Manager 向导后台执行的操作：

```
1. 用户在向导中填入 clientId + clientSecret

2. Manager Agent 构造配置补丁:
   {
     "channels": {
       "clawdbot-dingtalk": {
         "enabled": true,
         "clientId": "<用户输入>",
         "clientSecret": "<用户输入>",
         "aiCard": { "enabled": true }
       }
     }
   }

3. 通过 WS RPC 发送 config.patch:
   → Gateway 热更新配置
   → 钉钉插件自动重新连接

4. 轮询 channels.status 确认连接成功

5. 向用户展示 "✅ 钉钉连接成功"
```

**钉钉侧配置（需用户在钉钉开发者后台完成）**：
- 创建企业内部应用
- 开启机器人功能
- 配置消息接收地址（如果需要回调）
- 获取 Client ID / Client Secret

**Manager 的价值**：把上述步骤拆成图文教程嵌入向导，并自动完成 OpenClaw 侧配置。

### 9.3 企业微信适配器（需开发）

企业微信目前无官方 OpenClaw 插件，需要开发。参考钉钉插件架构：

```
openclaw-wechat-work 插件核心:
  - 基于企业微信 API SDK
  - 实现 OpenClaw ChannelAdapter 接口
  - 消息收发、事件回调
  - 支持应用消息 + 群聊
  - 支持 AI Card (如果企微支持)

配置字段:
  corpId      - 企业 ID
  agentId     - 应用 AgentID
  secret      - 应用 Secret
  token       - 消息回调 Token
  encodingAESKey - 消息加解密 Key
```

---

## 10. 技术选型

### 10.1 本地 Manager Agent

| 组件 | 选型 | 理由 |
|------|------|------|
| **语言** | Go 1.22+ | 单二进制部署、交叉编译、低资源占用 |
| **HTTP 框架** | Echo | 轻量、性能好、内置中间件 |
| **WebSocket** | gorilla/websocket | Go WS 标准 |
| **前端打包** | embed.FS | Go 内嵌静态文件，单二进制 |
| **PAM 认证** | msteinert/pam | Go PAM 绑定 |
| **进程管理** | os/exec | 调用 OpenClaw CLI |
| **安装方式** | deb 包 + apt | Ubuntu 标准 |

**为什么选 Go 而不是 Node.js？**
- 单二进制：不依赖 Node 运行时（OpenClaw 已占用 Node）
- 低资源：imini 可能只有 8-16GB 内存
- 交叉编译：一次构建 amd64/arm64
- 适合系统级守护进程

### 10.2 云端 Controller

| 组件 | 选型 | 理由 |
|------|------|------|
| **语言** | Go 1.22+ | 与本地 Agent 共享代码 |
| **数据库** | PostgreSQL 16 | 可靠、JSONB 支持好 |
| **缓存** | Redis 7 | 会话/设备状态缓存 |
| **WSS Hub** | gorilla/websocket | 设备长连接管理 |
| **认证** | JWT (RS256) + bcrypt | 标准 |
| **部署** | Docker Compose → K8s | 渐进式 |
| **TLS** | Caddy | 自动 HTTPS |

### 10.3 共享代码

```
openclaw-manager/
├── cmd/
│   ├── agent/          # 本地 Agent 二进制
│   └── cloud/          # 云端 Controller 二进制
├── internal/
│   ├── openclaw/       # OpenClaw WS RPC 客户端 (共享)
│   ├── protocol/       # Agent ↔ Cloud 协议定义 (共享)
│   ├── agent/          # 本地 Agent 逻辑
│   ├── cloud/          # 云端 Controller 逻辑
│   └── common/         # 工具函数
├── web/                # React 前端 (共享组件)
│   ├── packages/
│   │   ├── ui/         # 共享 UI 组件
│   │   ├── local/      # 本地管理页面
│   │   └── cloud/      # 云端管理台
│   └── package.json
├── deploy/
│   ├── deb/            # deb 包构建
│   └── docker/         # Docker 部署
└── docs/
```

---

## 11. 开发计划

### Phase 0: 基础验证（2 周）

**目标**：验证核心可行性

| 任务 | 说明 | 人天 |
|------|------|------|
| OpenClaw WS RPC Go 客户端 | 封装 connect/health/config.get/config.patch | 3 |
| PAM 认证 PoC | Ubuntu 账户登录 | 1 |
| 前端脚手架 | React + AntD + 路由 | 2 |
| 状态页面 | 调用 health API 展示 | 2 |
| 端到端演示 | 浏览器 → Agent → Gateway → 状态 | 2 |

**交付物**：能在浏览器上看到 OpenClaw 运行状态

### Phase 1: 本地管理 MVP（4 周）

**目标**：张姐能用浏览器完成基本管理

| 任务 | 人天 |
|------|------|
| 配置管理 UI（JSON Schema → Form） | 5 |
| 频道管理（添加/删除/状态 + 钉钉向导） | 5 |
| 初始化向导（5 步引导） | 3 |
| 日志查看器（实时流 + 过滤） | 2 |
| 诊断工具（doctor 结果可视化） | 2 |
| systemd 集成 + deb 包 | 2 |
| 用户文档（中文） | 1 |

**交付物**：可安装的 deb 包，本地管理功能完整

### Phase 2: 云端管理台（6 周）

**目标**：管理员能远程管理多台设备

| 任务 | 人天 |
|------|------|
| 云端 API（用户/租户/设备 CRUD） | 5 |
| WSS Hub（设备长连接 + 命令下发） | 5 |
| 设备注册/配对（Token + QR） | 3 |
| 管理台前端（设备列表/详情/远程操作） | 8 |
| 多租户（权限隔离 + 用户管理） | 4 |
| 告警系统（离线/断开/版本过旧） | 3 |
| Docker Compose 部署 + CI/CD | 2 |

**交付物**：可部署的云端管理台

### Phase 3: 企业功能（4 周）

**目标**：生产级可靠性

| 任务 | 人天 |
|------|------|
| 企业微信 OpenClaw 插件开发 | 8 |
| 批量 OTA 升级 | 3 |
| 审计日志 | 3 |
| 配置备份恢复 | 2 |
| 100+ 设备压测 | 2 |
| 安全审计 + 修复 | 2 |

**交付物**：生产就绪的完整平台

### 里程碑时间线

```
Week 0-2:   ████ Phase 0 - 基础验证
Week 3-6:   ████████ Phase 1 - 本地 MVP
Week 7-12:  ████████████ Phase 2 - 云端管理台
Week 13-16: ████████ Phase 3 - 企业功能
            ──────────────────────────────
            总计: ~16 周 (4 个月)
```

---

## 12. 风险评估

### 12.1 技术风险

| 风险 | 概率 | 影响 | 缓解措施 |
|------|------|------|----------|
| OpenClaw WS Protocol 变更 | 中 | 高 | 建 API 兼容层，跟踪上游 PROTOCOL_VERSION |
| WS RPC 缺少某些能力 | 中 | 中 | 回退到 CLI exec 调用 |
| config.patch 速率限制（3/60s） | 低 | 中 | 前端防抖 + 批量提交 |
| PAM 认证兼容性 | 低 | 低 | 备选：Basic Auth + htpasswd |
| 企业微信插件开发难度 | 高 | 高 | 降优先级，先做钉钉+飞书 |
| WSS 长连接断开/重连 | 中 | 中 | 指数退避 + 离线命令队列 |

### 12.2 产品风险

| 风险 | 概率 | 影响 | 缓解措施 |
|------|------|------|----------|
| 用户不愿在钉钉后台创建应用 | 高 | 高 | 极详细图文教程 + 视频 |
| OpenClaw 版本迭代破坏兼容 | 中 | 高 | 版本锁定 + 灰度升级 |
| imini 硬件差异 | 低 | 中 | 标准化 Ubuntu 镜像 |
| 多租户安全隔离不足 | 低 | 高 | 从设计阶段做好租户隔离 |

### 12.3 关键依赖

```
OpenClaw 生态:
  ├── openclaw npm 包 (Gateway + CLI)
  ├── Node.js 22+ (Gateway 运行时)
  ├── Gateway WS Protocol v3 (TypeBox Schema)
  ├── clawdbot-dingtalk 插件 (v0.3.24)
  └── Feishu 内置 Channel

Manager 基础设施:
  ├── Go 1.22+ (Agent + Cloud)
  ├── Ubuntu 22.04+ (systemd + PAM)
  ├── PostgreSQL 16 (云端)
  ├── Redis 7 (云端)
  └── Docker/K8s (云端部署)
```

---

## 附录 A: OpenClaw Gateway 已有 Control UI

OpenClaw 已��置 Control UI（`http://127.0.0.1:18789/`），提供：
- Config 表单编辑器（基于 JSON Schema）
- Raw JSON 编辑器
- 日志查看器（logs.tail RPC）
- 健康状态

**Manager 与 Control UI 的关系**：
- Control UI 面向开发者，直接暴露 OpenClaw 原生配置
- Manager 面向"张姐"，重新设计 UX，隐藏技术细节
- Manager 可嵌入 Control UI 作为"高级模式"入口
- 长期来看 Manager 替代 Control UI 成为主要管理界面

## 附录 B: 当前实例运行信息

```
OpenClaw 版本:  2026.2.26 (bc50708)
Gateway 端口:   18789 (loopback)
Gateway 认证:   token
系统服务:       systemd installed, enabled, running (pid 1845928)
已加载插件:     clawdbot-dingtalk v0.3.24 (6/38 总插件)
活跃会话:       19
Agent:          main (claude-opus-4-6, 200k ctx)
Memory:         102 files, 397 chunks
Heartbeat:      30m
```

## 附录 C: 配置文件关键路径

```
~/.openclaw/openclaw.json          # 主配置文件 (JSON5)
~/.openclaw/agents/main/           # Agent 目录
~/.openclaw/agents/main/sessions/  # 会话存储
~/.openclaw/extensions/            # 插件目录
~/.openclaw/credentials/           # 凭证存储
~/.openclaw/.env                   # 环境变量
/tmp/openclaw/openclaw-*.log       # 日志文件（按日滚动）
```

## 附录 D: 支持的 IM 频道完整列表（来自 OpenClaw 文档）

**内置频道**: WhatsApp, Telegram, Discord, IRC, Slack, Feishu/Lark, Google Chat, Signal, BlueBubbles (iMessage), iMessage (legacy), WebChat

**插件频道**: Mattermost, MS Teams, Synology Chat, LINE, Nextcloud Talk, Matrix, Nostr, Tlon, Twitch, Zalo, Zalo Personal, DingTalk

**需开发**: 企业微信 (WeCom), 微信公众号
