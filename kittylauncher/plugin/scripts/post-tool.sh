#!/bin/bash
set -euo pipefail

SESSION_NAME=""
if [ -n "${TMUX:-}" ]; then
  SESSION_NAME=$(tmux display-message -p '#{session_name}' 2>/dev/null || true)
fi

if [[ ! "$SESSION_NAME" =~ ^kl- ]]; then
  cat > /dev/null
  exit 0
fi

REPO="${SESSION_NAME#kl-}"
TOOL_NAME=$(cat | jq -r '.tool_name // ""' 2>/dev/null || echo "")

curl -s -X POST "http://127.0.0.1:9199/event" \
  -H "Content-Type: application/json" \
  -d "$(jq -n --arg s "$SESSION_NAME" --arg r "$REPO" --arg t "$TOOL_NAME" \
    '{session: $s, repo: $r, event: "tool", tool_name: $t}')" \
  --max-time 2 > /dev/null 2>&1 || true

exit 0
