# 工作日誌

## 2026-10-08 M4 銷售
- 已 push M3 到 `origin/main`(d919682),CI 三個 job 通過。
- 重構:採購與銷售共用的單頭驗證、明細計價、訂單狀態轉換抽到 `internal/trade`(D41),採購改用它;共用查詢移到 `queries/trade.sql`。重構後採購測試照常通過。
- 資料表(000006):sales_orders / sales_order_lines(報價與訂單共用,D37)、deliveries / delivery_lines(出貨與退回共用)、accounts_receivable。
- 後端 `internal/sales`:
  - 報價 / 訂單:CRUD、狀態動作(結案 / 重開)、報價轉訂單(檢查報價已核准、同客戶)、有下游單據時擋取消核准 / 作廢。
  - 訂單核准檢查信用額度(D39),先鎖客戶列。
  - 可用量 API(現有 − 其他已核准訂單未出貨)、未出貨明細、可退回明細。
  - 出貨 / 退回:來源檢查(開單時欄位錯誤、過帳時鎖訂單 / 出貨單再檢查),過帳扣 / 加庫存並產生應收;反過帳檢查退回單與已沖帳。
  - 發票號碼登錄(D40):格式檢查、部分唯一索引防重複。
  - 資料範圍(D38):所有讀寫依負責業務快照過濾,範圍外回 404;客戶選單 / 應收也依範圍。
- `masterdata.CustomerVisible` 改為公開供銷售共用;新增 `/masterdata/customer-options`;權限點 sales.order.*、sales.delivery.*、finance.receivable.read。
- 前端:`PurchaseEditView` 改寫為通用的 `views/trade/TradeEditView.vue` + `flows.ts`,採購、報價 / 訂單、出貨 / 退回共用;轉單(採購單 → 進貨、報價 → 訂單、訂單 → 出貨)、從來源帶入、可用量 / 現有量欄(不足紅字)、發票登錄對話框。`SupplierPicker` 改為 `PartnerPicker`(供應商 / 客戶);應付頁改為應收 / 應付共用的 `LedgerView`;新增報價 / 訂單、出貨 / 退回、未出貨清單頁。
- 測試:5 個銷售整合測試(報價 → 訂單 → 部分出貨 → 應收、可用量與保留量、超出未出貨量兩階段擋、缺貨接單但出貨擋負庫存、退回與可退量、已沖帳擋反過帳、信用額度、發票格式 / 重複 / 作廢後可重用、資料範圍);`internal/trade` 狀態轉換單元測試。
- 瀏覽器實測:新增報價單(可用量欄正確顯示 PEN-01 現有 3 個,數量 500 時紅字)→ 改 2 個 → 核准 → 轉訂單(自動帶入、顯示來源報價)→ 核准 → 轉出貨單 → 過帳 → 登錄發票 xy12345678(自動轉大寫)→ 應收帳款 42;採購進貨單在新編輯頁正常顯示。console 無錯誤。
- 實測與 CR 修正:
  - 轉單同屬「報價 / 訂單」路由時元件會被沿用 → `RouterView` 的 key 改為 path(換單據即重建)。
  - 報價單也會出現「轉出貨單」按鈕的條件錯誤;報價來源連結在停用的表單內無法點擊 → 改用 RouterLink。
  - 銷售明細欄位「未交」改為「未出」;轉單後可用量缺單位;過帳確認文字只提到進貨 / 應付 → 改為通用說明。

## 2026-10-08 M3 採購
- 已 push M2 到 `origin/main`(d4ff424),CI 三個 job 通過。
- 資料表(000005):purchase_orders / purchase_order_lines、goods_receipts / goods_receipt_lines(進貨與退出共用,D31)、accounts_payable。
- 後端 `internal/purchase`:
  - 共用單頭驗證(供應商、倉庫、幣別、匯率、稅別、付款條件)與明細計價 `priceLines`(D33)。
  - 採購單:CRUD、送審 / 核准 / 退回 / 取消核准 / 結案 / 重開 / 作廢(D30);已有未作廢進貨單時擋取消核准與作廢。
  - 進貨 / 退出:`checkRefs` 檢查來源(狀態、供應商、幣別、料品單位、剩餘量),開單時回欄位錯誤,過帳時先依 id 鎖來源單再檢查(D32);過帳呼叫 `inventory.Post`(單位成本 D36)並產生應付;反過帳檢查退出單 / 已沖帳後沖銷。
  - 未交貨明細、可退貨明細 API。
