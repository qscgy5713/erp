#!/usr/bin/env bash
# 備份正式環境資料庫(pg_dump 自訂格式,可用 pg_restore 還原)。建議每天由 cron 執行,見 doc/deploy.md。
#   scripts/backup.sh                 備份到 backups/,保留 30 天
#   KEEP_DAYS=90 BACKUP_DIR=/mnt/nas/erp scripts/backup.sh
# 備份只放在同一台機器上不算備份:請再把 BACKUP_DIR 同步到另一台機器 / 雲端。
set -euo pipefail
cd "$(dirname "$0")/.."

ENV_FILE=${ENV_FILE:-.env.prod}
DC=${DC:-"docker compose -f docker-compose.prod.yml --env-file $ENV_FILE"}
DIR=${BACKUP_DIR:-backups}
KEEP_DAYS=${KEEP_DAYS:-30}

[ -f "$ENV_FILE" ] || { echo "找不到 $ENV_FILE" >&2; exit 1; }
set -a; . "$ENV_FILE"; set +a
mkdir -p "$DIR"

# 要備份的資料庫:單租戶只有 POSTGRES_DB;多租戶(設定了 CONTROL_DATABASE_URL)為控制庫 + 控制庫登記的每個租戶資料庫
DBS=("$POSTGRES_DB")
if [ -n "${CONTROL_DATABASE_URL:-}" ]; then
  CONTROL_DB=${CONTROL_DATABASE_URL##*/}; CONTROL_DB=${CONTROL_DB%%\?*}
  DBS=("$CONTROL_DB")
  while IFS= read -r name; do [ -n "$name" ] && DBS+=("$name"); done < <($DC exec -T postgres psql -U "$POSTGRES_USER" -d "$CONTROL_DB" -Atc "SELECT db_name FROM tenants ORDER BY id")
fi

for db in "${DBS[@]}"; do
  out="$DIR/${db}_$(date +%Y%m%d_%H%M%S).dump"
  # 先寫成 .tmp,確認內容可讀才改名,避免留下半截的壞檔被當成備份
  $DC exec -T postgres pg_dump -U "$POSTGRES_USER" -d "$db" -Fc --no-owner > "$out.tmp"
  if ! $DC exec -T postgres pg_restore -l < "$out.tmp" > /dev/null; then
    rm -f "$out.tmp"
    echo "備份檔無法讀取,已刪除:$db" >&2
    exit 1
  fi
  mv "$out.tmp" "$out"
  find "$DIR" -maxdepth 1 -name "${db}_*.dump" -mtime +"$KEEP_DAYS" -delete
  echo "$out ($(du -h "$out" | cut -f1))"
done
