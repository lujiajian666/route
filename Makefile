BACKEND_PORT  ?= 8080
FRONTEND_PORT ?= 3000
BACKEND_ORIGIN ?= http://127.0.0.1:$(BACKEND_PORT)

.PHONY: dev dev-dry dev-root backend backend-dry backend-root frontend build test clean

# 同时启动前后端，Ctrl+C 一起退出
dev:
	@$(MAKE) -j2 backend frontend

# 只模拟路由命令，不改真实路由表
dev-dry:
	@$(MAKE) -j2 backend-dry frontend

# 用 sudo 启动后端（前端仍是当前用户）
dev-root:
	@$(MAKE) -j2 backend-root frontend

backend:
	@cd be && go run . --listen 127.0.0.1:$(BACKEND_PORT)

backend-dry:
	@cd be && go run . --listen 127.0.0.1:$(BACKEND_PORT) --dry-run

backend-root:
	@cd be && go build -o route-daemon . && sudo ./route-daemon --listen 127.0.0.1:$(BACKEND_PORT)

frontend:
	@cd fe && BACKEND_ORIGIN=$(BACKEND_ORIGIN) npm run dev -- --port $(FRONTEND_PORT)

build:
	@cd be && go build -o route-daemon .
	@cd fe && BACKEND_ORIGIN=$(BACKEND_ORIGIN) npm run build

test:
	@cd be && go test ./...
	@cd fe && npx tsc --noEmit && npm run lint

clean:
	@cd be && rm -f route-daemon
	@cd fe && rm -rf .next tsconfig.tsbuildinfo
