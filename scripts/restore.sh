#!/usr/bin/env bash
# 還原備份。
#   scripts/restore.sh backups/erp_20261008_020000.dump --verify   驗證:還原到暫存資料庫並核對筆數,不動正式資料(建議每月做一次)
#   scripts/restore.sh backups/erp_20261008_020000.dump --yes      還原到正式資料庫:會停掉 api / web、整個覆蓋資料庫,再重新啟動
set -euo pipefail
cd "$(dirname "$0")/.."

FILE=${1:-}; MODE=${2:-}
[ -n "$FILE" ] && [ -f "$FILE" ] || { echo "用法: $0 <備份檔> --verify|--yes" >&2; exit 1; }
[ "$MODE" = "--verify" ] || [ "$MODE" = "--yes" ] || { echo "請指定 --verify(驗證)或 --yes(還原到正式資料庫)" >&2; exit 1; }

ENV_FILE=${ENV_FILE:-.env.prod}
DC=${DC:-"docker compose -f docker-compose.prod.yml --env-file $ENV_FILE"}
[ -f "$ENV_FILE" ] || { echo "找不到 $ENV_FILE" >&2; exit 1; }
set -a; . "$ENV_FILE"; set +a
psql_admin() { $DC exec -T postgres psql -U "$POSTGRES_USER" -d postgres -v ON_ERROR_STOP=1 "$@"; }

if [ "$MODE" = "--verify" ]; then
  tmp="erp_verify_$$"
  trap 'psql_admin -c "DROP DATABASE IF EXISTS $tmp WITH (FORCE)" >/dev/null 2>&1 || true' EXIT
  psql_admin -c "CREATE DATABASE $tmp" >/dev/null
  $DC exec -T postgres pg_restore -U "$POSTGRES_USER" -d "$tmp" --no-owner < "$FILE"
  echo "--- 暫存資料庫 $tmp 還原完成,核對筆數 ---"
  $DC exec -T postgres psql -U "$POSTGRES_USER" -d "$tmp" -At -F '  ' -c "
    SELECT 'migration 版本', version FROM schema_migrations
    UNION ALL SELECT '使用者', count(*) FROM users
    UNION ALL SELECT '料品', count(*) FROM items
    UNION ALL SELECT '客戶', count(*) FROM customers
    UNION ALL SELECT '出貨單', count(*) FROM deliveries
    UNION ALL SELECT '庫存流水', count(*) FROM inventory_transactions
    UNION ALL SELECT '批號庫存列', count(*) FROM inventory_lot_balances
    UNION ALL SELECT '儲位庫存列', count(*) FROM inventory_bin_balances
    UNION ALL SELECT '工單', count(*) FROM work_orders
    UNION ALL SELECT '已啟用 2FA 的使用者', count(*) FROM users WHERE totp_enabled
    UNION ALL SELECT '已過帳傳票', count(*) FROM vouchers WHERE status = 'posted'
    UNION ALL SELECT '傳票借貸不平衡', count(*) FROM (SELECT v.id FROM vouchers v JOIN voucher_lines l ON l.voucher_id = v.id
        WHERE v.status = 'posted' GROUP BY v.id HAVING SUM(l.debit) <> SUM(l.credit)) x;"
  echo "--- 驗證完成,暫存資料庫已刪除。「傳票借貸不平衡」應為 0 ---"
  exit 0
fi

echo "即將用 $FILE 覆蓋資料庫 $POSTGRES_DB(目前的資料會消失)。建議先執行 scripts/backup.sh 備份現況。"
read -r -p "輸入 RESTORE 確認: " ans
[ "$ans" = "RESTORE" ] || { echo "已取消"; exit 1; }
$DC stop web api
psql_admin -c "DROP DATABASE IF EXISTS $POSTGRES_DB WITH (FORCE)" -c "CREATE DATABASE $POSTGRES_DB"
$DC exec -T postgres pg_restore -U "$POSTGRES_USER" -d "$POSTGRES_DB" --no-owner < "$FILE"
$DC up -d
echo "還原完成,服務已重新啟動(migration 會自動補上較新的版本)"