- 後端 `internal/finance`:`CreatePayable / RemovePayable`(D34)與應付查詢(meta 附本位幣合計)。
- `inventory.ErrNothingToReverse` 改為公開:只有服務類明細的進貨單沒有庫存分錄,反過帳時要能略過。
- 新增 `/masterdata/supplier-options`;權限點 purchase.order.*、purchase.receipt.*、finance.payable.read。
- 前端:採購單列表、進貨 / 退出列表、共用編輯頁 `PurchaseEditView`(從採購單 / 進貨單帶入、轉進貨單、外幣匯率帶入、金額預覽)、未交貨清單、應付帳款;`SupplierPicker`;docstate 加入採購單流程(結案 / 重開)。
- 測試:5 個採購整合測試(部分進貨與應付、超交於開單與過帳兩階段被擋、結案 / 重開、反過帳與已沖帳、退出與可退量、外幣捨入、服務類明細、權限分工);前端 docstate 加採購單流程測試(共 38 個)。
- 瀏覽器實測:新增採購單 10 個 @12.5 → 預覽 125 / 稅 6 / 131 → 送審 → 核准 → 轉進貨單(自動帶入未交 10)改 4 → 過帳 → 應付 53、未交貨清單未交 6 → 退出單從進貨單帶入 1 個 → 13 / 稅 1 / 14 → 過帳 → 應付 −14、合計 39。console 無錯誤。
- 實測與 CR 修正:
  - 匯率原本用 watch 監看幣別 / 日期,載入既有草稿時會被重新查詢覆蓋 → 改為只在使用者修改時查詢。
  - `SupplierPicker` 未傳 `disabled` 時 Vue 會把布林 prop 轉成 false,蓋過表單的停用 → 由父層明確傳入。
  - 採購單與進貨單共用編輯元件,「轉進貨單」時元件被重複使用、單據種類錯亂 → `RouterView` 依 `meta.kind` 設 key。
  - 庫存單據編輯頁 `canDo` 補上 close / reopen(DocAction 擴充後 switch 不完整)。
  - golangci-lint:測試中無效賦值。

## 2026-10-07(深夜)M2 庫存核心
- 已 push M1 到 `origin/main`(d135741),CI 通過。
- 資料表(000004):stock_documents / stock_document_lines(三種單據共用,D25)、inventory_balances、inventory_transactions(trigger 只允許回寫 unit_cost,D29)。
- 後端 `internal/inventory`:
  - `ledger.go`:`Post / Reverse`(D26)— 驗證商品類型、倉庫啟用、盤點凍結;依 (料品, 倉庫) 排序鎖定;同 key 合計後判斷負庫存並一次列出所有不足;反向分錄沿用原單據日期。
  - 庫存單據 API:建立 / 修改草稿、送審 / 核准 / 退回 / 取消核准 / 過帳 / 反過帳 / 作廢,各動作權限不同;盤點建立即快照並凍結倉庫(D27)。
  - 報表:現有量、收發存(反向分錄依原方向歸類,沖銷後收發抵銷)、料品異動明細(含累計結存)。
- 新增 `/masterdata/item-options`(開單選料,只需登入);料品有異動後不可改基本單位與類型(D24)。
- 前端:庫存單據列表 / 編輯頁(狀態 × 權限決定按鈕;過帳 / 反過帳 / 作廢需確認;有未存修改時動作前自動存檔)、現有量、收發存、料品異動明細抽屜;共用元件 DocStatusTag、ItemPicker、ItemLedgerDrawer。
- 測試:8 個庫存整合測試(過帳 / 反過帳與流水帳禁改、負庫存、調撥、盤點凍結與差異、權限分工、基本單位鎖定、報表、併發)+ 列鎖測試;前端 docstate 與後端一致性測試。
- **測試有效性驗證**:原本「20 個併發各扣 1」的測試,拿掉 `FOR UPDATE` 仍通過(交易太快,實際上沒有重疊),表示測不到問題 → 改寫為以兩個交易手動控制先後的確定性測試。進一步發現單拿掉 `FOR UPDATE` 仍正確:`INSERT … ON CONFLICT DO NOTHING` 遇到被未提交交易修改的列會等待該交易結束,附帶序列化效果。以突變測試(同時移除兩道防線)確認測試會失敗、還原後通過。
- 瀏覽器實測:新增調整單(5 箱 → 100 件)→ 送審 → 核准 → 過帳(有確認框)→ 現有量 100、異動明細正確;盤點單建立自動帶出帳面數並顯示凍結警告、實盤 95 差異 −5、未存檔直接送審會先自動存檔、過帳後解除凍結;收發存 0 + 100 − 5 = 95。
- 實測與 CR 修正:
  - 新增的空白明細以 0 當料品 / 單位 id,下拉直接顯示「0」→ 改用 null。
  - 盤點單可刪除快照中的料品,等於可藏盤虧 → 盤點明細只能新增(前後端都擋,STK-006)。
  - 空白列送出前被略過,後端回傳的列號會對不上畫面 → 前端記錄送出序號對應回畫面列。
  - 新增盤點單時頁首「儲存」與「建立盤點單」重複 → 只保留後者。

