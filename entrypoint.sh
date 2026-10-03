#!/bin/sh
set -eu

# AI runs through Codex CLI signed in with the owner's ChatGPT account, so it
# uses that plan's usage and never API credits. Upload ~/.codex/auth.json as a
# Render Secret File named codex-auth.json; without it, AI features answer 503.
auth_file="${CODEX_AUTH_FILE:-/etc/secrets/codex-auth.json}"
if [ -f "$auth_file" ]; then
  mkdir -p /run/meeple-ai/codex-home
  cp "$auth_file" /run/meeple-ai/codex-home/auth.json
  chown -R 65533:65000 /run/meeple-ai
  chmod 0770 /run/meeple-ai
  chmod 0700 /run/meeple-ai/codex-home
  chmod 0600 /run/meeple-ai/codex-home/auth.json
  env -i PATH="$PATH" HOME=/tmp AI_CODEX_HOME=/run/meeple-ai/codex-home AI_CODEX_MODEL="${AI_CODEX_MODEL:-}" \
    AI_WORKER_SOCKET=/run/meeple-ai/worker.sock \
    setpriv --reuid=65533 --regid=65000 --clear-groups /ai-worker &
  worker_pid=$!
  attempts=0
  until [ -S /run/meeple-ai/worker.sock ]; do
    if ! kill -0 "$worker_pid" 2>/dev/null; then
      echo "AI worker failed to start" >&2
      exit 1
    fi
    attempts=$((attempts + 1))
    if [ "$attempts" -ge 50 ]; then
      echo "AI worker startup timed out" >&2
      exit 1
    fi
    sleep 0.1
  done
  export AI_PROVIDER=codex-worker
else
  echo "Codex sign-in not found at $auth_file; AI features are disabled" >&2
  export AI_PROVIDER=disabled
fi

unset OPENAI_API_KEY CODEX_API_KEY
exec setpriv --reuid=65532 --regid=65000 --clear-groups /server
