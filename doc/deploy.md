# 部署與維運手冊

> 最後更新:2026-10-08
> 適用:正式環境(單機 Docker Compose)。開發環境請看 README。
> 本文的指令與流程(建置、啟動、建立管理員、登入、上傳大小限制、備份、驗證還原、真正還原)已在實際的正式堆疊上跑過;
> **HTTPS 代理(第 3 節)與 cron 排程(第 6 節)只提供範例,沒有實測**,請依你的環境確認。

## 1. 架構與需求

```
瀏覽器 ──HTTPS──▶ [TLS 代理(你提供)] ──HTTP──▶ web(nginx:靜態檔 + 反向代理 /api)──▶ api(Go)──▶ postgres
```

| 項目 | 建議 |
|---|---|
| 主機 | 2 CPU、4 GB 記憶體、50 GB SSD 起(資料庫約 1 GB / 年,另加備份空間) |
| 軟體 | Docker Engine 24+ 與 Docker Compose v2;不需要在主機安裝 Go / Node / PostgreSQL |
| 網路 | 一個網域名稱與 HTTPS 憑證(登入 cookie 帶 `Secure`,**沒有 HTTPS 就無法登入**;只有 `localhost` 例外) |
| 容量參考 | 開發機(4 CPU 共用)實測約 150–180 請求/秒,50 位同時使用者約需 10 請求/秒,見 [perf.md](perf.md) |

安全預設:資料庫不對外開埠、api 容器唯讀檔案系統並移除所有 Linux capability、web 預設只聽 `127.0.0.1`、日誌輪替(10 MB × 5)。

## 2. 首次部署

```bash
# 1. 取得程式碼
git clone git@github.com:qscgy5713/erp.git && cd erp

# 2. 設定:複製範本,全部改成自己的值
cp .env.prod.example .env.prod
openssl rand -base64 24    # → POSTGRES_PASSWORD
openssl rand -base64 48    # → JWT_SECRET
chmod 600 .env.prod
$EDITOR .env.prod

# 3. 建置並啟動(第一次約 3–5 分鐘;會自動套用資料庫 migration)
docker compose -f docker-compose.prod.yml --env-file .env.prod up -d --build
docker compose -f docker-compose.prod.yml --env-file .env.prod ps     # 全部 healthy / Up

# 4. 建立第一個超級管理員(密碼由環境變數提供,不留在 shell 歷史)
read -rs ADMIN_PASSWORD && export ADMIN_PASSWORD
docker compose -f docker-compose.prod.yml --env-file .env.prod run --rm -e ADMIN_PASSWORD cli create-admin -username admin
unset ADMIN_PASSWORD
```

5. 以 `https://你的網域` 登入,系統會要求**立即變更密碼**。
6. 依「上線檢查清單」(第 8 節)完成基本設定與期初資料匯入。

> 為了讓指令短一點,可以在 shell 設定 `alias erp='docker compose -f docker-compose.prod.yml --env-file .env.prod'`,
> 之後 `erp ps`、`erp logs -f api`、`erp run --rm cli ...`。本文以下沿用完整寫法。

## 3. HTTPS(必要)

web 容器只提供 HTTP(預設 `127.0.0.1:8080`),TLS 由前面再放一層。以 Caddy 為例(自動申請與更新憑證):

```
# /etc/caddy/Caddyfile
erp.example.com {
    reverse_proxy 127.0.0.1:8080
}
```

兩個必須對上的地方:

1. **`TRUSTED_PROXIES`**:填入 compose 內部網段 `172.28.0.0/24`(nginx),**以及你的 TLS 代理的 IP**(Caddy 在同一台主機時是 `172.17.0.1` 或 `127.0.0.1`,依你的網路而定)。
   沒設對,後端看到的來源 IP 全是代理,登入與刷新的限流(以 IP 計算)會變成**全公司共用同一個額度**。
2. **`X-Forwarded-Proto`**:nginx 會把前面代理傳來的值轉給後端;代理請務必送出(Caddy 預設會)。

驗證方式:登入後在稽核日誌看登入紀錄的來源 IP,應是使用者的真實 IP,不是代理的 IP。

## 4. 設定參考(`.env.prod`)