## 2026-10-07(深夜)M1 基本資料
- 已 push M0 + 限流到 `origin/main`(79e0257),GitHub Actions 三個 job 全部通過。
- 資料表(000003):currencies(全系統共用)、exchange_rates、tax_types、payment_terms、units、item_categories、warehouses、items、item_units、customers、suppliers;預載常用單位、稅別、付款條件、幣別。
- 後端 `internal/masterdata`:各資料的 CRUD、樂觀鎖、稽核;`CheckItemRefs` / `CheckPartnerRefs` 一次查詢驗證所有參照存在且同公司;`RateOn`(某日適用匯率)、`DueDate`(付款條件到期日)供後續開單使用;客戶依負責業務套用資料範圍;統一編號檢查碼驗證(`shared/taxid`,含 2023 年新規則與第 7 碼為 7 的特例)。
- 新增 `GET /system/user-options`,讓沒有使用者管理權限的人也能選負責業務。
- 前端:料品、料品分類、單位、倉庫、客戶/供應商(同一元件依 kind 切換)、財務設定(分頁籤)頁面;`useFormDialog` 統一新增/編輯對話框邏輯;金額與匯率一律以字串傳遞,避免浮點誤差。
- 測試:新增 6 個 API 整合測試(料品單位驗證與樂觀鎖、分類樹與篩選、匯率查詢、稅別規則、客戶資料範圍與信用額度權限、下拉清單只需登入)、DueDate、統編單元測試;前端統編驗證與後端用同一組測試資料。
- CR 修正:
  - 業務可自行修改自己客戶的信用額度 → 新增 `masterdata.customer.credit` 權限(D21)。
  - 稽核日誌頁不認得新資料類型 → 補中文名稱。
  - 料品稽核前後的單位結構不同(修改前多了單位名稱),比對會誤報差異 → 稽核時統一去掉名稱。
  - 前端 `toPayload` 原用 `as unknown as` 強轉,且把客戶欄位送到供應商 API → 依類型分別組 payload。
- 問題與解法:
  - sqlc 對 `(x IS NULL OR EXISTS(...))` 推斷為可為 NULL(產生 `*bool`),包 `COALESCE(...)` 又變 `interface{}` → 再加 `::boolean`。
  - sqlc 在子查詢中無法判斷未加表名的欄位 → 子查詢一律用別名限定欄位。
- 瀏覽器實測(擴充功能重新連線後補做):
  - admin:新增帶換算單位的料品(1 箱 = 20 件,換算單位下拉會排除基本單位;未選基本單位時「新增換算單位」停用)、編輯客戶聯絡人、財務設定四個分頁。
  - 業務 sales1(本人範圍、無信用額度權限):只看得到自己負責的客戶並顯示範圍提示、選單只有「客戶」、信用額度欄位鎖定、可修改其他欄位並儲存。
  - viewer1(無基本資料權限):選單不顯示「基本資料」,直接打網址得到 403。
- 實測抓到並修正的 UI 問題:
  - 表格儲存格內的 `el-form-item` 繼承表單 100px 標籤寬度,把單位下拉與換算數量擠到看不見 → `label-width="0"`。
  - 儲存格內的驗證錯誤文字被裁掉,只看得到紅框 → 錯誤訊息改一般排版撐高該列。
  - `required` 只在 blur 觸發,下拉選了值後「必填」仍殘留 → 改為 blur + change。
  - 儲存成功後關閉動畫期間仍顯示舊錯誤 → 儲存成功即清除欄位錯誤。
- 文件修正:上次更新 todo 時字串替換誤改到 M2 的「倉庫調撥單」(變成已勾選)→ 已改回。

