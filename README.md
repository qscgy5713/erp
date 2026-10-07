# ERP

給台灣中小企業(買賣業為主、預留輕製造)使用的 ERP 系統,串起採購、庫存、銷售、應收應付與會計總帳。

> 目前狀態:**M0 基礎建設完成**(登入、權限、稽核、單號、系統管理)。下一步 M1 基本資料。規劃見 [doc/plan.md](doc/plan.md)。

## 功能

### 已完成(M0)
- **登入**:JWT access token(15 分鐘)+ HttpOnly refresh cookie(7 天、每次刷新輪替、偵測重放)、連續 5 次密碼錯誤鎖定 15 分鐘、首次登入強制改密碼
- **限流中介層**:已登入以使用者計算、未登入以 IP 計算,超過回 429 並帶 `Retry-After`
- **權限**:RBAC(角色 → 權限點)+ 資料範圍(全部/本部門/本人);非超級管理員不能授予自己沒有的權限
- **稽核日誌**:所有寫入動作記錄前後差異、IP、request id;資料庫層禁止修改刪除
- **單號規則**:前綴 + 日期 + 流水號,可設定每日/月/年重置,併發不重號、回滾不跳號
- **系統管理頁面**:部門(樹狀)、使用者、角色權限、單號規則、稽核日誌
- **共用元件**:單據狀態機(草稿→待審→已核准→已過帳→已結案/作廢)、金額捨入與稅額計算

### 規劃中(第一期)
基本資料 → 庫存 → 採購 → 銷售 → 應收應付 → 會計 → 月結,詳見 [doc/todo.md](doc/todo.md)。

## 技術棧
| 層 | 技術 |
|---|---|
| 後端 | Go 1.27、Gin、pgx/v5、sqlc、golang-jwt、bcrypt |
| 前端 | Vue 3、TypeScript、Vite、Pinia、Vue Router、Element Plus |
| 資料庫 | PostgreSQL 18 |
| Migration | golang-migrate(純 SQL) |
| 環境 | Docker Compose(開發與正式都用容器) |
| CI | GitHub Actions(`.github/workflows/ci.yml`) |

## 安裝與環境設定
只需要 Docker(含 Compose v2)與 `make`。本機不必安裝 Go、Node 或 PostgreSQL。

```bash
cp .env.example .env                              # make up 若沒有 .env 也會自動複製
make up                                           # 建置並啟動所有服務
ADMIN_PASSWORD='至少8碼含英數' make admin u=admin   # 建立第一個超級管理員(首次登入需改密碼)
```

然後開啟 http://localhost:15173 登入。

`.env` 設定:

| 變數 | 預設 | 說明 |
|---|---|---|
| `POSTGRES_USER` / `POSTGRES_PASSWORD` / `POSTGRES_DB` | `erp` | 資料庫帳密 |
| `POSTGRES_PORT` | `15432` | 本機連 DB 的埠號 |
| `API_PORT` | `18080` | 本機連 API 的埠號 |
| `WEB_PORT` | `15173` | 前端埠號 |
| `APP_ENV` | `development` | `production` 時:log 改 JSON、cookie 加 Secure、拒絕開發用 JWT 密鑰 |
| `JWT_SECRET` | 開發用值 | 至少 32 字元;**正式環境必須換掉**(`openssl rand -base64 48`) |
| `ACCESS_TOKEN_TTL` / `REFRESH_TOKEN_TTL` | `15m` / `168h` | token 有效期 |
| `RATE_LIMIT_PER_MINUTE` | `600` | 已登入 API 的限流,**以使用者計算**(同一帳號不論幾個 IP 共用額度) |
| `LOGIN_RATE_LIMIT_PER_MINUTE` | `60` | 登入的限流,**以 IP 計算**(IPv6 以 /64 計);單一帳號的暴力破解另由「5 次失敗鎖定」處理 |
| `REFRESH_RATE_LIMIT_PER_MINUTE` | `300` | 刷新 token 的限流,以 IP 計算;與登入分開,避免整頁載入的刷新把登入額度用完 |
| `TRUSTED_PROXIES` | 空 | 可信任的反向代理 IP/CIDR。**正式環境經 nginx 時務必設定**,否則後端看到的 IP 全是 nginx,登入/刷新的限流會變成所有人共用 |

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
| `sqlc`、`golangci-lint` | 工具(profile `tools`),不隨 `up` 啟動 |

