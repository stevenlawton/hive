# Hive Plugin

Bridges Claude Code sessions with the Hive TUI.

## What it does

Reports session lifecycle events (start, stop, end) to status files at `/tmp/kl-status/`. Hive polls these files to show rich session status, trigger notifications, and manage tab colors.

## Status file format

Each active session writes to `/tmp/kl-status/<tmux-session-name>.json`:

```json
{
  "session": "hive-workspace",
  "repo": "workspace",
  "status": "running",
  "started_at": "2026-03-28T16:00:00Z",
  "updated_at": "2026-03-28T16:05:00Z",
  "tool_count": 0
}
```

## Hook events

| Event | Action |
|-------|--------|
| SessionStart | Creates status file, marks session as "running" |
| Stop | Updates status to "completed", records duration |
| SessionEnd | Final status update, marks "ended" |

## Installation

```bash
claude --plugin-dir /path/to/hive/plugin
```

Or add to your Claude Code settings for permanent use.
