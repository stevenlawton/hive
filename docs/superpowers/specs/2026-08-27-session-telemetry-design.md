# Hand off, or keep going — session verdicts in hive

**Status:** design, awaiting review
**Date:** 2026-08-27

## The question this answers

One question, per session: **does this session need to hand its work off so we
pick it up elsewhere, or is it good to keep going?**

Not a dashboard. Steve's framing, 2026-08-27: "all i care about - does this
session need to hand off work and we pick it up again else where ... or we good
to keep going". Context percentage, burn rate, cost and idle time are all
*inputs* to that verdict. Only the verdict and one line of reasoning are output.

An earlier draft of this spec proposed a weighted composite "heat" score across
context, cost and age. It is dropped. Measurements below showed it would dilute
the very signal it existed to raise, and a score does not answer the question
anyway — it makes you do the interpretation the tool should have done.

## Why context size is the whole story

Every turn re-reads the entire window from cache, so per-turn cost scales with
how full the context is. Measured on a real session, priced at Opus 5 rates
($5/$25 per MTok, cache write 1h at 2×, cache read at 0.1×):

```
context bucket   msgs   mean $/msg   vs first bucket
   0- 100k       68     $0.0838        1.0x
 100- 200k       82     $0.1833        2.2x
 200- 300k      169     $0.1932        2.3x
 300- 400k      167     $0.2594        3.1x
 400- 500k       74     $0.3172        3.8x
 500- 600k       81     $0.6781        8.1x
```

That session opened at 42,721 tokens and ended at 555,680 — 13×. The same turn
that cost 8p at the start cost 68p at the end. This is why handing off works:
you are not economising, you are resetting the multiplier.

### Idle time is not harmless

Leave a session sitting and the cache entry expires; the next turn re-writes the
whole window at the 2× premium instead of reading it at 0.1×. Same session,
bucketed by the gap before each message (turns above 50k context only):

```
gap since       n    mean cache_WRITE   mean cache_read    mean $/msg
<1m           599            6,921          296,577    $0.2470
1-5m           23            1,391          289,626    $0.2759
5m-1h           7           37,843          283,822    $0.5476
>4h             4          390,751                0    $3.9483
```

Past four hours `cache_read` is **zero** — total eviction — and the turn costs
16× the same turn resumed within a minute. Four messages cost ~$15.60 between
them, 9% of a $175 session, purely as resume tolls.

Confirmed live on a second session: after a 950-minute overnight gap, the first
message re-wrote 102,441 tokens with `cache_read: 0`, costing $1.05 to say
hello.

### The other cliff

Context does not degrade gracefully; it falls over. A `compact_boundary` record
from a real transcript: `preTokens: 1000077`, `postTokens: 26590`,
`cumulativeDroppedTokens: 973487`, `durationMs: 126529`. A two-minute stall and
973k tokens discarded, on the API's schedule rather than yours. A handoff you
control beats a compaction you do not.

## The verdict

Four states. Each session is in exactly one.

| Verdict | Meaning |
|---|---|
| **keep going** | Turns are cheap, room to spare. Carry on. |
| **wrap up** | Finish what is in flight; do not start anything large here. |
| **hand off** | Continuing costs materially more than restarting. Move the work. |
| **park** | The 5-hour quota is nearly gone *and* this session is cheap to resume. Stop; pick it up after reset. |

`park` is an **override**, not a fourth score, and it is **conditional** — a big
session near the quota must hand off rather than park. See The 5-hour window
below. This keeps the earlier decision intact: rate limits are not an input to
the per-session calculation, only an override on its result.

### How it is computed

Everything below is a **ratio**, deliberately. No price table, no exchange rate,
nothing that goes stale when Anthropic changes pricing or the pound moves.

- `ctx_pct` — `context_window.used_percentage`, straight from the payload.
- `growth` = current context tokens ÷ context at session open. Per-turn input
  cost scales with context, so this *is* "how many times a fresh session's turn
  cost this session now pays". The session measured above ended at 13×.
- `cache_cold` = time since the last observed change exceeds the assumed cache
  TTL.

```
hand off  when  ctx_pct >= handoff_at_pct
           or  (cache_cold and growth >= cold_growth)

wrap up   when  ctx_pct >= wrapup_at_pct
           or  growth >= wrapup_growth

keep going otherwise
```

Cost in dollars still displays — `cost.total_cost_usd` is handed to us and
answers "what has this cost" — but it drives nothing. It is a ratchet: it only
rises, so thresholding on it would eventually redden every long session
regardless of whether it was worth flagging.

### The 5-hour window

`rate_limits.five_hour` arrives in every payload as `used_percentage` and
`resets_at`. It differs from everything else here in two ways that matter.

