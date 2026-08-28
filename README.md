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
| `ctrl+space c` | Canned prompts for the focused session |
| `ctrl+space u` | Future prompts for the focused session |

Mouse scroll wheel browses tmux scrollback history.

### Canned prompts

Right-click a session pane (or `ctrl+space c`) for a popup of pre-written
prompts. Pick one with `1`-`9` / `0`, the arrow keys and Enter, the scroll
wheel, or a click; `esc` closes it without sending. If claude is mid-turn in
that pane hive sends Escape first, so the prompt interrupts the work rather
than queueing behind it — an idle pane is never interrupted.

The list lives in `~/.config/hive/canned.yaml`, seeded with defaults on first
use and re-read every time the popup opens, so hand-edits need no restart:

```yaml
prompts:
  - label: tests
    text: run the tests and fix whatever fails
```

`a` adds an entry, `e` edits the one under the cursor, `d` deletes it, and
`J`/`K` move it up and down the list — the order in the file is the order of
the number keys. Prompt text is one line: newlines would submit it half-typed,
so they are flattened to spaces on load.

### Future prompts

Canned prompts send now; future prompts park for later. When the account's
five-hour quota is spent every session is stuck until the window rolls over,
which is exactly when the next thing to say tends to occur to you.

`ctrl+space u` — or `ctrl+u` from the canned popup, which right-click already
opens — brings up the parked queue for that pane. Type a note and press Enter
to park it; `ctrl+d` deletes the one under the cursor; `esc` saves and closes.

Two tickboxes:

- **auto send** (`ctrl+s`) is ticked on open. It arms the queue against the
  five-hour reset, and the popup shows when the first prompt will actually go.
  Untick it and the notes just sit there as a reminder you send by hand.
- **auto resume** (`ctrl+r`) swaps the queue for a single canned payload,
  `resume` by default, and greys the editor out. Untick it and your notes come
  back untouched. Change the payload with `auto_resume_text` in the file below.

The quota is account-level, so the reset time is read from whichever session
reported most recently rather than from the pane the popup is over — a session
blocked on the limit stops reporting, and would have nothing to say.

That leaves one blind spot: if *every* session is stalled, nothing is rendering
a statusline and there is no telemetry to read. So hive falls back to the pane
itself, which is still showing claude's own limit message, and takes the reset
time from there — the header says `(from the pane)` when it did. Only if both
come up empty does the popup say the reset time is unknown, and it then refuses
to arm rather than promising a send that could never fire.

The banner is matched on shape rather than exact wording — a line naming a
limit and when it resets — because that wording has changed before and is not
worth pinning.

Firing waits five minutes past the published reset: a rolling window's reset
time has been seen landing slightly early, and a prompt typed into a session
that is still blocked is swallowed with nothing to retry against. Only the
first prompt goes then; the rest follow as the session finishes each turn, so
a queue of four does not land in one input box at once.

Arming is spent by any resume. If you type into the pane yourself before the
window rolls over, hive sees the session generating and cancels the send — the
parked notes stay put, but nothing is fired into work that has already moved
on.

The queues live in `~/.config/hive/future.yaml`, keyed by tmux session, and
survive a restart:

```yaml
auto_resume_text: resume
queues:
  hive-workspace-split-6:
    prompts:
      - check the bus, then carry on with the parked work
    auto_send: true
    armed_for: 1756400400
```

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
