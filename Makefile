# 所有指令都透過 docker compose 執行
DC := docker compose
MIGRATE := $(DC) run --rm migrate

.PHONY: help up down restart logs ps migrate-up migrate-down migrate-new psql test test-api test-web lint reset-db

help: ## 列出可用指令
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'

.env:
	cp .env.example .env

# -V:重建匿名 volume,避免 web 容器沿用舊的 node_modules(package.json 變動後)
up: .env ## 啟動所有服務(背景)
	$(DC) up -d --build -V

down: ## 停止所有服務
	$(DC) down

restart: ## 重啟 api 與 web
	$(DC) restart api web

logs: ## 追蹤 log(可指定 s=api)
	$(DC) logs -f $(s)

ps: ## 服務狀態
	$(DC) ps -a

migrate-up: ## 套用所有 migration
	$(MIGRATE) up

migrate-down: ## 回滾最後一支 migration
	$(MIGRATE) down 1

migrate-new: ## 新增 migration,例:make migrate-new name=create_users
	@test -n "$(name)" || (echo "請指定 name=..." && exit 1)
	$(MIGRATE) create -ext sql -dir /migrations -seq $(name)

psql: ## 進入資料庫
	$(DC) exec postgres sh -c 'psql -U $$POSTGRES_USER -d $$POSTGRES_DB'

test: test-api test-web ## 執行所有測試

# 用 run --rm:服務沒啟動也能跑;api 會自動帶起 postgres 並套用 migration 供整合測試使用
test-api: .env ## 後端測試(含資料庫整合測試)
	$(DC) run --rm api go test ./...

test-web: .env ## 前端測試
	$(DC) run --rm --no-deps web npx vitest run

# 只檢查不修改(npm run lint 會 --fix,這裡不用)
lint: .env ## 程式檢查
	$(DC) run --rm --no-deps api sh -c 'test -z "$$(gofmt -l .)" || { gofmt -l .; exit 1; }; go vet ./...'
	$(DC) run --rm --no-deps web sh -c 'npm run type-check && npx oxlint . && npx eslint . && npx prettier --check src/'

reset-db: ## 清空資料庫並重跑 migration(會刪資料!)
	$(DC) down
	docker volume rm -f $$(grep -E '^COMPOSE_PROJECT_NAME=' .env | cut -d= -f2)_pgdata
	$(DC) up -d
