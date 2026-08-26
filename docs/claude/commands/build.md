---
allowed-tools: Agent, Bash(hive:*), Bash(git:*), Read
description: Build backlog tickets whose plans you have approved — one builder agent per ticket in its own worktree, capped. Each lands in the triage queue for you to look at.
---

Build the tickets whose plans you have already approved. One builder per ticket,
each in its own worktree, each landing in triage for you to look at.

`$ARGUMENTS` may be a count (`/build 2`), a list of ids, or empty.

## 1. Select the tickets

```bash
hive todo list
```

Candidates are tickets at **`·ready`** and unclaimed. `·ready` means *you read
the plan and approved it* — nothing else qualifies. Never promote a
`·plan-review` ticket yourself to give the batch something to do; that gate is
the human's and skipping it is the one failure this pipeline cannot recover from.

**Cap the batch** at this workspace's `build_concurrency` from
`~/.config/hive/config.yaml`, defaulting to **3** when the key is absent. Each
builder holds a worktree, a branch and a slot in your triage queue, so the
default is deliberately small.

If the cap bites, **say how many you left queued** and name them. Silent
truncation reads as "that was everything", and the tickets you dropped look
finished when they never started.

## 2. Overlap check, before any dispatch

For each candidate, read **only the file list out of the contract** in
`<main worktree>/docs/plans/<id>.md` — the files it names under *The contract*.
Not the whole plan. You need the paths, not the reasoning.

Two tickets whose contracts share a file must not build at the same time; the
second one's builder would review a diff containing the first one's work. Hold
the later one back, and say which and why.

If a candidate has no plan artifact at all, it is not buildable. Report it and
move on — do not let a builder improvise a plan.

## 3. A worktree per ticket

Create the worktree **before** dispatching, so two builders can never share a
tree. This is also what gives each builder its own claim identity.

## 4. Dispatch one builder per ticket, all in a single message

One `builder` agent per ticket, every dispatch in one message so they run
concurrently. Give each one its ticket id and its worktree path.

## 5. Report the lines. Nothing else.

A table: ticket, outcome, files, findings.

**Do not read the diffs. Do not read the findings.** They are in the artifact and
on the branch, which is where the human reads them at triage. Pulling them in
here spends the context this whole design exists to protect.

Re-queue anything that came back `QUEUED` or `STALE` and say so plainly:

- `QUEUED` — it overlapped another build. It can go in the next batch.
- `STALE` — its plan is now back at `·plan-review` because the code moved under
  it. It needs `/refine` again, not `/build`.

## 6. Announce on the bus

`hive bus intent` before dispatching, `hive bus done` after — naming the repo and
the count. Builders create worktrees and branches, which peers on this repo can
see, so this one genuinely matters.

## 7. Point at the queue

End with how many tickets are now at `·triage`, and how they are cleared: read
the diff and the `## Review findings` in the artifact **in the hive drawer**,
then accept with `x:merge+close`.
