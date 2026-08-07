---
allowed-tools: Bash(hive todo:*)
description: Add / curate this repo's hive task list (the docs/TODO.md TASKS block on the main worktree) — the list shown in the hive drawer and the Claude statusline.
---

# /todo

Manage the local per-repo task list backed by the `hive todo` CLI. The list is
the `TASKS:BEGIN/END` block in `docs/TODO.md` (or `TODO.md`) on the repo's
**main worktree** (shared across all worktrees/branches); `hive todo` resolves
it from the current directory, so just run the commands — no path handling needed.

**This is the local dev list, NOT SliceWize.** Do not touch SliceWize tools here.

Request: **$ARGUMENTS**

Interpret the request and run the matching command(s):

| Intent | Command |
|---|---|
| show the list / no args | `hive todo list` |
| add a task | `hive todo add <subject> — <optional description>` |
| mark done | `hive todo done <ref>` |
| reopen | `hive todo reopen <ref>` |
| claim / release a task | `hive todo claim <ref>` (toggle; `claim clear` drops yours) |
| defer / un-defer (park) | `hive todo defer <ref>` (toggle) |
| delete | `hive todo rm <ref>` |

Deferred tasks render `[-]`, sink to the bottom, and are kept out of "next"
(the statusline never suggests one). Claiming a deferred task un-parks it.

**Claiming is per-worktree.** `claim` marks a task in-progress for *this*
worktree (by branch), recorded as `<!-- @branch -->` in the shared file so
parallel worktrees see it as taken (`🔒@branch`) and don't all grab the same
"next" item. The statusline shows your own claimed task, else the first
unclaimed one. `current` is an alias for `claim`.

`<ref>` is the id shown in the left column of `hive todo list` (three letters,
e.g. `kdx`); a positional number also works. Tasks are grouped under `###`
sections and rendered `- [box] **subject** — description` in the `docs/TODO.md`
(or `TODO.md`) `TASKS:BEGIN/END` block on the main worktree; content outside
that block is left untouched.

Rules:
- Address tasks by the **id** shown in the left column of `hive todo list` (three
  letters, e.g. `kdx`). Ids are stable — a peer session adding or removing tasks
  never changes them. A positional number still works but is unsafe when other
  worktrees are active, because positions shift.
- Run `hive todo list` to discover ids; you do not need to re-run it before each
  command the way positional numbers required.
- Keep headlines to a short title, not a paragraph.
- If the request is empty, just run `hive todo list`.
- After any change, run `hive todo list` once and show the user the result.

Note: `~/.claude/commands/todo.md` is a live mirror of this file, outside this
repo. It is not updated automatically — update it by hand if this file changes.
