# Move the Backlog Store Out of the Repos — Design

**Date:** 2026-08-25
**Status:** Approved

## Problem

The per-repo backlog lives at `docs/TODO.md` on the repo's main worktree
(`todoFilePath`, `todo.go:116`), inside a `TASKS:BEGIN`/`TASKS:END` block in a
git-tracked file. Hive owns the block; git owns the file. That split ownership
costs more than it returns.

### 1. A hive-owned file in a git tree fights every deploy

`stevenlawton.com/scripts/deploy-watch.sh` carries a `settle_tree()` function
whose only job is absorbing this file. It greps `docs/TODO.md` out of
`git status --porcelain`, auto-commits it as `chore(todo): sync hive task
list`, and holds the deploy on anything else. Its own comment records how the
need was discovered:

```
# Called TWICE - once here, and again immediately before the deploy. The gate
# between them can run 10+ minutes, and the drawer syncing the task list inside
# that window leaves a tree deploy.sh refuses to ship ("code that exists in no
# commit"). Checking once at the top is a time-of-check race, and it fired.
```

`scripts/gate.sh` exempts the path from the deploy sentinel.
`scripts/test-deploy-watch.sh` has cases built around a dirty or diverged
backlog. Both repos' `main-deploy-loop` skills carry their own
stash / commit / rebase choreography for it. None of this machinery is about
tasks. It exists because a file hive writes on its own schedule sits in a tree
that deploys demand be clean.

### 2. The file is in the working tree, so sessions edit it

The backlog is the one piece of hive state a session can reach without going
through hive. When the tool misbehaves, editing the markdown is the obvious
move — and it is what sessions have been told to do. That silently converts
tool bugs into private workarounds, so nobody fixes them. The multi-line
truncation bug fixed in `c95beff` had been eating task bodies across at least
two repos before anyone raised it; the standing advice at the time was to edit
the TASKS block by hand instead.

### 3. Being git-tracked means the file has copies

Every worktree checkout carries its own stale `docs/TODO.md`
(`he-events/.worktrees/{split-1,split-3,redteam}/docs/TODO.md` today). Hive
ignores them via `mainWorktree`, but a human or an agent reading the file in
the worktree they are standing in reads a lie.

Hive already reached this conclusion for the lock file. `todoLockPath`
(`todo_store.go:90`) puts it under `XDG_RUNTIME_DIR` with the comment: *"It
lives outside the repo deliberately: a sidecar in `docs/` would show up as
untracked in `git status` and would ride along with deploy rsyncs."* The same
reasoning applies to the data.

The 2026-08-07 concurrency design listed this as an explicit non-goal —
*"Whether `TODO.md` should be git-tracked, and the churn/deploy consequences of
it living in the main worktree's dirty tree. Real, separate, deliberately out
of scope."* This document is that follow-up.

## Phasing

**P1 (this document).** The store moves under hive's control, outside every
repo. The file format does not change. 251 live tasks across three repos
migrate with their ids intact.

**P2 (later, separate).** With hive owning the interface, the substrate is free
to change — SQLite or otherwise — and the repos get cleaned of the file and the
machinery built around it.

Splitting this way keeps P1 a *path* change rather than a format change, which
matters while agents hold live claims. The markdown layer earns one more
release and dies in P2.

## Goals

- Hive owns the backlog end to end. No part of it lives in a repo.
- The `hive todo` CLI surface is unchanged, verb for verb.
- Existing backlogs migrate with no flag day, no migration command, and no lost
  ids — peers address tasks by id and hold claims across sessions.
- The store survives a repo being moved or re-cloned.
- P1 lands safely on its own, without coordinating the P2 repo cleanup.

## Non-goals

- **The storage substrate.** Markdown stays in P1. Choosing SQLite or anything
  else is P2, and is exactly what P1 makes cheap.
- **Durability and backup.** Today's guarantees (atomic rename + flock) carry
  over unchanged and nothing is added. See Open questions — this is the one
  real regression P1 ships.
- **Deleting `docs/TODO.md` from the repos, and the machinery around it.** P2,
  done per repo by the sessions that own them.
- **A bug-raising channel.** The hive bus already carries this. P2's broadcast
  states the rule; no new mechanism is built.
- **Reworking claims.** Claim identity stays the git branch (`worktreeClaim`,
  `todo.go:99`), and `reap` still resolves live worktrees through git.

## The store

