# Concurrency-Safe TODO Mutations — Design

**Date:** 2026-08-07
**Status:** Approved

## Problem

The per-repo backlog (`docs/TODO.md` on the main worktree) is written by many
actors at once: one `hive todo` CLI process per Claude session, plus the hive
TUI drawer. Nothing serialises them, and nothing addresses a task by a stable
identity. Two failure modes result.

### 1. Positional indices select the wrong task

Mutating verbs take a 1-based position in the flattened list (`todoIndex`,
`cmd_todo.go:200`). Positions shift whenever any peer adds, removes, or
re-sections a task. The `/todo` skill instructs agents to run `hive todo list`
first to read the numbers, which opens an arbitrarily long window between read
and write:

```
session A: hive todo list      → sees #7 = "fix breadcrumb"
session B: hive todo rm 2      → everything below shifts up
session A: hive todo done 7    → marks "#178 deploy hardening" done
```

No error is reported. A prints success. The wrong task is now `[x]`, and the
corruption is committed to git. This is not a lost update but a *wrong* update,
which is worse: the list silently stops describing reality.

The drawer has the same hazard over an even longer window. Pressing `e`
captures `m.drawerEditIdx` (`drawer.go:222`); the edit is applied when you press
enter, however many seconds of typing later, to whatever now occupies that
index.

### 2. Whole-array writes clobber peers

Every mutation is `loadTodos` → mutate slice → `saveTodos`, which regenerates
the entire TASKS block (`todo.go:99`). There is no lock, no atomic rename, and
no compare-and-swap, so interleaved writers lose each other's changes.

For the CLI the window is milliseconds. The drawer is the serious offender: it
loads into `m.drawerTodos` on open and writes that whole array on every
mutating keypress (`persistDrawer`, five call sites). A tick-driven
`refreshDrawerFromDisk` re-reads from disk, but it deliberately skips while the
user is mid add/edit (`drawer.go:118`) — so anything peers do during your
typing is reverted when you hit enter.

## Goals

- A mutation targets the task the caller meant, regardless of concurrent peers.
- Concurrent mutations from any number of processes all survive.
- Existing files migrate with no flag day and no migration script.
- Content outside the `TASKS:BEGIN`/`TASKS:END` markers stays untouched, as now.

## Non-goals

- Whether `TODO.md` should be git-tracked, and the churn/deploy consequences of
  it living in the main worktree's dirty tree. Real, separate, deliberately out
  of scope.
  **Resolved 2026-08-25** by `2026-08-25-todo-store-out-of-repo-design.md`: the
  store moved out of the repos entirely.
- Reaping stale claims left by deleted worktrees.
- Windows support. `syscall.Flock` is unix-only, so this makes hive formally
  unix-only; it is already effectively so, since it shells out to tmux
  throughout.
- Merging divergent edits. Last-writer-wins per *field* is fine; the goal is
  that concurrent writes to *different* tasks all survive.

## Stable ids

`Todo` gains an `ID string`, persisted in the existing trailing HTML comment
alongside the claim:

```
- [~] **#114 Club recent_events: cap window** - desc <!-- @split-1 id:kdx -->
```

### Alphabet

Ids are **three lowercase consonants** (`bcdfghjklmnpqrstvwxyz`, 21 chars)
drawn from `crypto/rand`. Two properties matter:

- **No digits.** This makes "is this argument an id or a position?" unambiguous
  forever, which is what lets the positional fallback survive safely.
- **No vowels.** No accidental words in a file a human reads.

21³ = 9261, so with a few dozen tasks a collision is well under 1% per draw.
Generation retries against the ids present in the current list; after 100
collisions it widens to four characters rather than looping.

### Comment grammar

The trailing comment is parsed as space-separated tokens between `<!--` and
`-->`:

| Token   | Meaning        |
|---------|----------------|
| `@X`    | `Claim = X`    |
| `id:Y`  | `ID = Y`       |

Tokens may appear in any order, and either may be absent. Unrecognised tokens
are ignored, so a future key does not break an older binary. The comment is
stripped from the subject/description text only if **at least one token was
recognised** — a genuine HTML comment in a description is left alone. This
generalises the current parser, which only recognises an inner string starting
with `@` (`todo.go:214`).

Three shapes must round-trip:

```
<!-- @split-1 -->            legacy, no id yet
<!-- id:kdx -->              unclaimed but identified
<!-- @split-1 id:kdx -->     both
```