| 變數 | 說明 | 預設 / 建議 |
|---|---|---|
| `POSTGRES_USER` / `POSTGRES_PASSWORD` / `POSTGRES_DB` | 資料庫帳密與名稱;**首次啟動後再改密碼不會生效**(資料已建立),要改請用 `ALTER USER` | 密碼用長隨機值 |
| `JWT_SECRET` | 簽章密鑰,至少 32 字元,含 `dev-only` 會拒絕啟動。換掉只會讓所有人重新登入,不損毀資料 | `openssl rand -base64 48` |
| `TRUSTED_PROXIES` | 信任的反向代理網段,見第 3 節 | `172.28.0.0/24` + TLS 代理 |
| `WEB_BIND` / `WEB_PORT` | web 對外位址與埠 | `127.0.0.1` / `8080`;改 `0.0.0.0` 只限內網測試 |
| `ACCESS_TOKEN_TTL` / `REFRESH_TOKEN_TTL` | 登入憑證效期 | `15m` / `168h` |
| `RATE_LIMIT_PER_MINUTE` | 已登入使用者每分鐘請求上限 | `600` |
| `LOGIN_RATE_LIMIT_PER_MINUTE` / `REFRESH_RATE_LIMIT_PER_MINUTE` | 登入、刷新每個 IP 每分鐘上限(IPv6 以 /64 計)。公司共用同一個對外 IP 且人數多時調高 | 範本 `120` / `600` |
| `ADMIN_PASSWORD` | 只在 `create-admin` 時需要,用完清空 | 空 |

## 5. 日常維運

```bash
erp ps                         # 狀態
erp logs -f --tail=100 api     # 日誌(web、postgres 同理)
erp restart api                # 重啟單一服務
erp down                       # 停止全部(資料在 pgdata volume,不會消失;加 -v 才會刪資料,切勿)
```

| 項目 | 說明 |
|---|---|
| 健康檢查 | `curl https://你的網域/api/v1/health` 回 `{"data":{"status":"ok"}}`;api 容器有內建 healthcheck,web 等它 healthy 才啟動 |
| 重設使用者密碼 | 系統管理 → 使用者 → 重設密碼(對方下次登入須改密碼)。若所有管理員都無法登入,用 `cli create-admin` 另建一個,再用它處理 |
| 清理登入紀錄 | refresh token 紀錄會累積,每週執行 `erp run --rm cli cleanup-tokens`(預設保留 30 天) |
| 查稽核 | 系統管理 → 稽核日誌(新增 / 修改 / 動作皆有紀錄,且資料庫層禁止修改刪除) |
| 磁碟 | `docker system df`;日誌已輪替,備份檔由 `KEEP_DAYS` 控制 |

## 6. 備份與還原

**備份**(`scripts/backup.sh`):`pg_dump` 自訂格式,先寫暫存檔、確認可讀才改名,依 `KEEP_DAYS`(預設 30)清理舊檔。

```bash
scripts/backup.sh                              # → backups/erp_20261008_020000.dump
KEEP_DAYS=90 BACKUP_DIR=/mnt/nas/erp scripts/backup.sh
```

排程範例(未實測,請確認路徑與使用者權限)——每天 02:00 備份、每週日 03:00 清理 token:

```
0 2 * * *  cd /opt/erp && scripts/backup.sh >> /var/log/erp-backup.log 2>&1
0 3 * * 0  cd /opt/erp && docker compose -f docker-compose.prod.yml --env-file .env.prod run --rm cli cleanup-tokens >> /var/log/erp-cleanup.log 2>&1
```

> **備份只放在同一台機器上不算備份。** 請再把 `BACKUP_DIR` 同步到另一台機器或雲端儲存(`rsync`、`rclone`…),並保護好備份檔(內含全部業務資料)。
> 備份只含資料庫;`.env.prod` 要另外保管(特別是 `POSTGRES_PASSWORD`),但**不要**放進同一個備份位置。

**驗證還原(建議每月一次,不動正式資料)**:

```bash
scripts/restore.sh backups/erp_20261008_020000.dump --verify
```

在暫存資料庫還原並列出 migration 版本、各表筆數、「傳票借貸不平衡」(應為 0),完成後自動刪除暫存資料庫。
**沒演練過的備份不能算備份。**

**真正還原(災難復原)**:

```bash
scripts/backup.sh                                              # 先備份現況(即使它是壞的,也留個底)
scripts/restore.sh backups/erp_20261008_020000.dump --yes      # 輸入 RESTORE 確認;會停 api/web、覆蓋資料庫、重啟
```

還原後 migration 會自動補上較新的版本。從備份時間點之後輸入的資料會遺失,需補登。

## 7. 升級

```bash
scripts/backup.sh                          # 1. 先備份(升級前一定要)
git pull                                   # 2. 取得新版
erp up -d --build                          # 3. 重新建置;migration 會自動套用,api 在 migration 成功後才啟動
erp ps && curl https://你的網域/api/v1/health   # 4. 確認
```

