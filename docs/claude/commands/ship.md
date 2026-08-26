---
allowed-tools: Agent, Skill, Read, Write, Edit, Grep, Glob, Bash(hive:*), Bash(git:*), Bash(go:*), Bash(npm:*), Bash(make:*), Bash(php:*), Bash(composer:*), Bash(vendor/bin/pest:*), Bash(vendor/bin/phpunit:*), Bash(scripts/gate.sh:*), Bash(./scripts/gate.sh:*), Bash(bash scripts/gate.sh:*), Bash(bash ./scripts/gate.sh:*), mcp__hive-bus__hive_bus_intent, mcp__hive-bus__hive_bus_done, mcp__hive-bus__hive_bus_waiting, mcp__hive-bus__hive_bus_ask, mcp__hive-bus__hive_bus_reply
description: Run the full production line on a hive ticket — research, plan with review, TDD build, code review, then commit and close. Two human gates. Run /clear first.
---

# /ship

The deep line. `/pickup` looks and stops; `/ship` carries a ticket all the way
to a commit.

**Your context is for collaboration, not labour.** You hold the plan, the
findings, and the decisions. Agents do the reading, writing, and reviewing.
Do not do an agent's work yourself because it seems quicker — the point is that
your context stays clean enough to supervise the whole line.

Design: `~/.claude/docs/specs/2026-08-13-ship-production-line-design.md`.

Two gates, both real stops: **§5** (plan green) and **§8** (triage). Between
them you may run for several minutes with nothing to show. Offer the `notify`
skill up front so the gates ping.

Any stage can end the run — see **§10 Abort** before you need it, not after.

## 0. Preflight

Four checks, all before you claim anything.

**You must be in a git worktree.** `git rev-parse --git-dir`. If that fails,
stop — this command writes code and commits it, and it has nowhere to put
either.

**The tree must be clean.** `git status --porcelain`. If it is dirty, stop and
show the human what is there. Do not stash, reset, or check anything out: on
this machine dirty files in a main worktree are routinely someone's live work,
and `deploy-watch` is already holding on them. Let the human decide.

**Prefer a dedicated worktree.** If you are on a repo's main checkout, say so
before starting, because everything you build will sit uncommitted in the
shared tree and `deploy-watch` will HELD on it until §9 commits. That is
survivable and often what the human wants — but it should be a choice, not a
surprise. `superpowers:using-git-worktrees` if they want isolation instead.

**Check for an interrupted run.** Look for `docs/plans/<taskid>.md` on the main
worktree, plus a ticket state of `·plan-review` or `·ready` in `hive todo list`.
That pair means the plan already exists — either a previous `/ship` died after
gate 5 (a session limit, a crash, a reboot), or `/refine` planned this ticket and
nobody has built it yet. Either way the thinking is done.

Read it, run `hive todo show` to confirm the claim is still yours, tell the human
where the line stopped, and offer to resume from that stage rather than starting
over. A ticket at `·ready` has already cleared gate 5 — the human approved that
plan — so resume at §6 rather than re-running the gate on a plan they have
already signed off. The contract is the whole point of writing it down; do not
re-derive a plan that already exists.

## 1. Claim, and say so

Exactly as `/next` §1–2: don't take a second ticket, take the next unclaimed
one, or use **$ARGUMENTS** if it names a task id.

Use the ticket's **subject** whenever you name it to a human — bus, commit,
write-up. The id is for commands.

Then **announce intent on the bus** with `hive_bus_intent`: the subject, the
repo and worktree, and the files or subsystems you expect to touch if you
already know them. Several worktrees run in parallel here and they coordinate
through the bus. A line that runs silently for an hour and announces only when
it is finished is the worst possible shape for avoiding a collision — by the
time anyone learns what you were doing, you have already done it.

If a peer replies that they are in the same code, resolve it before §3.

## 2. Research — fan out

Pull the concrete anchors out of the ticket: files, symbols, `#NNN`, endpoints,
error strings.

Dispatch `context-loader` agents **in a single message** so they run
concurrently — one per independent anchor cluster, typically two or three. Give
each the ticket subject and its specific anchors. Do not send one agent to "go
look at everything".

Read what comes back. If the digests show the issue is already fixed or
obsolete, stop the line: report it with the evidence, recommend
`hive todo done <ref>`, and do not invent work.

**Learn how this repo verifies itself** while you are here, because §6 and §9
both need it and it differs per repo:

- Is there a `scripts/gate.sh`? **Read its header comment for the subcommands
  and use those.** They are not standardised — one repo here uses
  `fast | full | verify | ship`, another `run <suite> | record | verify | show`.
  Never guess a subcommand, and never invent one.
- Otherwise find the real test command: `go test ./...`, `php artisan test`,
  `vendor/bin/pest`, a `Makefile` target, whatever the repo actually uses.

## 3. Draft the plan

Write the plan yourself, in `/pickup`'s shape — **🐞 the bug**, **📊 current
status**, **🛠️ plan** (root cause / change / verify / blast radius).

Write it as a contract an agent will execute without being able to ask you
anything: name the files, name the functions, specify the API surface for new
code, give runnable verification steps. If you cannot write it that concretely,
you do not understand the problem yet — go back to §2.

## 4. Critique — fan out

Dispatch two `plan-critic` agents in one message. Give them the draft plan and
the research digests.

They verify the plan against the code, so expect them to find claims that don't
hold. Fold what they find into the plan before the human sees it. Where a
critic is wrong, say so and why — don't just absorb it.

## 5. 🚦 GATE — iterate to green

Show the human the plan, plus a short note on what the critics changed and any
**RETHINK** verdict.

Iterate until they call it green. This is a conversation, not an approval form.

Then write the agreed plan to:

