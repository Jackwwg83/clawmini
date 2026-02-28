# ClawMini — 睿动AI (RayStone AI) 设备管理平台

> OpenClaw Appliance 的集中管理平台——设备注册 + 远程管理 + IM 配置。

## 架构

```
Browser ──► ClawMini Server (Cloud) ──WSS──► ClawMini Client (imini)
                                                    ↓
                                               OpenClaw CLI
```

## 开发

```bash
# Server
cd cmd/server && go run .

# Client
cd cmd/client && go run . --server wss://your-server --token xxx

# Frontend
cd web && npm run dev
```

## 团队

- **PM/协调**: 墨 (Mo)
- **后端开发**: Codex (gpt-5.3-codex)
- **前端开发**: Claude Code (Opus 4.6)
- **架构/QA**: Claude Code (Opus 4.6)