### Emission

`formatTodos` currently suppresses the whole comment when a task is done or
deferred (`todo.go:261`). **Ids must outlive that** — otherwise a completed task
loses its identity and `hive todo reopen kdx` cannot find it. The rule becomes:

```go
var toks []string
if t.Claim != "" && !t.Done && !t.Deferred {
    toks = append(toks, "@"+t.Claim)
}
if t.ID != "" {
    toks = append(toks, "id:"+t.ID)
}
// emit " <!-- " + strings.Join(toks, " ") + " -->" when len(toks) > 0
```

The claim suppression is retained as belt-and-braces; `toggleTodoDone` and
`deferTodo` already clear `Claim` in memory.

## The lock + delta helper

Every mutation, CLI and TUI alike, funnels through one function:

```go
// withTodos serialises a read-modify-write against the repo's backlog. mutate
// receives the list as it exists on disk right now, under an exclusive lock.
func withTodos(repoPath string, mutate func([]Todo) []Todo) ([]Todo, error)
```

Steps:

1. Resolve the file via `mainWorktree` / `todoFilePath` (unchanged).
2. Acquire an exclusive `syscall.Flock` on a sidecar lock file, blocking.
3. Read the file **fresh, inside the lock**.
4. Parse; backfill an id onto every task lacking one; call `mutate`.
5. `replaceBlock` against the content read in step 3, preserving everything
   outside the markers.
6. Write a temp file in the same directory, then `os.Rename` over the target.
7. Release the lock (deferred close); return the resulting list.

`saveTodos` is no longer exported as a "write my whole array" entry point. That
removal is the load-bearing part of this design: it makes the drawer clobber
unrepresentable rather than merely unlikely.

### Why the lock file is a sidecar, and where it lives

It must not be a lock on `TODO.md` itself, because step 6 replaces the file by
rename. A peer blocking on the old inode would be guarding a file that is no
longer live.

It must not sit next to `TODO.md` either. A `docs/.TODO.md.lock` would appear as
untracked noise in `git status`, and on `stevenlawton.com` it would be rsynced
to production by `deploy.sh`.

So: `$XDG_RUNTIME_DIR/hive/todo-<hash>.lock`, falling back to `os.TempDir()`,
where `<hash>` is the first 8 hex chars of the SHA-256 of the resolved main
worktree path. The parent directory is created on demand. Locks are released by
the kernel when the process exits, so a killed session cannot wedge the file and
no timeout is needed.

### Temp file placement

The temp file *must* live in the target directory for the rename to be atomic
(same filesystem). It is created as `.TODO.md.<rand>.tmp` — a dotfile — and
removed if any step before the rename fails. It is visible in the working tree
for microseconds, which is an accepted, noted risk.

## Addressing: id first, position as fallback

`hive todo list` prints the id as the primary handle:

```
kdx  [ ] #114 Club recent_events: cap window to last 6-12 months
```

A new resolver maps a CLI argument to an index:

```go
// resolveTodoRef maps a CLI argument to an index: an exact (case-insensitive)
// id match first, then a 1-based position. Ids never contain digits, so the two
// forms cannot collide.
func resolveTodoRef(todos []Todo, arg string) (int, bool)
```

No prefix matching — three characters is already short enough to type whole.

**Resolution happens inside the `mutate` closure**, against the fresh list, not
before the call. This is the entire point; resolving outside the lock would
reintroduce the race the design exists to remove. The pure index-based helpers
(`toggleTodoDone`, `claimTodo`, `deferTodo`, `deleteTodo`) keep their current
signatures and tests; callers resolve within the closure and report outcomes via
captured variables:

```go
var msg string
var failed error
_, err := withTodos(cwd, func(ts []Todo) []Todo {
    i, ok := resolveTodoRef(ts, args[0])
    if !ok {
        failed = fmt.Errorf("no such task %q", args[0])
        return ts
    }
    ts = toggleTodoDone(ts, i)
    msg = "done " + ts[i].ID + ": " + ts[i].Subject
    return ts
})
```

The `/todo` skill doc (`docs/claude-commands/todo.md`, mirrored at
`~/.claude/commands/todo.md`) changes to instruct ids exclusively, and its
"run `list` first, numbers shift after `rm`" rule is replaced by "ids are
stable; `list` is only needed to discover them."

