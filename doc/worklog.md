# 工作日誌

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
