# fleet-ptt

面向司机和车队的语音优先群组对讲系统，产品角色名为 **小路**。用户通过自然语音完成建群、入群、切群、开始和结束对讲；浏览器中的 3D 对讲机用于验证未来硬件的操作、状态与音频体验。

## 文档

- [产品设计方案](docs/product-design.md)
- [详细技术方案](docs/technical-design.md)
- [灵矽接入方案](docs/linx-access.md)

## 配置

仓库仅提供 [`.env.example`](.env.example)。本地复制为 `.env` 后填写真实值，勿提交 `.env`。

灵矽回调地址为 `{PUBLIC_BASE_URL}/api/v1/linx/chat`，请求使用 `Authorization: Bearer <LINX_CHAT_TOKEN>`。回调负责将识别后的意图交给小路业务层，业务层执行频道、对讲和设备动作，并返回适合 TTS 的回答。

3D 原型通过 `/api/v1/linx/ws` 连接真实灵矽设备 WebSocket。配置 `LINX_API_KEY`、`LINX_AGENT_ID` 和虚拟 `LINX_DEVICE_ID` 后，按下 3D 原型电源键时通过官方 API 检查并自动完成 OTA、激活码绑定和 WebSocket 凭证获取；浏览器不接触灵矽凭证。完整本地联调需要用公网隧道暴露 `/api/v1/linx/chat`，例如 `cloudflared tunnel --url http://127.0.0.1:8080`，再将隧道地址配置到 `PUBLIC_BASE_URL`。

## 本地启动

```bash
cp .env.example .env
make up
```

`make up` 会执行 `docker compose up --build`，浏览器访问 <http://localhost:8080>。`make health` 检查进程探活，`make down` 停止并移除容器。数据目录 `data/` 挂载到容器 `/data`。

开发时可以分别运行：

```bash
make server
make web
```

`make web` 将 `/healthz`、`/api` 及 WebSocket 代理到 Go 服务。`make server` 默认从 `web/dist` 托管静态资源，并使用 `data/fleet-ptt.db` 作为本地数据库路径；运行会话功能前需设置 `SESSION_SECRET`。
