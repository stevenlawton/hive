#!/usr/bin/env bash
set -euo pipefail

# Hive — launch script
# Starts hive in the current terminal, or in kitty with nice defaults.

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
BINARY="${SCRIPT_DIR}/hive"

# Ensure tmux is installed
if ! command -v tmux &>/dev/null; then
    echo "tmux not found. Installing..."
    sudo apt-get install -y tmux
    if ! command -v tmux &>/dev/null; then
        echo "Error: tmux installation failed"
        exit 1
    fi
    echo "tmux installed."
fi

# Build if needed
if [ ! -f "$BINARY" ]; then
    echo "Building hive..."
    (cd "$SCRIPT_DIR" && go build -o hive .)
fi

# Launch
if command -v kitty &>/dev/null && [ "${HIVE_USE_KITTY:-}" = "1" ]; then
    # Optional: launch in kitty with nice defaults
    kitty --title "Hive" "$BINARY"
else
    # Run in whatever terminal we're in
    exec "$BINARY"
fi
