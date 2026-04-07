# Hive — Terminal Session Manager & Multiplexer

**Date:** 2026-04-07
**Status:** Design approved
**Replaces:** KittyLauncher

## Overview

Hive is a terminal-based Claude session manager and multiplexer built on Bubbletea v2. It replaces KittyLauncher by removing the kitty terminal dependency and bringing all session management, viewing, and interaction into a single TUI application. tmux remains the session engine.

## Core Principles

- **tmux stays** — sessions are tmux sessions, other apps (Telegram bridge, remote tools) depend on them
- **No kitty dependency** — no `kitten @` IPC calls, runs in any terminal
- **Two views** — Manager (overview) and Workspace (interactive)
- **Tabs = projects, splits = instances** — mental model matches current kitty tabs/splits
- **Chord navigation** — `Ctrl+Space` prefix for all workspace commands

## Views

### Manager View (default)

```
+-- Session List ----------+-- Preview --------------------------+
| -- active --             | $ claude                            |
| > SliceWize  * +42/-13   | I've updated the auth middleware    |
|   polybot    * +8/-2     | to handle token refresh...          |
| -- favourites --         |                                     |
|   telegram   (tg)       |                                     |
| -- repos --              +-- [Preview] [Diff] -----------------+
|   workspace  o           |                                     |
|   manuscripts o          |                                     |
|                          |                                     |
| -- notifications --      |                                     |
| SW completed 2m ago      |                                     |
| polybot waiting 5m       |                                     |
+--------------------------+-------------------------------------+
| 2 active  1 waiting  3 idle | enter:open  s:scratch  ?:help   |
+---------------------------------------------------------------|
```

- **Left pane:** Session list grouped by status (active > favourites > repos > archived), notification log at bottom
- **Right pane:** Tabbed between Preview (capture-pane output) and Diff (working tree diff) for selected session
- **Status bar:** Session counts + context-sensitive keybindings
- Standard keyboard navigation (same as current KittyLauncher: Enter, s, w, x, /, etc.)

### Workspace View (after opening a session)

```
+-- [SliceWize] [polybot] [workspace] ---------------------------+
| +-- main -------------------+-- wt:feature-auth --------------+ |
| | $ claude                  | $ claude --prompt "fix auth"   | |
| |                           |                                 | |
| | I've finished the         | Looking at the auth module...  | |
| | refactor. Ready for       |                                 | |
| | review.                   | Let me check the tests...      | |
| |                           |                                 | |
| +---------------------------+---------------------------------+ |
+-----------------------------------------------------------------+
| Ctrl+Space q:manager  n:next-tab  v:vsplit  x:close            |
+-----------------------------------------------------------------+
```

- **Tab bar:** One tab per open project, shows short name + status indicator
- **Splits:** One pane per Claude/shell instance within the project
- **Focused split** has highlighted border and receives keystrokes
- **Unfocused splits** update via capture-pane polling

## Terminal Capture & Interaction

### Preview Mode (Manager View)

- Poll `tmux capture-pane -p -e -t <session>` every 500ms
- ANSI colours preserved via `-e` flag
- Truncated to pane height (last N lines)
- Read-only, no input forwarding
- Scroll mode via `Shift+Up/Down`, `Esc` to exit

### Interactive Mode (Workspace View splits)

- `capture-pane` for rendering (polling ~100ms for responsiveness)
- Keystrokes forwarded via `tmux send-keys -t <session>`
- `Ctrl+Space` chord intercepted before forwarding
- Only the focused split receives input

### Full-Screen Attach (escape hatch)

- `Ctrl+Space, f` — PTY attach to tmux session, TUI steps aside
- Raw stdin/stdout piped to tmux session
- `Ctrl+Space` detaches back to workspace view
- For heavy editing, vim, complex interactions requiring full terminal fidelity

### Rationale

`send-keys` + `capture-pane` is simpler and more reliable than PTY piping for multiple simultaneous panes. Full-screen PTY attach covers the cases where true fidelity is needed. This is the same hybrid approach claude-squad uses.

## Chord Input System

Prefix: `Ctrl+Space`

On keypress:
1. Enter "chord pending" state
2. 500ms timeout — if no second key, cancel and forward `Ctrl+Space` to session
3. Second key arrives — execute action, never forward to session

### Chord Map

| Chord | Action | Context |
|-------|--------|---------|
| `Ctrl+Space, q` | Return to manager view | Workspace |
| `Ctrl+Space, n` | Next tab | Workspace |
| `Ctrl+Space, p` | Previous tab | Workspace |
| `Ctrl+Space, 1-9` | Jump to tab N | Workspace |
| `Ctrl+Space, v` | Vertical split | Workspace |
| `Ctrl+Space, h` | Horizontal split | Workspace |
| `Ctrl+Space, arrow` | Move focus between splits | Workspace |
| `Ctrl+Space, x` | Close focused split | Workspace |
| `Ctrl+Space, f` | Full-screen attach | Workspace |
| `Ctrl+Space, w` | Spawn worktree (new split) | Workspace |
| `Ctrl+Space, d` | Detach split (keep running) | Workspace |

In manager view: no chord needed, standard keys work directly.
In full-screen attach: `Ctrl+Space` is the only intercepted key.

## Tab & Split Management

### Tabs

- One tab per open project
- Created when opening a session from manager, or focused if already exists
- Tab bar shows short name + status indicator (e.g. `SW *`, `PB o`)
- Tabs with notifications flash/highlight
- Closing a tab does not kill sessions — they continue in background

