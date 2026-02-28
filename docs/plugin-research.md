# OpenClaw 钉钉 / 飞书 / 企业微信插件调研报告

> 调研时间：2026-02-28

---

## 一、总览

| 平台 | 官方支持 | 推荐插件 | 成熟度 |
|------|---------|---------|-------|
| 飞书 | ✅ 官方 stock 内置 | @openclaw/feishu | ⭐⭐⭐⭐ |
| 钉钉 | ❌ 非内置，社区 | clawdbot-dingtalk (已安装) | ⭐⭐⭐⭐ |
| 企业微信 | ❌ PR 中未合并 | 暂无推荐 | ⭐⭐ |

---

## 二、飞书

### @openclaw/feishu（官方 stock 插件，推荐）
- 状态：随 OpenClaw 内置，当前 disabled，需安装激活
- 维护：OpenClaw 官方，社区贡献者 @m1heng 维护
- 版本：2026.2.26
- 安装：openclaw plugins install @openclaw/feishu

配置：
```json5
{
  channels: {
    feishu: {
      enabled: true,
      dmPolicy: "pairing",
      accounts: { main: { appId: "cli_xxx", appSecret: "xxx" } }
    }
  }
}
```

关键配置字段：
- appId / appSecret（必填）
- domain: "feishu" | "lark"（国际版用 lark）
- connectionMode: "websocket"（无需公网 URL）
- streaming: true（流式卡片输出）
- dmPolicy: "pairing" | "open" | "allowlist"

功能：WebSocket长连接、流式输出、文本/图片/文件/音视频、群聊、多账号、Lark国际版

### @m1heng-clawd/feishu（社区 npm 包，备选）
- npm：@m1heng-clawd/feishu
- GitHub：https://github.com/m1heng/clawdbot-feishu
- 版本：0.1.14，最近更新：2026-02-27（非常活跃）
- 说明：本质上与 stock 插件同一维护者 @m1heng，配置几乎相同

### 其他（不推荐）
- @xzq-xu/feishu v0.3.1 - 个人项目
- feishu-openclaw v0.3.1 - 个人项目
- @overlink/openclaw-feishu - 个人项目

成熟度：⭐⭐⭐⭐

---

## 三、钉钉

### clawdbot-dingtalk（已安装，推荐保持）
- npm：clawdbot-dingtalk
- 版本：0.3.24（当前安装）
- 最近更新：2026-02-11
- 维护方：jerryyoung（阿里云生态）
- 功能：Stream API、阿里云百炼 MCP（4个工具）、AI Card 流式输出、文件发送

配置关键字段（易踩坑）：
- clientId（不是 appKey！）
- clientSecret（不是 appSecret！）
- aiCard: { enabled: true }（不是 aiCardMode: "enabled"）

### @soimy/dingtalk（更活跃社区插件，值得关注）
- npm：@soimy/dingtalk
- GitHub：https://github.com/soimy/openclaw-channel-dingtalk
- 版本：3.1.4，最近更新：2026-02-27（比现有插件更新！）
- 安装：openclaw plugins install @soimy/dingtalk
- 说明：前身是 @clawdbot/dingtalk，3.x 大版本重构，更新频率更高

对比：
| 维度 | clawdbot-dingtalk | @soimy/dingtalk |
|------|------------------|-----------------|
| 版本 | 0.3.24 | 3.1.4 |
| 最近更新 | 2026-02-11 | 2026-02-27 |
| 阿里云MCP | ✅ 内置 4 个 | 未知 |
| 当前状态 | 已安装运行 | 需迁移 |

成熟度：⭐⭐⭐⭐

---

## 四、企业微信

- 官方 PR #13228 (openclaw/openclaw) 尚未合并
- 社区插件均为个人项目，较零散：
  - sunnoy/openclaw-plugin-wecom（最完整，支持流式/群聊/指令白名单）
  - CreatorAris/openclaw-wecom-plugin（基础功能）
- 暂不推荐生产使用

成熟度：⭐⭐

---

## 五、最终推荐方案

### 立即可做：添加飞书
```bash
openclaw plugins install @openclaw/feishu
# 然后配置 ~/.openclaw/openclaw.json 添加 feishu 频道
```

### 钉钉：保持现状，评估升级
- 短期：保持 clawdbot-dingtalk 0.3.24（稳定运行）
- 中期：评估迁移到 @soimy/dingtalk（更新更活跃，社区文档更丰富）

### 企业微信：暂缓
等官方 PR 合并或 sunnoy 版本更成熟

---

## 六、插件速查表

| 插件 | npm 包 | GitHub | 最近更新 | 安装 |
|------|--------|--------|---------|------|
| 飞书官方 | @openclaw/feishu(内置) | openclaw/openclaw | 2026-02-26 | openclaw plugins install @openclaw/feishu |
| 飞书社区 | @m1heng-clawd/feishu | m1heng/clawdbot-feishu | 2026-02-27 | openclaw plugins install @m1heng-clawd/feishu |
| 钉钉当前 | clawdbot-dingtalk | 阿里云生态 | 2026-02-11 | 已安装 v0.3.24 |
| 钉钉活跃 | @soimy/dingtalk | soimy/openclaw-channel-dingtalk | 2026-02-27 | openclaw plugins install @soimy/dingtalk |
| 企业微信 | 无 npm | sunnoy/openclaw-plugin-wecom | 未知 | 手动克隆安装 |