- **升級期間服務會短暫中斷**(api 與 web 重啟,通常不到 1 分鐘),請避開上班時間。
- **回滾**:程式碼回到舊版後,若新版 migration 已改過資料表,舊程式可能無法運作。最安全的回滾是:`git checkout` 舊版 → `scripts/restore.sh <升級前備份> --yes`(會遺失升級後輸入的資料)。
  只有新版的 migration 純粹新增索引、且你確定時,才考慮 `erp run --rm migrate down 1`。
- 每個版本的 migration 在 `backend/migrations/`;`doc/worklog.md` 記錄各里程碑改了什麼。

## 8. 上線檢查清單

**部署**
- [ ] `.env.prod` 全部改成自己的值(`POSTGRES_PASSWORD`、`JWT_SECRET`),權限 600,**沒有**提交到 git
- [ ] HTTPS 可用;`TRUSTED_PROXIES` 含代理;登入後稽核日誌的來源 IP 是真實 IP
- [ ] 第一個管理員已建立並**改過密碼**,`ADMIN_PASSWORD` 已清空
- [ ] `erp ps` 全部 healthy;`/api/v1/health` 正常
- [ ] 備份已排程、已同步到另一台機器、**已做過一次 `--verify`**

**基本設定**(系統管理 / 基本資料 / 會計,細節見 [manual.md](manual.md))
- [ ] 部門、角色與權限、使用者(依 manual 的角色建議;業務的資料範圍設為「僅本人」)
- [ ] 財務設定:幣別啟用、稅別、付款條件;外幣客戶需有匯率
- [ ] 單號規則(前綴、日期格式、流水號位數)確認後再開始開單——單號開始使用後不要改
- [ ] 會計科目依公司需要調整;**拋轉規則**確認每個用途對到正確的科目
- [ ] 倉庫(是否允許負庫存)

**期初資料**(依序,系統管理 → 資料匯入;**先預檢,通過才匯入**)
- [ ] 1. 料品、客戶、供應商
- [ ] 2. 期初庫存(日期 = 上線前一天或上期期末)
- [ ] 3. 期初應收、期初應付(逐筆未沖帳款)
- [ ] 4. 期初科目餘額(借貸須相等)
- [ ] 5. **對帳檢查**:存貨、應收、應付與總帳全部「正常」。有差異就先釐清再開始作業(可撤銷該批重來)

**演練**
- [ ] 用測試資料走一遍:採購單 → 進貨 → 付款;報價 → 訂單 → 出貨 → 收款;確認傳票、應收應付、庫存都正確
- [ ] 至少一位使用者以非管理員的角色登入,確認看不到不該看的資料

## 9. 疑難排解

| 現象 | 原因與處理 |
|---|---|
| 登入後馬上又被登出 / 登入成功但畫面回到登入頁 | 沒走 HTTPS(cookie 帶 `Secure`)。確認用 `https://` 存取,且代理有送 `X-Forwarded-Proto` |
| 很多人同時登入時出現「請求過於頻繁」(429) | 公司共用一個對外 IP,登入額度由全公司共用,調高 `LOGIN_RATE_LIMIT_PER_MINUTE`;或 `TRUSTED_PROXIES` 沒設對,後端把全部使用者看成同一個 IP |
| 匯入 Excel 出現 413 | 檔案超過 5 MB(nginx 上限 6 MB、後端 5 MB),請分批 |
| `create-admin` 回報密碼不符規定 | 密碼要至少 8 字元且同時含英文字母與數字;並用 `-e ADMIN_PASSWORD`(見第 2 節)傳入,或在 `.env.prod` 暫填後用完清空 |
| api 一直重啟 | `erp logs api`:常見是 `JWT_SECRET` 太短或含 `dev-only`、`DATABASE_URL` 無法連線、migration 失敗(`erp logs migrate`) |
| 過帳 / 反過帳說「會計期間已關帳」 | 該月已關帳(會計 → 會計期間),需要有關帳權限的人先重開;說「已月結成本」則是庫存異動被鎖,需先取消最新的月結 |
| 對帳檢查出現差異 | 見 [manual.md](manual.md) 的「對帳檢查」;匯入期初前已過帳、沒有傳票的單據是常見原因 |
| 磁碟滿了 | `docker system df`;清舊備份、`docker image prune`;資料庫本身很少是主因 |

## 10. 已知限制

- 單機部署:沒有高可用(資料庫與 api 各一個)。資料安全靠備份;RTO 取決於還原時間(資料量小時為分鐘級)。
- 沒有內建的監控與告警;請用外部服務監看 `/api/v1/health` 與備份是否成功。
- 限流為單一程序的記憶體計數,**只能跑一個 api 實例**;要水平擴充需先把限流改成共用儲存(見 todo)。
- 負載產生器與服務同機測得的吞吐量是下限;正式環境請以獨立機器重測(`make perf-*` 僅供在開發 / 預備環境使用)。