### Splits

- First session opens full-width
- Worktree spawn (`w`) or shell open (`Shift+Enter`) adds vertical split
- Manual split via `Ctrl+Space, v/h`
- Equal sizing by default — 2 splits = 50/50, 3+ splits = equal vertical columns
- Closing a split does not kill the session unless explicitly killed

### Focus Model

- One split focused at a time (highlighted border)
- Only focused split receives keystrokes
- Unfocused splits still update via capture-pane polling
- `Ctrl+Space, arrow` moves focus

## Notifications

### In-TUI

- **Session list badges:** status indicators per entry (`*` running, `o` waiting, `+` completed, `x` crashed) with diff stats inline (+N/-M)
- **Tab bar badges:** flash/highlight on background session events
- **Status bar counter:** `2 active  1 waiting  1 completed`
- **Notification log:** Bottom of session list in manager view, scrollable, newest first

### External (unchanged from KittyLauncher)

- Desktop notifications via `notify-send`
- Sound on completion/crash
- ntfy.sh push
- Slack webhook
- Custom webhook POST

### Event Sources (unchanged)

- Plugin hooks via HTTP server on :9199
- tmux bell flag detection (5s polling)
- Dead session detection
- Status files in `/tmp/kl-status/` (migrates to `/tmp/hive-status/`)

## Diff Pane

- Working tree diff only (uncommitted changes)
- Computed in background goroutine to avoid blocking UI
- Colorized: green for additions, red for deletions, cyan for hunks
- Diff stats shown inline on session list entries (+N/-M)
- Scrollable via viewport

## Session Types

- **Claude** — `claude` or `claude --permission-mode bypassPermissions` (yolo)
- **Shell** — plain shell in the repo directory
- Architecture supports adding more program types later

## Migration from KittyLauncher

### Session Adoption

On first run, Hive scans for `kl-*` tmux sessions and adopts them:
- `kl-<repo>` interactive sessions
- `kl-rc-<repo>` remote-control sessions
- `kl-scratch-*` scratch sessions
- `kl-<repo>-wt-<branch>` worktree sessions

New sessions created with `hive-*` prefix. Old `kl-*` sessions age out naturally as they're killed and recreated.

### Config Migration

- Reads from `~/.config/kittylauncher/config.yaml` if `~/.config/hive/config.yaml` doesn't exist
- Copies config to new location on first run
- Config format unchanged

### Status Files

- Reads from both `/tmp/kl-status/` and `/tmp/hive-status/`
- Plugin updated to write to `/tmp/hive-status/`

### Rename

- Directory: `kittylauncher/` -> `hive/`
- Binary: `kl` -> `hive`
- Go module updated

## What Gets Removed

### kitty.go — deleted entirely

All `kitten @` IPC calls:
- KittyLaunchTab, KittySetTabColor, KittyFocusTab, KittyCloseTab
- KittyListTabs, KittySetTabTitle, KittyFocusTabByIndex, KittyResetTabColor

### Kitty calls in other files

- session.go: no more `kitten @ launch --type=tab`, `kitten @ close-tab`
- worktree.go: no more `kitten @ launch --type=window --match`
- notify.go: no more `kitten @ set-tab-color --self` for tab flashing

### launch-kl.sh -> launch.sh

Updated to be terminal-agnostic. Still useful for setting up a nice terminal window but not required.

## What Stays Unchanged

- `tmux.go` — all tmux wrappers
- `config.go` — config loading, repo discovery, defaults
- `server.go` — HTTP event server on :9199
- `bridge.go` — Telegram bridge integration
- `scratch.go` — scratch workspace creation/promotion
- `archive.go` — archive/unarchive repos
- `edit.go` — edit panel for repo metadata
- `plugin/` — hooks, scripts, status files

## File Structure

```
hive/
  main.go              — entry point
  config.go            — config loading, repo discovery
  model.go             — top-level Bubbletea model, view routing
  session.go           — session lifecycle (no kitty calls)
  tmux.go              — tmux wrappers (unchanged)
  notify.go            — notifications (no kitty flash)
  server.go            — HTTP event server (unchanged)
  bridge.go            — Telegram bridge (unchanged)
  scratch.go           — scratch workspaces (unchanged)
  archive.go           — archive/unarchive (unchanged)
  edit.go              — edit panel (unchanged)
  worktree.go          — git worktree ops (no kitty calls)
  keys.go              — keybindings + chord handler
  migrate.go           — kl-* session adoption

  ui/
    manager.go         — manager view (list + preview + diff + notifications)
    workspace.go       — workspace view (tabs + splits)
    tabbar.go          — tab bar component
    splitpane.go       — split pane layout component
    terminal.go        — capture-pane rendering + send-keys input
    preview.go         — preview pane (read-only capture)
    diff.go            — diff pane (git diff rendering)
    notifylog.go       — notification log component
    attach.go          — full-screen PTY attach mode

  plugin/              — unchanged

  config.example.yaml
  launch.sh            — generic launch script
  go.mod
```

## Future Considerations (not in initial build)

- **Pause/resume sessions** — commit WIP, free worktree, recreate on resume
- **Multi-agent profiles** — support for Aider, Codex, etc. per session
- **Drag-resize splits** — mouse-based split resizing
- **Session search** — search across all session outputs
- **Session recording** — capture full session history for replay