```
$XDG_DATA_HOME/hive/todos/<slug>-<hash8>.md      # default ~/.local/share/hive/todos/
```

Data, not runtime: the lock stays in `XDG_RUNTIME_DIR`, which is cleared on
reboot and is the right home for a lock and the wrong one for a backlog.

`<slug>` is the main worktree's directory name, so the store directory is
readable when something needs debugging — `he-events-a1b2c3d4.md`. It carries
no meaning; the hash is the identity.

## Repo identity

This is the decision P1 turns on. `todoLockPath` currently keys off
`sha256(mainWorktree(repoPath))[:4]` — a *path* hash. That is adequate for a
lock, which may be recreated freely, and wrong for data: moving or re-cloning a
repo would silently orphan its backlog.

The key resolves the first of these that is available:

1. **Normalized git remote URL** (`origin`, lowercased, scheme and `.git`
   suffix stripped, so `https://github.com/x/y.git` and `git@github.com:x/y`
   agree). Unique per project.
2. **First-commit SHA** (`git rev-list --max-parents=0 HEAD`, first entry).
   Survives a repo with no remote being moved or renamed.
3. **Main worktree path.** Last resort for a non-git directory, matching
   today's behaviour.

Consequences, all of them wanted:

- Every worktree of a repo shares one backlog, as today — they share a remote.
- A re-clone to a new path *finds* its backlog rather than starting empty.
- A fork gets its own backlog: different remote.
- A repo that gains its first remote re-keys once. Handled by the lookup order
  below.

`todoLockPath` re-keys off the same value, so the lock and the store cannot
disagree about which repo they are serving.

### Lookup order

Resolving a store is not a single key but a walk down the same ladder. Hive
takes the highest-precedence key that resolves, and if no store exists for it,
tries the stores for the lower-precedence keys before concluding there is none:

```
remote key      → store exists? use it
first-commit key → store exists? adopt it (rename to the remote key)
path key         → store exists? adopt it (rename to the remote key)
none             → import from the repo file, or start empty
```

Adoption renames the file, so the walk happens once per re-key rather than on
every call. This is what makes "a repo that gains a remote" and "a repo that
moves before it has a remote" non-events rather than silent data loss.

### Key resolution cost

`hive todo statusline` runs on **every Claude turn**, and already spends two
subprocesses there (`git worktree list`, `git rev-parse`). Resolving the key
would add a third.

So the resolved key is memoised at
`$XDG_RUNTIME_DIR/hive/repokey-<sha8 of main worktree path>`: a single line,
written on first resolution, read on every call after. Runtime dir, so it
clears on reboot and can be deleted at any time to force re-resolution — it is
a cache, never a source of truth. The store path it names is still checked for
existence, so a stale memo cannot point hive at a store that has moved.

## Import on first touch

Migration is automatic and has no command.

When hive resolves a store path that does not yet exist, it looks for that
repo's `docs/TODO.md` (or root `TODO.md`) and, if it holds a TASKS block,
imports the block wholesale — ids, claims, states, `since` timestamps, sections,
done and deferred flags, in order. Once the store file exists it is never
consulted again.

The import runs **inside the existing flock** in `withTodos`
(`todo_store.go:18`), so concurrent worktrees cannot both import. The lock is
taken before the store path is read, which it already is.

Import triggers on first *access*, read or write — not on writes alone.
`loadTodos` (`todo.go:133`) is a bare `os.ReadFile` today and must route
through the same lock-and-import path when the store is missing. Otherwise the
first thing to touch a repo after the move is usually `hive todo statusline`,
which would read nothing, render an empty backlog, and leave the import to
whatever ran next.

That puts the import on the statusline's path, so **the notice goes to
stderr**. `hive todo statusline` writes the status line itself to stdout, and a
migration notice on that stream would be rendered into Steve's prompt.

Preserving ids is not optional. Sessions record ids in plans, handovers and bus
messages, and `he-events` has live claims held by running agents right now. An
import that reminted ids would strand every one of them.

The first import for a repo prints a one-line notice naming the store path and
the count, so the move is visible rather than silent. The repo's file is left
untouched — deleting it is P2's business.

## What does not change

The file format. Same `TASKS:BEGIN`/`TASKS:END` markers, same `parseTodos` and
`formatTodos`, same single-line and six-space-continuation rendering from
`c95beff`. `extractBlock`/`replaceBlock` keep preserving content outside the
markers even though the store has none — the affordance costs nothing and its
removal is P2 churn.

