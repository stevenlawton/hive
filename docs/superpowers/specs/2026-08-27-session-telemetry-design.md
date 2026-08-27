# Hand off, or keep going — session verdicts in hive

**Status:** shipped on main, 2026-08-27. Corrected against what was actually built.
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
holding is evicted and re-written in full on the first turn back, at the
cache-write price with nothing read from cache — about **20× a warm turn**.

**Quoted as a ratio, not in currency, and this is a correction.** An earlier
version of this design put the figure in dollars from a price table. Checked
against ground truth, that table was **2.09× wrong**: it priced one real session
at $194.78 when Claude's own `total_cost_usd` said $93.38, the cache-read term
alone ($95.81) exceeding the true total. The "validation" that had accepted it
was circular — a prediction compared against an observation computed from the
same table. The ratio survives that error where an absolute cannot, because a
uniform mispricing cancels top and bottom.

Two consequences, and the second is the important one.

**That toll is paid out of the freshly reset quota.** You wait for a new bucket
and the first thing it buys is re-reading what you already had.

**So a big session must hand off *before* it parks.** A handoff itself spends
tokens rebuilding context in a new session. Let the quota run out first and you
can afford neither move: you wait for the reset, pay the full resume toll, and
only then move the work — paying twice. **Running out of quota holding a large
context traps the work.** The window to act closes before the quota does.

Hence the override is conditional:

```
time_to_reset  = resets_at - now
cache_survives = time_to_reset < remaining cache TTL

five_hour >= park_at_pct:
    cache_survives            ->  park     "5h 94% — resets 14:20, cache holds"
    ctx_pct <  handoff_at_pct ->  park     "5h 94% — resets 14:20"
    otherwise                 ->  hand off "5h 94% — hand off now, ≈20× to resume later"
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
5h 94% — resets 14:20, cache holds
5h 94% — hand off now, ≈20× to resume later
```

No reason quotes money. Displayed session cost comes from Claude directly and is
real; anything hive would have to derive is a ratio.

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
  rate_limit_floor_pct: 60
  stale_after_seconds: 300
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
2/11 · ████░░░░░░ 38% · $29.14 · $3.00/h
2/11 · ██████░░░░ 56% · $93.38
2/11 · ███████░░░ 74% · $47.80 · compaction likely soon
```

Three corrections against the original draft, all from watching it run.

**No verdict label.** The colour carries the verdict; spelling it out again cost
width the rest of the line needed.

**No task subject.** It was the longest thing on the line and the drawer already
shows it. Progress stays. This also fixed a bug the removal exposed: the
statusline returned early on an empty backlog, so telemetry never rendered for a
repo with no tasks — hiding the verdict exactly where there was no task text to
look at instead. The halves are independent now.

**Cost always shows**, not only on `keep going`, and the session's own burn rate
beside it once there is enough of a span to mean anything.

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

**Staleness does not clear a verdict, and this reverses the original draft.**
Statuslines refresh on activity rather than on a timer — no `refreshInterval` is
configured — so a session waiting on a human simply stops reporting. Four of
nine live snapshots were over 100 seconds old against a 30-second threshold.
Context and cost do not decay while a session idles, so its verdict stays true;
clearing the colour hid precisely the sessions worth flagging. The draft already
said as much — "an idle session with a large context is the strongest hand-off
candidate there is" — and the first implementation did the opposite.

Staleness is a display nuance only, and the threshold is 300s to match how the
statusline actually fires. Snapshots past `prune_after_hours` are deleted.

**Collisions resolve to the worst verdict.** Two Claude sessions in one
directory produce the same tmux session name, so snapshots collide. Taking the
freshest let an empty session mask a heavy one in the same directory — observed
live, a 59%/$102.23 wrap-up hidden behind a 0%/$0 keep-going.

## Fleet burn rate

What the machine costs per hour, across every session, shown once in the tab bar
filler beside the shared quota.

It needed no new capture. **`total_cost_usd` already includes subagent spend** —
measured: sessions with no agents predict at a ratio of 0.45–0.49 against the
(known-wrong) price table, while one session reported $32.61 with a main loop
accounting for $1.96 and 29 agent files behind it. Only the agents explain that.

Snapshots keep a short cost history rather than only the latest figure, because
a rate needs a span and the statusline fires on activity — successive samples
can be milliseconds or hours apart. The rate is measured oldest-to-newest across
the window, not from the last pair. A cost *decrease* means the session
restarted and its counter reset, so it yields no rate rather than a negative one.

## Cost per ticket

**A session is not a ticket.** It chats, explores, and touches several tickets;
its cost cannot be attributed to one. The unit that *is* the ticket is the agent
run plus every sub-agent it dispatches.

Those sub-agents never touch hive, so they cannot be recognised by what they do.
What they inherit is a **working directory**. So hive records the cwd whenever a
ticket is *committed to* on its own CLI — `claim`, `current`, `state` — and all
agent work under that directory counts, sub-agents included. Claude Code writes
one transcript per agent under `<project>/<session-id>/subagents/`, so the tree
walk is the whole join. `hive todo cost [<ref>]` reports it, heaviest first.

Two things this design rejects, both measured rather than assumed:

**Reconstructing attribution afterwards reaches 42%.** Only agents that ran a
hive command can be recognised, and those are not the ones doing the work.

**Matching ticket ids in prompt text is worse.** Three-letter ids collide with
English; the first attempt returned "ids", "add", "and" and "may".

**Only commitment counts, not reading.** Recording on `show` attributed 123k
tokens of unrelated agent work to a ticket that had merely been read, because a
session reads many tickets and the last one won the directory. A false figure
here is worse than a missing one — the whole point is judging whether a ticket
cost too much.

Spend is reported in **tokens**, not money: output plus cache writes, which is
work produced rather than context re-read. There is no back-fill; it counts from
when recording started.

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
