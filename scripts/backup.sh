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
out="$DIR/${POSTGRES_DB}_$(date +%Y%m%d_%H%M%S).dump"

# 先寫成 .tmp,確認內容可讀才改名,避免留下半截的壞檔被當成備份
$DC exec -T postgres pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Fc --no-owner > "$out.tmp"
if ! $DC exec -T postgres pg_restore -l < "$out.tmp" > /dev/null; then
  rm -f "$out.tmp"
  echo "備份檔無法讀取,已刪除" >&2
  exit 1
fi
mv "$out.tmp" "$out"
find "$DIR" -maxdepth 1 -name "${POSTGRES_DB}_*.dump" -mtime +"$KEEP_DAYS" -delete
echo "$out ($(du -h "$out" | cut -f1))"
