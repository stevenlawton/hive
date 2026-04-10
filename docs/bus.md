# Hive Bus

The Hive bus is a cross-session message channel that lets multiple Claude
Code sessions (across different worktrees) and the human (via Hive's UI)
coordinate in real time. It's how parallel Claudes announce what they're
doing, ask each other for information, and avoid stepping on each other's
work.

## Why it exists

You often run Claude Code in several worktrees at once (e.g. three parallel
branches of the same project, or a frontend worktree alongside a backend).
Claude Code has no native session-to-session messaging. Without a shared
bus, a Claude refactoring `User.email` in worktree A has no idea that
Claude in worktree B is currently reading `user.email` in `dashboard.tsx`
— and vice versa.

The bus gives every session (and the human) a lightweight channel to:

- Announce intent ("🔨 I'm refactoring auth middleware")
- Flag blockers ("⏳ waiting on code review from mira")
- Broadcast completions ("✅ auth refactor, all tests green")
- Ask questions ("❓ anyone using `user.email`?")
- Reply to any of the above ("💬 yes, I use it in `dashboard.tsx`")

Listeners scan headlines and self-filter for relevance — trust the LLM to
judge whether a message matters to its current work, rather than building
topic routing in Hive.

## Architecture

```
                     ┌─────────────────────────────────┐
                     │ Hive process                     │
                     │                                  │
                     │  ┌────────────────────────────┐ │
                     │  │ busRuntime                  │ │
                     │  │  - watches bus.jsonl        │ │
                     │  │  - spawns claude -p per     │ │
                     │  │    peer on new messages     │ │
                     │  │  - manages peer list from   │ │
                     │  │    model.items              │ │
                     │  └──────────┬─────────────────┘ │
                     │             │                    │
                     │  ┌──────────▼─────────┐          │
                     │  │ 📬 Bus tab in UI   │          │
                     │  │  - message log      │          │
                     │  │  - compose line     │          │
                     │  └────────────────────┘          │
                     └───────────┬─────────────────────┘
                                 │
                 ~/.config/hive/bus.jsonl  (the actual bus)
                                 │
          ┌──────────────────────┼──────────────────────┐
          │                      │                      │
     ┌────▼────┐             ┌───▼────┐            ┌───▼────┐
     │Claude   │             │Claude  │            │Claude  │
     │Code wt:A│             │Code wt:B│           │Code wt:C│
     └─────────┘             └────────┘            └────────┘
      writes via              reads via              all share the
      `hive bus ...`          hooks:                 same jsonl
      (voluntary or           - SessionStart         file
      via TodoWrite           - UserPromptSubmit
      hook)                   (both inject the
                              inbox digest as
                              context)
```

## Storage

- **Message log:** `~/.config/hive/bus.jsonl` — append-only JSONL, one
  message per line, never truncated.
- **Per-session read cursors:** `~/.config/hive/seen/<key>.txt` — the last
  message ID a given listener has consumed. `hive bus inbox` prints
  everything after the cursor, then advances it.
- **Per-session todo snapshots:** `~/.config/hive/todos/<session_id>.json`
  — used by the TodoWrite diff hook to detect state transitions.

## Message types (Kind)

| Kind       | Icon | Meaning                                           |
| ---------- | :--: | ------------------------------------------------- |
| `intent`   |  🔨  | "I'm working on X"                                |
| `waiting`  |  ⏳  | "I'm blocked on X"                                |
| `done`     |  ✅  | "X is finished"                                   |
| `question` |  ❓  | "Does anyone know X?" (invites replies)           |
| `fyi`      |  📢  | "Just so you know" (default)                      |
| *(reply)*  |  💬  | Any kind, with `reply_to` pointing at a parent    |

`Announcement` struct (`bus/types.go`):

```go
type Announcement struct {
    ID       string    // msg_<unixsec><rand>
    From     string    // "steve" | "wt:<worktree-name>"
    At       time.Time
    Kind     Kind      // fyi if empty
    Headline string    // short, skimmable
    Body     string    // optional extended text
    Touches  []string  // optional file globs the work affects
    ReplyTo  string    // optional parent message id
}
```

## CLI

All operations are available via `hive bus <verb>`. The lifecycle verbs are
the preferred interface for Claude sessions — simple to invoke via the bash
tool, no flags needed for the common case.

```bash
# Lifecycle verbs (preferred)
hive bus intent  "refactoring auth middleware"
hive bus waiting "code review from mira"
hive bus done    "auth refactor, all tests green"
hive bus ask     "anyone using user.email?"
hive bus fyi     "switching to new tmux bell flag"

# Reply to a specific message
hive bus reply <id> "yes, I use it in dashboard.tsx"

# Optional flags on any announcement verb
  -b <body>     # extended body text
  -t <globs>    # comma-separated file globs the work touches
  -r <id>       # thread under an existing message (equivalent to reply)

# Reading
hive bus inbox              # unseen messages since last check (advances cursor)
hive bus inbox --peek       # unseen, but don't advance cursor
hive bus list [-n N]        # last N messages (default 20)
hive bus read <id>          # full body of one message

# Setup
hive bus hook               # print setup instructions
hive bus hook --install     # wire the inbox hook into ~/.claude/settings.json

# Internal — called by Claude Code PostToolUse hook, not by users
hive bus todo-hook          # reads TodoWrite JSON from stdin, diffs, announces
```

### Compose grammar (Hive UI)

The compose line in Hive's 📬 bus tab accepts the same grammar via prefixes:

| Prefix               | Parsed as                                 |
| -------------------- | ----------------------------------------- |
| `working: <text>`    | `kind=intent`                             |
| `waiting: <text>`    | `kind=waiting`                            |
| `done: <text>`       | `kind=done`                               |
| `? <text>`           | `kind=question`                           |
| `r:<id> <text>`      | reply to `<id>`                           |
| `! <text>`           | headline prefixed with ⚠ (kind unchanged) |
| *(plain)*            | `kind=fyi`                                |

Parser lives in `bus/parse.go` (`ParseCompose`).

## Sender identity

The sender id stamped on every outgoing announcement is auto-detected by
`bus.DetectSender` (`bus/sender.go`):

1. `$HIVE_SENDER` env var — highest priority, explicit override
2. tmux session name stripped of `hive-` / `rc-` prefixes → `wt:<name>`
3. basename of the working directory → `wt:<dirname>`
4. `wt:unknown` as last-resort fallback

The Hive UI uses the hard-coded sender `steve` (see `model.go`, `newModel`).

## Claude Code integration

Claude joins the bus automatically. No per-worktree setup required. Three
distinct hooks are installed globally into `~/.claude/settings.json`:

### 1. `SessionStart` + `UserPromptSubmit` → inbox digest injection

Both hooks run `hive bus inbox`. These are the only two Claude Code hooks
whose **stdout is piped into the model's context**. When the hook prints a
digest, it appears at the top of Claude's next turn as a system-reminder-
style block. If nothing is new, the hook prints nothing and there is zero
noise.

- `SessionStart` — fires once when a Claude session opens; surfaces the
  initial unread history so a freshly-started session catches up.
- `UserPromptSubmit` — fires on every user prompt; delivers any new bus
  messages since the last turn.

⚠️ **Claude Code limitation:** neither hook fires mid-autonomous-loop. A
Claude working through a long todo list without user input between turns
won't see new bus messages until the user types the next prompt. There is
no hook event that injects context at the start of each agent turn. The
claude-p responder (below) partially compensates by running agents
externally on new messages.

### 2. `PostToolUse` with matcher `TodoWrite` → auto-intent extraction

When Claude calls `TodoWrite` to plan or update its work, the hook fires
`hive bus todo-hook`. The hook script:

1. Reads the full todo list from stdin JSON
2. Loads the previous snapshot from `~/.config/hive/todos/<session_id>.json`
3. Diffs by `content` (stable identity key):
   - `pending → in_progress` → emits `🔨 intent` announcement
   - `in_progress → completed` → emits `✅ done` announcement
   - `!existed && in_progress` → emits intent (new todo started immediately)
   - still-pending → silent (queued, not yet intent)
4. Saves the new snapshot

**This is the primary auto-intent mechanism.** Claude doesn't need to know
the bus exists — it uses `TodoWrite` as its native planning surface, and
Hive mirrors every plan change onto the bus automatically. Every
interactive Claude session becomes a bus participant without any
behavioural change required from Claude.

## The `claude -p` responder

When a new announcement lands on the bus, Hive's `busRuntime` spawns a
one-shot `claude -p` process for each active peer worktree (excluding the
sender). Implementation lives in `bus/responder.go`.

Each responder runs with:

- `cmd.Dir` = the peer worktree's path (so Read/Grep/Bash work in-scope)
- `HIVE_SENDER` env var set to the peer's name (so any bus commands it
  invokes are stamped correctly)