## 2026-10-07(深夜)限流中介層
- 使用者要求限流改為「依 IP 或登入者計算、放在中介層」。
- 新增 `internal/platform/ratelimit`:key 為 `user:<id>`(已登入)或 `ip:<來源 IP>`(未登入);受保護路由群組在 `Authenticate` 之後掛限流,因此以使用者計算;登入/刷新以 IP 計算。超過回 429 + `Retry-After`,被拒的請求不消耗額度,閒置 10 分鐘的 key 自動清除。
- 移除 auth 套件內原本只看 IP 的限流;額度改由 `RATE_LIMIT_PER_MINUTE`(預設 600)、`LOGIN_RATE_LIMIT_PER_MINUTE`(預設 20)設定。
- 測試:ratelimit 單元測試(burst 與補充、拒絕不扣額度、閒置清除、同使用者換 IP 共用額度、同 IP 不同使用者互不影響、匿名以 IP 計算)、API 整合測試、config 驗證。
- CR(使用者追問後補做逐行檢查)修正:
  - 前端刷新只要失敗就登出,連 429 也是 → `refresh()` 回傳 `ok/expired/throttled`,429 時保留登入、原請求回 429 訊息。
  - 登入與刷新共用每 IP 20 次/分,共用對外 IP 的辦公室整頁載入就可能用完,進而觸發上一點的誤登出 → 刷新另設額度(300),登入放寬為 60(單一帳號暴力破解已有帳號鎖定)。
  - IPv6 用戶可在自己的 /64 內任意換位址繞過 IP 限流 → IPv6 以 /64 計算。
  - 大量不同 IP 可讓限流表無限成長 → key 上限 10 萬,超過時清除閒置 30 秒以上的 key。
  - 已知未處理:帶無效 token 的請求在驗證階段就被拒,不經過限流(驗證無效 token 不查 DB,成本低)。
- 問題:`make lint` 的 golangci-lint 在編譯 `ugorji/go/codec` 時被 OOM 終止(Docker 只有 2GB,且有其他專案容器)→ 設 `GOFLAGS=-p=1`、`--concurrency=1`,並加 cache volume。

## 2026-10-07(晚)M0 系統基礎完成
- 已 push M0 骨架到 `origin/main`(a33999d),分支由 master 改名為 main。
- 後端:
  - 共用:`apperr`(錯誤碼)、`response.Error`(業務錯誤照回、其他記 log 回 500)、`httpx`(JSON 綁定 + 中文欄位訊息、request id)、`page`、`money`(四捨五入、稅額、含稅拆分)、`authctx`(登入者與資料範圍)、`docstate`(單據狀態機)。
  - 資料表(000002):departments、users、roles、role_permissions、user_roles、refresh_tokens、audit_logs(trigger 禁止改刪)、doc_number_rules(預載 13 種單據)、doc_number_counters。
  - sqlc:查詢放 `backend/queries`,產生到 `internal/db`;以 compose 的 tools profile 執行。
  - 登入:bcrypt(cost 12)、帳號不存在也做一次雜湊比對(防時間差探測)、5 次失敗鎖 15 分、IP 速率限制、refresh token 輪替與重放偵測、`token_version` 讓改密碼/停用立即生效、首次登入強制改密碼。
  - 系統管理 API:部門、使用者、角色、權限清單、稽核日誌、單號規則;樂觀鎖(version)、防提權。
  - CLI:`create-admin`、`cleanup-tokens`。
- 前端:auth store(token 只放記憶體、單一飛行刷新 + Web Locks 跨分頁排隊)、http client 401 自動刷新重試、路由守衛(登入/強制改密碼/權限)、依權限過濾的選單、登入/改密碼/403 頁、系統管理 5 頁、JsonDiff 稽核差異元件。
- 測試:後端 12 個套件(含 11 個 API 整合測試:鎖定與解鎖、停用帳號、強制改密碼、refresh 重放偵測、防提權、部門循環、樂觀鎖、稽核不含密碼、單號併發 30 筆不重號、回滾不跳號);前端 17 個。CI 加入 GitHub Actions。
- Chrome 實測:未登入導向登入頁並保留 redirect、重新整理後以 cookie 維持登入、新增部門/角色/使用者、viewer 帳號首次登入被強制改密碼且無法用網址繞過、選單只顯示有權限的項目、無權限頁面顯示 403、使用者頁為唯讀。
- 問題與解法:
  - sqlc 不支援遞迴 CTE 的部分別名寫法 → 改用欄位清單 `tree (dept_id)` 並限定表名。
  - sqlc 的 `TIMESTAMPTZ` 型別名稱是 `timestamptz` 而非 `pg_catalog.timestamptz`,兩種都要 override 才會產生 `time.Time`。
  - 整合測試一開始連到開發庫:pgx `ConnConfig.ConnString()` 回傳原始字串,改 `Database` 不會反映 → 改用 `net/url` 換資料庫名。
  - `vue-tsc` 輸出帶 ANSI 色碼,`grep "error TS"` 比對不到,導致型別錯誤一度沒被發現 → 一律以指令結束碼判斷,`make lint` 抓出並修正。
  - 瀏覽器實測時誤點遮罩讓對話框關閉、填好的資料遺失 → 表單對話框一律 `close-on-click-modal=false`。
  - CR:`append` 可能寫入共用底層陣列 → 改 `slices.Concat`;文件補上正式環境必須設定 `TRUSTED_PROXIES`(否則登入速率限制變全公司共用)。

