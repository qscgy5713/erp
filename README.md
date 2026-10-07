# ERP

給台灣中小企業(買賣業為主、預留輕製造)使用的 ERP 系統,串起採購、庫存、銷售、應收應付與會計總帳。

> 目前狀態:**M0 基礎建設進行中**。開發環境可啟動,業務功能尚未實作。規劃見 [doc/plan.md](doc/plan.md)。

## 功能範圍(第一期)
- 系統基礎:登入、RBAC 權限與資料範圍、稽核日誌、單號規則、單層簽核
- 基本資料:料品(含單位換算)、客戶、供應商、倉庫、幣別匯率、稅別
- 採購:採購單 → 進貨單 → 進貨退出
- 銷售:報價單 → 訂單 → 出貨單 → 銷貨退回
- 庫存:流水帳、調整、調撥、盤點、月加權平均成本與月結
- 應收應付:收付款沖帳、對帳單、帳齡
- 會計:科目表、傳票、業務單據自動拋轉、關帳、試算表

## 技術棧
| 層 | 技術 |
|---|---|
| 後端 | Go 1.27、Gin、pgx/v5(之後加 sqlc、River) |
| 前端 | Vue 3、TypeScript、Vite、Pinia、Vue Router、Element Plus |
| 資料庫 | PostgreSQL 18 |
| Migration | golang-migrate |
| 環境 | Docker Compose(開發與正式都用容器) |

## 安裝與環境設定
只需要 Docker(含 Compose v2)與 `make`。本機不必安裝 Go、Node 或 PostgreSQL。

```bash
cp .env.example .env   # 可調整埠號與 DB 帳密;make up 若沒有 .env 會自動複製
make up                # 建置並啟動所有服務
```

`.env` 主要設定:

| 變數 | 預設 | 說明 |
|---|---|---|
| `POSTGRES_USER` / `POSTGRES_PASSWORD` / `POSTGRES_DB` | `erp` | 資料庫帳密 |
| `POSTGRES_PORT` | `15432` | 本機連 DB 的埠號 |
| `API_PORT` | `18080` | 本機連 API 的埠號 |
| `WEB_PORT` | `15173` | 前端埠號 |
| `APP_ENV` | `development` | `production` 時 log 改輸出 JSON |
| `TRUSTED_PROXIES` | 空 | 可信任的反向代理 IP/CIDR(逗號分隔);只有來自這些位址的 `X-Forwarded-For` 會被採用 |

## 啟動 / 使用方式
啟動後:
- 前端:http://localhost:15173
- API 健康檢查:http://localhost:18080/api/v1/health
- 資料庫:`127.0.0.1:15432`(或 `make psql`)

服務組成:

| 服務 | 說明 |
|---|---|
| `postgres` | PostgreSQL 18,資料存在 volume `erp_pgdata` |
| `migrate` | 一次性執行 migration,成功後 `api` 才啟動 |
| `api` | Go API,改程式會由 air 自動重新編譯 |
| `web` | Vite 開發伺服器,`/api` 代理到 `api` |

常用指令(`make help` 可列出全部):

```bash
make up                      # 啟動
make down                    # 停止
make logs s=api              # 看 log
make test                    # 前後端測試(含資料庫整合測試,服務未啟動也可執行)
make lint                    # 只檢查不修改:gofmt / go vet / vue-tsc / oxlint / eslint / prettier
make migrate-new name=xxx    # 新增 migration
make migrate-up              # 套用 migration
make migrate-down            # 回滾一支
make psql                    # 進入資料庫
make reset-db                # 清空資料庫並重跑 migration(會刪資料)
```

正式環境映像檔(`prod` target):

```bash
docker build --target prod -t erp-api backend    # alpine + 靜態 binary
docker build --target prod -t erp-web frontend   # nginx 提供靜態檔並代理 /api 到 api:8080
```

## 目錄結構
```
erp/
├── README.md
├── docker-compose.yml   # 開發環境
├── .env.example
├── Makefile
├── doc/                 # 規劃與過程文件
├── backend/
│   ├── cmd/api/         # API 進入點
│   ├── internal/
│   │   ├── platform/    # config、database、httpserver
│   │   └── shared/      # 共用元件(回應格式…)
│   ├── migrations/      # golang-migrate SQL
│   ├── Dockerfile       # dev / build / prod
│   └── .air.toml        # 熱重載設定
└── frontend/
    ├── src/
    │   ├── api/         # API client
    │   ├── layouts/
    │   ├── views/
    │   ├── router/
    │   └── __tests__/
    ├── Dockerfile       # dev / build / prod
    └── nginx.conf       # 正式環境 nginx 設定
```

## 文件索引
- [doc/plan.md](doc/plan.md):計畫與設計(範圍、決策、架構、資料模型、里程碑)
- [doc/todo.md](doc/todo.md):待辦清單
- [doc/worklog.md](doc/worklog.md):工作日誌
