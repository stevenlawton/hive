---
allowed-tools: Agent, Bash(hive:*), Bash(git:*), Read
description: Walk the backlog one pass — refine what is unrefined, build what you have approved, report both queues. Run /clear first.
---

Walk the backlog one pass. `$ARGUMENTS` is a number of passes; default **1**.

## 1. Reap first

```bash
hive todo reap
```

Subagents die with the session that dispatched them, and a dead planner or
builder leaves its ticket claimed forever. Reaping first stops a previous run's
corpses from locking this batch out of its own work.

Report anything released — a ticket that keeps needing a reap is a ticket whose
builds keep dying, and that is worth knowing.

## 2. One pass: build, then refine

In that order, deliberately.

1. `/build` — drain the tickets you have already approved
2. `/refine` — top up the plan-review queue

Building first means you clear work you have already made a decision about before
generating more decisions for yourself. Refining first would grow the review
queue while the approved queue sat there, which is how a backlog pipeline turns
into a plan graveyard.

## 3. Report both queues

Two numbers, at the end, and they are the entire status:

- **N awaiting plan review** — plans written, waiting on you to read and approve
- **M awaiting triage** — branches built, waiting on you to read and land

## 4. Stop when there is nothing to do, and name the bottleneck

Do not spin. When a pass moves nothing, say which queue is the constraint:

- **Empty `ready` queue, full `plan-review` queue** → *your plan review* is the
  bottleneck. The machine is waiting on you.
- **Full `triage` queue** → *your triage* is the bottleneck. Same.
- **Both empty, nothing unrefined** → the backlog is genuinely done. Say so and
  stop rather than inventing tickets.

## 5. The batch-size caution

State it plainly whenever you run a large pass: **subagents die with this
session.** If a session limit or a crash lands mid-pass, every in-flight planner
and builder dies with it, and the only trace is the claims they were holding.

`hive todo reap` is the recovery. Small batches are the prevention. A pass of 3
that finishes beats a pass of 12 that dies at ticket 9.
