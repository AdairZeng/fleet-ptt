# fleet-ptt

面向司机和车队的语音优先群组对讲系统，产品角色名为 **小路**。用户通过自然语音完成建群、入群、切群、开始和结束对讲；浏览器中的 3D 对讲机用于验证未来硬件的操作、状态与音频体验。

## 文档

- [产品设计方案](docs/product-design.md)
- [详细技术方案](docs/technical-design.md)
- [灵矽接入方案](docs/linx-access.md)

## 配置

仓库仅提供 [`.env.example`](.env.example)。本地复制为 `.env` 后填写真实值，勿提交 `.env`。

灵矽智能体负责语音识别、语义理解、参数追问和语音播报，通过云端 MCP 调用 fleet-ptt 的频道、对讲和设备工具。MCP SSE 地址为 `{PUBLIC_BASE_URL}/api/v1/mcp`，在灵矽私有自定义 MCP 中配置请求头 `Authorization: Bearer <LINX_MCP_TOKEN>`，并把 MCP 关联到“小路”智能体。配置步骤见 [灵矽接入方案](docs/linx-access.md)。

3D 原型和真实硬件使用统一设备协议。模拟设备注册时自动生成 `device_id`、`mac_id` 和 `device_credential`；真实硬件使用预配的同名字段。按下电源键后，服务端按该设备的 `mac_id` 检查并自动完成灵矽绑定，随后进入昵称设置和业务流程。指令音频通过 `/api/v1/linx/ws` 桥接到灵矽，普通对讲音频走七牛 RTN。

完整本地联调需要公网隧道暴露 MCP 的 SSE 与消息接口，并配置真实灵矽和 RTN 凭证。当前尚未完成真实灵矽音频、工具调用和双端对讲的联合验收；首次语音设置和持续云端唤醒仍待实现与验证。

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
