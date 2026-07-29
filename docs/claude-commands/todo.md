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
| mark done | `hive todo done <n>` |
| reopen | `hive todo reopen <n>` |
| claim / release a task | `hive todo claim <n>` (toggle; `claim clear` drops yours) |
| defer / un-defer (park) | `hive todo defer <n>` (toggle) |
| delete | `hive todo rm <n>` |

Deferred tasks render `[-]`, sink to the bottom, and are kept out of "next"
(the statusline never suggests one). Claiming a deferred task un-parks it.

**Claiming is per-worktree.** `claim` marks a task in-progress for *this*
worktree (by branch), recorded as `<!-- @branch -->` in the shared file so
parallel worktrees see it as taken (`🔒@branch`) and don't all grab the same
"next" item. The statusline shows your own claimed task, else the first
unclaimed one. `current` is an alias for `claim`.

`<n>` is the number shown by `hive todo list`. Tasks are grouped under `###`
sections and rendered `- [box] **subject** — description` in the `docs/TODO.md`
(or `TODO.md`) `TASKS:BEGIN/END` block on the main worktree; content outside
that block is left untouched.

Rules:
- To act on an existing task, run `hive todo list` **first** to get current numbers — they are positional and shift after `rm`.
- Keep headlines to a short title, not a paragraph.
- If the request is empty, just run `hive todo list`.
- After any change, run `hive todo list` once and show the user the result.
