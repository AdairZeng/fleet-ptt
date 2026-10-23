# fleet-ptt

面向司机/车队场景的群语音对讲（Push-to-Talk）。

## 文档

- [产品设计方案](docs/product-design.md)
- [详细技术方案](docs/technical-design.md)
- [灵矽接入结论](docs/linx-access.md)

## 配置

仓库仅提供 [`.env.example`](.env.example)。本地复制为 `.env` 后填写真实值，勿提交 `.env`。

`LINX_CHAT_TOKEN` 留空时，`POST /api/v1/linx/chat` 会拒绝回调。灵矽侧要配置的地址是 `{PUBLIC_BASE_URL}/api/v1/linx/chat`。

## 本地启动

```bash
cp .env.example .env
make up
```

`make up`（等同 `make start`）会执行 `docker compose up --build`。浏览器打开 <http://localhost:8080>。另开终端执行 `make health`，应输出 `OK`。停止并移除容器：`make down`。数据目录 `data/` 挂载到容器 `/data`（已 gitignore）。

开发时前后端分开跑（各开一个终端，在仓库根目录）：

```bash
make server
```

```bash
make web
```

`make web` 把 `/healthz` 与 `/api` 代理到 `http://127.0.0.1:8080`。`make server` 的静态资源默认从仓库根目录的 `web/dist` 提供，可用 `STATIC_DIR` 覆盖。未设置 `SQLITE_PATH` 时数据库写到 `data/fleet-ptt.db`。签发会话需要 `SESSION_SECRET`：`make up` 从 `.env` 注入；只跑 `make server` 时请先导出该变量。未配置时 `/healthz` 仍返回 `OK`，昵称进入会失败。
