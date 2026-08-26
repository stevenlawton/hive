---
allowed-tools: Bash(hive todo:*)
description: Add / curate this repo's hive task list (stored by hive outside the repo) — the list shown in the hive drawer and the Claude statusline.
---

# /todo

Manage the local per-repo task list backed by the `hive todo` CLI. The list is
stored by hive outside the repo, keyed by the repo's git remote and shared
across all its worktrees and branches; `hive todo` resolves it from the current
directory, so just run the commands — no path handling needed.

**This is the local dev list, NOT SliceWize.** Do not touch SliceWize tools here.

Request: **$ARGUMENTS**

Interpret the request and run the matching command(s):

| Intent | Command |
|---|---|
| show the list / no args | `hive todo list` |
| add a task | `hive todo add <subject> — <optional description>` |
| add with a long body | `hive todo add --description "<text>" <subject>` |
| reword a task | `hive todo edit <ref> <subject> — <description>` |
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
sections and stored by hive under `~/.local/share/hive/todos/`, keyed by the
repo's git remote. It is not in the repo and not in git: every worktree of a
repo shares one backlog, and adding a task leaves the working tree untouched.
A description holding more than one line moves off
the task line onto continuation lines indented six spaces, with the marker left
on the task line — so a body can carry paragraphs, its own bullets, even its own
`###` heading without any of it being read as list structure.

Rules:
- Address tasks by the **id** shown in the left column of `hive todo list` (three
  letters, e.g. `kdx`). Ids are stable — a peer session adding or removing tasks
  never changes them. A positional number still works but is unsafe when other
  worktrees are active, because positions shift.
- Run `hive todo list` to discover ids; you do not need to re-run it before each
  command the way positional numbers required.
- **Ids address, subjects describe.** The id rule above is about *commands*. When
  you name a task to a human — a plan, a bus announcement, a commit message, a
  handover — lead with the subject: `hive todo show` prints it. "claiming ffy"
  tells the reader nothing, and nobody reading the bus has the list in front of
  them. Give both when they may want to act on it: *staff pivot for per-event
  scoping* (`ffy`).
- Keep headlines to a short title, not a paragraph.
- `add` takes the description **after a ` - ` separator** (an em-dash reads too),
  via `--description`/`-d`, from a file with `--body-file <path>`, or piped in on
  stdin (`--body-file -`, or just a bare pipe). Flags work on either side of the
  subject. Giving the body twice is refused — including a pipe alongside a ` - `
  separator, which is easy to hit by accident when the *subject* contains " - ".
  It errors rather than picking one, because silently dropping the other is how
  a long body gets destroyed with an exit code of 0. Any unrecognised flag is
  refused too —
  folding it into the subject is how `--description` used to end up as literal
  text inside a task. `--` ends flag parsing if the subject itself starts with a
  dash.
- **For anything longer than a line, pipe it — do not pass prose through argv.**
  Quoting every apostrophe and backtick by hand is the failure mode this exists
  to remove, and the shell mangles them silently:

  ```
  cat <<'EOF' | hive todo add "the subject"
  Apostrophes, `backticks`, "quotes", $VARIABLE, £ — all safe.

  Second paragraph.
  EOF
  ```

  A quoted heredoc (`<<'EOF'`) passes the bytes through untouched. `--body-file
  notes.md` does the same from a file, and `edit` takes both, which is what makes
  rewriting a long body practical. Keep the *subject* to one line:
  a newline in a subject is flattened to a space, because a task line is one
  line by definition. The drawer's `e` key edits one line only and refuses a
  multi-line task, pointing you at `hive todo edit <id>` instead.
- **Never edit the store by hand.** It is hive's file, not the repo's, and
  nothing outside `hive todo` is expected to write it. If a verb misbehaves,
  say so on the bus (`hive_bus_ask` / `hive_bus_intent`) and let it be fixed —
  a hand-edit hides the bug from everyone else who is hitting it too.
- To reword a task use `edit`, never `rm` + `add`: it rewrites in place, so the
  id and any claim survive. Peers address tasks by id, so a new one strands
  every reference to it. `edit` takes the same subject/description forms as
  `add`.
- If the request is empty, just run `hive todo list`.
- After any change, run `hive todo list` once and show the user the result.

Note: `~/.claude/commands/todo.md` is a live mirror of this file, outside this
repo. It is not updated automatically — update it by hand if this file changes.
