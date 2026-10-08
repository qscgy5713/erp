# ERP

給台灣中小企業(買賣業為主、預留輕製造)使用的 ERP 系統,串起採購、庫存、銷售、應收應付與會計總帳。

> 目前狀態:**M3 採購完成**。下一步 M4 銷售。規劃見 [doc/plan.md](doc/plan.md)。

## 功能

### 已完成(M0)
- **登入**:JWT access token(15 分鐘)+ HttpOnly refresh cookie(7 天、每次刷新輪替、偵測重放)、連續 5 次密碼錯誤鎖定 15 分鐘、首次登入強制改密碼
- **限流中介層**:已登入以使用者計算、未登入以 IP 計算,超過回 429 並帶 `Retry-After`
- **權限**:RBAC(角色 → 權限點)+ 資料範圍(全部/本部門/本人);非超級管理員不能授予自己沒有的權限
- **稽核日誌**:所有寫入動作記錄前後差異、IP、request id;資料庫層禁止修改刪除
- **單號規則**:前綴 + 日期 + 流水號,可設定每日/月/年重置,併發不重號、回滾不跳號
- **系統管理頁面**:部門(樹狀)、使用者、角色權限、單號規則、稽核日誌
- **共用元件**:單據狀態機(草稿→待審→已核准→已過帳→已結案/作廢)、金額捨入與稅額計算

### 已完成(M1 基本資料)
- **料品**:料號、品名規格、分類(樹狀,篩選含下層)、商品/服務、基本單位與**單位換算**(1 箱 = 12 個)、條碼、稅別、預設倉、安全庫存、建議售價
- **倉庫**:可依倉庫設定是否允許負庫存
- **客戶 / 供應商**:統一編號(含檢查碼驗證)、多聯絡人、多地址、幣別、稅別、付款條件;客戶有信用額度(需另外權限)與負責業務,**客戶依負責業務套用資料範圍**
- **財務設定**:幣別、匯率(依日期,取當天或之前最近一筆)、稅別(應稅/零稅率/免稅)、付款條件(月結/天數,自動算到期日)
- 預載常用單位、稅別、付款條件、幣別

### 已完成(M2 庫存核心)
- **庫存流水帳 + 現有量**:所有進出寫流水帳(只增不改,反過帳以反向分錄沖銷),現有量依料品 × 倉庫維護;過帳時鎖定現有量,併發不超賣
- **庫存單據**:調整單、調撥單、盤點單,共用「草稿 → 待審 → 已核准 → 已過帳」流程;明細可用任一換算單位輸入
- **盤點**:建立時快照帳面數並**凍結該倉庫**(過帳或作廢前其他單據不可異動),過帳時依差異調整;盤點明細只能新增不能刪除
- **負庫存控管**:倉庫未允許負庫存時,出庫後低於 0 即拒絕並列出不足的料品
- **報表**:現有量(可篩低於安全庫存)、收發存(期初 + 收 − 發 = 期末)、料品異動明細
- 權限分為檢視 / 開單送審 / 核准 / 過帳,可由不同人負責
- 料品有庫存異動後不可修改基本單位或類型

### 已完成(M3 採購)
- **採購單**:草稿 → 待審 → 已核准;可結案(剩餘不再進貨)與重開;列表顯示交貨狀態(未交 / 部分交貨 / 已交齊)
- **進貨單**:可「從採購單帶入」或在採購單按「轉進貨單」,支援部分進貨;不可超過未交量(開單與過帳時都檢查,併發也不會超交);也可不經採購單直接進貨
- **進貨退出單**:從已過帳進貨單帶入,不可超過可退量
- **多幣別**:依單據日期帶入匯率(可手動修改),原幣與本位幣金額分開保存;稅額依單頭合計計算
- **過帳**:進貨增加庫存(含入庫成本)、退出減少庫存;同時產生應付帳款(退出為負數);反過帳一併沖銷
- **應付帳款**查詢(未沖餘額、本位幣合計)、**未交貨清單**(可只看逾期)
- 服務 / 費用類料品可採購,不入庫但計入應付

### 規劃中(第一期)
銷售 → 應收應付 → 會計 → 月結,詳見 [doc/todo.md](doc/todo.md)。

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
| `GET /system/user-options` | 使用者下拉選單(只需登入) |
| `/masterdata/items`、`/masterdata/customers`、`/masterdata/suppliers` | 料品、客戶、供應商(分頁、篩選) |
| `/masterdata/units`、`/item-categories`、`/warehouses`、`/tax-types`、`/payment-terms`、`/currencies` | 下拉選單會用到的清單,讀取只需登入;修改需對應權限 |
| `/masterdata/exchange-rates`、`GET /masterdata/exchange-rates/lookup?currency=USD&date=…` | 匯率維護與查詢某日適用匯率 |
| `GET /masterdata/item-options?keyword=` | 開單選料(只需登入) |
| `/inventory/documents`、`POST /inventory/documents/{id}/actions/{submit\|approve\|reject\|unapprove\|post\|unpost\|void}` | 庫存單據與狀態動作(需帶 `version`) |
| `GET /inventory/balances`、`/inventory/movement-summary`、`/inventory/items/{id}/ledger` | 現有量、收發存、料品異動明細 |
| `/purchase/orders`、`POST /purchase/orders/{id}/actions/{submit\|approve\|reject\|unapprove\|close\|reopen\|void}` | 採購單與狀態動作 |
| `/purchase/receipts`、`POST /purchase/receipts/{id}/actions/{…\|post\|unpost}` | 進貨單 / 進貨退出單(`doc_type` receipt / return) |
| `GET /purchase/outstanding-lines`、`GET /purchase/returnable-lines` | 未交貨明細、可退貨明細 |
| `GET /masterdata/supplier-options?keyword=` | 開單選供應商(只需登入) |
| `GET /finance/payables` | 應付帳款(`meta.base_amount_sum` 為本位幣合計) |

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
│   │   ├── masterdata/      # 基本資料 API(料品、客戶、供應商、倉庫、財務設定)
│   │   ├── inventory/       # 庫存核心:ledger.go(Post/Reverse,供各模組過帳)、庫存單據、報表
│   │   ├── purchase/        # 採購單、進貨 / 退出單(過帳呼叫 inventory.Post 與 finance.CreatePayable)
│   │   ├── finance/         # 應收應付(目前:應付帳款產生與查詢)
│   │   ├── db/              # sqlc 產生的程式碼(勿手改)
│   │   ├── platform/        # config、database、httpserver、httpx、ratelimit
│   │   ├── shared/          # apperr、authctx、docstate、money、page、response、taxid
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
    │   ├── views/           # 頁面(system/、masterdata/、inventory/、purchase/、finance/)
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