**It is account-wide.** Every session on the machine reports the same figure and
draws down the same pool. Nine parallel worktrees are nine drains on one bucket.
So it renders **once, fleet-level** — never nine times in nine panes — and hive
is the only thing positioned to show it, because it is the only thing that sees
all the sessions at once.

**Near the limit, "hand off" becomes wrong advice.** A handoff is not free: the
fresh session has to re-read the ticket, the plan and the files before it can do
anything. The session measured here opened at 49,485 tokens of context — that is
the rebuild cost, and it is spent against the *same* exhausted quota. So when
the window is nearly gone there is nowhere to hand off to, and the correct move
is the opposite: finish what is in flight, or park.

**The reset outlasts the cache, so resuming is not free.** Waiting for quota
means waiting; the cache TTL is an hour at best. Whatever context the session is
holding gets evicted and re-written in full on the first turn back, at the 2×
write price with nothing read from cache:

```
 context     1h-TTL write   5m-TTL write   vs a warm turn
   50,000   $       0.50   $       0.31       20x
  250,000   $       2.50   $       1.56       20x
  500,000   $       5.00   $       3.12       20x
  555,680   $       5.56   $       3.47       20x
1,000,000   $      10.00   $       6.25       20x
```

Validated against observation, within 3% both times: a 102,441-token resume
predicted $1.02 and cost $1.05; the four >4h resumes predicted $3.91 and
averaged $3.95.

Two consequences, and the second is the important one.

**That toll is paid out of the freshly reset quota.** You wait five hours for a
new bucket and the first thing it buys is re-reading what you already had.

**So a big session must hand off *before* it parks.** Resuming a 555,680-token
session costs $5.56; resuming a fresh 49,485-token one costs $0.49 — 11× less,
and every turn after that is cheaper too. But a handoff itself spends tokens
rebuilding context in the new session, and that has to come from somewhere. Let
the quota run out first and you cannot afford to hand off either: you wait for
the reset, pay the full resume toll, and only then move the work — paying twice.
**Running out of quota holding a large context traps the work.** The window to
act closes before the quota does.

Hence the override is conditional:

```
time_to_reset  = resets_at - now
cache_survives = time_to_reset < remaining cache TTL

five_hour >= park_at_pct:
    cache_survives            ->  park    "5h quota 94% — resets 14:20, cache holds"
    ctx_pct <  handoff_at_pct ->  park    "5h quota 94% — resets 14:20, ~$0.50 to resume"
    otherwise                 ->  hand off "5h quota 94% — hand off NOW, $5.56 to resume later"
```

`cache_survives` matters because the 5-hour limit is a **rolling** window: a
reset can be twenty minutes away, not five hours. When it lands inside the cache
TTL, parking costs nothing and none of the above applies.

`resets_at` is a real deadline rather than a vague warning, so the reason line
gives the clock time work can resume.

Two cautions. The field is **conditional** — it appears only when the API
reports it, so its absence means unknown, never zero. And when the quota is
actually exhausted the failure is visible in transcripts as a 429 carrying
`quotaLimits: {status: "rejected", rateLimitType: "five_hour", resetsAt, overageStatus,
isUsingOverage}` — on this account `overageDisabledReason: "org_level_disabled"`,
so there is no overage to fall through to. The wall is a wall.

### The reason line

A verdict with no reason is not trustworthy, and the reason is usually the
actionable part. One short clause, naming whichever rule fired:

```
context 23% — turns ≈4.6× a fresh session
context 74% — compaction likely soon
idle 16h — cache cold, next turn ≈20× normal
5h quota 94% — resets 14:20, cache holds
5h quota 94% — hand off NOW, $5.56 to resume later
```

The quota reasons are the only ones that quote money, because the resume toll is
a real figure the payload lets us predict and it is the number that decides the
action. Everything else stays a ratio.

### Config

```yaml
telemetry:
  enabled: true
  wrapup_at_pct: 50
  handoff_at_pct: 70
  wrapup_growth: 6
  cold_growth: 5
  park_at_pct: 90          # five_hour quota; fleet-wide override
  cache_ttl_minutes: 60
  stale_after_seconds: 30
  prune_after_hours: 24
```

`cache_ttl_minutes` is an assumption, not an observation: this box runs a 1-hour
prompt cache TTL, but that drops to 5 minutes under usage overage and **nothing
in the payload reports which is active**. So `cache_cold` is an estimate and the
reason line must read as one ("cache likely cold"), never as fact.

`enabled: false` restores exactly today's behaviour on every surface.

## Where the data comes from

`~/.claude/settings.json` points `statusLine` at `hive todo statusline`. Claude
Code pipes a JSON payload to that command on every refresh, for every live
session. `statuslineCwd` (`cmd_todo.go:604`) decodes two fields — `cwd` and
`workspace.current_dir` — and discards the rest, which includes everything this
design needs.