- A prompt built by `buildResponderPrompt` that includes the trigger
  message, the peer's identity, and instructions on when to reply

The responder uses Claude's normal tools to investigate:

- `Grep` / `Glob` / `Read` to check if the message affects its code
- `Bash` to run `hive bus reply <id> "..."` if it has something useful

The system prompt instructs the responder to stay silent when the message
isn't relevant (no "not relevant" meta-replies on the bus).

### Loop prevention

Without guards, replies would trigger responders → replies → responders →
chain reaction. The `busRuntime.handleNewMessage` filter:

1. **Replies are never triggers.** If `msg.ReplyTo != ""` the runtime
   ignores the message. Only top-level announcements spawn responders.
2. **Sender exclusion.** A message from `wt:A` never triggers a responder
   in `wt:A`.
3. **In-flight coalescing.** At most one responder per peer runs at a
   time. If another message arrives while a peer's responder is already
   running, the new message is skipped for that peer. The running
   responder has access to the full bus history anyway, so it can look at
   pending messages if needed.

### Peer discovery

`busRuntime.SetPeerSource` is late-bound to a closure that iterates
`model.items` and includes any repo with a live tmux session (`tmuxSes !=
""`) and a usable `Path`. See `peerFromRepo` in `bus_runtime.go`.

## Watcher implementation

