---
allowed-tools: Bash(hive todo:*), Bash(git log:*), Bash(git show:*), Bash(git grep:*), Read, Grep, Glob
description: Start the next task — claim this worktree's next unclaimed hive item and run the /pickup workflow on it. Run /clear first for a fresh context.
---

# /next

Take the next task off the hive list and pick it up. Like `/pickup`, this ends at
a write-up the user confirms — **do NOT start editing code**.

Meant to be typed straight after `/clear`, so each task starts on a clean
context. This command cannot clear the context itself — see the last section.

## 1. Don't take a second ticket

```
hive todo show
```

If this worktree already has a claim, **that is the task** — go to step 3 and
leave the list alone. A worktree working two tickets at once is how half-finished
work gets stranded.

## 2. Otherwise, take the next one

```
hive todo list | sed 's/\x1b\[[0-9;]*m//g' | grep -E '^[a-z]{3}[[:space:]]+\[ \]' | grep -v '🔒@' | head -1
```

The boxes are `[ ]` open, `[x]` done, `[-]` deferred — deferred is parked on
purpose and sinks to the bottom, so never pick one. A `🔒@branch` suffix means
another worktree holds it; `(yours)` means this one does. This selects the same
item `hive todo statusline` reports as `next:` — if the two disagree, trust
`statusline` and say so.

Take the id from the first column and claim it:

```
hive todo claim <id>
```

Then run `hive todo show` to read what you just claimed, and use its **subject**
whenever you tell a human what you are working on — announcing on the bus, in a
commit, in the write-up. `claiming ffy` means nothing to anyone; the id is for
commands only.

Nothing matched? Every task is done, deferred, or claimed elsewhere. Say so and
STOP — do not invent work or steal another worktree's ticket.

If **$ARGUMENTS** names a task id, use that instead of the next one.

## 3. Pick it up

Read `~/.claude/commands/pickup.md` and follow it exactly for the claimed task.
It is the single definition of the pickup workflow — load the real context, decide
whether the issue is still real, and only then plan. Don't paraphrase it from
memory.

## Why you still type /clear yourself

`/clear` is handled by the CLI, not by the model, and there is no tool that
invokes it — so no command or skill can clear its own context. The sequence is
`/clear` then `/next`.

To collapse that to one step, a `SessionStart` hook with the `clear` matcher can
run this automatically after every clear. That is a settings.json change, not a
command — and it fires on *every* `/clear`, including the ones where you cleared
to do something else entirely.
