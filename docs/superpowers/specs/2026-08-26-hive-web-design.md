# Hive Web — Design

**Date:** 2026-08-26
**Status:** Shipped 2026-08-27 — `5362d1d` (plan review), `d2a104a` (serve), `59ebd99` (stdin fix). Amended below with what building it taught.

## Problem

Two of hive's five pipeline stages stall on a human. `plan-review` means a plan
wants Steve's yes; `triage` means a build wants his eyes. Both are invisible
unless he is sitting at the terminal with the drawer open, and both require
reading a document — plans currently run to 725 and 1,086 lines — that the
drawer cannot show at all.

The result is that work reaches a gate and stops there silently. Worse, the
only way to *give* feedback today is prose typed at an agent, which is exactly
the unverifiable channel that let a builder fabricate a decision in Steve's name
(see the fabrication ticket, and `2026-08-07-todo-concurrency-design.md` for the
pipeline itself).

A web view fixes the visibility half. Line-anchored review fixes the feedback
half, and is the more valuable of the two.

## What already exists

A working prototype, built from the real backlog and both real plan documents:
`https://claude.ai/code/artifact/4c8e8736-470f-4d04-b996-24b6bdb9c2b1`

It is the design conversation in artifact form; the decisions below were taken
against it, by Steve, in conversation on 2026-08-26. It is not the
implementation — it is a static page with the data baked in.

## Decisions already taken

- **Full control, not a dashboard.** Everything the drawer does, plus review.
- **Reachable over LAN / Tailscale**, not localhost-only. The phone is the
  point.
- **Markdown renders.** Reviewing raw source is punishment. Prose renders, and
  every rendered block is commentable.
- **Comments anchor to source lines.** A block carries the line range it came
  from, so a comment left while reading prose still resolves to a line number an
  agent can act on. One comment model, two views.
- **A plan you hold comments against cannot be approved.** Approve is disabled
  whenever a comment exists. Request-changes requires at least one comment.
- **No "approve with nits".** Steve, verbatim: *"im not gunna nit — either i
  want stuff changed - or not"*. The verdict is binary and there is no third
  path. Do not add one later without asking him again.
- **A review is bound to the plan's content hash.** Rewrite the plan and the
  approval stops applying.
- **The planner finds out by polling its ticket's state.** The review file and
  the state change are the message. Recommended by this design and not objected
  to; see Open questions.

## Goals

- Steve can see, from a phone, what is waiting on him across every repo.
- He can read a plan properly and comment on any part of it.
- A review reaches the agent that wrote the plan, in a form it can act on
  line by line.
- An approval records *what was approved*, so a later rewrite invalidates it.
- The store stays the single source of truth. The web view is another front-end
  onto it, never a second copy.

## Non-goals

- **Replacing the drawer.** The TUI stays. This is for when the terminal is not
  in front of you.
- **Multi-user.** One human. No accounts, no roles, no per-user attribution
  beyond "Steve".
- **Approving from a list.** Deliberately impossible. The decision controls
  exist only at the end of the document.
- **Editing plan text in the browser.** Comments say what is wrong; the agent
  rewrites. A human editing the plan would break the hash binding and blur who
  authored what.

## Architecture

A new subcommand, `hive serve`, in a new file `cmd_serve.go`, plus a `web/`
directory of static assets embedded with `embed.FS`. No new dependencies:
`net/http` and `embed` are stdlib, and `notify.go` already imports `net/http`.

```
browser ──HTTP──> hive serve ──withTodos()──> ~/.local/share/hive/todos/*.md
                       │
                       └──os.ReadFile──────> <repo>/docs/plans/<id>.md
```

**Repo enumeration.** `DiscoverRepos(cfg)` (`config.go:326`) already returns
every repo with its path, and is what the TUI uses. The server calls it once per
request cycle rather than caching, so a new repo appears without a restart.

**Store access goes through `withTodos`**, unchanged. That gives the server the
same flock the CLI and drawer take, so a write from the phone cannot race a
write from the drawer. No new concurrency design is needed — this is the payoff
from the store move earlier today.

**The server holds no state.** Every request reads the store. Comments in
progress live in the browser until the review is submitted; a half-written
review is not hive's problem.

### Endpoints

| Method | Path | Does |
|---|---|---|
| GET | `/` | the app shell (embedded) |
| GET | `/api/backlog` | every repo's tasks, as JSON |
| GET | `/api/plan/{repo}/{id}` | plan text + sha256, or 404 |
| POST | `/api/review/{repo}/{id}` | submit a review (below) |
| POST | `/api/task/{repo}/{id}` | state, claim, defer, done, edit |

`{repo}` is the repo's directory name, resolved against `DiscoverRepos`. A repo
name that does not resolve is a 404, never a path join.

## The review model

A review is one JSON POST:

```json
{ "verdict": "changes",
  "planHash": "edb1b7d398c8",
  "comments": [ {"line": 412, "text": "this is wrong because…"} ] }
```

The server:

1. **Re-reads the plan and re-hashes it.** If the hash does not match
   `planHash`, the review is rejected with 409 and the reason — the plan changed
   under the reviewer, and the comments may point at lines that have moved.
   This is the whole reason the hash exists.
