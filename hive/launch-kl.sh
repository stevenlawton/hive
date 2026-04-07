#!/usr/bin/env bash
set -euo pipefail

# KittyLauncher — launch script
# Starts kitty with the TUI in tab 0 (orange), IPC enabled.

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
BINARY="${SCRIPT_DIR}/kittylauncher"
SOCKET_PATH="/tmp/kl-kitty-$$"

# Ensure dependencies are installed
if [ -x "${HOME}/.local/kitty.app/bin/kitty" ]; then
    KITTY="${HOME}/.local/kitty.app/bin/kitty"
elif command -v kitty &>/dev/null; then
    KITTY="kitty"
else
    echo "kitty not found. Installing..."
    curl -L https://sw.kovidgoyal.net/kitty/installer.sh | sh /dev/stdin
    KITTY="${HOME}/.local/kitty.app/bin/kitty"
    if [ ! -x "$KITTY" ]; then
        echo "Error: kitty installation failed"
        exit 1
    fi
    echo "kitty installed."
fi

if ! command -v tmux &>/dev/null; then
    echo "tmux not found. Installing..."
    sudo apt-get install -y tmux
    if ! command -v tmux &>/dev/null; then
        echo "Error: tmux installation failed"
        exit 1
    fi
    echo "tmux installed."
fi

# Check if KL is already running
EXISTING_SOCKET="$(ls /tmp/kl-kitty-* 2>/dev/null | head -1 || true)"
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
"$KITTY" \
    --listen-on "unix:${SOCKET_PATH}" \
    -o allow_remote_control=socket-only \
    -o tab_bar_style=powerline \
    -o "tab_title_template={title}" \
    --title "KittyLauncher" \
    "$BINARY"

# Cleanup socket on exit
rm -f "$SOCKET_PATH" 2>/dev/null