Fields used, read from the payload builder in the installed CLI
(`~/.local/share/claude/versions/2.1.247`):

| Group | Fields used |
|---|---|
| context_window | `used_percentage`, `context_window_size`, `current_usage` |
| cost | `total_cost_usd`, `total_duration_ms`, `total_api_duration_ms` |
| identity | `session_id`, `cwd`, `transcript_path`, `session_name` |
| model | `model.id`, `effort.level` |
| place | `workspace.project_dir`, `worktree.{name,branch}` |
| rate_limits | `five_hour{used_percentage, resets_at}` — fleet-wide override and display; never a per-session input. Conditional: absent means unknown, not zero |

The payload is a **push**: free, but only while a session is alive and
refreshing. It carries current values only — no history. Per-turn duration,
compaction events and rate-limit rejections live in the transcript JSONL, which
this design does not read (see Non-goals).

**Version risk.** The shape was read from a specific CLI build, not captured
live. Treat every field as optional; a payload from a future build that renames
or drops one must degrade, never break the statusline.

## Non-goals

- **Reading transcript JSONL.** A separate collector with its own parsing cost.
- **History, charts, trends.** Latest value per session, nothing else.
- **Doing the handoff.** The verdict says *whether*; you still act. Automating
  it — release the claim, write the handover, start a fresh session on the same
  ticket — is a real feature and gets its own spec.
- **Rate limits in the per-session calculation.** They never enter it, per
  decision. They act only as a fleet-wide override and a displayed figure.
- **Currency conversion.** Costs display in USD as reported. Pounds would mean a
  hardcoded fx rate that drifts, and every threshold here is a ratio anyway.

## Architecture

### Collector

`runTodoStatusline` gains a second job: decode the payload, derive the verdict,
write a snapshot. Constraints in priority order:

1. **Never break the statusline.** Every failure — malformed payload,
   unwritable directory, full disk — is swallowed and the line renders as today.
2. **Stay fast.** The line already does `loadTodos` plus a `git rev-parse` in
   `worktreeClaim` every tick. This adds one atomic write and no subprocesses.
3. **Need no lock.** One writer per session id.

`growth` needs the session's opening context size, which a single snapshot
cannot supply. The collector reads the existing snapshot, carries `opened_at_tokens`
forward, and only writes it fresh when no snapshot exists — at which point the
current size becomes the baseline. For a session already running when this
ships, that baseline is wrong (too high), so `growth` under-reports until the
session restarts. Acceptable: `ctx_pct` still fires independently, and the error
decays. It must be stated in the reason line's wording, not silently absorbed.

### Storage

Per-session JSON at `$XDG_RUNTIME_DIR/hive/sessions/<session_id>.json`, written
temp-then-rename. Follows `repoKeyMemoPath` (`repo_key.go:81`), falling back to
`os.TempDir()`. Runtime, not data: a snapshot is worthless after a reboot, and
putting it here means the OS clears it for us.

Rejected: one shared append-only JSONL (needs a lock on the hot path, grows
without bound, forces the TUI to parse history for the present); and the todo
store (`todo_backup.go` commits it to git — a commit storm).

### Record

```json
{
  "session_id": "945515c2-…",
  "captured_at": "2026-08-27T09:44:11Z",
  "tmux_session": "hive-workspace",
  "project_dir": "/home/steve/repos/workspace",
  "worktree": {"name": "split-2", "branch": "split-2"},
  "model": "claude-opus-5",
  "ctx_pct": 22.8,
  "ctx_tokens": 227793,
  "opened_at_tokens": 49485,
  "growth": 4.6,
  "cost_usd": 25.07,
  "last_change_at": "2026-08-27T09:41:02Z",
  "verdict": "keep_going",
  "reason": "context 23% — turns ≈4.6× a fresh session"
}
```

Verdict and reason are stored, not recomputed per frame, so both surfaces agree
by construction.

### Join key

The statusline process resolves `workspace.project_dir` → repo `DirName` →
`TmuxSessionName(dirName, false)` (`tmux.go:75`) at write time and records it.
The TUI does no reverse lookup. Risk: `project_dir` for a session started inside
a worktree may be the worktree rather than the parent, and hive names worktree
sessions after the worktree dir. Both paths go through the same helper, but this
is the likeliest thing to be subtly wrong, so it gets tests against both layouts.

## Rendering

### Statusline

```
▸ Hive web: see the backlog… · 3/12 · ● keep going · 23% · $25.07
▸ Hive web: see the backlog… · 3/12 · ● wrap up · 58% · turns ≈8× fresh
▸ Hive web: see the backlog… · 3/12 · ● hand off · 74% · compaction likely soon
```

