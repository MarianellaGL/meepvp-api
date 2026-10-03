# MeepVP API

Go API for the MeepVP board-game scoring app. It stores scoring sheets, tables, game sessions, schedules, and user accounts in PostgreSQL. The Go module and local Docker database still use the original `tablescore` identifiers to preserve existing data and imports.

Routing uses Gin with the existing `/v1` JSON API. Every response includes an
`X-Request-ID`; structured logs include the route, status, latency, client IP
and request ID, omitting bodies, query strings and tokens. Panics before a
response is written return a generic JSON 500. Gin trusts all proxies by default,
accepting forwarded client IPs in dev and production.

Login and signup share a limit of 20 requests per minute per IP, per server
process. Blocked requests return JSON 429 with a positive `Retry-After` in seconds.
Logout, account reads and scoring remain outside this limit. CORS exposes
`X-Request-ID` and `Retry-After` to browser clients.

The server sets header, read, write and idle timeouts, allowing up to three
minutes for responses including PDF extraction. SIGINT and SIGTERM drain
in-flight requests for up to ten seconds before closing the database.

## API documentation

Open `/docs` on the running API for Swagger UI (for example,
`http://localhost:8080/docs`). The OpenAPI 3.0.3 contract is served at
`/openapi.json` and maintained in [docs/openapi.json](docs/openapi.json).
Both are embedded in the Go binary and available in the production image.
Swagger UI loads its pinned JavaScript/CSS from unpkg.

Use **Authorize** for the bearer token returned by `/v1/auth/login` or for the
table's `hostToken` as `X-Table-Token`. Foundation `/api/auth/login` sets HttpOnly
cookies and returns a user, not a token; same-origin requests from `/docs` use
those cookies. Foundation endpoints require JWT/cookie auth, not a legacy `/v1`
token. Swagger can upload PDFs and board images. WebSockets are described but
must be opened with a WebSocket client. **Try it out** uses the current server
and can change real data.

## Foundation backend

The backend adopts [Foundation](FOUNDATION.md): Gin, GORM/PostgreSQL, cleanenv,
slog/tint, Goose migrations, Argon2id, JWT with rotating refresh tokens, goth
OAuth, SMTP email verification/password reset, admin roles/settings and the
WebSocket hub. The mobile app remains the frontend. Existing `/v1` endpoints,
accounts, bearer sessions and game records stay compatible; new Foundation
features are exposed under `/api` and `/ws`.

```sh
make setup
make docker-up
make run
# Process supervision and live reload:
make dev
# Server and admin CLI:
make build
# Dedicated integration database only:
TEST_DATABASE_URL=postgres://tablescore:tablescore@localhost:5432/tablescore_test?sslmode=disable make test-integration
```

Copy `config.example.yaml` to `config.yaml` if needed. Set `AUTH_JWT_SECRET` (or
`auth.jwt_secret`) to a persistent random secret. Existing `DATABASE_URL` and
`PORT` values in `.env` still work; environment variables override YAML. Configure
OAuth credentials and `server.base_url` to activate social login, and SMTP to
send emails (development logs them). Create an admin with:

```sh
go run ./cmd/cli adduser --email admin@example.com --name Admin --password 'ReplaceWithYourPassword123!' --admin
```

See [FOUNDATION.md](FOUNDATION.md) for routes, compatibility and migration details.

## Deploy on Render with Neon

Use [render.yaml](render.yaml) to build the Go Docker image on Render while
keeping PostgreSQL on the existing Neon production branch. See
[DEPLOYMENT.md](DEPLOYMENT.md) for required variables, direct database connection
selection, SMTP setup and deployment verification.

## Run locally

```sh
docker compose up -d postgres
cp .env.example .env
go run ./cmd/server
```

Set `DATABASE_URL` in `.env` and provide `BGG_API_TOKEN` for BoardGameGeek requests. The `.env` file is ignored by Git. The server applies additive schema migrations at startup and listens on port 8080 unless `PORT` is set.

```sh
curl http://localhost:8080/health
go test ./...
```

