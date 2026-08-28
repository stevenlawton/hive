---
allowed-tools: Agent, Bash(hive:*), Bash(git:*), Read
description: Refine unrefined backlog tickets — one planner agent per ticket, in parallel, each leaving a plan for review. Your context stays at one line per ticket.
---

Refine the backlog: turn unrefined tickets into reviewable plans, without pulling
a single plan into this session.

`$ARGUMENTS` may be a count (`/refine 3`), a list of ids (`/refine lxg dmy`), or
empty. Default: **1**.

## 1. Select the tickets

```bash
hive todo list
```

A ticket is a candidate when it is **all** of:

- open — not done, not deferred
- **unrefined** — carries no ` ·<state>` tag at all. A ticket already at
  `·plan-review` is waiting on *you*, and re-refining it throws away the
  review it is queued for. `·ready` and `·triage` are likewise not yours.
- unclaimed — no `🔒@<owner>`

If ids were given explicitly, use those and skip the filter, but still refuse a
done or deferred one and say why.

**Cap the batch** at this workspace's `refine_concurrency` from
`~/.config/hive/config.yaml`, defaulting to **1** when the key is absent. The
cap binds on an explicit id list too — `/refine a b c d e f` refines the first
one and queues the rest.

One, because a planner is not one agent. Each dispatches up to three
context-loaders and a critic of its own, so even a single planner is about five
agents, most of them on opus. Three planners is fifteen; six is thirty, which
spends a great deal of money in ten minutes and can still leave nothing behind
if the batch is interrupted before its planners finish.

Raise it per workspace with `refine_concurrency` once you have watched a batch
of one run all the way through and know what one ticket actually costs.

If the cap bites, **say how many you left queued** and name them. Silent
truncation reads as "that was everything", and the tickets you dropped look
finished when they never started.

If there are no candidates, say so and stop. Do not go looking for work.

## 2. Dispatch one planner per ticket, all in a single message

One `planner` agent per ticket, **every dispatch in one message** so they run
concurrently. Give each one exactly one ticket id.

No worktrees. Planners only read code and write a file to `docs/plans/` — there
is nothing for them to collide over in the tree.

Hand each sibling a **distinct id**. The claim will not catch a duplicate for
you: claim identity is per-worktree, so two planners you dispatch share an owner
and both claims would succeed on the same ticket.

## 3. Report the lines. Nothing else.

Put the returned lines in a table — ticket, outcome, open questions.

**Name every ticket by its subject, not its id.** The workers already lead their
returned line with a short subject paraphrase and keep the id as a parenthetical
— `worktree bootstrap (dmy) → plan-review, 3 open questions`, not a bare id — so
just carry that straight into your table. Do not spend an extra `hive todo show
<id>` call re-deriving what the line already told you; that lookup was only ever
a workaround for workers that returned the id alone, and they no longer do.

If a line still comes back id-first — an older worker, or one that slipped —
resolve it with `hive todo show <id>` rather than showing the human a bare id.

Then stop.

**Do not read the plans. Do not summarise them. Do not comment on whether they
look any good.** Every one of those is a lapse, not a helpful extra: the plans
staying out of this context is the entire reason the work was done in subagents.
If you find yourself opening `docs/plans/`, the command has failed even though
the tickets moved.

If a planner returned `FAILED`, `SKIPPED` or `OBSOLETE`, that line is the whole
report for that ticket. Do not go and investigate why.

## 3b. Sending a plan back

`$ARGUMENTS` naming a ticket already at `·plan-review` is normally a mistake —
§1 refuses it, because re-refining throws away the review it is queued for.

The exception is a plan that came back **answered**. When the human has read a
plan and settled its open questions, the round trip is:

1. Append a `## Decisions` section to `docs/plans/<id>.md`, writing each answer
   under the question it settles. The artifact is the carrier, not the ticket —
   ticket bodies are awkward to extend from the CLI, and the next planner reads
   the artifact anyway.
2. Send it back to unrefined with the reason:

   ```bash
   hive todo state <id> clear --note "<what was decided, in one clause>"
   ```

3. `/refine <id>` — the planner reads the decisions as settled and rewrites the
   plan on top of them.

Do this when an answer is **load-bearing** — when it changes the shape of the
contract rather than a detail inside it. A planner will tell you which of its
questions those are. For a small answer, edit the plan yourself and mark it
`ready`; a full re-plan costs more than the correction is worth.

## 4. Announce on the bus

`hive bus intent` before dispatching, `hive bus done` after. Name the repo and
the count — the bus is machine-wide, so "refining 4 tickets" without a repo is
noise to everyone else.

## 5. Point at the queue

End with how many tickets are now at `·plan-review`, and say plainly that they
are reviewed **in the hive drawer, not here** — read the plan, then
`hive todo state <id> ready` to approve it for building.
