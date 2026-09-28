#!/bin/bash
# Dumps the Postgres database of the compose stack, keeps local copies for
# KEEP_DAYS days and optionally uploads them with rclone.
#
# Cron example (daily at 03:00):
#   0 3 * * * /home/me/repos/app/deploy/backup/db-backup.sh >> /home/me/backups/backup.log 2>&1
#
# Optional settings in deploy/backup/.env (see sample.env).
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
APP_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"
[[ -f "$SCRIPT_DIR/.env" ]] && source "$SCRIPT_DIR/.env"
[[ -f "$APP_DIR/.env" ]] && source <(grep -E '^POSTGRES_(USER|DB)=' "$APP_DIR/.env")

BACKUP_DIR="${BACKUP_DIR:-$HOME/backups/postgres}"
KEEP_DAYS="${KEEP_DAYS:-14}"
POSTGRES_USER="${POSTGRES_USER:-postgres}"
POSTGRES_DB="${POSTGRES_DB:-app}"

notify() {
    echo "$1"
    if [[ -n "${TELEGRAM_BOT_TOKEN:-}" && -n "${TELEGRAM_CHAT_ID:-}" ]]; then
        curl -s -X POST "https://api.telegram.org/bot$TELEGRAM_BOT_TOKEN/sendMessage" \
            -d "chat_id=$TELEGRAM_CHAT_ID" -d "text=$1" >/dev/null || true
    fi
}
trap 'notify "Database backup FAILED on $(hostname) at $(date)"' ERR

mkdir -p "$BACKUP_DIR"
FILE="$BACKUP_DIR/db_$(date +%Y%m%d_%H%M%S).sql.gz"

echo "Backing up $POSTGRES_DB to $FILE"
docker compose --project-directory "$APP_DIR" exec -T db \
    pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" --no-owner | gzip > "$FILE"
echo "Backup size: $(du -h "$FILE" | cut -f1)"

find "$BACKUP_DIR" -type f -name 'db_*.sql.gz' -mtime +"$KEEP_DAYS" -delete

if [[ -n "${RCLONE_REMOTE:-}" ]]; then
    rclone copy "$FILE" "$RCLONE_REMOTE"
    echo "Uploaded to $RCLONE_REMOTE"
fi

echo "Backup completed at $(date)"
