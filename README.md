# Hive

A terminal multiplexer TUI for managing multiple Claude Code sessions across repos. Built with [Bubbletea](https://charm.land/bubbletea) and tmux.

## What it does

Hive gives you a single dashboard to manage all your repos and Claude sessions:

- **Manager view** — browse repos, see which have active Claude/shell sessions, preview terminal output
- **Workspace view** — tabbed workspace with split panes showing live tmux capture
- **Worktree splits** — `ctrl+space v` creates a git worktree + Claude session as a side-by-side split
- **Session persistence** — tmux sessions survive restarts; hive reconnects and rebuilds the workspace layout
- **Mouse scrollback** — scroll wheel browses tmux history, any keypress snaps back to live

## Install

```bash
go install github.com/stevenlawton/hive@latest
```

Or build from source:

```bash
git clone https://github.com/stevenlawton/hive.git
cd hive
go build -o hive .
```

## Setup

Create a config file at `~/.config/hive/config.yaml`:

```yaml
repos_dir: ~/repos
scratch_dir: /tmp/hive-scratch
default_action: claude

workspaces:
  my-project:
    name: "My Project"
    short: "MP"
    color: "#ff6b6b"
    favourite: true
```

See [config.example.yaml](config.example.yaml) for all options.

## Keybindings

### Manager view

| Key | Action |
|-----|--------|
| `enter` | Open session (claude) |
| `shift+enter` | Open session (shell) |
| `r` | Toggle remote session |
| `s` | Create scratch instance |
| `w` | Create worktree |
| `tab` | Toggle preview/diff |
| `/` | Filter repos |
| `E` | Edit repo config |
| `F` | Toggle favourite |
| `A` | Toggle archive |
| `x` | Kill session |
| `d` | Detach session |
| `?` | Help |
| `q` | Quit |

### Workspace view

All keys are forwarded to the focused tmux session except chord sequences:

| Chord | Action |
|-------|--------|
| `ctrl+space q` | Return to manager |
| `ctrl+space v` | Vertical split (new worktree) |
| `ctrl+space h` | Horizontal split (new worktree) |
| `ctrl+space n/p` | Next/previous tab |
| `ctrl+space 1-9` | Jump to tab |
| `ctrl+space left/right` | Focus split |
| `ctrl+space x` | Kill focused split |
| `ctrl+space f` | Fullscreen attach |

Mouse scroll wheel browses tmux scrollback history.

## Claude Code Plugin

Hive includes a Claude Code plugin that reports session lifecycle events:

```bash
claude --plugin-dir /path/to/hive/plugin
```

## License

MIT
