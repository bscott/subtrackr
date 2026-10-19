# SubTrackr threat model

SubTrackr is a self-hosted subscription tracker: a single-user web application written in Go (Gin) with an
HTMX front end and a SQLite database (GORM + mattn/go-sqlite3). Users record recurring subscriptions, see
spending analytics and a renewal calendar, and receive renewal reminders by email, Pushover, Telegram or
webhook. It is normally deployed as a Docker container, optionally behind a reverse proxy, and is often
exposed on home networks or the public Internet.

## Layout

- `cmd/server` — the HTTP server (`/usr/local/bin/subtrackr`). Routes are registered in `cmd/server/main.go`.
- `cmd/mcp` — a Model Context Protocol server over stdio (`/usr/local/bin/subtrackr-mcp`) that opens the same
  SQLite file directly. It is run locally by a trusted MCP client; it is not network-exposed.
- `cmd/migrate-dates` — one-off data migration helper. Out of scope.
- `internal/handlers` — HTTP handlers. `internal/middleware/auth.go` — session and API-key gates.
- `internal/service` — business logic, including outbound integrations (`email.go`, `pushover.go`,
  `telegram.go`, `webhook.go`, `currency.go`, `oidc.go`, `logo.go`), sessions and settings.
- `internal/repository`, `internal/models`, `internal/database` — persistence.
- `templates/` (Go html/template) and `web/static` — the UI. `web/locales` — i18n strings.
- `tests/` — Playwright browser tests (not runnable in this image). `screenshots/`, `*.md` — docs, out of scope.

## Trust model

There is exactly one user, and that user is the operator. Authentication is optional and off by default:

- With auth disabled, every route is open. That is a documented deployment choice, not a bug. Do not report
  "unauthenticated access" findings that assume auth is disabled.
- With auth enabled (local password and/or OIDC), the attacker we care most about is an **unauthenticated
  remote client**. Treat every request to the server as untrusted.
- An **authenticated session** is the operator. There is no privilege ladder to climb, so post-auth findings
  matter when they reach beyond the application: arbitrary file read/write or code execution on the host,
  escaping the configured `DATABASE_PATH`, or leaking secrets to third parties.

Routes that are reachable without a session when auth is on (see `isPublicRoute`): `/login`,
`/forgot-password`, `/reset-password`, `/api/auth/*`, `/auth/oidc/*`, `/ical/:token`, `/healthz`,
`/static/*`, `/favicon.ico`, and everything under `/api/v1/*`, which is instead gated by `APIKeyAuth`
(API keys are created in Settings). These are the highest-value targets.

## Where untrusted input enters

1. HTTP requests: query, path, form, JSON bodies, headers (including `Accept`, which selects HTML vs JSON
   error handling), cookies.
2. The OIDC provider: discovery document, tokens and claims on `/auth/oidc/callback`. The provider is
   configured by the operator, but state/nonce handling, redirect handling and claim mapping must be correct.
3. Imported files: Wallos import (`/api/import/wallos`), backup restore (`/api/restore`). Treat their contents
   as attacker-controlled; the operator may restore a file obtained elsewhere.
4. Responses from outbound services: exchange-rate API (`currency.go`, fixer.io), SMTP servers, Pushover,
   Telegram, and operator-configured webhook endpoints.
5. The iCal subscription token in `/ical/:token`: unauthenticated by design, protected only by the token.
6. Environment variables (`DATABASE_PATH`, `PORT`, `GIN_MODE`, `FIXER_API_KEY`, `PUSHOVER_*`, `TELEGRAM_*`):
   operator-controlled, trusted.

## What we want to hear about (rough severity rubric)

- **Critical**: unauthenticated remote code execution; authentication bypass (session forgery or fixation,
  password-reset token weaknesses that allow takeover, OIDC state/nonce/redirect flaws that log an attacker in,
  API-key check bypass); unauthenticated read or modification of subscription data or settings; disclosure of
  stored secrets (SMTP password, Pushover/Telegram tokens, API keys, OIDC client secret, iCal token) to an
  unauthenticated party.
