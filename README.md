# TableScore API

TableScore API is a Go backend-for-frontend (BFF) for a React Native board-game scoring app.

It supports anonymous tables, shared score sheets, reusable scoring rules, queued PDF imports, and PostgreSQL persistence.

## Run locally

```bash
docker compose up -d postgres
cp .env.example .env
go run ./cmd/server
```

The server listens on `http://localhost:8080` by default. Override it with `PORT`.

Copy `.env.example` to `.env`, configure `DATABASE_URL`, and put the BoardGameGeek token in `BGG_API_TOKEN`. The `.env` file is ignored by Git and the API never returns this value. The server runs its schema migrations on startup.

```bash
curl http://localhost:8080/health
```

## Mobile API flow

1. `POST /v1/tables` creates an anonymous table and returns a shareable `code` and a private `hostToken`.
2. `POST /v1/scoring-rules` creates a score-sheet template with counter or checkbox fields.
3. `POST /v1/tables/{code}/sessions` creates a shared session. Send the host token in `X-Table-Token`.
4. `PUT /v1/sessions/{sessionID}/scores` updates the values for a player. A checkbox is represented as a numeric count, so a field can be checked more than once.
5. `POST /v1/scoring-rules/{ruleID}/pdf-imports` accepts a PDF and creates an import job. The response is queued for the future extraction and AI review worker; the raw PDF is not made public.
6. `GET /v1/bgg/collections/{username}` reads a BGG collection and returns JSON for React Native. BGG may answer `202`; TableScore forwards it as `{ "status": "processing" }` so the client can retry.
7. `GET /v1/bgg/games/{gameID}/rules` finds the game's Rules forum on BGG and returns its latest thread titles, counts, and links. These are community discussions; they are not an official rulebook or an inferred scoring sheet.
8. `POST /v1/pdf/extract` accepts a multipart `file` containing a PDF (up to 20 MB and 100 pages). It returns selectable text and up to 12 passages mentioning scoring. The file is not retained by this endpoint; scanned/image-only PDFs need OCR and may return no text. Users review the passages before creating a scoring sheet.
9. `POST /v1/sessions/{sessionID}/players` with `{ "name": "Ana" }` adds a player to an active game and returns the updated scores and totals. Repeating a name (case-insensitively) is safe and does not add a duplicate.
10. `POST /v1/sessions/{sessionID}/points` with `{ "playerId": "...", "delta": 5 }` adds or subtracts direct points from an active player's score. Direct points are included in `totals` alongside score-sheet fields.
11. `POST /v1/sessions/{sessionID}/reopen` reopens a finished game without clearing its scores. Send the table host token in `X-Table-Token`.
12. `PATCH /v1/sessions/{sessionID}/scores` with `{ "playerId": "...", "fieldId": "...", "value": 3 }` updates one scoring field atomically, so another player's concurrent changes are preserved. The earlier `PUT` route remains available for whole-sheet replacements.
13. `GET /v1/community/scoring-rules?query=wingspan&bggId=266192` searches only scoring templates with `isPublic: true`. Both filters are optional; results include fields and point values, never finished game scores.
14. `POST /v1/tables/{code}/scheduled-games` with `{ "gameName": "Wingspan", "scheduledAt": "2026-10-10T20:00:00-03:00", "players": ["Ana"] }` plans a future game without requiring a scoring sheet. Send `X-Table-Token`. `GET` on the same path lists that table's plans with the token.
15. `PATCH /v1/scheduled-games/{id}/rule` with `{ "ruleId": "..." }` attaches a scoring sheet later. Send `X-Table-Token`.
16. `PATCH /v1/scheduled-games/{id}/session` with `{ "sessionId": "..." }` connects the started game to its plan. The session must belong to the same table and use the plan's scoring sheet.

## Example scoring rule

```json
{
  "gameName": "Example Game",
  "name": "Standard scoring",
  "winCondition": "highest_total",
  "fields": [
    {"name": "Completed goals", "kind": "checkbox", "pointsPerUnit": 5},
    {"name": "Coins", "kind": "counter", "pointsPerUnit": 1},
    {"name": "Penalty", "kind": "checkbox", "pointsPerUnit": -3}
  ]
}
```

## Current boundaries

- Anonymous tables do not require a user account.
- The QR code belongs in React Native and encodes the table code; joining by manually entered code uses the same API.
- The older rule-specific PDF upload route only creates a private processing job; `POST /v1/pdf/extract` extracts selectable text immediately and does not retain the file.
- Authentication, WebSocket broadcasts, BGG synchronization, and the PDF extraction worker are deliberately separate follow-up modules.
- Mobile schedules a device-local reminder 24 hours before a game without a scoring sheet when notification permission is granted. Remote push delivery requires an installed development or production build and push credentials.
