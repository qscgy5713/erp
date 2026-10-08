# 所有指令都透過 docker compose 執行
DC := docker compose
MIGRATE := $(DC) run --rm migrate

.PHONY: help sqlc sqlc-check admin up down restart logs ps migrate-up migrate-down migrate-new psql test test-api test-web lint osv reset-db perf-db perf-api perf-run perf-down

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

sqlc: ## 由 backend/queries/*.sql 產生 Go 程式碼(internal/db)
	$(DC) run --rm sqlc generate

sqlc-check: ## 檢查產生的程式碼是否為最新
	$(DC) run --rm sqlc diff

admin: .env ## 建立超級管理員,例:ADMIN_PASSWORD='...' make admin u=admin
	@test -n "$(u)" || (echo "請指定 u=帳號" && exit 1)
	@test -n "$$ADMIN_PASSWORD" || (echo "請設定環境變數 ADMIN_PASSWORD" && exit 1)
	$(DC) run --rm -e ADMIN_PASSWORD api go run ./cmd/cli create-admin -username "$(u)"

psql: ## 進入資料庫
	$(DC) exec postgres sh -c 'psql -U $$POSTGRES_USER -d $$POSTGRES_DB'

test: test-api test-web ## 執行所有測試

# 用 run --rm:服務沒啟動也能跑;api 會自動帶起 postgres 並套用 migration 供整合測試使用
test-api: .env ## 後端測試(含資料庫整合測試)
	$(DC) run --rm api go test ./...

test-web: .env ## 前端測試
	$(DC) run --rm --no-deps web npx vitest run

# 相依套件弱點掃描(需 python3 與網路;已評估的項目見 scripts/osv-ignore.txt)
osv: ## 相依套件弱點掃描(OSV)
	scripts/osv-scan.sh

# 只檢查不修改(npm run lint 會 --fix,這裡不用)
lint: .env ## 程式檢查
	$(DC) run --rm --no-deps api sh -c 'test -z "$$(gofmt -l .)" || { gofmt -l .; exit 1; }; go vet ./...'
	$(DC) run --rm golangci-lint
	$(DC) run --rm sqlc diff
	$(DC) run --rm --no-deps web sh -c 'npm run type-check && npx oxlint . && npx eslint . && npx prettier --check src/'

reset-db: ## 清空資料庫並重跑 migration(會刪資料!)
	$(DC) down
	docker volume rm -f $$(grep -E '^COMPOSE_PROJECT_NAME=' .env | cut -d= -f2)_pgdata
	$(DC) up -d

# ---- 壓力測試:獨立的 erp_perf 資料庫與第二個 API 實例,完全不碰開發資料 ----
# 流程:make perf-db → make perf-api → make perf-run [C=30 D=60s] → make perf-down
PERF_DB := erp_perf
PERF_PW ?= Perf-Test-9x!
PERF_URL = postgres://$$POSTGRES_USER:$$POSTGRES_PASSWORD@postgres:5432/$(PERF_DB)?sslmode=disable

perf-db: .env ## 建立壓測資料庫、套用 migration、灌入約一年規模的資料(約 1 分鐘)
	docker rm -f erp-perf-api >/dev/null 2>&1 || true
	set -a && . ./.env && set +a && \
	$(DC) exec -T postgres psql -U $$POSTGRES_USER -d postgres -c "DROP DATABASE IF EXISTS $(PERF_DB)" -c "CREATE DATABASE $(PERF_DB)" && \
	$(DC) run --rm -T -e DATABASE_URL="$(PERF_URL)" migrate up && \
	$(DC) run --rm -T -e DATABASE_URL="$(PERF_URL)" -e ADMIN_PASSWORD='$(PERF_PW)' api go run ./cmd/cli create-admin -username perfadmin && \
	$(DC) exec -T postgres psql -U $$POSTGRES_USER -d $(PERF_DB) -c "UPDATE users SET must_change_password = false" && \
	$(DC) exec -T postgres psql -U $$POSTGRES_USER -d $(PERF_DB) -v ON_ERROR_STOP=1 -q -f - < backend/loadtest/seed.sql

perf-api: .env ## 啟動連到壓測資料庫的 API(容器名稱 erp-perf-api,限流放寬)
	set -a && . ./.env && set +a && \
	docker rm -f erp-perf-api >/dev/null 2>&1; \
	$(DC) run -d --name erp-perf-api --no-deps -e DATABASE_URL="$(PERF_URL)" -e RATE_LIMIT_PER_MINUTE=100000000 \
	  -e LOGIN_RATE_LIMIT_PER_MINUTE=100000 -e REFRESH_RATE_LIMIT_PER_MINUTE=100000 api \
	  sh -c 'go build -o /tmp/erpapi ./cmd/api && exec /tmp/erpapi'
	$(DC) run --rm -T --no-deps api sh -c 'for i in $$(seq 1 60); do wget -qO- http://erp-perf-api:8080/api/v1/health >/dev/null 2>&1 && echo "API 已就緒" && exit 0; sleep 2; done; echo "API 未就緒"; exit 1'

perf-run: ## 執行負載測試:make perf-run C=30 D=60s(併發數、時間)
	$(DC) run --rm -T --no-deps -e LOADTEST_PASSWORD='$(PERF_PW)' api go run ./cmd/loadtest \
	  -url http://erp-perf-api:8080 -c $(or $(C),30) -d $(or $(D),60s)

perf-down: ## 停掉壓測 API 並刪除壓測資料庫
	docker rm -f erp-perf-api >/dev/null 2>&1 || true
	set -a && . ./.env && set +a && \
	$(DC) exec -T postgres psql -U $$POSTGRES_USER -d postgres -c "DROP DATABASE IF EXISTS $(PERF_DB)"