常用指令(`make help` 可列出全部):

```bash
make up                      # 啟動
make down                    # 停止
make logs s=api              # 看 log
make test                    # 前後端測試(後端整合測試會建立暫存資料庫,不影響開發資料)
make lint                    # 只檢查不修改:gofmt / go vet / golangci-lint / sqlc diff / vue-tsc / oxlint / eslint / prettier
make sqlc                    # 修改 backend/queries/*.sql 後重新產生 Go 程式碼
make migrate-new name=xxx    # 新增 migration
make migrate-up              # 套用 migration
make migrate-down            # 回滾一支
make admin u=帳號            # 建立超級管理員(密碼用環境變數 ADMIN_PASSWORD)
make psql                    # 進入資料庫
make reset-db                # 清空資料庫並重跑 migration(會刪資料)
```

維運指令(在 api 容器內):

```bash
docker compose run --rm api go run ./cmd/cli cleanup-tokens -keep-days 30   # 清除過期的 refresh token 紀錄
```

正式環境映像檔(`prod` target):

```bash
docker build --target prod -t erp-api backend    # alpine + 靜態 binary
docker build --target prod -t erp-web frontend   # nginx 提供靜態檔並代理 /api 到 api:8080
```

## API 概要
所有 API 在 `/api/v1` 下,回應格式統一為 `{ "data": ..., "meta": ..., "error": { "code", "message", "details" } }`。
需登入的 API 帶 `Authorization: Bearer <access_token>`。

| 路徑 | 說明 |
|---|---|
| `POST /auth/login`、`/auth/refresh`、`/auth/logout` | 登入、刷新、登出(refresh token 在 HttpOnly cookie) |
| `GET /auth/me`、`POST /auth/change-password` | 目前使用者與權限、變更密碼 |
| `/system/departments`、`/system/users`、`/system/roles` | 部門、使用者、角色(修改需帶 `version`,衝突回 409) |
| `GET /system/permissions` | 權限點清單(定義在 `backend/internal/system/permission`) |
| `GET /system/audit-logs` | 稽核日誌 |
| `/system/doc-number-rules` | 單號規則 |

## 目錄結構
```
erp/
├── README.md
├── docker-compose.yml       # 開發環境
├── .env.example
├── Makefile
├── .github/workflows/ci.yml
├── doc/                     # 規劃與過程文件
├── backend/
│   ├── cmd/
│   │   ├── api/             # API 進入點
│   │   └── cli/             # 維運指令(create-admin、cleanup-tokens)
│   ├── internal/
│   │   ├── app/             # 組裝路由與模組(api 與整合測試共用)
│   │   ├── auth/            # 登入、token、驗證中介層
│   │   ├── system/          # 系統管理 API;permission/ 權限點、audit/ 稽核、docno/ 單號
│   │   ├── db/              # sqlc 產生的程式碼(勿手改)
│   │   ├── platform/        # config、database、httpserver、httpx、ratelimit
│   │   ├── shared/          # apperr、authctx、docstate、money、page、response
│   │   └── testutil/dbtest/ # 整合測試用暫存資料庫
│   ├── queries/             # sqlc 的 SQL
│   ├── migrations/          # golang-migrate SQL
│   ├── sqlc.yaml
│   └── Dockerfile           # dev / build / prod
└── frontend/
    ├── src/
    │   ├── api/             # API client 與型別
    │   ├── stores/          # Pinia(auth)
    │   ├── router/          # 路由與權限守衛
    │   ├── layouts/
    │   ├── views/           # 頁面(system/ 系統管理)
    │   ├── components/
    │   ├── composables/
    │   ├── utils/
    │   └── __tests__/
    ├── Dockerfile           # dev / build / prod
    └── nginx.conf
```

## 文件索引
- [doc/plan.md](doc/plan.md):計畫與設計(範圍、決策、架構、資料模型、里程碑)
- [doc/todo.md](doc/todo.md):待辦清單
- [doc/worklog.md](doc/worklog.md):工作日誌
