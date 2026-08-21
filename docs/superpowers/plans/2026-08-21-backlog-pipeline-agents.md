# Backlog Pipeline Agents and Commands Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the `planner` and `builder` agents and the `/refine`, `/build` and `/backlog-loop` commands that walk the backlog states, keeping the orchestrating session's context to one line per ticket.

**Architecture:** Everything is prompt files under `~/.claude/`. Work happens in in-process subagents; the orchestrating command dispatches one `planner` or `builder` per ticket and collects a single line back. Ticket state and plan artifacts on disk are the only persistent state.

**Tech Stack:** Markdown prompt files with YAML frontmatter. No code.

**Spec:** `docs/superpowers/specs/2026-08-21-backlog-pipeline-design.md`

**Depends on:** `docs/superpowers/plans/2026-08-21-backlog-states.md` must be implemented and installed first — every command here calls `hive todo state`, which does not exist until then.

## Global Constraints

- Files live in `~/.claude/agents/` and `~/.claude/commands/`, outside any repo.
- **These are prompt files: they have no unit tests.** Each task's verification is a live run against a real ticket with a stated expected outcome. Do not claim a task is done without running it and reading the output.
- Agent frontmatter must be valid YAML with `name`, `description`, `tools`, `model`. Verify with the loop in the Manual verification section.
- A worker returns **one line**. This is the single most important constraint in this plan: a subagent's result lands verbatim in the orchestrator's context, so a chatty worker defeats the entire design.
- States are exactly `plan-review`, `ready`, `triage`, set via `hive todo state <ref> <state>`.
- Plan artifacts live at `docs/plans/<id>.md` on the repo's main worktree.

---

### Task 1: The plan artifact template

**Files:**
- Create: `~/.claude/docs/templates/plan-artifact.md`

**Interfaces:**
- Produces: the artifact shape every `planner` writes and every `builder` and human reads. Tasks 2, 3 and 6 reference this path.

- [ ] **Step 1: Write the template**

```markdown
# <subject>

**Ticket:** <id>
**Repo:** <repo name>
**Base commit:** <full sha at the time of writing>
**Written:** <YYYY-MM-DD>

## Current behaviour

What the code does today, with `file:line` anchors. Verified by reading, not
assumed.

## Root cause

For a bug: the mechanism, and the evidence for it. For a feature: why the
current shape does not accommodate the requirement.

## The contract

Exact files to change. Exact functions. The full API surface for anything new —
signatures, parameters, return types. Written so an agent that cannot ask a
question can execute it without inventing anything.

## Verification

Runnable commands, and what output means success. Not "run the tests".

## Blast radius

Other call sites, other implementations of a changed interface, persisted data
whose shape changes, tests that encode current behaviour.

## Critic findings

What the critics found and what changed as a result. Where a critic was wrong,
that it was wrong and why.

## Open questions

Anything that could not be settled without a human. **Each needs enough context
to answer cold** — the question, the options, and what you would choose. Empty
is fine; guessing is not.
```

- [ ] **Step 2: Verify it is complete**

Read it back and confirm every section would be answerable by an agent that has just finished researching a ticket, and that "Open questions" makes clear a guess is worse than a question.

- [ ] **Step 3: Commit**

Not in a repo — no commit. Confirm the file exists at the path above.

---

### Task 2: The `planner` agent

**Files:**
- Create: `~/.claude/agents/planner.md`

**Interfaces:**
- Consumes: the template from Task 1; existing `context-loader` and `plan-critic` agents.
- Produces: agent `planner`, taking a ticket id, returning exactly `<id> → plan-review, N open questions` or `<id> → FAILED, <one clause>`.

- [ ] **Step 1: Write the frontmatter verbatim**

```yaml
---
name: planner
description: Refines one backlog ticket end to end — researches the code, drafts an executable plan contract, has it critiqued, writes the plan artifact, and moves the ticket to plan-review. Returns a single line. Never asks questions; unresolved ambiguity is written into the plan's Open Questions.
tools: Agent, Read, Grep, Glob, Bash, Write
model: opus
---
```

- [ ] **Step 2: Write the body, covering these points**

Required content, in this order:

