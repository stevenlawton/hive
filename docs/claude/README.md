# Claude Code assets for the hive backlog pipeline

The agents, slash commands and templates that drive `hive todo`. **This
directory is the source of truth** — the files under `~/.claude/` are symlinks
pointing here, so editing either path edits the same file and the two cannot
drift apart.

```
docs/claude/agents/planner.md         -> ~/.claude/agents/planner.md
docs/claude/agents/builder.md         -> ~/.claude/agents/builder.md
docs/claude/agents/implementer.md     -> ~/.claude/agents/implementer.md
docs/claude/agents/test-writer.md     -> ~/.claude/agents/test-writer.md
docs/claude/agents/plan-critic.md     -> ~/.claude/agents/plan-critic.md
docs/claude/agents/context-loader.md  -> ~/.claude/agents/context-loader.md
docs/claude/agents/review-router.md   -> ~/.claude/agents/review-router.md
docs/claude/agents/go-reviewer.md     -> ~/.claude/agents/go-reviewer.md
docs/claude/agents/php-reviewer.md    -> ~/.claude/agents/php-reviewer.md
docs/claude/commands/*.md             -> ~/.claude/commands/*.md
docs/claude/templates/plan-artifact.md -> ~/.claude/docs/templates/plan-artifact.md
```

## Why symlinks and not copies

This directory used to hold *reference copies* of the live commands. That
drifted: `next.md` sat at 68 lines here while the live file had moved to 91, and
nothing flagged it. A stale prompt file in a repo is worse than no copy at all,
because it reads as authoritative.

The pattern is borrowed from `llm-tools`, which learned it the hard way — it
tracked its hook scripts in the repo but symlinked only the skill, so
`/notify` was a silent no-op box-wide for two weeks. **Anything added here needs
both a symlink and a commit.**

## The failure mode to know about

These symlinks point at an absolute path inside this worktree. If this checkout
is moved or deleted, every command and agent below silently stops existing —
box-wide, in every repo, not just this one. Re-point or restore them before
deleting the checkout.

Verify them at any time:

```bash
for f in ~/.claude/agents/{planner,builder,implementer,test-writer,plan-critic,context-loader,review-router,go-reviewer,php-reviewer}.md \
         ~/.claude/commands/{refine,build,backlog-loop,ship,next,pickup,todo}.md \
         ~/.claude/docs/templates/plan-artifact.md; do
  [ -s "$f" ] || echo "BROKEN: $f"
done
```

## Model allocation

Set per agent in frontmatter. `effort` accepts `medium`, `high`, `xhigh`.

| agent | model | why |
|---|---|---|
| `go-reviewer`, `php-reviewer` | opus | where the non-obvious findings come from |
| `plan-critic` | opus | adversarial reasoning about a design |
| `planner` | opus, `effort: medium` | full capability, less deliberation — its contract is what every later agent executes |
| `builder` | opus | orchestrates; the thinking is delegated, so this is the weakest case for opus |
| `implementer` | sonnet | executes an already-detailed contract, and a reviewer checks it |
| `test-writer` | sonnet | note: reproduce mode is the risky half — a test that passes against the bug certifies it |
| `context-loader` | sonnet | token-heavy digest work; exactly what you do not want on opus |
| `review-router` | haiku | detects language and delegates |

Sizing, before anyone spends time here: model choice is roughly 5x. Session
length is roughly 26x — cost per turn rises as context accumulates, so a long
session re-reads itself to death. Measure with `hive tokens` before tuning
models; this is the smaller lever.

## What each file is

**The pipeline** — walks a ticket from unrefined to landed, keeping the
orchestrating session's context at one line per ticket:

- `agents/planner.md` — researches one ticket, drafts a contract, has two
  critics attack it, writes `docs/plans/<id>.md`, moves the ticket to
  `plan-review`. Returns one line.
- `agents/builder.md` — builds one ticket from an approved contract: staleness
  and overlap guards, TDD with a verified red, review fan-out, commit, moves the
  ticket to `triage`. Returns one line. Never fixes its own review findings.
- `commands/refine.md` — `/refine`: one planner per unrefined ticket, in parallel.
- `commands/build.md` — `/build`: one builder per approved ticket, each in its
  own worktree, capped by `build_concurrency`.
- `commands/backlog-loop.md` — `/backlog-loop`: reap, build, refine, report both
  queues.
- `templates/plan-artifact.md` — the shape every plan takes.

**The single-ticket line**, for work you want to sit with rather than batch:

- `commands/ship.md` — `/ship`: research, plan, TDD, review, land. Two human
  gates. Writes its contract to the same `docs/plans/<id>.md`, so a ticket
  refined by `/refine` can be shipped by `/ship` and vice versa.
- `commands/next.md` — `/next`: claim the worktree's next pickable ticket and
  run `/pickup` on it. Skips tickets parked in a human queue.
- `commands/pickup.md` — `/pickup`: load a claimed ticket's context, check it is
  still a real issue, then plan.
- `commands/todo.md` — `/todo`: add and curate the task list.

## The states

`hive todo state <id> <state>` moves a ticket. Two of the four are human queues:

| state | meaning | who acts |
|---|---|---|
| *(none)* | unrefined | `/refine`, or `/next` |
| `plan-review` | plan written, awaiting approval | **you**, in the drawer |
| `ready` | plan approved | `/build`, or `/next` |
| `triage` | built, findings recorded | **you**, in the drawer |
