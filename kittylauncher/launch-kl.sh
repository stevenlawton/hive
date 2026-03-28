#!/usr/bin/env bash
set -euo pipefail

# KittyLauncher — launch script
# Starts kitty with the TUI in tab 0 (orange), IPC enabled.

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
BINARY="${SCRIPT_DIR}/kittylauncher"
SOCKET_PATH="/tmp/kl-kitty-$$"

# Check if KL is already running
EXISTING_SOCKET=$(ls /tmp/kl-kitty-* 2>/dev/null | head -1)
if [ -n "${EXISTING_SOCKET:-}" ] && [ -S "$EXISTING_SOCKET" ]; then
    echo "KittyLauncher already running. Focusing existing window."
    kitten @ --to "unix:${EXISTING_SOCKET}" focus-window 2>/dev/null || true
    exit 0
fi

# Build if needed
if [ ! -f "$BINARY" ]; then
    echo "Building kittylauncher..."
    (cd "$SCRIPT_DIR" && go build -o kittylauncher .)
fi

# Start kitty with:
# - Remote control enabled via socket
# - TUI as the initial tab
# - allow_remote_control set to socket-only
kitty \
    --listen-on "unix:${SOCKET_PATH}" \
    -o allow_remote_control=socket-only \
    -o tab_bar_style=powerline \
    -o "tab_title_template={title}" \
    --title "KittyLauncher" \
    "$BINARY"

# Cleanup socket on exit
rm -f "$SOCKET_PATH" 2>/dev/null