1. **The one-line contract, stated first and unmissably.** The body must say that the caller's context is the scarce resource, that the full plan goes in the artifact and *only* a single line comes back, and give both formats verbatim: `lxg → plan-review, 2 open questions` and `lxg → FAILED, could not locate the reported endpoint`.
2. **Claim before working** — `hive todo claim <id>`. If the claim is refused, return `<id> → SKIPPED, claimed by <owner>` and stop. This is what stops two `/refine` passes colliding.
3. **Research** — dispatch `context-loader` agents in a single message, one per independent anchor cluster from the ticket. Not one agent for everything.
4. **Stop the line if the ticket is obsolete** — if research shows it is already fixed, return `<id> → OBSOLETE, <evidence>` and do not invent work.
5. **Draft the contract** into the Task 1 template, recording the base commit from `git rev-parse HEAD`.
6. **Critique** — dispatch two `plan-critic` agents in one message, fold in what holds, and record in Critic findings where a critic was wrong and why.
7. **Write** the artifact to `docs/plans/<id>.md` on the main worktree (`git worktree list --porcelain`, first entry).
8. **Transition** — `hive todo state <id> plan-review`, then release the claim with `hive todo claim clear`.
9. **Never guess.** State explicitly: a plan reaching plan-review with three open questions is a success; a plan that guessed and looks confident is the failure this design is most exposed to.

- [ ] **Step 3: Verify the frontmatter parses**

Run:
```bash
python3 -c "
import yaml,sys
t=open('/home/steve/.claude/agents/planner.md').read()
print(yaml.safe_load(t.split('---',2)[1])['name'])"
```
Expected: `planner`

- [ ] **Step 4: Live-run it on one real ticket**

In a repo with an unrefined ticket, dispatch the `planner` agent with that ticket id.
Expected: one line back matching the contract; `docs/plans/<id>.md` exists and has a base commit; `hive todo list` shows `·plan-review`; the claim is released.
If the agent returned more than one line, fix the body and repeat — this is the failure mode that breaks the design.

---

### Task 3: The `builder` agent

**Files:**
- Create: `~/.claude/agents/builder.md`

**Interfaces:**
- Consumes: the artifact from Task 1; existing `test-writer` (reproduce mode), `implementer`, `review-router`, `go-reviewer`, `php-reviewer`, `plan-critic` (conformance mode).
- Produces: agent `builder`, returning exactly `<id> → triage, N files, M Confirmed findings` or a `FAILED`/`STALE` line.

- [ ] **Step 1: Write the frontmatter verbatim**

```yaml
---
name: builder
description: Builds one backlog ticket from its approved plan contract — TDD with a verified red, then a parallel review fan-out, then a commit on its own branch, then moves the ticket to triage. Returns a single line. Works only from the contract; never redesigns.
tools: Agent, Read, Write, Edit, Grep, Glob, Bash
model: opus
---
```

- [ ] **Step 2: Write the body, covering these points**

1. **The one-line contract**, as in Task 2, with formats: `lxg → triage, 3 files, 1 Confirmed finding`, `lxg → STALE, plan base 9fd348c but api.go moved`, `lxg → FAILED, could not reach a green suite`.
2. **Claim, then check the guards before doing any work:**
   - **Staleness** — compare the plan's base commit against the files the contract names (`git log <base>..HEAD -- <files>`). If they moved, `hive todo state <id> plan-review --note "plan stale: <files> moved since <base>"` and return the `STALE` line.
   - **Overlap** — if another in-flight build's plan names any of the same files, return `<id> → QUEUED, overlaps <other id>` without starting. The caller re-queues it.
