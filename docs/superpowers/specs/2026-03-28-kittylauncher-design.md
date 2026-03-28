# KittyLauncher — Design Spec

A Go TUI application that runs as a permanent "home base" tab in kitty, managing Claude Code workspaces across `~/repos`.

## Overview

KittyLauncher (KL) is a Bubbletea TUI that lives in kitty tab 0 (orange). From it, the user can browse repos, launch Claude sessions, manage remote-control instances, and create scratch workspaces. Kitty provides the tab/window chrome via IPC; tmux provides session persistence.

## Architecture

### Three layers

1. **Kitty** — the terminal emulator. Manages tabs, titles, colors. Controlled via `kitty @` IPC commands from the TUI.
2. **tmux** — session persistence. Every workspace tab runs inside a tmux session. Sessions survive kitty restarts. All sessions use a `kl-` prefix for identification. Remote-control sessions use `kl-rc-` prefix.
3. **TUI (Go/Bubbletea)** — the control hub. Runs in kitty tab 0 with no tmux wrapping. Orchestrates kitty and tmux via shell commands.

### Session lifecycle

1. User selects a repo in the TUI and presses Enter.
2. TUI creates a tmux session: `tmux new-session -d -s kl-<repo> -c ~/repos/<repo>`
3. TUI sends the startup command: `tmux send-keys -t kl-<repo> 'claude' Enter`
4. TUI opens a kitty tab attached to the session: `kitty @ launch --type=tab --title="<short>" tmux attach -t kl-<repo>`
5. TUI sets tab color: `kitty @ set-tab-color -m title:<short> active_bg=<color>`

For shell-only (Shift+Enter), step 3 is skipped.

For remote-control sessions, step 3 uses `claude remote-control` and the tmux session is named `kl-rc-<repo>`.

### Reconnection on restart

When the TUI starts, it runs `tmux list-sessions` and filters for `kl-*` prefixed sessions. For each existing session, it opens a kitty tab and re-attaches. This means kitty can be closed and reopened without losing any Claude sessions.

## TUI Interface

### Main view — Repo list

```
⚡ KittyLauncher
~/repos  ·  3 active  ·  1 remote
─────────────────────────────────
▶ SliceWize          claude interactive    ⟳ remote
  Legend of Mir       claude interactive
  Telegram Bridge     shell only
  scratch-20260328    claude interactive    tmp
  ...
─────────────────────────────────
Filter: ▌
Enter open+claude  Shift+Enter shell  r remote  s scratch  p promote  ? help  q quit
```

### Sections (top to bottom)

1. **Active sessions** — repos with running tmux sessions, sorted by most recently used.
2. **Favourites** — repos marked `favourite: true` in config, not currently active.
3. **All repos** — everything else in `~/repos`, alphabetical.

### Fuzzy filter

Typing filters the repo list instantly (like fzf). Matches against both the directory name and the configured display name.

### Help overlay

Pressing `?` shows a full-screen overlay with all keybindings. Press `?` or `Esc` to dismiss.

## Keybindings

| Key | Action |
|---|---|
| `Enter` | Open repo + start `claude` interactive session |
| `Shift+Enter` | Open repo + shell only (no claude) |
| `r` | Toggle remote-control session for selected repo |
| `s` | Create new scratch instance |
| `p` | Promote selected scratch to `~/repos/<name>` |
| `Tab` | Focus the kitty tab for the selected repo |
| `x` | Kill tmux session + close kitty tab |
| `d` | Detach kitty tab (tmux session stays alive) |
| `/` | Focus the filter input |
| `1-9` | Jump to kitty tab by number |
| `?` | Toggle help overlay |
| `q` | Quit TUI (all tmux sessions persist) |

## Tab Naming & Colors

### Title format

- Active claude session: `<short> — <claude session title>`
- Shell only: `<short>`
- Remote-control: `<short> ⟳`

The TUI periodically reads the tmux pane title to pick up Claude's session title and updates the kitty tab title via `kitty @ set-tab-title`.

### Colors

- **TUI tab (tab 0):** orange background — always visually distinct.
- **Workspace tabs:** use the `color` from config. Falls back to kitty default if unset.
- **Remote-control tabs:** use the repo's config color (same as workspace tabs, but the `⟳` in the title distinguishes them).

## Configuration

### Location

`~/.config/kittylauncher/config.yaml`

### Schema

```yaml
# Global settings
repos_dir: ~/repos              # Where to scan for repos
scratch_dir: /tmp/kl-scratch    # Where scratch instances live
default_action: claude           # "claude" or "shell"

# Per-workspace overrides (key = directory name in repos_dir)
workspaces:
  SliceWise:
    name: "SliceWize"            # Display name in TUI
    short: "SW"                  # Short name for tab title
    color: "#ff6b6b"             # Kitty tab color
    remote: true                 # Auto-start remote-control session
    favourite: true              # Pin to favourites section
  tgclaudebridge:
    name: "Telegram Bridge"
    short: "TGB"
    color: "#0088cc"
    remote: true
    favourite: true
  lom2:
    name: "Legend of Mir"
    short: "LOM"
    color: "#c792ea"
    favourite: true
```

