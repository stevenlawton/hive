# KittyLauncher Feature Roadmap

## Current Features

- [x] Repo discovery from ~/repos with config overrides (name, short, color)
- [x] Tmux session management (create, attach, kill, detach)
- [x] Kitty tab integration (launch, color, title sync, focus by index)
- [x] Claude + shell session modes
- [x] Remote-control sessions (background `claude remote-control`)
- [x] Auto-start configured remotes on launch
- [x] Session reconnection on restart (tmux sessions survive kill)
- [x] Scratch workspaces (create in /tmp, promote to ~/repos)
- [x] Collections (nested repo groups)
- [x] Favourites + display ordering (active > favourites > repos)
- [x] Fuzzy filter search (/)
- [x] Edit panel (name, short, color, remote, favourite, collection flags)
- [x] Dead session detection + desktop notifications
- [x] Tab flash on alerts (bell detection for Claude waiting)
- [x] Help overlay (?)

## UI Overhaul (In Progress)

- [x] Color-coded status (green claude, blue shell, orange remote, red dead, yellow waiting, dim idle)
- [x] Bordered list panel (full terminal width)
- [x] Status info bar (session counts + selected path)
- [x] Two-row key bar (all bindings visible)
- [x] Mouse support (click to select, click key bar, scroll wheel)
- [x] Help screen shows all bindings (R, F, E added)
- [x] Arrow-down to navigate while filtering

## Planned Features

### KittyLauncher Claude Plugin (hooks + IPC)
A Claude Code plugin that hooks into session lifecycle events. Foundation for notifications, status tracking, and session awareness.

**Hook events to use:**
- `SessionStart` → write session metadata to `/tmp/kl-status/<session>.json`, set env vars via $CLAUDE_ENV_FILE
- `Stop` → update status file (completed), trigger notifications
- `SessionEnd` → final cleanup, log duration
- `PostToolUse` → track tool activity (optional, for detailed status)
- `Notification` → route Claude notifications to KittyLauncher

**IPC approach:** Status files at `/tmp/kl-status/<tmux-session-name>.json`
```json
{
  "session": "kl-workspace",
  "repo": "workspace",
  "status": "running|completed|errored|waiting",
  "started_at": "2026-03-28T16:00:00Z",
  "updated_at": "2026-03-28T16:05:00Z",
  "tool_count": 42,
  "last_tool": "Edit"
}
```

KittyLauncher polls these alongside its existing 5s health tick.

**Tasks:**
- [ ] Create plugin scaffold (plugin.json, hooks/hooks.json, scripts/)
- [ ] SessionStart hook — writes status file, sets KL_SESSION env var
- [ ] Stop hook — marks status completed, writes duration
- [ ] SessionEnd hook — final status update
- [ ] KittyLauncher: poll /tmp/kl-status/ in handleTick()
- [ ] KittyLauncher: update repoItem with rich status from status files
- [ ] KittyLauncher: show "completed" / "errored" states in the list

### Yolo Mode
Auto-approve tool calls when launching Claude sessions. Uses `--permission-mode bypassPermissions`.

**Implementation:** Claude already supports this via CLI flags. KittyLauncher just needs to pass the right args.

- [ ] Add `yolo` flag per-repo in config.yaml + WorkspaceConfig
- [ ] Toggle yolo per-repo from TUI (keybinding, e.g., `Y`)
- [ ] Pass `--permission-mode bypassPermissions` when launching Claude in yolo mode
- [ ] Visual indicator in list (⚡ badge or colored status modifier)
- [ ] Global yolo toggle in config for "launch everything in yolo"
- [ ] Edit panel: add yolo toggle checkbox

### Git Worktrees + Parallel Sessions
Spawn multiple Claude sessions in the same repo using git worktrees. Multiple sessions share one kitty tab via splits.

**Key discovery:** Claude Code has built-in `--worktree [name]` and `--tmux` flags. KittyLauncher can leverage these.

**Kitty splits model:** Instead of 1 repo = 1 tab, a repo tab can have multiple kitty windows (splits). `kitten @ launch --type=window --match title:^REPO` creates a split in the existing tab.

- [ ] Key to spawn a worktree session (e.g., `w` — prompts for branch/name)
- [ ] Use `claude --worktree <name>` to let Claude handle worktree creation
- [ ] New split in existing kitty tab: `kitten @ launch --type=window --match title:^SHORT`
- [ ] Each split gets its own tmux session (e.g., kl-repo-wt-branchname)
- [ ] Worktree sessions show as children of parent repo in the list (indented, like collections)
- [ ] Track worktree paths for cleanup on kill (`git worktree remove`)
- [ ] Worktree status in list: branch name + session state
- [ ] Kill worktree: close split, kill tmux session, remove worktree
- [ ] Max concurrent worktrees per repo (configurable, default 3)

### Archive / Move Projects
Move repos to ~/repos/.archive/ to hide from main list. Reversible.

- [ ] Archive keybinding (e.g., `A`) — moves directory to ~/repos/.archive/
- [ ] Create .archive dir on first use
- [ ] Kill any active sessions before archiving
- [ ] Archived repos hidden from main list by default
- [ ] Toggle to show archived repos (dimmed/separate "archived" section)
- [ ] Unarchive action — moves back to ~/repos/
- [ ] Preserve config.yaml entry, update paths
- [ ] Confirmation prompt before archive (destructive-ish action)

### Notifications — Sound
Audible alerts for key events. Driven by status file changes from the plugin.

- [ ] Play sound when Claude finishes (Stop hook → status file → KittyLauncher detects)
- [ ] Play sound on session crash/error
- [ ] Play sound when Claude is waiting for input (existing bell detection)
- [ ] Configurable per-event in config.yaml (enable/disable, sound file path)
- [ ] Default: system bell via `printf '\a'` or `paplay`/`aplay` for custom sounds

### Notifications — Status Tracking
Track Claude session outcomes with timestamps. Notification history in the TUI.

- [ ] Log events from status files: started, completed, errored, waiting, crashed
- [ ] Timestamps for each event
- [ ] Notification log view in TUI (new `viewLog` mode, keybinding `l`)
- [ ] Per-repo event history
- [ ] Session duration tracking (started_at → completed_at)
- [ ] Summary in status bar: "3 completed today, 1 errored"

### Notifications — External Integrations
Remote monitoring when away from the terminal.

- [ ] Webhook support (POST JSON to a URL on events)
- [ ] ntfy.sh push notifications (simple HTTP POST)
- [ ] Slack incoming webhook
- [ ] Configurable per-event type and per-repo
- [ ] Rate limiting to avoid notification spam
- [ ] Config section:
  ```yaml
  notifications:
    desktop: true
    tab_flash: true
    sound: true
    webhook_url: ""
    ntfy_topic: ""
    slack_webhook: ""
  ```
