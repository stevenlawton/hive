---
allowed-tools: Agent, Bash(hive:*), Bash(git:*), Read
description: Refine unrefined backlog tickets — one planner agent per ticket, in parallel, each leaving a plan for review. Your context stays at one line per ticket.
---

Refine the backlog: turn unrefined tickets into reviewable plans, without pulling
a single plan into this session.

`$ARGUMENTS` may be a count (`/refine 3`), a list of ids (`/refine lxg dmy`), or
empty. Default: **5**.

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

Then stop.

**Do not read the plans. Do not summarise them. Do not comment on whether they
look any good.** Every one of those is a lapse, not a helpful extra: the plans
staying out of this context is the entire reason the work was done in subagents.
If you find yourself opening `docs/plans/`, the command has failed even though
the tickets moved.

If a planner returned `FAILED`, `SKIPPED` or `OBSOLETE`, that line is the whole
report for that ticket. Do not go and investigate why.

## 4. Announce on the bus

`hive bus intent` before dispatching, `hive bus done` after. Name the repo and
the count — the bus is machine-wide, so "refining 4 tickets" without a repo is
noise to everyone else.

## 5. Point at the queue

End with how many tickets are now at `·plan-review`, and say plainly that they
are reviewed **in the hive drawer, not here** — read the plan, then
`hive todo state <id> ready` to approve it for building.