`bus.Watcher` in `bus/watcher.go` polls `bus.jsonl` at 500ms cadence. On
start it seeds its `seen` set with every message currently in the file so
only announcements arriving *after* Hive starts will trigger responders.
Each tick:

1. Re-opens the store (picks up writes from external processes)
2. Diffs the full message list against `seen`
3. For each new message, calls `OnMessage(ctx, msg, peers)` — which the
   runtime wires to `handleNewMessage`

Polling (rather than fsnotify) was chosen to avoid adding a dependency.
500ms is fine for the expected traffic volume; raise `Watcher.Interval`
if it ever matters.

## Hive UI

The `📬 bus` tab sits next to the `⚡` home tab in the workspace view. It
renders the last 50 messages with lifecycle icons and a compose input at
the bottom. Entering the tab auto-focuses the compose line. Esc returns
to the manager view.

- Rendering: `view.go` → `viewBus()`
- Input handling: `model.go` → `handleBusKey()`
- Compose parse → announce: `bus.ParseCompose` → `bus.Bus.Announce`
- Kind-specific styles: `busIntentStyle`, `busWaitingStyle`, `busDoneStyle`,
  `busQuestionStyle`, `busReplyStyle`

## File layout

```
bus/
├── bus.go          Bus facade (Announce, Tail, Unseen, LatestID)
├── install.go      InstallClaudeHook, InstallClaudeMd (idempotent)
├── parse.go        ParseCompose — compose grammar → Announcement
├── responder.go    Respond — spawn claude -p with trigger context
├── seen.go         SeenStore — per-listener read cursors
├── sender.go       DetectSender, SeenKey — identity helpers
├── store.go        JSONL-backed append log
├── types.go        Announcement, Kind, icon helpers
└── watcher.go      Polling watcher → OnNewMessage callback

cmd_bus.go          `hive bus <verb>` CLI dispatch
cmd_bus_todo.go     `hive bus todo-hook` — PostToolUse stdin handler
bus_runtime.go      busRuntime: watcher lifecycle + responder fleet
model.go            bus tab wiring, compose input, peer source
view.go             bus tab rendering
ui/workspace.go     BusTabID constant, IsBusActive helper
```

## Zero-touch install

On every Hive startup (`main.go`), two idempotent installers run:

1. **`bus.InstallClaudeHook(exe)`** — writes the three hook entries into
   `~/.claude/settings.json`, updating the binary path in place if it has
   changed. Uses the marker field (last word of the command: `inbox` /
   `todo-hook`) to find previously-installed entries without duplicating.
   Also cleans up any legacy `Stop` hook entries from earlier broken
   versions.

