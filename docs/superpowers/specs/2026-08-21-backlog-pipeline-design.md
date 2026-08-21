# Backlog pipeline — states, plan artifacts, and `/refine` / `/build`

**Date:** 2026-08-21
**Status:** draft, awaiting review

## Problem

`/ship` carries one ticket from claim to commit, and it works, but it holds
everything in the main session: the research, the plan, the diff, the findings.
That is correct for one hard ticket you want to sit with. It does not scale to a
backlog, for two reasons.

The context fills. Ten tickets through `/ship` is ten sessions, each cleared by
hand between tickets, because there is nowhere to put the state except the
conversation.

And the gates block. `/ship` stops at plan-green and at triage and waits for a
human *in the session*. A worker that is waiting is a worker doing nothing, and
a human servicing gates across several sessions is context-switching between
half-built tickets with no record of what any of them were about.

## Principle

**The main session orchestrates. It does not work.**

It reads the backlog, spawns a worker per ticket, collects one line back, and
reports. It never holds a plan, a diff, or a set of findings. Ten tickets
refined costs ten lines of context, not ten plans.

This is only possible because of a second principle:

**The backlog is the context store.** Ticket state and plan artifacts live on
disk, in the repo, not in a conversation. That is what makes the main session
disposable — it can be cleared at any point between dispatches and lose
nothing.

And it changes what a gate is. A human gate stops being an interruption that
blocks a worker and becomes **a state in the backlog** — a queue you work when
you choose, from an artifact written to be read cold.

## States

Today a task carries four pieces of status: the box (`' '` open, `'x'` done,
`'-'` deferred) and a per-worktree `Claim`. States extend this with a marker
suffix, the same shape `parseTodoMarker` (`todo.go:187`) already parses for the
`🔒@branch` claim.

```
(none)  →  @plan-review  →  @ready  →  @triage  →  [x]
unrefined   human queue      buildable  human queue   done
```

`Claim` stays orthogonal — it records *who holds* a ticket, not *what stage* it
is at, and both facts are needed at once. `[-]` deferred stays orthogonal too:
a parked ticket is parked whatever its stage.

Two encodings were rejected. `### section` headers are topical grouping the
human owns, so states would fight that and every transition would churn the
file. Extra box characters would overload a single character already carrying
four meanings.

**Transitions** need a verb: `hive todo state <ref> <state>`, alongside the
existing `done` / `defer` / `claim`, with the drawer calling the same code
path. Machine transitions (`@plan-review`, `@triage`) are written by the worker
that finished the stage; human transitions (`@ready`, and bouncing backwards)
come from the drawer. Backwards moves take a note, because a ticket sent back
without a reason will be planned identically the second time.

**A worker claims before it works.** `Claim` is what stops two concurrent
`/refine` passes planning the same ticket, and it is already atomic across
worktrees (`docs/superpowers/specs/2026-08-07-todo-concurrency-design.md`). A
ticket whose claim is held is not eligible for dispatch, whatever its state.

## Plan artifacts

`docs/plans/<id>.md`, on the main worktree — the same main-worktree resolution
`hive todo` already performs for `docs/TODO.md`. Linked to its ticket by id
convention, so the todo schema needs no plan field. Committed, so every
worktree and every later reader sees the same artifact.

Contents, in order:

- ticket id and subject
- **the commit it was written against**
- the research digest — real current behaviour, with `file:line` anchors
- the contract — exact files, exact functions, the API surface for new code,
  runnable verification steps
- the critics' verdicts
- **Open Questions** — anything the planner could not settle

That last section is load-bearing rather than decorative. A planner cannot ask
a human anything. When it hits genuine ambiguity its options are to guess or to
write the question down, and writing it down is how a blocked agent hands over
a decision without blocking. A plan that reaches `@plan-review` with three open
questions is a success, not a failure.

The artifact supersedes the `.git/ship/` contract location: `/ship`'s
interrupted-run recovery reads the same file.

## Agents

The work happens one layer down, in **in-process subagents** — everything stays
inside the one claude session. No new processes.

The nesting this needs is already proven. A probe on 2026-08-12 ran
main → `probe-router` → `probe-go-specialist` and got a real, verified finding
back. `planner` → `context-loader` is the same shape at the same depth, so
there is nothing novel here.

Spawning headless `claude -p` workers instead was considered and rejected. It
would buy workers that survive the orchestrator's death and can be killed
individually, but it costs process management, a permissions story for
non-interactive execution, and a fleet of sessions to reason about. Keeping it
in one session is simpler, and the state that matters is on disk regardless.

| Agent | Status | Owns |
|---|---|---|
| `planner` | new | one ticket's refinement, end to end |
| `builder` | new | one ticket's build, end to end, in its worktree |
| `context-loader` | exists | research, fanned out by `planner` |
| `plan-critic` | exists | plan critique and diff-vs-contract conformance |
| `test-writer` | exists | tests; reproduce mode for the red |
| `implementer` | exists | source, from the contract |
| `go-reviewer` / `php-reviewer` / `review-router` | exists | review |

`planner` fans out `context-loader`s, drafts the contract, runs `plan-critic`
over it, writes the artifact, sets `@plan-review`, and returns **one line**:
`lxg → plan-review, 2 open questions`.

`builder` runs the TDD build in its worktree, fans out the reviewers, commits
on the branch, sets `@triage`, and returns one line: `lxg → triage, 3 files,
1 Confirmed finding`.

