#!/bin/sh
set -eu

if [ "${AI_PROVIDER:-}" = "codex-cli" ]; then
  echo "Use AI_PROVIDER=codex-worker in the public API image" >&2
  exit 1
fi

if [ "${AI_PROVIDER:-}" = "codex-worker" ]; then
  if [ -n "${CODEX_API_KEY:-}" ]; then
    mkdir -p /run/meeple-ai
    chown 65533:65000 /run/meeple-ai
    chmod 0770 /run/meeple-ai
    env -i PATH="$PATH" HOME=/tmp CODEX_API_KEY="$CODEX_API_KEY" AI_WORKER_SOCKET=/run/meeple-ai/worker.sock \
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
  else
    echo "CODEX_API_KEY is unset; using Responses API provider" >&2
    export AI_PROVIDER=responses
  fi
fi

unset CODEX_API_KEY
exec setpriv --reuid=65532 --regid=65000 --clear-groups /server
