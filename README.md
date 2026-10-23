# fleet-ptt

面向司机/车队场景的群语音对讲（Push-to-Talk）。

## 文档

- [产品设计方案](docs/product-design.md)
- [详细技术方案](docs/technical-design.md)

## 配置

仓库仅提供 [`.env.example`](.env.example)。本地复制为 `.env` 后填写真实值，勿提交 `.env`。

## 本地启动

```bash
cp .env.example .env
docker compose up --build
```

浏览器打开 <http://localhost:8080>。`GET /healthz` 返回 `OK`。数据目录 `data/` 挂载到容器 `/data`（已 gitignore）。

开发时前后端分开跑（在仓库根目录）：

```bash
go run ./server/cmd/server
```

```bash
cd web && npm install && npm run dev
```

Vite 把 `/healthz` 与 `/api` 代理到 `http://127.0.0.1:8080`。静态资源默认从仓库根目录的 `web/dist` 提供；可用 `STATIC_DIR` 覆盖。