**Returning one line is a hard requirement, not a style note.** A subagent's
result lands in the orchestrator's context verbatim. A worker that returns its
plan, its diff, or its findings defeats the entire design — the main session
fills up exactly as it would have without the pipeline. Both agent definitions
must state the one-line contract explicitly and give the format.

## Commands

**`/refine [n | ids]`** — dispatches a `planner` per unrefined ticket, all in
one message so they run concurrently. Read-only work, so no worktrees and no
isolation needed. Fans out as wide as you point it.

**`/build [n]`** — dispatches a `builder` per `@ready` ticket, up to a cap
(default 3, a `build_concurrency` key alongside the existing per-workspace
config). One worktree each, via the path `createWorktree` (`worktree.go:177`)
already walks.

**`/backlog-loop`** — alternates the two: refine what is unrefined, build what
is ready, report, repeat. The loop is thin by design, because the state machine
carries continuity between passes rather than the session doing so.

## Concurrency, and its two guards

Capped concurrency's real cost is merge conflicts between in-flight builds. The
plan contract names exact files, so both guards come almost free:

**Overlap guard.** `/build` will not start a ticket whose plan touches files an
in-flight build's plan also touches. It stays queued. Concurrency without
conflicts that become the human's problem.

**Staleness guard.** The plan records its base commit. If the files it names
have moved since, `/build` refuses and returns the ticket to `@plan-review`
rather than executing a contract that no longer describes the code. This is the
same principle as `he-events`'s `gate.sh` sentinel, which refuses to vouch for a
tree it was not recorded against.

## The review surface

Both human queues are worked **outside any claude session** — that is the whole
point of keeping the main context free. For v1 that is the hive drawer, which
already has the list, the keybindings, and the attention ladder in
`attention.go`. Accepting at `@triage` uses the `x:merge+close` binding already
wired to `mergeWorktree` (`worktree.go:385`).

A browser UI for plan review is deliberately deferred, not rejected — see
Deliberately not built.

## Relationship to `/ship`

`/ship` survives unchanged as the attended, single-ticket path.

The two have deliberately **opposite context strategies**. `/ship` pulls work
into the main context because you are collaborating on one hard ticket. The
pipeline pushes everything out because you are supervising many ordinary ones.
Same agents underneath. This is a feature, and the two should not later be
unified into one command with a flag — the flag would be "is a human paying
attention", which changes every stage's behaviour.

## Risks

- **A planner that guesses instead of asking.** The Open Questions section is
  the mitigation, but nothing mechanically enforces its use. A plan that looks
  confident and is wrong costs a build before anyone notices.
- **Plan review becomes a rubber stamp.** A queue of ten plans invites
  skimming, and skimming a contract is how an under-specified plan reaches a
  builder. Batch size is the lever; start small.
- **Two-deep nesting is hard to debug.** When a builder's implementer fails,
  the main session sees one line. The worker transcripts are the record, and
  they are not currently surfaced anywhere convenient.
- **The orchestrator's death kills every in-flight worker.** Subagents live
  inside the one session, so a session limit or a crash takes the whole batch
  with it. This is not hypothetical — the `/ship` design itself was killed by
  session limits twice while being written, and the recovery cost is why plan
  artifacts exist. On-disk state means little is *lost*, but tickets are left
  mid-stage with claims held by a session that no longer exists, and nothing
  currently reaps those. **A stale-claim recovery path is required, not
  optional**: a way to identify claims whose session is gone and return those
  tickets to their last clean state. Prefer small batches until it exists.
- **Ticket quality becomes the bottleneck.** The pipeline consumes tickets
  faster than they are written, and a vague ticket produces a confident,
  useless plan.
- **In-repo plan artifacts couple to every repo's deploy tooling.** Confirmed,
  not hypothetical: `stevenlawton.com` reported two exact-string checks on
  `docs/TODO.md` in `scripts/deploy-watch.sh` — the dirty-tree filter at line
  104 and the docs-only rebase gate at line 135 — either of which would hold
  the staging loop the first time a plan file appeared beside a TODO change.
  One line each to widen, and they have offered to make the change, but this
  will recur in every repo with tooling that reasons about which paths are
  "just docs". The alternative — keeping plans in hive state outside the
  worktree — avoids the coupling entirely at the cost of plans no longer being
  versioned with the code they describe, reviewable in a diff, or present in a
  fresh clone. Keeping them in-repo is the deliberate choice; the recurring
  one-line fix is the price.

## Deliberately not built

- **A browser review UI.** Hive has no inbound HTTP surface today —
  `server.go` is 88 lines of event channels and the only `net/http` is
  outbound notification. The drawer already carries the queue. Build the web UI
  when the drawer has demonstrably run out of room, not before.
- **Hive as the orchestrator.** Walking the state machine in Go would make the
  main context literally zero rather than merely small, and hive already owns
  the todo state, worktrees and tmux. Rejected for now in favour of a claude
  session, which can read an unexpected failure and adapt where Go would only
  fail. Revisit if dispatch turns out to be pure bookkeeping in practice.
- **Headless `claude -p` workers.** Would give workers that survive the
  orchestrator and can be killed individually — the direct mitigation for the
  session-death risk above. Rejected to keep everything inside one session:
  no process fleet, no non-interactive permissions story, no second thing to
  operate. Revisit if session death during a batch turns out to hurt in
  practice rather than in theory.
- **Auto-merge on clean review.** Triage stays a human stop even when review
  finds nothing. Seeing what was built is the point.
- **Auto-refill of the backlog.** The pipeline consumes tickets; it does not
  invent them.
