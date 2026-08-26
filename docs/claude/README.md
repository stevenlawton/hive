# Claude slash-command backups

Reference copies of the global Claude Code commands that drive the hive todo
system. The live source is `~/.claude/commands/` (not version-controlled); these
are committed here so they're backed up alongside the code they wrap.

- `todo.md` — `/todo`: add / curate the per-repo hive task list (`hive todo`).
- `pickup.md` — `/pickup`: load a claimed ticket's context, check it's still an
  issue, and plan.
- `next.md` — `/next`: claim the worktree's next unclaimed task and run the
  `/pickup` workflow on it. Typed after `/clear`, so each task starts on a clean
  context; a command cannot clear its own context.