2. Rejects `approve` with a non-empty `comments` array, and `changes` with an
   empty one. The UI enforces both; the server does not trust the UI.
3. Writes `<repo>/docs/plans/<id>.review.md`.
4. Moves the ticket: `approve` → `ready`; `changes` → unrefined (`""`).
5. Posts a bus announcement naming the ticket, the verdict, the comment count
   and the plan hash.

### The review file

Markdown, because the reader is an agent and agents read markdown:

```markdown
# Review — Agents have no way to discover the wt-init convention

ticket: wdd
plan: docs/plans/wdd.md
plan-hash: edb1b7d398c8
reviewer: Steve
verdict: changes requested
comments: 3

## Line 412

> the source line, quoted verbatim

this is wrong because…
```

Quoting the source line matters: it survives the plan being rewritten, so a
comment is still legible after the line number has stopped meaning anything.

The review file lives **in the repo**, next to the plan, and is committed like
one. This is deliberate and is not a relapse of the store move: a plan and its
review are work product with a place in the project's history. The backlog is
machine state and is not. The distinction is what gets committed by a human as
part of the work, versus what hive writes on its own schedule.

## Reach and auth

Binds `0.0.0.0` so a tailnet address works. **There is no authentication.**

There was a shared token, minted into `$XDG_DATA_HOME/hive/web-token` and
carried as `?t=` on first visit. It was removed on 2026-08-28 at Steve's
instruction — the cost of finding a token to open a UI on your own machine was
not worth what the token bought. It was never a defence against anyone who
could read that file or reach the tailnet, and it leaked into browser history
and shell scrollback by riding in the URL.

The posture is now stated plainly rather than half-defended: the network the
port is bound to is the only boundary. Anyone who can reach it can read every
backlog and plan, and can approve or reject work. Given the backlog carries
unfixed security findings for a live site — scanned 2026-08-26, no credentials,
but the Stripe webhook rotation hole and the erasure gaps are in there — this
must not be exposed to the public internet, and a laptop that joins untrusted
networks is exposing it to them.

## Testing

- **Hash mismatch is refused.** Submit a review whose `planHash` no longer
  matches; expect 409 and no file written, no state change, no bus post.
- **Approve with comments is refused server-side**, with the UI bypassed.
- **Changes with no comments is refused**, same.
- **A review writes exactly one file** and leaves the store consistent: the
  ticket's state moved, its id unchanged.
- **Concurrent write with the drawer.** A review and a drawer edit at once both
  survive — the existing `withTodos` concurrency test extended to the server.
- **No credential.** Every endpoint answers a bare request; nothing 401s.
- **Repo resolution.** A `{repo}` that is not in `DiscoverRepos` 404s and never
  reaches the filesystem.

## Open questions

1. **Waking the planner.** This design assumes it polls its ticket's state. The
   alternatives — the bus announcement wakes a live session, or a watcher spawns
   a planner when a review file appears — are faster and are more machinery.
   Recommended: ship the polling version, and only add a wake if reviews are
   observed rotting.
2. **Palette.** The prototype offers three (Quiet, Slate, Terminal) so Steve
   could choose. He rejected the original amber and has not picked a
   replacement. Default to Quiet; keep the switcher only if he wants it.
3. ~~**Triage.**~~ **Resolved, and it should not have been deferred.** Shipping
   plan review while triage showed the *plan* was not a smaller first step — it
   was a mislabelled one, and it cost a real false record (see below). Triage
   now resolves the ticket to its unmerged branch, renders the commit diff with
   the same per-line comments, and re-hashes the diff rather than the plan.
   Transitions differ by kind: a plan approved becomes `ready`; a build approved
   becomes done; a build sent back returns to `ready`, because the plan stands
   and only the build does not.

## What building it taught

**A correct hash does not make a record true.** The design above leans on
binding a decision to the content hash of the artifact as read. On the first
day of use, the UI offered "Review the plan" for tickets at *both* gates. A
triage ticket was reviewed as a plan; the hash of that plan was correct; and
the system recorded `reviewer: Steve, verdict: approved` for a decision he was
never meaningfully asked to make, then moved the ticket out of triage. Every
integrity property here held and the record was still false.

A decision has three parts, and the hash covers only the second: the **question**
asked and whether it was the right question for the state the work is in; the
**artifact** read; the **answer** given. Verification must check that the
artifact matches what the ticket's state required — a plan hash on a triage
decision should be refused as incoherent without a human noticing. Recorded in
full on the "human decisions need a verifiable record" ticket.

**A rule enforced only in the browser is not a rule** — already stated above,
and the same day a duplicate-submit bug posted one verdict six times, because
nothing server-side made a repeated review idempotent. Filed separately.

**Starting it is part of shipping it.** `hive serve` as a command you must
remember to run is a feature nobody uses. `web_port` in `config.yaml` starts it
alongside the TUI and prints the URL; failures are reported, never fatal, since
hive is a terminal tool first and a port in use must not stop it opening.
