# ERP

給台灣中小企業(買賣業為主、預留輕製造)使用的 ERP 系統,串起採購、庫存、銷售、應收應付與會計總帳。

> 目前狀態:**第一期(M0–M7)全部完成;第二期進行中——M8 財報與年結、M9 營業稅 401、M10 多層簽核、M11 媒體申報檔第一版、M12 批號與效期、M13 BOM 與工單、M14 儲位、M15 雙因素驗證已完成**。規劃見 [doc/plan.md](doc/plan.md)。

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

### 已完成(M4 銷售)
- **報價單 → 訂單 → 出貨單 → 銷貨退回單**:報價核准後可「轉訂單」,訂單核准後可「轉出貨單」或在出貨單「從訂單帶入」;支援部分出貨,不可超出未出貨量 / 可退量
- **可用量**:開單時顯示「現有量 − 其他訂單保留量」,不足以紅字提示(允許缺貨接單,出貨過帳時才擋負庫存)
- **信用額度**:訂單核准時檢查未沖應收 + 未出貨訂單 + 本單是否超過額度
- **過帳**:出貨扣庫存、退回加回庫存,同時產生應收帳款(退回為負數)
- **發票號碼登錄**:出貨後可隨時登錄 / 修改,檢查格式與重複
- **資料範圍**:業務只看得到自己(或本部門)客戶的單據與應收
- **應收帳款**查詢、**未出貨清單**(可只看逾期)

### 已完成(M5 應收應付)
- **收款單 / 付款單**:選客戶(供應商)與幣別 → 挑未沖帳款 → 輸入本次沖帳金額;一筆收付款可沖多張、可部分沖;過帳才沖銷,反過帳還原
- 退回 / 退出的負數帳款可與正數互抵(合計不可為負);同一筆帳款被兩張單同時沖,過帳時會擋下超沖
- 被收付款單引用(含草稿、待審)的帳款,來源出貨 / 進貨單不可反過帳
- **對帳單**:單一對象單一幣別的期初、明細、期末,可列印
- **帳齡分析**:本位幣、依到期日分組(未到期、逾期 1–30 / 31–60 / 61–90 / 90 天以上)
- 收款單、應收、對帳單與帳齡依客戶負責業務套用資料範圍
- 應收 / 應付帳款頁的合計改為「未沖餘額」

### 已完成(M6 會計總帳)
- **會計科目**:預載台灣常用科目(資產 / 負債 / 權益 / 收入 / 成本 / 費用),可自訂、可設彙總科目與上層;已有分錄的科目不可改類別
- **自動拋轉傳票**:進貨、進貨退出、出貨、銷貨退回、收款、付款過帳時,**同一交易內**產生已過帳傳票(進貨:借存貨 / 進項稅額、貸應付帳款;出貨:借應收帳款、貸銷貨收入 / 銷項稅額;收付款依現金 / 銀行);反過帳自動產生沖銷傳票
- **拋轉規則**:各分錄使用的科目可在畫面調整
- **手動傳票**:草稿 → 過帳(借貸須平衡)→ 沖銷;已過帳的傳票由資料庫禁止修改,更正以沖銷傳票;支援客戶、供應商輔助核算
- **期間關帳**:關帳後該期不可過帳 / 反過帳單據或新增傳票,重開後恢復
- **報表**:試算表(期初、本期借貸、期末,附借貸平衡核對)、總分類帳(含累計餘額)、日記帳
- 銷貨成本與庫存調整 / 盤點的傳票於 M7 月結後拋轉;M6 之前已過帳的單據不補傳票

### 已完成(M7 月結成本與對帳檢查)
- **月結成本**:月加權平均成本(以料品為單位,期初 + 本月進貨);依月份順序月結,算出各料品平均成本、期末存貨金額;**自動拋銷貨成本與存貨盤損益傳票**,並把平均成本回寫到流水帳
- 月結後該月的庫存異動鎖定;可取消最新一個月的月結並重算
- **對帳檢查**:存貨 / 應收 / 應付子帳與總帳核對、庫存現有量與流水帳核對、傳票借貸平衡、未月結月份提醒