3. **TDD, following `superpowers:test-driven-development`.** Bug: `test-writer` **in reproduce mode** (state that the agent defaults to coverage mode and will iterate to green unless told otherwise), then **verify the red yourself** — an assertion failure matching the reported bug, not a compile error, and not a test that passes by asserting the buggy behaviour. Then `implementer`. Feature: stubs, then `test-writer` and `implementer` in one message, then verify the red at the join.
4. **Verify with the repo's own gate.** Read `scripts/gate.sh`'s header for its subcommands rather than guessing — they are not standardised across repos. Fall back to the repo's plain test command.
5. **Review** — dispatch `review-router` on the diff and `plan-critic` in conformance mode on diff-versus-contract, in one message. Define the diff explicitly: `git diff` plus `git status --porcelain` for untracked files, which `git diff` will not show.
6. **Commit** on the worktree's branch, ticket subject in the message, never the id.
7. **Append findings to the plan artifact** under a `## Review findings` heading, preserving Confirmed versus Suspected. This is what the human reads at triage, so it is not optional.
8. **Transition** — `hive todo state <id> triage`, release the claim.
9. **Do not fix review findings.** Triage is a human decision.

- [ ] **Step 3: Verify the frontmatter parses**

Run the same python check as Task 2 with `builder.md`.
Expected: `builder`

- [ ] **Step 4: Live-run it on one ticket that has an approved plan**

Move a refined ticket to `ready`, then dispatch `builder` in a worktree.
Expected: one line back; a commit on the branch; `·triage` in the list; review findings appended to the artifact; no findings auto-fixed.

---

### Task 4: `/refine`

**Files:**
- Create: `~/.claude/commands/refine.md`

**Interfaces:**
- Consumes: the `planner` agent from Task 2.
- Produces: `/refine [n | ids...]`, default n=5.

- [ ] **Step 1: Write the frontmatter verbatim**

```yaml
---
allowed-tools: Agent, Bash(hive:*), Bash(git:*), Read
description: Refine unrefined backlog tickets — one planner agent per ticket, in parallel, each leaving a plan for review. Your context stays at one line per ticket.
---
```

- [ ] **Step 2: Write the body, covering these points**

1. **Select** unrefined tickets: `hive todo list`, taking those with no state tag, not done, not deferred, unclaimed. `$ARGUMENTS` may name ids explicitly or give a count; default 5.
2. **Dispatch one `planner` per ticket, all in a single message** so they run concurrently. Read-only work — no worktrees.
3. **Report a table** of the returned lines and nothing else. State explicitly: do not read the plans, do not summarise them, do not comment on their quality. The whole point is that the plans stay out of this context.
4. **Announce** with `hive bus intent` before dispatching and `hive bus done` after, naming the repo and the count.
5. **Point at the queue** — end by saying how many tickets are now in `plan-review` and that they are reviewed in the hive drawer, not here.

- [ ] **Step 3: Live-run it**

Run `/refine 2` in a repo with at least two unrefined tickets.
Expected: two plan artifacts written, two tickets at `·plan-review`, and a main-session context containing two lines — not two plans. Check the transcript: if plan content leaked into the session, the command body needs to be firmer.

---

### Task 5: `/build`

**Files:**
- Create: `~/.claude/commands/build.md`

**Interfaces:**
- Consumes: the `builder` agent from Task 3; `buildConcurrency` from the states plan (Task 7 there).
- Produces: `/build [n | ids...]`.

- [ ] **Step 1: Write the frontmatter verbatim**

```yaml
---
allowed-tools: Agent, Bash(hive:*), Bash(git:*), Read
description: Build backlog tickets whose plans you have approved — one builder agent per ticket in its own worktree, capped. Each lands in the triage queue for you to look at.
---
```

- [ ] **Step 2: Write the body, covering these points**

1. **Select** `·ready` tickets, unclaimed. Cap at the workspace's `build_concurrency` (default 3); say plainly how many were left queued rather than silently truncating.
2. **A worktree per ticket** before dispatch, so builders cannot collide in one tree.
3. **Overlap check before dispatching** — read each candidate's `docs/plans/<id>.md` contract for the files it names, and do not dispatch two tickets whose contracts share a file. Say which were held back and why.
4. **Dispatch one `builder` per ticket in a single message.**
5. **Report the returned lines as a table.** Do not read the diffs. Re-queue anything that came back `QUEUED` or `STALE`, and say so.
6. **Announce** on the bus before and after.
7. **Point at the queue** — how many are now in `triage`, reviewed in the drawer, accepted with `x:merge+close`.

- [ ] **Step 3: Live-run it**

With one `ready` ticket, run `/build 1`.
Expected: a worktree, a commit on its branch, ticket at `·triage`, one line in your context.

