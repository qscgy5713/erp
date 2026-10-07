# 工作日誌

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