### 已完成(M7 Excel 匯入)
- **資料匯入**頁(系統管理 → 資料匯入):選類型 → 下載範本 → 上傳填好的 Excel → **預檢**(列出每一列每個欄位的所有問題,不寫入)→ 確認匯入;任何錯誤整批都不會寫入
- 主檔:料品、客戶、供應商(已存在的代號視為錯誤,不覆蓋)
- 期初資料(需指定期初日期):**期初庫存**(數量 + 單位成本)、**期初應收 / 應付帳款**(逐筆未沖帳款,可照常收付款沖帳)、**期初科目餘額**(產生一張已過帳傳票,借貸須相等)
- 期初資料整批可**撤銷**(已被沖帳、已月結成本、期間已關帳者無法撤銷);匯入後以「對帳檢查」確認庫存、應收、應付與總帳一致

### 已完成(M7 首頁儀表板)
- **首頁儀表板**:銷售(今日、本月、近 7 天長條)、待處理單據(只列你有核准權限的)、庫存警示、應收 / 應付(未沖、已逾期、7 日內到期)、月結提醒;**每張卡片依權限出現**,銷售與應收只含你資料範圍內的客戶;各項可點擊直達已篩選的列表

### 已完成(M7 壓力測試)
- **併發正確性**:同時過帳不超賣、不超收、不超沖、不重號、不突破信用額度、不寫進已關帳期間(每項都用拿掉鎖的突變測試驗證過)
- **負載測試**:`make perf-db && make perf-api && make perf-run C=30 D=60s`,在獨立資料庫灌入約一年規模資料後量測;開發機上約 **150–180 請求/秒**,100 併發也零錯誤、平順降級
- 依量測結果優化(連線計畫、訂單列表、部分索引),詳見 [doc/perf.md](doc/perf.md)

### 已完成(M7 部署與操作文件)
- **正式環境部署**:`docker-compose.prod.yml`、`.env.prod.example`、強化的 nginx、`cli` 維運工具;流程見 [doc/deploy.md](doc/deploy.md)
- **備份與還原**:`scripts/backup.sh`、`scripts/restore.sh`(`--verify` 在暫存資料庫還原並核對,不動正式資料)
- **操作手冊**:[doc/manual.md](doc/manual.md)

### 第二期
- **M8 財務報表與年度結帳**:損益表、資產負債表(可與去年同期比較、匯出 Excel)、年度結帳(損益轉保留盈餘,可撤銷)
- **M9 營業稅 401 申報資料**:依雙月期別彙總銷項 / 進項、明細與待處理單據,匯出 Excel(申報參考數字)
- **M10 多層簽核**:依單據類型與金額追加第 2 層以後的指定角色核准(第 1 層維持原本的核准權限);流程在送審時快照,同一人不可核准兩層
- **M11 營業稅媒體申報檔(第一版)**:依官方規格產出進銷項申報檔(銷項 31 / 32、進項 21 / 22 / 25),未支援的單據列入「未納入」清單;新增公司資料(統編、稅籍編號)與進貨單憑證種類 / 發票日期。**尚未用財政部檢核軟體驗證**
- **M12 批號與效期**:料品可管理批號 / 效期;入庫須批號,出庫先到期先出(可指定批號)、過期不可出貨;批號庫存與追溯、儀表板效期警示;盤點、調撥、期初匯入都支援批號

- **M13 BOM 與工單**:單階 BOM(循環檢查)、工單一次完工(領料並成品入庫,可反完工)、開單依 BOM 展開領料;成品成本在月結以「材料平均成本 + 加工費」計算,加工費轉入存貨
- **M14 儲位**:倉庫可啟用儲位;入庫須指定儲位,出庫先出庫存多的儲位(可拆分、可指定);支援同倉儲位間調撥、儲位庫存報表
- **M15 雙因素驗證**:TOTP(驗證器 App)、一次性備援碼、管理員重設、公司可要求全員啟用;登入分兩步,驗證碼錯誤與密碼錯誤共用鎖定

