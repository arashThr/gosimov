# Deployment

The production stack (`compose.yml` at the repo root) runs three containers:

| Service | Purpose |
|---------|---------|
| `app`   | The Go server (built from `Dockerfile`), listening on :8000 inside the network |
| `db`    | Postgres 18, data in the `db-data` volume |
| `caddy` | Reverse proxy with automatic HTTPS, config in `deploy/Caddyfile` |

## First deployment

1. Prepare a server with Docker (optionally: `cd deploy/ansible && ansible-playbook -i hosts setup.yml`).
2. Point your domain's DNS A/AAAA record to the server.
3. Put the code on the server, e.g. with the push-to-deploy hook below, or `git clone`.
4. `cp .env.example .env` and set at least:
   `ENVIRONMENT=production`, `DOMAIN`, `BASE_URL=https://<DOMAIN>`, `POSTGRES_PASSWORD`, SMTP settings.
5. `docker compose up -d --build` (or `make up`).

Migrations run automatically when the app starts.

## Push to deploy

`post-receive` is a git hook that checks out `main` and rebuilds the stack on every push.
Setup instructions are at the top of the file.

## Backups

See [backup/README.md](backup/README.md).
