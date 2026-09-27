# MeepVP API

Go API for the MeepVP board-game scoring app. It stores scoring sheets, tables, game sessions, schedules, and user accounts in PostgreSQL. The Go module and local Docker database still use the original `tablescore` identifiers to preserve existing data and imports.

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

Accounts are optional for playing. Sign up with a 3–30 character username containing letters, numbers, or underscores, and a password of 12–1024 bytes. Passwords are stored as salted PBKDF2-HMAC-SHA256 hashes with 600,000 iterations; plaintext passwords are never saved. The API issues random bearer tokens, stores only their SHA-256 hashes, and expires them after 30 days. Use HTTPS outside local development.

| Method | Route | Purpose |
| --- | --- | --- |
| `POST` | `/v1/auth/signup` | Create an account and receive `{user, token}` |
| `POST` | `/v1/auth/login` | Sign in and receive `{user, token}` |
| `POST` | `/v1/auth/logout` | Revoke the current bearer token |
| `GET` | `/v1/me` | Return the signed-in user |
| `GET` | `/v1/me/stats` | Return only this account's finished games, wins, ties, and total points |
| `GET` | `/v1/me/sessions` | Return only sessions linked to this account, including scores and winners |
| `POST` | `/v1/me/claim-session` | Attach an older anonymous session using `{sessionId, playerId, hostToken}` |

Send the token as `Authorization: Bearer <token>`. A signed-in host's new session is automatically linked to the account's first player. The claim route requires the private table host token and prevents two accounts from claiming the same player in a session. Existing anonymous sessions are not assigned to an account automatically without that proof.

## Game and scoring routes

| Method | Route | Purpose |
| --- | --- | --- |
| `POST` | `/v1/tables` | Create an anonymous table; returns shareable code and private `hostToken` |
| `POST`, `GET` | `/v1/scoring-rules` | Create or list all scoring sheets stored in the database |
| `GET` | `/v1/community/scoring-rules?query=&bggId=` | Search sheets marked `isPublic: true` |
| `POST` | `/v1/tables/{code}/sessions` | Start a game with `X-Table-Token` |
| `GET` | `/v1/sessions/{id}` | Read the current scores and totals |
| `POST` | `/v1/sessions/{id}/players` | Add a player to an active game |
| `PATCH` | `/v1/sessions/{id}/scores` | Change one player's scoring field atomically |
| `PUT` | `/v1/sessions/{id}/scores` | Replace all score field values |
| `POST` | `/v1/sessions/{id}/points` | Add or subtract direct points |
| `POST` | `/v1/sessions/{id}/finish` | Finish the game with `X-Table-Token` and return `winners` |
| `POST` | `/v1/sessions/{id}/reopen` | Reopen a finished game with `X-Table-Token` |
| `POST`, `GET` | `/v1/tables/{code}/scheduled-games` | Create or list game plans with `X-Table-Token` |
| `PATCH` | `/v1/scheduled-games/{id}/rule` | Assign a scoring sheet to a plan |
| `PATCH` | `/v1/scheduled-games/{id}/session` | Attach a started session to a plan |

To share a newly created sheet in community search, send `isPublic: true` with a valid bearer token. Creating an unlisted sheet remains available without an account. The `winners` array appears in finished-session responses and includes all players tied for the best score. The sheet's `winCondition` determines whether the highest or lowest total wins; `totals` is returned throughout the session.

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
| `GET` | `/v1/bgg/games/{gameID}/rules` | List the game's Rules forum discussions and links |
| `POST` | `/v1/pdf/extract` | Extract selectable text and scoring passages from a multipart PDF |
| `POST` | `/v1/scoring-rules/{ruleID}/pdf-imports` | Create a queued legacy import job |
| `GET` | `/v1/pdf-imports/{id}` | Read a queued import job |

`POST /v1/pdf/extract` accepts files up to 20 MB and 100 pages and does not retain the PDF. Image-only scanned pages need OCR and may return no text. The legacy PDF import route has no worker yet, so its jobs stay queued. The mobile app reads standalone scoring-table images on the device and asks users to confirm the extracted text and scores before saving a sheet.

## Current limits

`GET /v1/scoring-rules` currently lists **all** sheets to unauthenticated callers, including sheets omitted from community search. `isPublic: false` means unlisted, not private. Do not store confidential material in scoring sheets. Authentication protects account statistics, while anonymous tables and session IDs retain their existing access model. Rate limiting, password recovery, email verification, and publication moderation are still needed before exposing account creation and community publishing broadly on the public internet.
