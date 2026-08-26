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

## Worktree bootstrap

`git worktree add` materialises tracked files only, so a fresh worktree of a
PHP/JS project has no `.env`, no `vendor/`, no `node_modules/` and no
`public/build/` — everything gitignored but required is simply absent.

If a repo has `scripts/wt-init.sh` **and** its workspace has `Worktree init`
enabled (`E` in the manager, or `worktree_init: true` in the config), hive runs
that script before starting claude, in the visible tmux pane of the new
worktree.

**hive runs the parent checkout's copy of the script**, with the new worktree as
the working directory. This is deliberate: the trust flag is per repo, so the
script that runs is the one in the checkout you reviewed, not one that arrived
on a fetched PR or fork branch. It also means an old branch that predates the
script still gets bootstrapped.

The consequence for script authors is that `$PWD` is the new worktree but
`${BASH_SOURCE[0]}` points into the **parent**. Do not use
`dirname "${BASH_SOURCE[0]}"` to locate the worktree — that is the `gate.sh`
idiom and it is wrong here.

The script is passed:

- `$1` — absolute path of the parent checkout
- `$2` — the new branch name

Claude starts even if the script fails, so an agent is in the pane to read the
error.

Without the script, hive behaves exactly as before. With the script but without
the flag, hive prints a one-line notice into the pane and runs nothing.

Example `scripts/wt-init.sh`:

```bash
#!/usr/bin/env bash
#
# Prepare a fresh git worktree for work. Run by hive immediately after
# `git worktree add`, from the new worktree, before claude starts.
#
#   $1  absolute path of the parent checkout
#   $2  the new branch name
#
# NOTE: $PWD is the new worktree; BASH_SOURCE points into the parent.
#
# git worktree add materialises tracked files only, so everything gitignored
# but required has to be recreated here.

set -euo pipefail

parent="${1:?parent checkout path required}"

if [ -f "$parent/.env" ] && [ ! -f .env ]; then
	cp "$parent/.env" .env
fi

composer install
npm ci
npm run build
```

That example is illustrative, not a template to copy unchanged: `npm ci` versus
`npm install`, and `npm run build` versus `npm run dev`, are per-repo choices.

## Claude Code Plugin

Hive includes a Claude Code plugin that reports session lifecycle events:

```bash
claude --plugin-dir /path/to/hive/plugin
```

## License

MIT
