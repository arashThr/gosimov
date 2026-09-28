# Backups

`db-backup.sh` dumps the Postgres database of the compose stack with `pg_dump`,
keeps compressed dumps for `KEEP_DAYS` days and, if `RCLONE_REMOTE` is set,
copies each dump to object storage (e.g. Cloudflare R2, S3) with rclone.

1. `cp sample.env .env && chmod 600 .env` and fill in what you need.
2. Configure rclone if you want off-site copies: `rclone config`
   ([R2 guide](https://developers.cloudflare.com/r2/examples/rclone/)).
3. Add a cron job: `0 3 * * * /path/to/app/deploy/backup/db-backup.sh >> ~/backups/backup.log 2>&1`

Restore: `gunzip -c db_XXXX.sql.gz | docker compose exec -T db psql -U postgres -d app`

`status-check.sh` prints row counts and table sizes.

Using SQLite in production? Back up the `app-data` volume, or use
[Litestream](https://litestream.io) for continuous replication.