### Retaining the positional fallback

Positions remain valid arguments. The race they carry is between agents, and
agents are steered to ids by the skill doc; a human at a terminal keeps
`hive todo done 3` working. Dropping the fallback would be strictly safer at the
cost of that ergonomics — decided in favour of keeping it.

## Drawer changes

`persistDrawer()` is deleted. Each mutating keypress handler calls `withTodos`
with an id-addressed delta and assigns the returned fresh list to
`m.drawerTodos`. Note that `enter` appears twice below: in input mode it submits
an add or an edit, and in navigation mode it toggles a claim.

| Key                 | Delta                                    |
|---------------------|------------------------------------------|
| `enter` (add mode)  | append a task to the cursor's section    |
| `enter` (edit mode) | set subject/description of a captured id |
| `space` / `x`       | toggle done on the cursor's id           |
| `~` / `enter` / `c` | toggle claim on the cursor's id          |
| `>`                 | toggle deferred on the cursor's id       |
| `d`                 | delete the cursor's id                   |

Two consequences follow:

- **The mid-edit window stops being dangerous.** An in-flight add is now
  "append one task", not "write my 40-item snapshot", so peer changes made while
  you type survive. `refreshDrawerFromDisk` is unchanged — it is read-only, and
  its skip-while-editing guard is now merely a display nicety.
- **`m.drawerEditIdx` becomes `m.drawerEditID string` plus `m.drawerAdding
  bool`.** The edit is applied to the task that was on screen when `e` was
  pressed, not to whatever now sits at that index. The separate boolean matters:
  using `drawerEditID == ""` to mean "adding" would silently turn an edit into an
  add whenever the edited task has no id yet.

Opening the drawer loads through `withTodos`, which stamps ids on any legacy
rows. Every delta below addresses its task by id, so a row without one would make
`x`, `>`, `d` and `c` silently no-op. Once a file is stamped the open-time write
is content-identical and causes no git churn.

Separately, the cursor tracks the selected task's **id** rather than its index:
capture the id before mutating, restore the index by id afterwards. This fixes
the cursor jumping when a peer inserts a task above it. Where the id is no
longer present (deleted, by you or a peer), the cursor clamps to the same index
as today.

## Migration

Lazy, with no flag day. `withTodos` stamps an id onto every id-less task on
each write, so a file heals on first touch. `hive todo normalize`
(`cmd_todo.go:173`) already re-reads and re-writes the block, so it becomes the
explicit "stamp everything now" command with no change to its interface.

Files never written keep working through the positional fallback. A file edited
by hand outside the tool loses only the ids of the lines the human rewrote, and
those are re-stamped on the next write.

## Testing

The regression test that matters, and which fails against today's code:

> N goroutines against a `t.TempDir()` file, each applying a *distinct*
> mutation; assert all N are present afterwards.

Existing `todo_test.go` cases are pure-function table tests and stay that way.
The comment-parsing cases gain id variants. New coverage:

- Ids round-trip through parse → format in all four box states, including done
  and deferred (guarding the `formatTodos` suppression trap).
- Both legacy shapes still parse: `<!-- @owner -->` and no comment at all.
- An unrecognised token is ignored without eating the description; a genuine
  HTML comment in a description is not stripped.
- Id backfill is idempotent — a second `withTodos` does not renumber.
- Generated ids are unique within a list and contain no digits.
- `resolveTodoRef` reads an all-digit argument as a position and a consonant
  argument as an id, case-insensitively, and rejects both an out-of-range
  position and an unknown id.
- The atomic rename preserves prose outside the markers, including the
  "Recently completed" archive sections that `stevenlawton.com` keeps below the
  block.

## Risks

- **Unix-only.** `syscall.Flock` does not exist on Windows; hive stops building
  there. Accepted (see Non-goals). If it ever matters, a `_unix.go` /
  `_windows.go` pair isolates it.
- **The temp file is briefly visible** in the working tree before the rename.
  Microseconds, dotfile-named, cleaned up on failure.
- **A repo on a filesystem without working `flock`** (some network mounts)
  degrades to the current behaviour rather than failing loudly: the lock error
  goes to stderr from the CLI and to `m.err` from the drawer, and the write
  proceeds unserialised. Correctness is no worse than today, and the fresh
  read plus id addressing still remove the wrong-task hazard.