Point the mobile app's `EXPO_PUBLIC_API_URL` at this server. Use a LAN IP instead of `localhost` from a physical phone.

## Accounts and statistics

Accounts are optional for playing. Sign up with a 3–30 character username containing letters, numbers, or underscores, and a password of 12–1024 bytes. New passwords are stored as salted Argon2id hashes; existing PBKDF2-HMAC-SHA256 hashes remain supported; plaintext passwords are never saved. The API issues random bearer tokens, stores only their SHA-256 hashes, and expires them after 30 days. Use HTTPS outside local development.

| Method | Route | Purpose |
| --- | --- | --- |
| `POST` | `/v1/auth/signup` | Create an account and receive `{user, token}` |
| `POST` | `/v1/auth/login` | Sign in and receive `{user, token}` |
| `POST` | `/v1/auth/logout` | Revoke the current bearer token |
| `GET` | `/v1/me` | Return the signed-in user |
| `GET` | `/v1/me/stats` | Return only this account's finished games, wins, ties, and total points |
| `GET` | `/v1/me/sessions` | Return only sessions linked to this account, including scores and winners |
| `GET` | `/v1/me/tables` | Return this account's owned tables, including host tokens for recovery on another device |
| `POST` | `/v1/me/claim-session` | Attach an older anonymous session using `{sessionId, playerId, hostToken}` |
| `POST` | `/v1/me/claim-table` | Attach an older anonymous table using `{code, hostToken}` |
| `GET`, `PUT`, `DELETE` | `/v1/me/avatar` | Read, upload, or remove the account avatar (JPEG/PNG/WebP, up to 5 MB) |

Send the token as `Authorization: Bearer <token>`. A signed-in host's new table is linked to the account; a new session is linked to its first player. A signed-in guest who joins an active session is linked to a new or preloaded player of that name and can see it in account history. A player already linked to another account cannot be claimed. The claim routes require the private table host token. Existing anonymous tables and sessions are not assigned to an account automatically without that proof. Treat tokens returned by `/v1/me/tables` as private credentials.

## Game and scoring routes

| Method | Route | Purpose |
| --- | --- | --- |
| `POST` | `/v1/tables` | Create an anonymous table; returns shareable code and private `hostToken` |
| `GET` | `/v1/tables/{code}/current-session` | Return the newest active or paused session for a table code, or 404 when there is none |
| `POST`, `GET` | `/v1/scoring-rules` | Create or list all scoring sheets stored in the database |
| `GET` | `/v1/community/scoring-rules?query=&bggId=` | Search sheets marked `isPublic: true` |
| `POST` | `/v1/tables/{code}/sessions` | Start a game with `X-Table-Token` |
| `GET` | `/v1/sessions/{id}` | Read the current scores and totals |
| `POST` | `/v1/sessions/{id}/players` | Add a player to an active game |
| `PATCH` | `/v1/sessions/{id}/scores` | Change one player's scoring field atomically |
| `PUT` | `/v1/sessions/{id}/scores` | Replace all score field values |
| `POST` | `/v1/sessions/{id}/points` | Add or subtract direct points |
| `POST` | `/v1/sessions/{id}/finish` | Finish the game with `X-Table-Token` and return `winners` |
| `POST` | `/v1/sessions/{id}/pause`, `/resume` | Pause or resume a long game with `X-Table-Token`; paused time is excluded from `durationSeconds` |
| `POST`, `GET` | `/v1/sessions/{id}/board-photo` | Store or read the latest board photo (JPEG, PNG or WebP, up to 5 MB); upload requires `X-Table-Token` |
| `POST` | `/v1/sessions/{id}/reopen` | Reopen a finished game with `X-Table-Token` |
| `POST`, `GET` | `/v1/tables/{code}/scheduled-games` | Create or list game plans with `X-Table-Token` |
| `PATCH` | `/v1/scheduled-games/{id}/rule` | Assign a scoring sheet to a plan |
| `PATCH` | `/v1/scheduled-games/{id}/session` | Attach a started session to a plan |
| `PUT`, `DELETE` | `/v1/scheduled-games/{id}` | Edit or cancel a plan before its session starts, with `X-Table-Token` |