---

### Task 6: `/backlog-loop`

**Files:**
- Create: `~/.claude/commands/backlog-loop.md`

**Interfaces:**
- Consumes: `/refine` and `/build`.
- Produces: `/backlog-loop [passes]`, default 1.

- [ ] **Step 1: Write the frontmatter verbatim**

```yaml
---
allowed-tools: Agent, Bash(hive:*), Bash(git:*), Read
description: Walk the backlog one pass — refine what is unrefined, build what you have approved, report both queues. Run /clear first.
---
```

- [ ] **Step 2: Write the body, covering these points**

1. **Reap first** — `hive todo reap`, so claims from a dead previous run do not lock the batch. Report anything released.
2. **One pass** is: `/build` the ready tickets, then `/refine` the unrefined ones. Build first, deliberately — it drains the queue you have already approved before adding more for you to review.
3. **Report both queues** at the end: how many await plan review, how many await triage. These two numbers are the whole status.
4. **Stop when there is nothing to do**, and say which queue is the bottleneck — an empty ready queue means your plan review is the constraint; a full triage queue means your triage is.
5. **State the batch-size caution**: subagents die with this session, so a large batch loses more when a session limit hits. `hive todo reap` is the recovery; small batches are the prevention.

- [ ] **Step 3: Live-run it**

Run `/backlog-loop` on a repo with tickets in both states.
Expected: reap runs, both halves dispatch, two queue counts reported, context still small.

---

### Task 7: Point `/ship` at the shared plan artifact

**Files:**
- Modify: `~/.claude/commands/ship.md` (§0 resume check, §5 contract write, §9 cleanup)

**Interfaces:**
- Consumes: the artifact path convention from Task 1.

`/ship` currently writes its contract to `$(git rev-parse --git-dir)/ship/plan-<taskid>.md`. The spec supersedes that: one artifact location, so a ticket refined by `/refine` can be built by `/ship` and vice versa.

- [ ] **Step 1: Replace the path in §5**

Change the contract location to `docs/plans/<taskid>.md` on the main worktree, and replace the paragraph justifying the git-dir location with the reason for this one: it is the same artifact the pipeline reads and the human reviews, committed and shared, rather than private to one run.

- [ ] **Step 2: Update §0's resume check**

Look for `docs/plans/<taskid>.md` plus a ticket state of `plan-review` or `ready`, rather than a file in the git dir.

- [ ] **Step 3: Update §9's cleanup**

The artifact is now a committed record, so do not delete it on landing. Commit it with the change instead.

- [ ] **Step 4: Verify consistency**

Run: `grep -n 'git-dir\|\.git/ship' ~/.claude/commands/ship.md`
Expected: no matches.

---

### Task 8: Make `/next` state-aware

**Files:**
- Modify: `~/.claude/commands/next.md` (§2 selection)

`/next`'s selector greps for `^[a-z]{3}\s+\[ \]` and excludes `🔒@`. With states rendering as ` ·plan-review`, it would happily claim a ticket that is waiting on your review and start coding it — jumping the gate.

- [ ] **Step 1: Update the selector**

Exclude tickets carrying a state tag other than `ready`, and note in the body that `·plan-review` and `·triage` are human queues worked in the drawer, not picked up here.

- [ ] **Step 2: Verify against a real list**

With one ticket in each state, run the selector command from `next.md` by hand.
Expected: the unrefined and `ready` tickets are candidates; `plan-review` and `triage` ones are not.

---

## Manual verification

Frontmatter across everything written here:

```bash
for f in ~/.claude/agents/planner.md ~/.claude/agents/builder.md; do
  python3 -c "
import yaml,sys
t=open('$f').read()
d=yaml.safe_load(t.split('---',2)[1])
print('OK', d['name'], '|', d['model'], '|', d['tools'])"
done
```

Then a full lap on one throwaway ticket: `/refine 1` → read the plan in the drawer → `hive todo state <id> ready` → `/build 1` → read the diff → `x:merge+close`.

The check that matters at the end is not that it worked — it is **how much of your context it cost**. If the session holds plans, diffs or findings rather than one line per ticket, the design has not landed, whatever the tickets say.
