# Skeleton

A lean starting point for Go web applications. Accounts, sessions, OAuth,
billing and deployment are wired up, so a new project starts at the part
that makes it unique.

- **Go + chi**, dependency injection with **fx**, structured logging with **zap**
- **templ** components, **htmx 2** and plain CSS, no frontend build step
- **Postgres or SQLite**: portable SQL through `database/sql`, embedded **goose** migrations
- **Auth**: email and password, magic links, Google and GitHub OAuth, email verification,
  password reset, optional Cloudflare Turnstile, rate limiting
- **Account page**: sign-in methods, password, personal API tokens, plan, account deletion
- **Stripe** subscriptions (Checkout, customer portal, webhooks)
- **Admin** area behind HTTP basic auth
- **Docker**, Compose, **Caddy** (automatic HTTPS), push-to-deploy hook and backup scripts
- Live reload with **air**

## Quick start

Requires Go 1.26+.

```sh
cp .env.example .env
make dev            # http://localhost:8000
```

That's it: the app uses SQLite (`data/app.db`) and prints emails to the console
(look for the verification link after signing up).

Want Postgres and a real inbox locally?

```sh
make services       # Postgres on :5432, Mailpit on :1025 (web UI http://localhost:8025)
# in .env:
#   DB_DRIVER=postgres
#   DATABASE_URL=postgres://postgres:postgres@localhost:5432/app?sslmode=disable
#   SMTP_HOST=localhost
#   SMTP_PORT=1025
make dev
```

Run `make` to list all tasks.

## Project layout

The code is organized by feature. Each feature package holds its handlers,
business logic, storage and templates.

```
cmd/server/            main: fx.New(app.Options()).Run()
internal/
  app/                 composition root: the list of modules
  home/                landing page and dashboard (start building here)
  user/                user model, repository, context helpers
  auth/                sign up/in, sessions, magic links, OAuth, verification, reset
  account/             account settings page, assembled from sections
  apitoken/            personal API tokens and bearer-token middleware
  billing/             Stripe subscriptions
  admin/               basic-auth admin area
  ui/                  layout, shared components, static assets (CSS, htmx)
  platform/            infrastructure shared by all features
    config/            environment configuration
    logger/            zap logger, request-scoped logging
    database/          Postgres/SQLite connection and migrations (+ dbtest for tests)
    email/             SMTP sender (or console output in development)
    server/            router, HTTP server, route and middleware groups
    web/               error-returning handlers, error pages, render helpers, middleware
    errs/              error kinds mapped to HTTP status codes
    flash/             one-shot messages across redirects
deploy/                Caddyfile, push-to-deploy hook, backups, ansible
```

## How it fits together

### Modules

Every feature exposes an `fx.Module`, listed in `internal/app/app.go`.
Features plug into the rest of the app through fx value groups, so nothing
central needs to know about them:

| Group | Helper | Purpose |
|-------|--------|---------|
| `routes` | `server.AsRoutes` | a type with `Register(chi.Router)` that adds endpoints |
| `middleware` | `server.AsMiddleware` | middleware applied to every request |
| `account_sections` | `account.AsSection` | a card on the account page |
| `account_deletion_hooks` | `account.AsDeletionHook` | cleanup before a user is deleted |

### Adding a feature

1. Create `internal/<feature>/` with a repository, handler and `views.templ`.
2. Export a module:

   ```go
   var Module = fx.Module("posts", fx.Provide(
       NewRepository,
       server.AsRoutes(NewHandler),
   ))
   ```

3. Register routes, protecting them with `auth.RequireUser` where needed:

   ```go
   func (h *Handler) Register(r chi.Router) {
       r.With(auth.RequireUser).Get("/posts", web.Handle(h.list))
   }
   ```

4. Add `posts.Module` to `internal/app/app.go`.
5. Add a migration: `make migration name=create_posts` and fill in both files.

### Handlers and errors

Handlers have the signature `func(w, r) error` and are wrapped with
`web.Handle`. A returned error is logged and rendered as an HTML error page,
or as JSON for `/api/` routes. The status code comes from the error kind:

```go
return errs.ErrNotFound                                   // 404
return errs.Public(errs.ErrInvalid, "Title is required")  // 400 with a message shown to the user
return fmt.Errorf("load post: %w", err)                   // 500, details only in the logs
```

Get the signed-in user with `user.FromContext(ctx)` and the request logger
(tagged with request and user ids) with `logger.FromContext(ctx)`.

### Frontend

Pages are templ components wrapped in `ui.Layout`. Styling lives in
`internal/ui/static/css/app.css` (design tokens at the top, light and dark
themes). htmx is loaded on every page; see the API token section for an
example of partial updates. Static files are embedded in the binary and
served with content-hash cache busting via `ui.Asset("css/app.css")`.

## Database

`DB_DRIVER` selects the engine: `sqlite` (default) or `postgres`.
Queries are written once, so keep them portable:

- use `$1, $2, ...` placeholders;
- pass timestamps from Go (`time.Now().UTC()`), never `NOW()`;
- stick to syntax both engines support (`RETURNING`, `ON CONFLICT`, `CASE`, ...).

Migrations live in `internal/platform/database/migrations/{postgres,sqlite}`,
with the same version numbers in both folders. They are embedded in the binary
and applied automatically on startup. Unique-constraint errors can be detected
for both engines with `database.IsUniqueViolation(err)`.

## Authentication

| Flow | Routes |
|------|--------|
| Email + password | `/signup`, `/signin`, `/signout` |
| Magic link | `/magic-link` → email → `/magic-link/verify` |
| Email verification | sent on sign up, `/verify-email`, resend from the dashboard |
| Password reset | `/forgot-password` → email → `/reset-password` |
| OAuth | `/oauth/{provider}` → `/oauth/{provider}/callback` |

Sessions are server-side (`sessions` table, 30 days); the cookie holds a
random token and the database only its SHA-256 hash. The same applies to all
emailed tokens and API tokens. Browser CSRF protection uses Go's
`http.CrossOriginProtection` (Fetch metadata), so forms need no tokens.

**OAuth providers** are enabled by setting their client id and secret. To add
another provider (Microsoft, Apple, ...), implement `auth.Provider` in a new
file next to `google.go`, add its credentials to `config.Config` and register
it in `auth.NewProviders`. Users can connect several providers to one account
(`user_identities` table) from the account page.

## Billing

Set `STRIPE_SECRET_KEY`, `STRIPE_PRICE_ID` and `STRIPE_WEBHOOK_SECRET`, and
point a Stripe webhook to `$BASE_URL/webhooks/stripe` with the
`customer.subscription.*` and `checkout.session.completed` events. Locally:

```sh
stripe listen --forward-to localhost:8000/webhooks/stripe
```

Gate paid features with `billing.Service.IsPremium(ctx, userID)`.

## Testing

```sh
make test                                   # SQLite
TEST_POSTGRES_URL=postgres://postgres:postgres@localhost:5432/app?sslmode=disable make test
```

`dbtest.New(t)` gives each test a fresh, migrated database. `internal/app/app_test.go`
boots the whole application for end-to-end HTTP tests.

## Deployment

```sh
cp .env.example .env    # ENVIRONMENT=production, DOMAIN, BASE_URL=https://..., POSTGRES_PASSWORD, SMTP_*
docker compose up -d --build
```

This starts the app, Postgres and Caddy, which obtains TLS certificates
automatically. See [deploy/README.md](deploy/README.md) for push-to-deploy,
backups and server setup.