To share a newly created sheet in community search, send `isPublic: true` with a valid bearer token. Creating an unlisted sheet remains available without an account. The `winners` array appears in finished-session responses and includes all players tied for the best score. The sheet's `winCondition` determines whether the highest or lowest total wins; `totals` is returned throughout the session.

Sessions now return `durationSeconds`, the time actually played. Pausing stores the accumulated time and stops scoring; resuming starts the clock again, even days later. `GET /v1/tables/{code}/current-session` finds active or paused games, while finished games return 404. One board photo per session is kept in `session_board_photos`; a new upload replaces it. Anyone with the session ID can read the photo, as with the other anonymous session data.

BGG endpoints return `429` with `Retry-After` when BGG limits requests. A `202` response means BGG is still processing and includes `retryAfterSeconds`.

```json
{
  "gameName": "Example Game",
  "name": "Standard scoring",
  "winCondition": "highest_total",
  "isPublic": false,
  "fields": [
    {"name": "Goals", "kind": "checkbox", "pointsPerUnit": 5},
    {"name": "Coins", "kind": "counter", "pointsPerUnit": 1},
    {"name": "Penalty", "kind": "checkbox", "pointsPerUnit": -3}
  ]
}
```

## BoardGameGeek and rulebooks

| Method | Route | Purpose |
| --- | --- | --- |
| `GET` | `/v1/bgg/collections/{username}` | Import a BGG collection; may return `202` while BGG processes it |
| `GET` | `/v1/bgg/search?query=` | Search actual BGG games; optional AI query correction and ranking |
| `GET` | `/v1/discovery/search?query=` | Search BGG games, public scoring sheets, and EN rulebooks in one request |
| `GET` | `/v1/bgg/games/{gameID}/rules` | List the game's Rules forum discussions and links |
| `GET` | `/v1/rulebooks` | Search EN/FR rulebooks and save catalog metadata |
| `POST` | `/v1/rulebooks/{id}/extract` | Read a catalog PDF and propose editable scoring fields |
| `POST` | `/v1/pdf/extract` | Extract selectable text and scoring passages from a multipart PDF |
| `POST` | `/v1/ocr/scoring-text` | Suggest editable scoring fields from text recognized on the device |
| `POST` | `/v1/ai/scoring-suggestion` | Retry an editable AI scoring proposal from previously extracted rulebook text |
| `POST` | `/v1/scoring-rules/{ruleID}/pdf-imports` | Deprecated: returns 410; use `/v1/pdf/extract` |
| `GET` | `/v1/pdf-imports/{id}` | Deprecated: returns 410; use `/v1/pdf/extract` |

`GET /v1/discovery/search?query=Catan` returns `games`, `communityRules`, and
`rulebooks` in one response. Each list is always present. `unavailableSources`
identifies sources that failed while other results remain usable;
`cachedRulebooks` marks catalog fallback. If BGG is still processing, the route
returns 202 with `status: "processing"`, `retryAfterSeconds`, and any planillas
or rulebooks already found. The mobile client combines records by BGG ID or
game name for display. The search is limited to 15 requests/minute/IP.

`GET /v1/rulebooks?query=Catan&language=en` searches rule-book.org and stores
catalog metadata in PostgreSQL. English and French are supported by the source;
this endpoint does not need a BGG token. Omitting `query` lists saved metadata.
Migration 00003 seeds the English base rulebooks for Catan and Everdell. When the
provider fails, matching saved records are returned with `cached: true`; a search
without saved matches returns 502. Searches are limited to 20/minute/IP.

`POST /v1/rulebooks/{id}/extract` downloads the selected catalog PDF, extracts its
text and returns the normal PDF response plus `rulebook` metadata. Downloads are
limited to allowlisted HTTPS PDFs on cdn.1j1ju.com, 20 MB and 100 pages; redirects
outside that source are rejected. Extraction is limited to 3/minute/IP. PDFs and
extracted text are not retained by the server. The mobile reader keeps a local
copy and opens the scoring editor for review. Creating a sheet can include
`rulebookId`; the API verifies that catalog record exists and retains the source
ID in the sheet. Reviewed suggestions cover the English base Catan/Everdell/Wingspan
rulebooks; other games use printed tables or manual configuration. The catalog
does not infer arbitrary game rules or grant rights to republish source PDFs.

