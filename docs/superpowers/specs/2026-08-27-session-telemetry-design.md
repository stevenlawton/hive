# Session telemetry in hive — context, cost and heat

**Status:** design, awaiting review
**Date:** 2026-08-27

## Why

hive can see which sessions exist and what they claim, but nothing about their
condition. The two questions you cannot answer from the drawer today are "which
of these nine sessions is about to fall off a context cliff" and "which one has
quietly become expensive". Both answers already arrive on hive's doorstep every
few seconds and are thrown away.

`~/.claude/settings.json` points `statusLine` at `hive todo statusline`. Claude
Code pipes a JSON payload to that command on every refresh, for every live
session. `statuslineCwd` (`cmd_todo.go:604`) decodes exactly two fields out of
it — `cwd` and `workspace.current_dir` — and discards the rest, which includes
cost, context-window occupancy and rate-limit headroom.

## What the payload contains

Read from the statusline payload builder in the installed CLI
(`~/.local/share/claude/versions/2.1.247`, the build currently running):

| Group | Fields |
|---|---|
| cost | `total_cost_usd`, `total_duration_ms`, `total_api_duration_ms`, `total_lines_added`, `total_lines_removed` |
| context_window | `total_input_tokens`, `total_output_tokens`, `context_window_size`, `current_usage`, `used_percentage`, `remaining_percentage` |
| rate_limits | `five_hour{used_percentage, resets_at}`, `seven_day{used_percentage, resets_at}` |
| identity | `session_id`, `transcript_path`, `cwd`, `session_name`, `version` |
| model | `model.id`, `model.display_name`, `fast_mode`, `effort.level`, `thinking.enabled`, `exceeds_200k_tokens` |
| place | `workspace.{current_dir, project_dir, added_dirs, git_worktree, repo}`, `worktree{name, path, branch, original_cwd, original_branch}` |
| extras | `agent.name`, `pr{number, url, review_state, kind}`, `remote.session_id`, `output_style.name`, `vim.mode` |

`used_percentage` and `remaining_percentage` arrive precomputed, so hive never
needs to know the context window size or the autocompact reserve.

Two properties of this source shape the whole design. It is a **push**, so hive
gets it for free but only while a session is alive and refreshing. And it is
**current-value only** — no history, no per-turn breakdown. Per-turn duration,
compaction events and rate-limit rejections all live in the transcript JSONL
instead; reading that is out of scope here (see Non-goals).

### Version risk

The payload shape was read out of a specific CLI build, not captured live. It is
accurate for 2.1.247 and the fields are long-standing, but the decoder must
treat every field as optional and degrade to today's behaviour when one is
absent. A payload from a future build that drops or renames a field must never
break the statusline.

## Non-goals

- **Reading transcript JSONL.** Rich, and it survives the session, but it is a
  separate collector with its own cost model and parsing. Not in v1.
- **Historical charts or trends.** v1 stores the latest value per session and
  nothing else.
- **Cost attribution across sessions, or budgets and alerts.** Display only.
- **Rate limits driving the colour.** Explicitly excluded — they render as text.

## Architecture

### Collector

`runTodoStatusline` gains a second job: decode the full payload, derive what the
TUI will need, and write a snapshot file. Constraints, in priority order:

1. **It must never break the statusline.** Every telemetry failure — malformed
   payload, unwritable directory, full disk — is swallowed, and the line renders
   as it does today.
2. **It must stay fast.** The line already does `loadTodos` plus a `git
   rev-parse` in `worktreeClaim` on every tick. Telemetry adds one atomic write
   and no subprocesses.
3. **It must not need a lock.** One writer per session id, so there is no
   contention to arbitrate.

### Storage

Per-session JSON at `$XDG_RUNTIME_DIR/hive/sessions/<session_id>.json`, written
via write-temp-then-rename. This follows `repoKeyMemoPath` (`repo_key.go:81`),
falling back to `os.TempDir()`. Runtime, not data: a snapshot of a session's
context usage is worthless after a reboot, and putting it here means the OS
clears it for us.