## 2026-10-07(傍晚)M0 骨架 code review 與補測試
- 逐檔 CR,修正:
  - Gin 預設信任所有 proxy → `X-Forwarded-For` 可偽造來源 IP(之後登入鎖定、稽核會用到)。新增 `TRUSTED_PROXIES` 設定,預設不信任任何 proxy。
  - `make lint` 原本呼叫 `npm run lint`(帶 `--fix`)會改檔案;改為只檢查(oxlint、eslint、prettier --check)。改完立刻抓到一個測試 mock 缺型別參數,已修。
  - web 的 `node_modules` 是匿名 volume,`up --build` 不會更新 → `make up` 加 `-V`。
  - `make test` / `make lint` 原用 `exec`,服務沒啟動就失敗 → 改 `run --rm`;`test-api` 會自動帶起 postgres 並套用 migration。
  - 404 頁頂部標題空白、`<a>` 包 `<button>` 巢狀互動元素 → 標題改用 route meta(同時更新分頁標題),按鈕改 `router.push`。
- 補測試:config(預設值、必填、TRUSTED_PROXIES 解析)、路由(ClientIP 是否可偽造、無效 proxy 設定)、資料庫整合測試(連線時區 UTC、預設公司、`updated_at` trigger、統編 CHECK;在交易內執行並回滾)、前端 HomeView(正常/異常顯示)。後端 4 個測試檔、前端 5 個測試,全部在容器內通過。
- 用 Claude in Chrome 實測畫面:首頁顯示正常、主控台無錯誤;停掉 postgres 後顯示「異常:資料庫無回應」,重啟後自動恢復;404 頁與「回首頁」正常。

## 2026-10-07(下午)M0 骨架
- 決策定案:使用者指定 Go + Vue + PostgreSQL、全部用 docker compose 啟動;其餘 D2–D7 授權由我決定,採原建議。
- 改用 PostgreSQL 後的連帶決定:非同步工作改用 River(PG 佇列,可與業務資料同一交易),不用 RabbitMQ;資料存取用 pgx + sqlc;第一期不用 Redis。詳見 plan.md D8–D10。
- 後端:Gin + pgxpool,`/api/v1/health`(會 ping DB)、統一回應格式、優雅關機;單元測試 2 支。
- 前端:create-vue(TS、Router、Pinia、Vitest、ESLint、Prettier)+ Element Plus 繁中;版面、選單、首頁顯示系統狀態;`api/http.ts` 對應後端回應格式,測試 3 支。
- docker compose:postgres 18、migrate(一次性,api 等它成功才啟動)、api(air 熱重載)、web(Vite,proxy `/api` 到 api)。對外埠號用 15432 / 18080 / 15173,避開本機其他專案已占用的 5432 / 8080。
- 驗證:`make up` 全部起來;API 與經 Vite proxy 的健康檢查都回 200;migration up/down/new 正常;熱重載生效;`make test`、`make lint` 通過;兩個 prod image 可建置。
- 問題:本機 npm 10.9.3 安裝時出現 `Cannot read properties of null (reading 'edgesOut')`(arborist peer 依賴解析的 bug)。解法:用 `npx npm@11 install` 產生 lockfile;容器改用 node:24-alpine(內建 npm 11)。
- 問題:`vitest.config.ts` 的 `mergeConfig` 不接受函式形式的 vite 設定。解法:vite 設定維持物件形式,proxy 目標直接讀 `process.env.API_PROXY_TARGET`。

## 2026-10-07
- 建立專案文件骨架:`README.md`、`doc/plan.md`、`doc/todo.md`、`doc/worklog.md`。
- 完成初版 ERP 規劃:範圍、模組分期(P1/P2/P3)、核心單據流程、狀態機、資料模型原則、API 規範、目錄結構、測試策略、里程碑。
- 技術棧暫定 Go(Gin + GORM)+ Vue 3 + MySQL 8 + Redis + RabbitMQ,理由:與 `~/www/E-commerce` 一致,並可沿用 `~/www/docker-compose.yml` 的共用 MySQL / RabbitMQ(`jtg-shared` 網路)。尚待確認(plan.md D1)。
- 尚未開始寫程式;等 D1–D7 決策確認後進入 M0。
