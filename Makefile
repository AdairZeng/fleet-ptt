.DEFAULT_GOAL := up

COMPOSE := docker compose
HEALTH_URL := http://127.0.0.1:8080/healthz

.PHONY: up start down health server web

# 构建并前台启动。实际执行 docker compose up --build。
up:
	$(COMPOSE) up --build

start: up

down:
	$(COMPOSE) down

health:
	curl -fsS $(HEALTH_URL)

# 只启动 Go。监听 HTTP_ADDR（默认 :8080），静态目录 STATIC_DIR（默认 web/dist）。
server:
	go run ./server/cmd/server

# 只启动 Vite。安装 web 依赖后把 /healthz、/api 代理到 Go。
web:
	cd web && npm install && npm run dev
