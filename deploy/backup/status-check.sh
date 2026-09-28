#!/bin/bash
# Prints row counts and sizes of the Postgres database of the compose stack.
set -euo pipefail

APP_DIR="$(cd "$(dirname "$0")/../.." && pwd)"
[[ -f "$APP_DIR/.env" ]] && source <(grep -E '^POSTGRES_(USER|DB)=' "$APP_DIR/.env")
POSTGRES_USER="${POSTGRES_USER:-postgres}"
POSTGRES_DB="${POSTGRES_DB:-app}"

psql() { docker compose --project-directory "$APP_DIR" exec -T db psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -c "$1"; }

echo "--- Rows per table ---"
psql "SELECT relname AS table, n_live_tup AS rows FROM pg_stat_user_tables ORDER BY n_live_tup DESC;"
echo "--- Table sizes ---"
psql "SELECT relname AS table, pg_size_pretty(pg_total_relation_size(relid)) AS size FROM pg_stat_user_tables ORDER BY pg_total_relation_size(relid) DESC;"
echo "--- Database size ---"
psql "SELECT pg_size_pretty(pg_database_size(current_database())) AS size;"