```
<main worktree>/docs/plans/<taskid>.md
```

Find the main worktree with `git worktree list --porcelain | head -1 | cut -d' ' -f2`.

That file is the contract everything downstream executes against, and its
location matters: it is **the same artifact the rest of the pipeline uses**. A
`planner` agent writes here, a `builder` agent reads here, and the human reviews
here in the hive drawer. So a ticket refined by `/refine` can be shipped by
`/ship`, and a ticket you plan here can be built by `/build` — one shape, one
place, whichever route the ticket takes.

It is committed and shared rather than private to one run, which is the point:
the plan is the record of why the change looks the way it does, and it should
outlive the branch that carried it. It still **survives the session**, so a crash
or a session limit costs you the conversation but not the agreement. Never put
the contract in a session scratchpad — that is exactly the thing that disappears.

Record the ticket id, the repo, and the stage you are entering at the top of
the file, so §0 of a later run can pick it up cold.

## 6. Build — TDD

Follow `superpowers:test-driven-development`. The Iron Law holds: **no
production code without a failing test first, and you must watch it fail.**

Branch on ticket type.

### Bug fix — sequential

1. `test-writer` **in reproduce mode**: say explicitly that you want a test
   which reproduces the bug and that it **must end RED** — the agent defaults
   to writing coverage tests that end green, and will iterate to green unless
   you tell it not to. Pass it the plan file.
2. **Run it yourself.** Confirm it fails, and that it fails for the *right
   reason* — an assertion failure whose actual-vs-expected matches the reported
   bug. A compile error, missing import, or setup panic is **not** a red: it
   proves the test doesn't build. Neither is a test that passes because it
   asserts the buggy behaviour — that hard-codes the bug into the suite
   forever. This check is yours; do not delegate it.
3. `implementer`: fix it. Pass the plan file and the failing test's output.
4. Run the tests. Green, and nothing else broke.

### New feature — stub, then parallel

1. `implementer`: signatures and stubs only, exactly the API surface the plan
   specifies. No bodies.
2. Dispatch `test-writer` (reproduce mode again — tests against stubs must
   fail) and `implementer` **in one message** so they run concurrently. Their
   file fences are disjoint, so they cannot collide.
3. At the join, **run the tests yourself** against what exists. You need a
   genuine failing assertion. A compile error is not a red.
4. `implementer`: fill in whatever remains until green.

If an agent reports the plan was wrong, stop and go to §8 — do not improvise a
different fix.

## 7. Review — fan out

Work out what the diff *is* before dispatching, and state it explicitly in each
agent's prompt — reviewers cannot ask you, and on an uncommitted build there is
no commit for them to find. Give them the output of `git diff` plus
`git status --porcelain` for anything untracked, or name the files directly.
Untracked new files are invisible to `git diff` and get silently skipped; that
is the most common way a review misses the main change.

Dispatch, in one message:

- `review-router` on the diff — it routes to `go-reviewer`, `php-reviewer`, or
  a repo-local specialist. In a repo you already know is single-stack, call
  that specialist directly and skip the hop.
- `plan-critic` **in conformance mode** on the diff plus the contract file:
  did this build what was agreed?

Keep the reviewers' **Confirmed vs Suspected** split intact. Do not act on
findings yet.

## 8. 🚦 GATE — triage

Present the findings grouped and ranked, and route each one:

- **plan was wrong** → back to §3
- **plan was right, code isn't** → back to §6
- **real, but out of scope** → file it with `/todo`
- **clean** → §9

Say what you'd choose and why, then let the human decide. Do not auto-fix.

## 9. Document, memories, admin

Once the human accepts:

- **Verify** — follow `superpowers:verification-before-completion`. Run the
  repo's gate or full suite (the one you identified in §2) and read the output
  before you claim anything passes. Evidence before assertions.
- **Document** — update the docs the change actually invalidates. Not a
  changelog entry nobody reads.
- **Memories** — if the line surfaced something durable (a preference, a
  landmine, a project constraint), write it per the memory instructions. Only
  what isn't already recoverable from the code or git history.
- **Commit** — the ticket subject in the message, never the id. Follow the
  repo's existing log style.
- **Close** — `hive todo done <ref>`.
- **File** — any out-of-scope findings from §8, via `/todo`.
- **Announce** — `hive_bus_done` with the subject, if the change touches
  anything shared.
- **Keep the contract** — do not delete it. It is a committed record now, not
  scratch: commit `docs/plans/<taskid>.md` along with the change, so the plan and
  the diff it produced land together. §0 will not mistake it for an interrupted
  run, because the ticket is closed and no longer sits at `·plan-review` or
  `·ready`.

Report what landed, what you filed, and anything you deliberately left.

## 10. Abort

A line that stops halfway is normal — the build won't go green, review says the
job is three times bigger than the ticket, the human runs out of time. What is
not acceptable is leaving the ticket claimed and the tree half-built with no
record, because a held claim blocks every other worktree from that ticket
indefinitely and nothing on the list says why.

To stop cleanly, in this order:

1. **Say where you stopped** and what you learned — the stage, and the finding
   that ended it.
2. **Decide the tree with the human.** Commit work-in-progress to a branch,
   keep it dirty deliberately, or revert. Never revert or clean without being
   told to.
3. **Update the contract file** with the stopping point, or delete it if the
   plan is dead. A stale contract will be offered as a resume by §0 and mislead
   whoever picks it up.
4. **Hand the ticket back** unless the human wants to keep it:
   `hive todo claim clear` releases this worktree's claims. If what you learned
   changes the ticket, `hive todo edit` it first so the next reader starts
   ahead of you rather than repeating the work.
5. **Announce it** — `hive_bus_done` or `hive_bus_waiting` as fits. Peers need
   to know the ticket is back on the list and what you found.
