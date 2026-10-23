.DEFAULT_GOAL := up

COMPOSE := docker compose
HEALTH_URL := http://127.0.0.1:8080/healthz

.PHONY: up start down health

# 构建并前台启动。实际执行 docker compose up --build。
up:
	$(COMPOSE) up --build

start: up

down:
	$(COMPOSE) down

health:
	curl -fsS $(HEALTH_URL)
