# Open work

<!-- TASKS:BEGIN (managed by hive — edit tasks via the drawer / `hive todo`, not by hand) -->
Last sync: **2026-08-12**

### Tasks

- [ ] **hive todo add: accept body on stdin or --body-file** - long descriptions are awkward to shell-quote and impossible to review before they land (cmd_todo.go:89 joins argv with spaces) <!-- id:lxg -->
- [ ] **Worktree creation never bootstraps the new checkout** - createWorktree() (worktree.go:177) runs 'git worktree add' then TmuxNewSession then launches claude, and nothing else: no copying of gitignored-but-required files (.env), no install or build step. Every fresh worktree of a PHP/JS repo therefore starts with no vendor/, node_modules/ or public/build — a he-events worktree hit the 'Vite manifest not found' cascade and a subagent spent minutes bootstrapping by hand before it could get a clean test run. Proposed fix: a per-workspace 'worktree_bootstrap' config key listing files to copy from the parent repo plus commands to run in the new worktree dir (composer install, npm install, npm run build), run via TmuxSendKeys before claude starts so the human can watch it. Per-workspace config is cosmetic today (name/short/color/remote/favourite) and config.example.yaml has no hook for this. Diagnosed by the workspace auto-responder on the bus (msg_6a7c9a0bb66ca92ffc5e), reported from he-events split-1; unclaimed. <!-- id:dmy -->

<!-- TASKS:END -->
