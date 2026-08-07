---
allowed-tools: Bash(hive todo:*), Bash(git log:*), Bash(git show:*), Bash(git grep:*), Read, Grep, Glob
description: Pick up this worktree's claimed task — load its full context, check it's still a real issue, and plan the work before touching code.
---

# /pickup

Work the task this worktree has claimed on the hive todo list. Do NOT start
editing code — this ends at a write-up (**the bug → its current status → a plan
if needed**) that the user confirms before anything is touched.

## 1. Get the ticket

If **$ARGUMENTS** is a task number, claim it first: `hive todo claim $ARGUMENTS`.

Then read your claimed task: `hive todo show` (prints `section` / `subject` /
`description`). If it says nothing is claimed, run `hive todo` and STOP — ask the
user which task to claim by its id (`hive todo claim <ref>`).

## 2. Load the full context

Mine the subject + description for concrete anchors and chase them down:
- the `#NNN` id, file names, function/class names, endpoints, symbols
- Read the referenced files; `git grep` the named code to find every call site
- `git log --oneline -20 -- <paths>` on the touched areas — and skim
  `git log --oneline -20` overall. A peer may have already fixed or changed it.

Load enough that you could explain the current behaviour without guessing.

## 3. Report — in this exact shape

Write up what you found under these three headings:

### 🐞 The bug
Describe the actual problem in your own words, from the code you read — what's
wrong, where (`file:line`), and the impact/symptom. This is the real behaviour
you found, not a restatement of the ticket title.

### 📊 Current status
Pick one, with evidence:
- **✅ Already fixed / obsolete** — cite the commit SHA or the code that now
  handles it, and recommend `hive todo done <ref>` (or drop the claim with
  `hive todo claim clear`). STOP here — nothing to plan.
- **⚠️ Still an issue** — pin the concrete repro: the exact code path, input, or
  failing case, quoting the offending lines.
- **🔶 Partially addressed** — what's already done vs what remains.

### 🛠️ Plan  *(only if still an issue)*
Tight bullets, not prose:
- **Root cause** — one or two lines.
- **Change** — the files/functions to touch and the approach.
- **Verify** — tests to add/run, the gate, a manual check.
- **Blast radius** — shared code, callers, anything to coordinate on the bus first.

Then STOP and ask the user to confirm before implementing. For a large or
open-ended change, offer plan mode / brainstorming instead.
