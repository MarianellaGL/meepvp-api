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
- PDF upload only creates a private processing job at this stage. It does not republish a game manual.
- Authentication, WebSocket broadcasts, BGG synchronization, and the PDF extraction worker are deliberately separate follow-up modules.
