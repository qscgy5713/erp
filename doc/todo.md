# 待辦清單

> 對應 [plan.md](plan.md) 的里程碑。完成打勾,不刪除。

## 規劃
- [x] 撰寫初版規劃(plan.md)
- [x] 確認決策 D1–D10(見 plan.md 第 2 節)
- [x] 前提假設採 plan.md 1.2 預設(使用者授權決定)

## M0 基礎建設
- [x] 建立 backend(Go module)與 frontend(Vite + Vue 3 + TS + Element Plus)骨架
- [x] docker-compose:postgres / migrate / api(air 熱重載)/ web(Vite)
- [x] Dockerfile 的 prod target(api: alpine binary;web: nginx)
- [x] Makefile、.gitignore、.env.example
- [x] golang-migrate 與第一支 migration(companies + set_updated_at trigger)
- [x] sqlc 設定(`make sqlc`、`make sqlc-check`)
- [x] CI:gofmt、go mod tidy、go vet、golangci-lint、sqlc diff、測試(-race)、migration 回滾、前端 type-check/lint/test/build、prod image
- [x] 統一回應格式 `{data, meta, error}`、健康檢查 `/api/v1/health`
- [x] 共用套件:decimal 金額與稅額、錯誤碼(apperr)、分頁、request id、欄位驗證中文訊息
- [x] 登入、JWT / Refresh Token 輪替與重放偵測、登入失敗鎖定、IP 速率限制、強制改密碼
- [x] 組織:部門(樹狀、防循環);員工即使用者(第一期不另建員工表)
- [x] RBAC 與資料範圍權限、權限中介層、防提權規則
- [x] 稽核日誌(service 層同交易寫入、DB 禁止修改刪除)
- [x] 單號規則產生器(併發不重號、回滾不跳號)+ 設定頁
- [x] 單據狀態機共用元件(`internal/shared/docstate`)
- [x] 前端:版型、側邊選單、API client、首頁系統狀態
- [x] 前端:登入、自動刷新 token、選單權限、路由守衛、系統管理頁面
- [x] 前端:共用單據頁元件(DocStatusTag、ItemPicker、狀態動作按鈕、經手紀錄)— M2 完成
- [ ] 前端:Element Plus 改為按需載入(目前整包 >500KB)
- [x] M0 骨架 code review、補單元與資料庫整合測試、瀏覽器實測
- [x] M0 系統基礎 code review、整合測試、瀏覽器實測
- [x] 限流中介層:已登入以使用者、未登入以 IP 計算(可用環境變數調整)
- [ ] 限流改用共享儲存(api 多實例部署時才需要)
- [ ] 定期清理過期 refresh token(目前有 CLI,M6 加入 worker 後改排程)
- [x] 系統參數(負庫存開關)→ 改為倉庫層級設定(M1),不另建系統參數表

## M1 基本資料
- [x] 料品、分類(樹狀、篩選含下層)、單位與單位換算
- [x] 客戶(統編檢查碼、聯絡人、地址、信用額度權限、付款條件、依負責業務的資料範圍)
- [x] 供應商(含匯款資訊)
- [x] 倉庫(可設定允許負庫存)
- [x] 幣別、匯率表(查詢某日適用匯率)
- [x] 稅別、付款條件(到期日計算)
- [ ] 價格表(客戶價、數量折扣)→ 第二期
- [x] 有庫存異動後禁止修改料品基本單位(與類型)
- [ ] Excel 匯入主檔 → M7

## M2 庫存核心
- [x] 庫存流水帳 + 現有量表(列鎖、一致上鎖順序、反向分錄沖銷、流水帳禁改)
- [x] 庫存調整單
- [x] 倉庫調撥單
- [x] 盤點單(快照凍結 / 實盤 / 差異調整;明細只增不刪)
- [x] 庫存報表:現有量(低於安全庫存)、收發存、料品異動明細
- [ ] 批號 / 效期、儲位 → 第二期
- [ ] 料品異動明細超過 1000 筆時提示(目前直接截斷)
- [ ] 庫存單據列印 → M7

## M3 採購
- [ ] 採購單(含核准)
- [ ] 進貨單(部分進貨、未交量追蹤、過帳)
- [ ] 進貨退出單
- [ ] 應付帳款產生

## M4 銷售
- [ ] 報價單
- [ ] 訂單(信用額度、可用庫存檢查)
- [ ] 出貨單(部分出貨、過帳)
- [ ] 銷貨退回單
- [ ] 應收帳款產生、發票號碼登錄

## M5 應收應付
- [ ] 收款單沖帳
- [ ] 付款單沖帳
- [ ] 對帳單、帳齡分析

## M6 會計
- [ ] 會計科目表(預載台灣常用科目)
- [ ] 手動傳票、過帳
- [ ] 拋轉規則設定 + 業務單據自動拋轉(worker)
- [ ] 期間關帳
- [ ] 日記帳、總分類帳、試算表

## M7 月結與上線
- [ ] 月加權平均成本計算、回寫銷貨成本、可重算
- [ ] 自動對帳檢查(庫存、應收付 vs 科目)
- [ ] 首頁儀表板
- [ ] Excel 匯入:主檔、期初庫存、期初應收付、期初科目餘額
- [ ] 併發與壓力測試
- [ ] 部署與操作文件

## 第二期(待評估)
- [ ] 電子發票串接
- [ ] 多層簽核、依金額分流
- [ ] 批號 / 效期、儲位
- [ ] BOM、工單、領料、完工入庫
- [ ] 資產負債表、損益表、401 申報匯出
- [ ] 2FA、Email / LINE 通知