Green, amber, red. The verdict is the message; the figures are supporting
evidence. With no payload seen — a session predating this, or `enabled: false` —
the line is exactly what it is today.

### Split pane

`TerminalPane.View` (`ui/terminal.go:159`) recomposes captured content line by
line, and `sgrCarry` (`ui/ansistate.go`) already manages background carry-over.
The verdict tint wraps that composition.

Measured safe: capturing three live Claude panes with `capture-pane -e` and
counting background SGR codes gave 0 of 37, 0 of 37 and 0 of 73 lines setting
one. Claude Code paints foreground only, even in `fullscreen` TUI mode, so a
tint shows on every cell. Kept subtle — Claude's foreground palette was chosen
against the terminal's own background.

### Full attach

`AttachSession` (`ui/attach.go:24`) pipes the raw PTY; hive is not compositing
and cannot paint. `tmux set-option -t <session> window-style 'bg=…'` tints from
tmux's side, set on entry and cleared on exit. hive sets no tmux styles today.
It must be reverted on **every** exit path including errors, or a stale tint
outlives its reason.

### Border and tab

`BorderStyle` / `FocusedBorderStyle` (`ui/styles.go:20`) and `ui/tabbar.go` are
hive's own pixels and cost no contrast against session content. They carry the
loud end: a red border and red tab are readable across a nine-session drawer in
a way a subtle wash is not. This is where "which of these needs handing off" is
actually answered at a glance.

### Stale

No write inside `stale_after_seconds` means idle, backgrounded or dead: render
grey, not the last verdict. A stale "hand off" sends you to a session that
finished hours ago. Snapshots past `prune_after_hours` are deleted on TUI start.

Note the interaction: a genuinely idle session goes grey *and* is the one most
likely to be cache-cold. Grey must not hide a hand-off verdict — an idle session
with a large context is the strongest hand-off candidate there is. The reason
line survives into the grey state.

## Failure modes

| Failure | Behaviour |
|---|---|
| Payload missing or unparseable | Statusline as today; no snapshot written |
| Field absent (version drift) | That rule cannot fire; others still do |
| No prior snapshot (`growth` unknown) | `ctx_pct` rules only; reason says so |
| Snapshot dir unwritable | Silent; statusline unaffected; TUI all grey |
| Snapshot stale | Grey, reason retained |
| tmux `window-style` fails | Attach proceeds untinted |
| `enabled: false` | Collector and tints off; today's behaviour exactly |

## Testing

Pure, table-tested: the verdict function across every rule and boundary
(including `growth` unknown, and `cache_cold` with and without growth), the
reason-line wording per rule, and the payload decoder against a full payload, an
empty object, and one with every optional field missing.

Round-tripped through a temp dir: snapshot write-then-read, `opened_at_tokens`
carry-forward across successive writes, and the stale and prune boundaries with
an injected clock.

Asserted through the existing `tmuxRun` seam, as `tmuxNewSessionArgs` already
is: `window-style` set on attach and cleared on exit, including the error path.

Tinting is visual and is checked by eye at the gate below, not asserted.

## Verify before building

One assumption is load-bearing and unproven: **that Claude Code renders ANSI
colour in the statusline at all.** The docs say yes; it could not be confirmed
from the CLI bundle. If escapes are stripped, the statusline shows the verdict
as text and the colour lives only in the TUI.

First step is therefore throwaway: emit one coloured statusline from the
worktree build and look at it. Everything else waits on that answer.

## Open questions

1. **Thresholds are guesses.** 50/70 percent, 6×/5×, and 90% for the quota are
   reasoned from the tables above, not tuned. They want a week of real sessions.
   `park_at_pct` is the least defensible: it should probably account for how
   fast the fleet is burning quota, not just where it stands — with nine
   worktrees drawing on one bucket, 90% can become 100% in minutes, and the
   hand-off-before-you-park window is exactly what that burn rate closes.
4. **Predicting the resume toll needs a price table** keyed by model — the one
   place prices re-enter a design that is otherwise all ratios. It is small
   (input price × 2), it only feeds a displayed estimate, and the alternative is
   saying "20× a warm turn", which is drift-free but does not tell you whether
   to care.
2. **Sessions with no hive pane.** A Claude session started outside hive still
   writes snapshots. Show it as unmanaged, or ignore? Leaning ignore for v1.
3. **Subagents.** The payload carries `agent.name` when a subagent is driving.
   v1 records it and ignores it.

## Rollout

Built in a git worktree, merged to `main` to go live — `main` in the primary
checkout is what recompiles on restart, so an unmerged worktree is invisible
however many times hive is restarted.
