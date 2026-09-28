# Foundation backend in MeppVP

Adapted from [decoda-ar/foundation](https://github.com/decoda-ar/foundation),
commit `16c9c67`. Foundation itself remains unchanged. The mobile application
continues to be the frontend; there is no copied React web application.

## Stack and request flow

Gin routes the existing `/v1` game API and the Foundation `/api` endpoints.
`internal/httpapi` preserves the mobile contract; `handlers` contains Foundation
HTTP handlers, `services` owns shared business logic, and `models` contains GORM
records. Configuration is YAML plus cleanenv overrides. Logging is slog with tint
in development and JSON in production. JWT signing uses golang-jwt; password
hashing uses Argon2id; Google/GitHub/Apple OAuth uses goth. Transactional email
uses go-mail through the mailer interface. WebSockets use coder/websocket and
Foundation's hub, rooms and typed message envelope.

GORM owns the PostgreSQL pool and the Foundation accounts/settings queries.
The existing game repository uses that same pool and retains its SQL row locks
and JSONB records. This preserves Tablescore's atomic score updates and stored
data. Game mutations go through `services.GameRepository`, which broadcasts a
`query.invalidate` message to `session:<id>` only after a successful write.

## Data and account compatibility

Users retain their text IDs, usernames, existing password hashes and account
links. New Foundation users receive UUID strings and generated unique usernames.
New hashes are Argon2id; legacy PBKDF2 hashes remain verifiable. Both Foundation
and mobile accounts live in the same `users` table. Foundation's email fields,
roles and provider records are additive.

Goose SQL migrations are embedded, versioned and guarded by a PostgreSQL advisory
lock. The first migration adopts the existing schema with `IF NOT EXISTS`; its
rollback deliberately keeps all legacy tables and rows. The second adds
Foundation's user fields, providers, refresh tokens, email tokens and settings.
Rolling that migration back removes those new records/fields, but preserves
Tablescore users, tables, games, photos and account links. Do not roll it back
on a live deployment that relies on the new authentication system.

`/v1/auth` continues to return the existing 30-day opaque bearer token so the
mobile application keeps working. Foundation's `/api/auth` issues 15-minute JWTs
and rotating 7-day refresh tokens as httpOnly cookies. JWT cookies and bearer
JWTs also authenticate the existing `/v1` account/game endpoints. Refresh tokens
are stored hashed, rotate atomically, and revoke sessions on detected reuse;
concurrent retries have a 30-second grace window. The refresh cookie is scoped
to `/api/auth`, covering both refresh and logout. Password reset synchronizes
both password records and revokes existing mobile/refresh sessions. Soft-deleted
users cannot authenticate through either path.

## Auth, admin and settings

- `POST /api/auth/register`, `/login`, `/refresh`, `/logout`
- `GET /api/auth/me`
- `GET /api/auth/:provider`, `/:provider/callback`
- `POST /api/auth/verify-email`, `/resend-verification`, `/forgot-password`, `/reset-password`
- `GET /api/settings`
- `GET/POST /api/admin/users`; `PATCH/DELETE /api/admin/users/:id`; `PATCH /api/admin/users/:id/role`
- `GET /api/admin/ws/connections`; `PUT /api/admin/settings/:key`

Admin authorization checks the current database role, not just the JWT claim.
Role changes and deletions revoke sessions and disconnect existing sockets.
Create an initial admin explicitly with `cmd/cli adduser --admin`; startup does
not create or print a default admin password. OAuth requires provider credentials
and registered callback URLs. SMTP is required in production; development can
log emails instead. Clients consume verification and reset tokens through the
API; browser confirmation forms are available at the corresponding email links.

## WebSockets

Connect to `/ws`, optionally with an access cookie or JWT bearer header. In
production the origin must match `server.base_url`; development accepts any
origin. Anonymous sockets remain available for the existing anonymous game flow.
Join a session with:

```json
{"type":"room.join","payload":{"room":"session:SESSION_ID"}}
```

A successful score/player/photo/state mutation emits:

```json
{"type":"query.invalidate","payload":{"queryKey":["sessions","SESSION_ID"]}}
```

Re-fetch the existing session endpoint on receipt. Knowledge of a session ID
retains the current anonymous read-access model. Personal user rooms and admin
rooms have the hub's identity/role checks. Hub shutdown cancels socket pumps and
beacons. Settings control the development beacons.

## Configuration and tooling

`config.example.yaml` documents the cleanenv variables; `config.yaml` is ignored.
`DATABASE_URL` and `PORT` remain compatible with the existing `.env` startup.
`DATABASE_URL` takes precedence over `db.*`; `SERVER_PORT` takes precedence over
`PORT`. `server.trusted_proxies: []` retains Gin's default trust of all proxies
in development and production. Optional configured proxy entries are validated.

`make setup` downloads the application dependencies and process-compose's
separate `tools/go.mod`. `make dev` supervises PostgreSQL and rebuilds the API
on Go/YAML/SQL edits. `make build` creates server and CLI binaries. The CLI also
supports migration up/down/status/create, database ping, and account creation.
Docker builds a nonroot Debian image with Poppler and Tesseract for PDFs; the devcontainer supplies a PostgreSQL
sidecar. CI runs formatting, vet, golangci-lint, race tests and a build with a
dedicated PostgreSQL test service.

Set `TEST_DATABASE_URL` to a dedicated database whose name ends in `_test` to
exercise adoption, old PBKDF2 accounts, game/account links, both auth paths,
refresh races, password reset, deletion and migration down/up. The integration
test creates and removes its own schema and never uses the application database.
Without that variable it is explicitly skipped.