Rejected alternatives:

- **One append-only JSONL for all sessions.** Needs a lock on the hot path,
  grows without bound, and forces the TUI to parse history to find the present.
  Only the latest value per session is ever wanted.
- **The existing todo store.** `todo_backup.go` commits that store to a git
  repo. Telemetry writes on every statusline tick would produce a commit storm.

### Record

```json
{
  "session_id": "945515c2-…",
  "captured_at": "2026-08-27T09:44:11Z",
  "tmux_session": "hive-workspace",
  "cwd": "/home/steve/repos/workspace",
  "project_dir": "/home/steve/repos/workspace",
  "worktree": {"name": "split-2", "branch": "split-2"},
  "model": "claude-opus-5",
  "effort": "high",
  "context": {"used_pct": 31.4, "size": 1000000, "input_tokens": 313904, "output_tokens": 8213},
  "cost": {"total_usd": 0.42, "wall_ms": 4210000, "api_ms": 1080000},
  "rate_limits": {"five_hour": {"used_pct": 81.0, "resets_at": 1787685000}},
  "heat": 31.4
}
```

`heat` is stored as well as computed so the TUI does no arithmetic per frame and
both surfaces agree by construction.

### Join key

To tint the right pane, a snapshot must name a tmux session. The statusline
process knows its own identity, so it resolves `workspace.project_dir` → repo
`DirName` → `TmuxSessionName(dirName, false)` (`tmux.go:75`) at write time and
records the result. The TUI does no reverse lookup and no guessing.

Risk: `project_dir` for a session started inside a worktree may be the worktree
rather than the parent, and hive names worktree sessions after the worktree dir.
Both cases resolve through the same helper, but this is the part most likely to
be subtly wrong, so it gets direct tests against both layouts.

## Heat

A single composite, per the decision on 2026-08-27:

```
heat = 100 · ( w_ctx·(ctx_pct / 100)
             + w_cost·(cost_usd / cost_full)
             + w_age·(api_ms / age_full) )
```

on a 0–100 scale, clamped. Defaults: weights `0.6 / 0.2 / 0.2`, `cost_full` $10.00,
`age_full` 120 minutes. Thresholds: green below 50, amber 50–80, red above 80.

Two notes on the inputs:

- **Age uses `total_api_duration_ms`, not wall clock.** A real session measured
  during research showed 42.17h of wall against 1.57h of turn time — 4%. Wall
  clock would peg every long-lived session red for having existed, which is not
  information.
- **Cost is a running total and only ever rises**, so its contribution is a
  ratchet within a session. That is intended, but it means `cost_full` is the
  knob that decides how quickly a long session drifts amber regardless of what
  it is doing.

The known weakness of a composite — that a red light does not say *why* — is
mitigated by the chosen rendering, which shows all three parts as text beside
the bar. The colour summarises; the numbers explain. If the weights turn out to
be unsatisfying in use, all six values are config, not code.

### Config

```yaml
telemetry:
  enabled: true
  heat:
    weights: {context: 0.6, cost: 0.2, age: 0.2}
    cost_full_usd: 10.0
    age_full_minutes: 120
    amber_at: 50
    red_at: 80
  rate_limit_floor_pct: 60
  stale_after_seconds: 30
  prune_after_hours: 24
```

`enabled: false` restores exactly today's behaviour on every surface.

## Rendering

### Statusline

Bar **length** tracks context %; bar **colour** carries composite heat. A bar
reads as fullness, so tying its length to the composite would let it disagree
with the number printed beside it whenever cost or age were driving the colour.

```
▸ Hive web: see the backlog… · 3/12 · ███░░░░░░░ 31%  $0.42  18m         (green)
▸ Hive web: see the backlog… · 3/12 · ███████░░░ 74%  $0.51  22m         (amber)
▸ Hive web: see the backlog… · 3/12 · █████████░ 93%  $2.10  2h  5h:81%  (red)
```