- **High**: SQL injection anywhere; path traversal or arbitrary file read/write via backup, restore, import,
  export, `DATABASE_PATH` handling or template rendering; stored XSS where the payload arrives from a
  third party (imported file, OIDC claims, exchange-rate or webhook responses) rather than typed by the
  operator; CSRF on endpoints that change authentication settings or create API keys
  (`/api/settings/auth/*`, `/api/settings/apikeys`); iCal token weaknesses
  (low entropy, non-constant-time comparison, enumeration); unauthenticated denial of service that is cheap
  for the attacker (e.g. unbounded allocation from one request).
- **Medium**: CSRF on ordinary data endpoints; missing brute-force protection on `/api/auth/login` and the
  password-reset flow; reflected XSS; SSRF where the target URL comes from subscription data rather than
  from settings; insecure defaults that weaken auth when the documented reverse-proxy setup is used
  (cookie flags, trusted-proxy handling).
- **Low / informational**: self-XSS from values only the operator can enter; SSRF through URLs the operator
  configured on purpose (webhook URL, OIDC issuer, SMTP host); missing security headers; version disclosure;
  anything that requires auth to be disabled.

Dependencies (Gin, GORM, go-oidc, go-jose, gorilla/sessions, the MCP SDK) are in scope only when a
vulnerability in them is reachable through SubTrackr; please show the SubTrackr code path.

## Out of scope

- The MCP server as attacked by its own MCP client: it runs locally with the user's privileges and talks to
  the database directly. Flaws in it that matter would be data corruption or crashes from valid tool input.
- The Playwright suite, `node_modules/`, `package*.json`, screenshots and documentation.
- Rate limiting of outbound notification sends, cosmetic/UI issues, and purely theoretical timing attacks
  without a practical demonstration.

## How to build, run and test

The checkout is at `/src` and is already built; the Go module cache is warm and `GOPROXY=off`.

```sh
cd /src
go test -count=1 ./...                 # baseline: all packages pass in about a minute
go build ./... && go vet ./...

# Start the server (templates/ and web/ are resolved relative to the working directory, so stay in /src).
DATABASE_PATH=/tmp/subtrackr.db PORT=8080 /usr/local/bin/subtrackr &
curl -s localhost:8080/healthz

# Turn on local authentication. Setup refuses until SMTP is configured (password recovery), so set a
# dummy SMTP config first; nothing is sent until a reminder fires.
curl -s -X POST localhost:8080/api/settings/smtp --data \
     'smtp_host=localhost&smtp_port=25&smtp_username=u&smtp_password=p&smtp_from=a@example.org&smtp_to=b@example.org'
curl -s -X POST localhost:8080/api/settings/auth/setup \
     --data 'username=admin&password=correct-horse-battery&confirm_password=correct-horse-battery'
curl -si -H 'Accept: application/json' localhost:8080/api/subscriptions   # expect 401 (HTML requests redirect to /login)
curl -s -c /tmp/jar -X POST localhost:8080/api/auth/login --data 'username=admin&password=correct-horse-battery'
curl -s -b /tmp/jar -H 'Accept: application/json' localhost:8080/api/subscriptions   # expect 200
```

Handler tests in `internal/handlers/*_test.go` show how to stand up a router with an in-memory database;
copying that pattern is the quickest way to write a Go reproducer.

## How we would like reports and patches

- One report per root cause. Group multiple sinks of the same unescaped value or the same missing check
  into one report, listing the affected routes.
- Reproducers as `curl` commands against a fresh server, or as a Go test in the existing style. State whether
  auth was enabled and how.
- Patches as a minimal, merge-ready diff against `main`, with a regression test where the surrounding code
  already has tests. Keep the existing Gin/GORM patterns; do not introduce new dependencies for a fix.
