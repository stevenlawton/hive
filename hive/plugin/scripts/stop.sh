#!/bin/bash
set -euo pipefail
cat > /dev/null

SESSION_NAME=""
if [ -n "${TMUX:-}" ]; then
  SESSION_NAME=$(tmux display-message -p '#{session_name}' 2>/dev/null || true)
fi

[[ "$SESSION_NAME" =~ ^(kl|hive)- ]] || exit 0

if [[ "$SESSION_NAME" =~ ^hive- ]]; then
  REPO="${SESSION_NAME#hive-}"
else
  REPO="${SESSION_NAME#kl-}"
fi

curl -s -X POST "http://127.0.0.1:9199/event" \
  -H "Content-Type: application/json" \
  -d "$(jq -n --arg s "$SESSION_NAME" --arg r "$REPO" \
    '{session: $s, repo: $r, event: "completed"}')" \
  --max-time 2 > /dev/null 2>&1 || true

exit 0
