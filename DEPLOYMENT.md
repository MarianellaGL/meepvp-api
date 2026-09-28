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
| `SMTP_HOST` | Your transactional email provider's SMTP hostname. |
| `SMTP_PORT` | `2525`, supplied by the Blueprint; confirm your provider supports it. |
| `SMTP_TLS` | `starttls`, supplied by the Blueprint. |
| `SMTP_USERNAME` / `SMTP_PASSWORD` | Your provider's credentials. |
| `SMTP_FROM` | A verified sender, e.g. `MeppVP <no-reply@your-domain.com>`. |
| `BGG_API_TOKEN` | Optional: add the existing BoardGameGeek token to enable authenticated BGG requests. |

Render supplies `PORT`; the server already listens on that port on all interfaces.
No local `.env` or `config.yaml` is included in the image. Production refuses to
start without a configured mail provider, a strong JWT secret and an absolute
base URL. Do not use development mode or a fake SMTP host to bypass these checks.

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

Render Free blocks outbound SMTP on ports 25, 465 and 587. Use a provider with
STARTTLS on 2525, or add an HTTPS email adapter. Sending real emails requires
valid provider credentials and a verified sender.

No Render database or persistent disk is created. Game data, photos and user
sessions remain in PostgreSQL; temporary PDF/OCR files use ephemeral storage.
The current Free plan limits and Blueprint syntax are documented in
[Render Free](https://render.com/docs/free) and the
[Blueprint reference](https://render.com/docs/blueprint-spec).
