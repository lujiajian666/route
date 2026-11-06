.PHONY: dev backend frontend

# 同时启动后端（sudo）和前端，Ctrl+C 退出
dev:
	@$(MAKE) -j2 backend frontend

backend:
	@cd be && go build -o route-daemon . && sudo ./route-daemon

frontend:
	@cd fe && npm run dev
