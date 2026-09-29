# Render + Neon

The Go API runs on Render using the existing Dockerfile, including Poppler and
Tesseract for PDF extraction and OCR. PostgreSQL stays on Neon. The mobile app
remains the frontend.

## Create the service

1. Open the existing [Render project](https://dashboard.render.com/project/prj-datfm1flot8c73fcnae0).
2. Create a Blueprint from `MarianellaGL/meepvp-api`, branch `main`, using
   `render.yaml`. Assign the service to the existing project/environment. If
   `meppvp-api` already exists, inspect it before applying the Blueprint: a
   matching service name can update that service.
3. Fill the variables below. Keep credentials in Render's Environment settings.
4. Deploy and check `GET /health` returns `{"status":"ok"}`. Check startup logs
   for completed migrations and `MeppVP API listening`.
5. Set the mobile app's `EXPO_PUBLIC_API_URL` to the public HTTPS service URL and
   rebuild/restart the app with that environment value.

The Blueprint uses the Free plan and Ohio region, matching the current Neon
project's `aws-us-east-2` region. It builds directly from GitHub and does not
require a Docker Hub image or registry credentials. Deploys are manual initially;
after fixing the Docker Hub publish check, set `autoDeployTrigger: checksPass`
to deploy only commits whose CI checks pass.

## Runtime variables

| Variable | Value |
| --- | --- |
| `DATABASE_URL` | The existing Neon production branch's **direct** `DATABASE_URL_UNPOOLED`, keeping its TLS parameters. |
| `AUTH_JWT_SECRET` | Generated once by Render; keep it stable across deploys. |
| `SERVER_BASE_URL` | The service's public HTTPS URL; update it if adding a custom domain. |
| `APP_ENV` | `production`, supplied by the Blueprint. |
| `LOG_LEVEL` | `info`, supplied by the Blueprint. |
| `BGG_API_TOKEN` | Optional: add the existing BoardGameGeek token to enable authenticated BGG requests. |

Render supplies `PORT`; the server already listens on that port on all interfaces.
No local `.env` or `config.yaml` is included in the image. Production requires
a strong JWT secret and an absolute base URL. SMTP is optional. Without an
SMTP host, production disables email delivery and never logs email bodies or
verification/reset tokens. Registration and login keep working; accounts are
not automatically marked as email-verified. Resend-verification and
forgot-password return HTTP 503 with `email delivery is disabled`, without
issuing tokens or claiming that an email was sent.

The current server applies embedded Goose migrations at startup through the same
GORM pool used by the app. Goose uses a **session advisory lock**, so use the
direct Neon connection for this deployment. Neon's pooled URL uses transaction
pooling and cannot preserve that session lock. The application already limits
its local pool to 25 open connections. Separating migration and application
connections would allow a pooled application URL later.

## Free plan behavior

Render Free services sleep after 15 minutes without inbound traffic and can take
about a minute to wake. Clients should reconnect WebSockets after interruptions.
The mobile app still needs its WebSocket subscription implemented; hosting the
API does not add that client behavior.

If you later want email verification or password recovery, set `SMTP_HOST`,
`SMTP_USERNAME`, `SMTP_PASSWORD` and a verified `SMTP_FROM`. Render Free blocks
outbound SMTP on ports 25, 465 and 587; use a provider supporting
`SMTP_PORT=2525` with `SMTP_TLS=starttls`, or add an HTTPS email adapter.
Leave `SMTP_HOST` unset when email is not needed.

No Render database or persistent disk is created. Game data, photos and user
sessions remain in PostgreSQL; temporary PDF/OCR files use ephemeral storage.
The current Free plan limits and Blueprint syntax are documented in
[Render Free](https://render.com/docs/free) and the
[Blueprint reference](https://render.com/docs/blueprint-spec).

## Rulebook catalog

Deploy the latest API commit before using **Biblioteca → Buscar reglamentos**
in the mobile app. Startup applies additive migration 00003, which creates
`rulebooks` and seeds Catan/Everdell English base metadata. Existing game and
account data is preserved. No new environment variables or database credentials
are needed. Verify `GET /v1/rulebooks` returns those two records, then select a
PDF and review its scoring fields in the app before saving. The provider supports
EN/FR; BGG integration remains separate and requires its own token.