### Auto-discovery

The TUI scans `repos_dir` on startup and lists all directories. Config overrides are matched by directory name. Repos without config entries display their directory name as-is and use default kitty tab colors. No config entry is needed for a repo to appear in the list.

### Defaults

- `name`: directory name
- `short`: first 3 characters of directory name, uppercased
- `color`: kitty default
- `remote`: false
- `favourite`: false

## Scratch Instances

### Creation

Press `s` in the TUI:

1. Creates directory: `/tmp/kl-scratch/scratch-<YYYYMMDD>-<NNN>` (incrementing number).
2. Creates tmux session: `kl-scratch-<NNN>`.
3. Launches claude (or shell, based on `default_action`).
4. Opens kitty tab with title `SCR-<NNN>`.
5. Appears in TUI list with a `tmp` badge.

### Promotion

Press `p` on a scratch entry:

1. TUI shows inline text input prompting for a name.
2. Moves the directory from `/tmp/kl-scratch/scratch-...` to `~/repos/<name>`.
3. Runs `git init` in the new location.
4. Renames the tmux session from `kl-scratch-<NNN>` to `kl-<name>`.
5. Updates the kitty tab title.
6. Removes the `tmp` badge.

### Cleanup

No automatic cleanup. Scratch dirs live in `/tmp` and are cleaned up by the OS on reboot. The TUI shows them while they exist.

## Notifications & Alerting

The TUI monitors all active tmux sessions and surfaces state changes through multiple channels.

### What triggers alerts

| Event | Detection | Severity |
|---|---|---|
| Remote-control session died | `tmux has-session -t kl-rc-<name>` fails | High |
| Claude session exited | tmux pane exited (session still alive, shell returned) | Medium |
| tmux session crashed | `kl-*` session disappears from `tmux list-sessions` | High |
| Claude waiting for input | tmux pane title changes / idle detection | Low |

### How alerts surface

1. **TUI badge** — the repo entry in the list gets a status indicator:
   - `✗` (red) for dead/crashed sessions
   - `⏳` for claude waiting for input
   - Normal status indicators for healthy sessions

2. **Tab flash** — on high-severity events, the TUI tab (tab 0) flashes by toggling `kitty @ set-tab-color` between orange and red briefly. Draws the eye even if you're in another tab.

3. **Tab title badge** — the affected workspace tab's title gets prefixed with an alert marker: `⚠ SW — fixing auth bug`. Cleared when the issue is resolved or acknowledged.

4. **Desktop notification** — `notify-send` for high-severity events (remote died, session crashed). Includes the workspace name and what happened. Low-severity events (claude idle) do not trigger desktop notifications to avoid spam.

### Polling

The TUI runs a background tick (every 5 seconds) that:

1. Runs `tmux list-sessions -F '#{session_name} #{session_attached} #{pane_dead}'` to check session health.
2. For active sessions, reads the tmux pane title to detect claude session title changes.
3. Updates the TUI list and kitty tab titles/colors as needed.

### Config

Notifications can be tuned in `config.yaml`:

```yaml
notifications:
  desktop: true          # Enable notify-send for high-severity events
  tab_flash: true        # Flash TUI tab on alerts
  poll_interval: 5       # Seconds between health checks
```

## Launch Script

`launch-kl.sh` — a shell script that:

1. Starts kitty with `--listen-on` flag to enable IPC: `kitty --listen-on unix:/tmp/kl-kitty-<PID> ...`
2. Sets the `KITTY_LISTEN_ON` environment variable for the TUI process.
3. Launches the TUI binary as the initial command in the first tab.
4. Sets tab 0 color to orange.

If kitty is already running with KL, the script focuses the existing window instead of launching a new one.

## Technology

- **Language:** Go
- **TUI framework:** Bubbletea + Bubbles (list, textinput, help) + Lipgloss (styling)
- **Config parsing:** `gopkg.in/yaml.v3`
- **Kitty IPC:** shell exec of `kitty @` commands (using `KITTY_LISTEN_ON` socket)
- **tmux control:** shell exec of `tmux` commands
- **Build:** single static binary, no runtime dependencies beyond kitty + tmux + claude

## Project Structure

```
kittylauncher/
├── main.go              # Entry point, arg parsing, launch
├── config.go            # YAML config loading, defaults, repo discovery
├── model.go             # Bubbletea model, state management
├── view.go              # All rendering (list, help overlay, filter)
├── keys.go              # Keybinding definitions
├── kitty.go             # kitty @ IPC wrapper functions
├── tmux.go              # tmux command wrapper functions
├── session.go           # Session lifecycle (create, attach, kill, reconnect)
├── scratch.go           # Scratch creation and promotion
├── notify.go            # Health polling, alerts, desktop notifications
├── launch-kl.sh         # Shell script to start kitty + TUI
└── config.example.yaml  # Example configuration
```