`POST /v1/pdf/extract` accepts files up to 20 MB and 100 pages and does not retain the PDF. The old queued import routes are disabled because they never processed jobs. The mobile app reads standalone scoring-table images on the device, sends only recognized text to `/v1/ocr/scoring-text`, and asks users to confirm the extracted text and scores before saving a sheet.

Set `OPENAI_API_KEY` on the API service (Render Environment or an untracked local `.env`) to enable optional AI assistance. `OPENAI_MODEL` defaults to `gpt-4o-mini`. The API sends bounded extracted text, never the original PDF or image, to the Responses API with `store: false`. A validated structured response can propose editable fields when no reviewed template matches. If the key is absent or the model fails, text extraction, manual sheet creation, and search still work. AI never searches or ranks results: it only drafts sheets when the person asks, interprets photographed scoring tables, and may suggest a query when a search found nothing. PDF and rulebook extraction do not call the model. Proposed field names and notes are requested in Spanish even for English rulebooks. Limit AI usage with the route rate limits and a monthly spend limit on the OpenAI project. Never put the key in the mobile app.

When installed, Poppler extracts text with column layout; Tesseract reads pages without selectable text using English and Spanish language data. The Docker image includes both. On macOS without Poppler, extraction uses the Go parser with PDFKit/Vision as a fallback via Xcode Command Line Tools. Temporary uploads and rendered pages are removed after extraction. For local Poppler/OCR support, install `poppler`, `tesseract`, and its English/Spanish language data.

The response can include `scoringSuggestion: {gameName, fields, notes}`. Reviewed base-game templates are selected only when the extracted text contains the game's name and its scoring breakdown; filenames alone never select one. Everdell imports five manual point categories. Catan imports settlement/city/VP-card counters and the two unique bonus checkboxes. The mobile reader previews the suggestion and preserves field kinds and multipliers in the editable creation form. When extraction has no usable scoring structure, the reader can explicitly request an AI proposal from the extracted text; it still requires review in the editor. Unknown rulebooks still return text and excerpts; existing printed scoring sheets remain supported.

These templates cover scorekeeping for the base games. Everdell tie-breakers and Catan's own-turn victory condition are described for review, but the generic ranking engine still uses totals and does not enforce either. Catan's hidden VP cards should be entered at the end when using a shared sheet. Expansions and Everdell solo scoring require manual review. Re-upload older saved rulebooks to receive the new suggestion metadata.

Validate a downloaded rulebook with `TABLESCORE_TEST_PDF=/path/to/rulebook.pdf go test ./internal/pdfreader -run TestExtractProvidedPDF -v`. The PDFs themselves are never committed.

## Current limits

`GET /v1/scoring-rules` currently lists **all** sheets to unauthenticated callers, including sheets omitted from community search. `isPublic: false` means unlisted, not private. Do not store confidential material in scoring sheets. Authentication protects account statistics, while anonymous tables and session IDs retain their existing access model. Foundation provides email verification and password recovery for email accounts. Publication moderation remains application-specific. Existing username-only mobile accounts do not have an email address to use for recovery.

### Wingspan base

Migration 00004 adds the English Wingspan rulebook to the saved catalog. The
reviewed import requires both its base-game cover and the full scoring section;
Automa, appendices and expansion covers do not select this template. Its six
fields match the scorepad: bird points, bonus points and round-goal points are
manual totals; eggs, cached food and tucked cards are counters worth one each.
Unused food and cards in hand do not score automatically. Notes explain the
four-round finish, manual round-goal tie allocation and unused-food tiebreaker.
The API still returns point ties rather than calculating that tiebreaker.
Source: [official Wingspan reference](https://wingspan.rulepop.com/).

## Product backlog

See [PRODUCT_BACKLOG.md](PRODUCT_BACKLOG.md) for planned score cancellations and
duel mechanics, including data consistency and victory conditions.