The CLI. All twelve verbs (`list`, `claim`, `show`, `done`, `edit`, `state`,
`reap`, `add`, `statusline`, `rm`, `reopen`, `defer`) keep their arguments and
their output shape, except the one line noted below. Every slash command and
agent on this machine drives hive through these verbs and touches no file
directly, so none of them need changing.

## Changes

| Location | Change |
|---|---|
| `todo.go:116` `todoFilePath` | Becomes `todoStorePath(repoPath)`, returning the XDG path. Repo-file lookup survives only inside the importer. |
| `todo.go` (new) | `repoKey(repoPath)` — remote → first-commit → path, with the adoption walk and the runtime-dir memo. |
| `todo.go:133` `loadTodos` | Routes through the lock-and-import path when the store is missing, instead of a bare `os.ReadFile`. |
| `todo_store.go:18` `withTodos` | Import-on-first-touch, inside the lock; notice to stderr. |
| `todo_store.go:90` `todoLockPath` | Re-keys off `repoKey`. |
| `cmd_todo.go:169-173` | The `(uncommitted)` line and its comment go; prints the store path instead. |
| `.gitignore:4` | `.TODO.md.*.tmp` no longer needed — temp files are written beside the store. |
| `todo_store_test.go` | Layout assertions (`docs/TODO.md` / root `TODO.md` precedence, lock-outside-repo) rework against the store path. |
| `docs/claude-commands/todo.md` + `~/.claude/commands/todo.md` | Four lines of prose describe the storage location. Also reconcile `next.md`, whose repo copy has drifted from the live one. |
| `2026-08-07-todo-concurrency-design.md:57-59` | Mark the deferred question resolved, pointing here. |

`mainWorktree` (`todo.go:84`) stays: `liveWorktreeBranches` (`todo.go:568`)
needs it for `reap`, and it provides the slug and the last-resort key.

`stripSyncLine` (`todo_store.go:53`) loses its stated justification — there is
no git-tracked file to avoid churning — but the no-op-write behaviour is still
correct and cheap. It stays in P1 and dies with the format in P2.

## Why P1 is safe to land alone

The P2 cleanup is not a prerequisite. After the move, each repo's
`docs/TODO.md` simply stops changing:

- `settle_tree()` finds no dirty `docs/TODO.md` and returns early. It never
  fires again.
- `git stash push -m loop -- docs/TODO.md` on an unchanged tracked file is a
  no-op.
- `gate.sh`'s exemption for the path becomes irrelevant, not wrong.

All of it goes quiet and becomes dead code. There is no window in which P1 has
landed and something is broken pending P2.

## Testing

- **Key resolution.** Remote wins over first-commit wins over path; two
  worktrees of one repo resolve identically; a repo copied to a new path keeps
  its key; a fork with a different remote does not collide.
- **Adoption.** A store written under the path key is found and renamed when the
  repo later gains a remote, and the tasks survive the rename. A stale memo
  naming a store that no longer exists falls back to resolution rather than
  reporting an empty backlog.
- **Statusline.** `hive todo statusline` triggering the first import writes the
  status line to stdout and the migration notice to stderr — nothing but the
  status line reaches stdout.
- **Import.** A repo with a TASKS block imports every task with ids, claims,
  states, `since`, sections and flags intact and in order. A second call does
  not re-import. A repo whose file has no TASKS block imports nothing and
  starts empty. Concurrent importers under the lock produce one store, not two.
- **Round trip.** The existing format tests keep passing unchanged against the
  new path — the strongest evidence that P1 changed only where the bytes live.
- **No repo writes.** After a mutation, the repo tree is unchanged: no modified
  `docs/TODO.md`, and no `.TODO.md.*.tmp` left in `docs/`.

## Open questions

**Durability.** The moment P1 lands, 251 tasks live in exactly one
unbacked-up place. Today `git push` is the backup; afterwards there is none.
Deliberately deferred, but it should not outlive P2 — a deleted worktree took
13 finished review batches with it on this machine today, for exactly this
reason.

**Backlog history.** 135 commits across the three repos record who added and
closed what, alongside the code change that did it. Those commits survive in
git, but the backlog stops accumulating history at P1 and `git log -p
docs/TODO.md` stops being useful going forward. Accepted; noted because it is
not recoverable later.