### 之後
電子發票串接、在製品與分批完工、媒體申報檔的退回折讓 / 零稅率等,見 [doc/todo.md](doc/todo.md)。

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
| `/sales/orders`、`POST /sales/orders/{id}/actions/{…\|close\|reopen}` | 報價單 / 訂單(`doc_type` quotation / order) |
| `/sales/deliveries`、`POST /sales/deliveries/{id}/actions/{…\|post\|unpost}`、`PUT /sales/deliveries/{id}/invoice` | 出貨單 / 銷貨退回單(`doc_type` delivery / return)、登錄發票 |
| `GET /sales/availability?warehouse_id=&item_ids=`、`/sales/unshipped-lines`、`/sales/returnable-lines` | 可用量、未出貨明細、可退回明細 |
| `GET /masterdata/customer-options?keyword=` | 開單選客戶(只需登入,依資料範圍) |
| `GET /finance/receivables` | 應收帳款(依資料範圍) |
| `/finance/collections`、`/finance/payments`、`POST …/{id}/actions/{…\|post\|unpost}` | 收款單 / 付款單(沖帳) |
| `GET /finance/statements?side=receivable\|payable&partner_id=&currency=&from=&to=` | 對帳單 |
| `GET /finance/aging?side=receivable\|payable&as_of=` | 帳齡分析 |
| `/gl/accounts`、`GET /gl/account-options`、`/gl/mappings` | 會計科目、開傳票選科目(只需有會計權限)、拋轉規則 |
| `/gl/vouchers`、`POST /gl/vouchers/{id}/actions/{post\|void\|reverse}` | 傳票與動作 |
| `GET /gl/periods`、`POST /gl/periods/{YYYY-MM}/{close\|reopen}` | 會計期間關帳 / 重開 |
| `GET /gl/reports/{trial-balance\|ledger\|journal}` | 試算表、總分類帳、日記帳 |
| `GET /costing/closings`、`GET …/{YYYY-MM}/items`、`POST …/{YYYY-MM}/{run\|cancel}` | 月結成本與各料品計算明細、月結 / 取消月結 |
| `GET /costing/reconcile` | 自動對帳檢查 |
| `GET /dashboard` | 首頁儀表板(依權限回傳卡片) |
| `GET /imports/types`、`GET /imports/types/{type}/template`、`POST /imports/types/{type}?dry_run=&date=`(multipart `file`)、`GET /imports/batches`、`POST /imports/batches/{id}/undo` | Excel 匯入(類型:items、customers、suppliers、opening_stock、opening_ar、opening_ap、opening_balance) |
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
│   │   ├── sales/           # 報價 / 訂單、出貨 / 退回(過帳呼叫 inventory.Post 與 finance.CreateReceivable)
│   │   ├── trade/           # 採購與銷售共用:單頭驗證、計價、訂單狀態轉換
│   │   ├── finance/         # 應收應付:帳款、收付款沖帳、對帳單、帳齡
│   │   ├── gl/              # 會計總帳:科目、拋轉(PostSource / ReverseSource)、傳票、期間關帳、報表
│   │   ├── costing/         # 月結成本(月加權平均)、銷貨成本傳票、對帳檢查
│   │   ├── imports/         # Excel 匯入:主檔與期初資料(預檢、範本、撤銷)
│   │   ├── dashboard/       # 首頁儀表板(依權限與資料範圍)
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
    │   ├── views/           # 頁面(system/、masterdata/、inventory/、purchase/、sales/、trade/ 共用單據編輯頁、finance/、gl/、costing/)
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
- [doc/perf.md](doc/perf.md):效能與壓力測試(併發正確性、負載測試、優化數據)
- [doc/deploy.md](doc/deploy.md):部署與維運(正式環境、HTTPS、備份還原、升級、上線檢查清單)
- [doc/manual.md](doc/manual.md):操作手冊(使用者與管理員)
- [doc/vat-media-spec.md](doc/vat-media-spec.md):營業稅進銷項媒體申報檔規格筆記