The five-hour figure appears only above `rate_limit_floor_pct`, so the common
case stays short. When no payload has been seen — a session that predates this
change, or `enabled: false` — the line is exactly what it is today.

### Split pane

`TerminalPane.View` (`ui/terminal.go:159`) already recomposes captured content
line by line, and `sgrCarry` (`ui/ansistate.go`) exists to manage background
carry-over across those lines. The heat tint wraps that composition.

Measured safe: capturing three live Claude panes with `capture-pane -e` and
counting background SGR codes gave 0 of 37, 0 of 37 and 0 of 73 lines setting a
background. Claude Code paints foreground only, even in `fullscreen` TUI mode,
so a tint shows on every cell.

Kept deliberately subtle. Claude's foreground palette was chosen against the
terminal's own background, and a saturated wash costs contrast on text you are
trying to read.

### Full attach

`AttachSession` (`ui/attach.go:24`) pipes the raw PTY, so hive is not
compositing and cannot paint. `tmux set-option -t <session> window-style
'bg=…'` tints from tmux's side instead, set on entry and cleared on exit. hive
sets no tmux styles today, so this introduces the first — it must be reverted on
every exit path, including the error paths, or a stale tint outlives the reason
for it.

### Border and tab

`BorderStyle` / `FocusedBorderStyle` (`ui/styles.go:20`) and `ui/tabbar.go` are
hive's own pixels and cost no contrast against session content. They carry the
loud end of the scale: a red border and a red tab are readable at a glance
across a nine-session drawer in a way a subtle background wash is not.

### Staleness

A session with no write inside `stale_after_seconds` is idle, backgrounded or
dead. It renders grey rather than holding its last heat, because a stale red is
worse than no colour — it sends you to a session that finished ages ago.
Snapshots older than `prune_after_hours` are deleted on TUI start.

## Failure modes

| Failure | Behaviour |
|---|---|
| Payload missing or unparseable | Statusline renders as today; no snapshot written |
| A field absent (version drift) | That part contributes 0 to heat; others still render |
| Snapshot dir unwritable | Silent; statusline unaffected; TUI shows all sessions grey |
| Snapshot stale | Grey, not last-known-heat |
| tmux `window-style` fails | Attach proceeds untinted |
| `telemetry.enabled: false` | Collector and all tints off; today's behaviour exactly |

## Testing

Pure and table-tested: the heat function (including clamping and a zeroed
weight), the threshold-to-colour mapping, the bar renderer at 0/50/100 and at
narrow widths, and the payload decoder against a full payload, an empty object
and a payload with every optional field missing.

Round-tripped through a temp dir: the collector's write-then-read, plus the
stale and prune boundaries with an injected clock.

Asserted as arguments through the existing `tmuxRun` seam, the way
`tmuxNewSessionArgs` already is: the `window-style` set on attach and its clear
on exit, including the error path.

Tinting itself is visual and is verified by eye at the gate below, not asserted
in a test.

## Verify before building

One assumption is load-bearing and unproven: **that Claude Code renders ANSI
colour in the statusline at all.** The docs say yes; I could not confirm it from
the CLI bundle. If it strips escapes, the statusline half of this design
collapses to plain text and the whole visual scale lives in the TUI instead.

First implementation step is therefore a throwaway: emit one coloured statusline
from the worktree build, merge nothing, and look at it. Everything else waits on
that answer.

## Open questions

1. **Where does heat go for a session with no hive tmux pane** — a Claude
   session started outside hive still writes snapshots. Show it in the drawer as
   an unmanaged session, or ignore it? Leaning ignore for v1.
2. **Subagent sessions.** The payload carries `agent.name` when a subagent is
   driving. Roll its cost into the parent's heat, or show separately? Not
   resolved; v1 records the field and ignores it.
3. **`cost_full` default.** $10 is a guess. It wants a week of real numbers.

## Rollout

Built in a git worktree, merged to `main` to go live — `main` in the primary
checkout is what recompiles on restart, so an unmerged worktree is invisible no
matter how many times hive is restarted.