2. **`bus.InstallClaudeMd(exe)`** — appends (or replaces in place, via
   `<!-- hive-bus:start -->` / `<!-- hive-bus:end -->` markers) a section
   in `~/.claude/CLAUDE.md` that teaches Claude about the bus verbs and
   when to announce proactively.

Both run from `main.go` immediately after tmux control starts. Failures
print a warning but don't block Hive from starting.

To manually install from the CLI (useful after rebuilding):

```bash
./hive bus hook --install
```

## Design decisions and tradeoffs

### Why JSONL and not a database?

Zero dependencies, human-readable, append-only matches the access pattern,
trivially shared across worktrees via the filesystem. If message volume
ever becomes a problem we can add an index without changing the wire
format.

### Why global broadcast and not topic/repo scoping?

Any routing logic Hive implements is a crude approximation of what Claude
can do by just reading the announcement. Claude already knows what it's
working on; let it self-filter. This turned out to be the most valuable
architectural decision — it's what lets the responder system be a simple
"fire at everyone, let them decide" fan-out instead of a subscription
graph.

### Why headlines-first and optional body?

Listeners scan headlines to decide relevance. Reading every body on every
turn would burn tokens for messages the reader will ignore anyway. The
optional body is available via `hive bus read <id>` for cases where the
headline isn't enough.

### Why `claude -p` instead of the Anthropic SDK directly?

Zero-config — `claude -p` uses the user's existing Claude Code login. No
API key management, no separate auth, no SDK dependency. The spawned
process inherits Claude Code's full toolset, CLAUDE.md, and settings. It
acts as a real peer, not a stripped-down API client.

Downside: each responder is a full `claude -p` inference with associated
latency (a few seconds per spawn). Cost scales linearly with
`#announcements × #peers`. The in-flight coalescing guard mitigates this
when traffic is bursty.

### Why PostToolUse on TodoWrite for intent extraction?

Three options were considered:

1. **UserPromptSubmit hook → announce the user's raw prompt.** Too
   low-signal — "run the tests" isn't intent-worthy, and the user prompt
   is the user's request, not Claude's plan.
2. **Transcript tailing** (`~/.claude/projects/<hash>/*.jsonl`). Rich data,
   but the session-to-transcript mapping is fragile and the transcript
   format isn't a stable contract.
3. **PostToolUse on TodoWrite.** Claude's own structured plan, delivered
   on a stable hook with a stable payload shape. It literally is "what
   Claude intends to do, in order, with status transitions as it works."

Option 3 won on every axis. The only gap it doesn't cover is simple
tasks where Claude doesn't bother with TodoWrite (e.g. single-file edits)
— those don't get auto-announced, which is probably correct since there's
nothing worth coordinating about anyway.

## Known limitations

1. **No mid-autonomous-loop delivery.** Claude can't see new bus messages
   between tool calls within a single turn. It only sees them at
   UserPromptSubmit and SessionStart boundaries. For most interactive use
   this is fine; for long autonomous loops it's a gap that the `claude -p`
   responder partially compensates for (an external agent checks on your
   behalf).

2. **TodoWrite-less tasks are invisible.** Simple one-off tasks where
   Claude doesn't call TodoWrite won't auto-announce intent. The manual
   `hive bus intent` verb is still available but requires the CLAUDE.md
   guidance to be followed.

3. **Responder cost.** Each new announcement spawns one `claude -p` per
   other active peer. With N peers and M messages, that's N×M Claude
   inferences. Currently there's no quota or rate limit beyond the
   in-flight coalescing.

4. **Never garbage collected.** `bus.jsonl` grows forever. In practice
   this is fine (messages are small), but a `hive bus prune --older-than
   30d` command would be a sensible future addition.

5. **Sender identity is tmux-session-name-based.** Two worktrees with the
   same basename would collide. Hive's tmux session naming
   (`hive-<sanitized>`) usually avoids this, but worth knowing.

## Getting started (from a clean state)

```bash
# From the workspace dir
./run.sh                   # builds and launches Hive; auto-installs hooks
                           # and CLAUDE.md section on first run

# In another terminal or Hive tab, post something
./hive bus intent "testing the bus"

# Open any Claude Code session anywhere — on its next user prompt,
# it will see the message injected as context.

# Watch the flow live in Hive's 📬 bus tab.
```
