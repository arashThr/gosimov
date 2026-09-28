# CLAUDE.md

Guidance for Claude Code when working in this repository. See README.md for the full picture.

## Commands

- `make dev`: run with live reload (air); SQLite and console emails by default
- `make generate`: regenerate `*_templ.go` (they are gitignored; run after editing `.templ` files)
- `make test`: tests on SQLite; add `TEST_POSTGRES_URL=...` to run them on Postgres too
- `make lint`: `go vet` and gofmt check
- `make migration name=xyz`: create a migration pair (postgres + sqlite)
- `make services`: local Postgres and Mailpit via compose.dev.yml
- Tools run via `go tool` (templ, air); no global installs needed

## Architecture

- Feature packages under `internal/` (`auth`, `account`, `apitoken`, `billing`, `admin`, `home`, `user`),
  infrastructure under `internal/platform/`, shared UI in `internal/ui/`.
- Every feature has an `fx.Module`; all modules are listed in `internal/app/app.go`.
- Features register HTTP routes by providing a `server.Routes` via `server.AsRoutes(NewHandler)`.
  Global middleware uses `server.AsMiddleware`. Account page cards use `account.AsSection`.
- Handlers are `func(w, r) error` wrapped in `web.Handle`. Return errors from `internal/platform/errs`
  (`ErrNotFound`, `ErrInvalid`, ...) and wrap user-facing messages with `errs.Public(err, msg)`.
- Current user: `user.FromContext(ctx)`. Request logger: `logger.FromContext(ctx)` (zap).
- Views are templ components wrapped in `ui.Layout`; CSS is plain in `internal/ui/static/css/app.css`.

## Database rules

- SQL must run on both Postgres and SQLite: `$n` placeholders, timestamps passed from Go
  (`time.Now().UTC()`, never `NOW()`), only portable syntax.
- Every migration needs a Postgres and a SQLite version with the same number.
- Repositories return `errs.ErrNotFound` for missing rows; use `database.IsUniqueViolation` for conflicts.

## Conventions

- Store only hashes of secrets (sessions, emailed tokens, API tokens): `auth.HashToken`.
- Optional integrations (SMTP, Turnstile, OAuth, Stripe, admin) are enabled by their env vars
  and must keep working when unset.
- Add tests with `dbtest.New(t)`; full-app HTTP tests live in `internal/app/app_test.go`.
