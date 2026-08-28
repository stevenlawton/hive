# Triage gives the human no way to test the change

**Ticket:** qxy
**Repo:** hive (github.com/stevenlawton/hive)
**Base commit:** 87acdeb0ae82594a1d3e69fa0c1dbd19e4f251f2
**Written:** 2026-08-27

## How to test this

_Not built yet. The builder fills this in before moving the ticket to triage._

## Current behaviour

### The plan template has no human-facing test section

`docs/claude/templates/plan-artifact.md` (39 lines, one commit ever — `1bb0614`)
has a level-1 title, four metadata lines, and exactly seven `##` sections:

```
Current behaviour · Root cause · The contract · Verification ·
Blast radius · Critic findings · Open questions
```

`Verification` is "Runnable commands, and what output means success." Nothing
in the template addresses the human at the triage gate.

`~/.claude/docs/templates/plan-artifact.md` is a **symlink** to the repo copy
(`docs/claude/README.md:1-6,19`), as are the agent and command files. All
symlinks target `/home/steve/repos/workspace` — the **main** worktree — so a
change on a branch does not go live until it is merged to main.

### The builder is never told to write one

`docs/claude/agents/builder.md` has nine steps. The only artifact write is §6
(`builder.md:177-184`): a `## Review findings` heading, "the entire content of
the human's triage step". §3 (`builder.md:138-152`) is the machine gate. The
string "How to test" appears nowhere in `builder.md`, `planner.md`, or the
template. It exists in exactly one place in the repo: `docs/plans/dmy.md:10`.

### That one occurrence was written by hand, after the fact

`docs/plans/dmy.md` (1170 lines) carries `## How to test this (triage step)` at
**line 10**. It was not written by the builder — it arrived in `757eaf0`
("docs: put the manual test steps at the top of the worktree bootstrap plan"),
**two commits after** the build commit `6c88153`. `git show
6c88153:docs/plans/dmy.md` has no such heading. It is Steve's hand-repair of
the failure this ticket reports, and it is the worked example the contract
below is modelled on: fixture repo path (`~/repos/wt-demo`), binary path
(`/tmp/hive-test`), four numbered steps in the imperative, expected output
quoted verbatim in fences, a cleanup step, and a closing paragraph naming what
the test does **not** cover.

`docs/plans/wdd.md` (618 lines, untracked) has no such section.

### Nothing validates plan content

No Go code parses the artifact. `runTodoState` (`cmd_todo.go:404-451`) checks
only that the target state is one of four strings (`todo.go:50-55`) and that a
backwards move carries `--note`. `hive todo state <id> triage` cannot fail on a
missing section.

### The triage gate does not render the plan at all

`web/app.js:285`:

```js
const kindOf=t=>t.state==="triage"?"build":"plan";
```

`renderReader` (`app.js:219-226`) hides both markdown toggles when `isBuild`
and calls `renderDiff()`. `apiBuild` (`cmd_serve.go:544-564`) returns
`buildFor`'s `git show` output (`cmd_serve.go:534-536`). **At triage the human
sees a commit diff, never the plan document.**

`buildFor` (`cmd_serve.go:526-527`) resolves the ticket to a commit with:

```go
git("log", "-1", "--format=%H", b, "--", "docs/plans/"+id+".md")
```

`-1` is the **most recent** commit on that branch touching the plan file.

## Root cause

Two gaps. The ticket's stated fix addresses only the first.

**Gap 1 — nobody is assigned the work.** The procedure has no home in the
template and no owner in the pipeline. `Verification` is written to a machine;
the builder's only writing duty is `Review findings`. So in `dmy.md` the
procedure landed inside `Verification` around line 700, addressed to the
builder ("the builder should report it as outstanding rather than claim it").
The builder read it as an instruction to itself, complied, and moved on.
Nothing was addressed to Steve because nothing in the pipeline says anything is.

**The audience mismatch is the mechanism, not the position.** Moving
badly-addressed text to the top produces badly-addressed text at the top.

**Gap 2 — position in a document cannot fix a gate that renders a diff.** The
ticket reasons that "the web UI added in d2a104a treats plan-review and triage
as the same kind of gate — both open the plan document … so whatever the plan
says at the top IS the triage experience."

That was **true when the ticket was written and is no longer true.** `d2a104a`
landed 2026-08-26 and did behave that way. `5362d1d` ("feat(web): review a
build's diff at triage, not its plan") landed 2026-08-27 and changed it,
because of a closely related failure — Steve was shown `dmy`'s plan at a triage
gate, approved it, and a review file recorded his approval against the wrong
artifact. The ticket is a day stale, not careless.

The consequence stands either way: a `How to test this` section at the top of
`docs/plans/<id>.md` is **not visible at triage**. The builder does commit the
artifact with the change (`builder.md:172-175`), so its text is in the diff —
but as ~1170 lines of `+` in a hand-rolled diff renderer (`renderDiff`,
`app.js:170-182`) with no headings, no markdown and the toggle hidden. That is
worse than line 700 of a rendered document, not better.

So item 4 of the ticket — the one it calls optional — is the only part that
puts the procedure in front of the reviewer at the gate it was written for.

## The contract

Three tiers. **A** is the ownership fix, **B** the visibility fix, **C** the
enforcement. Each lands independently; A is a prerequisite for B and C. See
Q1 before cutting any.

---

### Tier A — prompt files

#### A1. `docs/claude/templates/plan-artifact.md`

Insert after the `**Written:**` line and its blank line, **before**
`## Current behaviour`, so it is the first `##` in every plan:

```markdown
## How to test this

_Not built yet. The builder fills this in before moving the ticket to triage._
```

Exactly one line of body. Do **not** add guidance prose here: this section is
rendered verbatim to the human at the triage gate, and any instruction to an
agent left in it will be read by Steve as if it were addressed to him. The
ownership rule goes in `planner.md` (A2), not in the template.

Leave the seven existing sections and their guidance text unchanged and in
their current order. `Verification` keeps its wording — it is the machine gate.

#### A2. `docs/claude/agents/planner.md`

In §4 ("Draft the contract"), after "Read the template at
`~/.claude/docs/templates/plan-artifact.md` and fill every section.", add:

```markdown
One exception: **How to test this** is not yours. Copy the heading and its one
stub line through verbatim and move on. You have not built anything, so you
cannot write a procedure anyone can follow — the builder has the fixtures in
hand and fills it in before triage. A speculative procedure there is worse than
the stub, because the stub is honest and a guess reads as tested.
```

In §2, alongside the existing rule about copying `## Decisions` through verbatim,
add:

```markdown
- If the artifact carries a filled-in **How to test this** from a previous
  build, replace it with the one-line stub again. The build it described is
  being replanned; its procedure no longer describes what will exist.
```

#### A3. `docs/claude/agents/builder.md` — new step, and a reorder

The current order writes the artifact **after** committing it:

```
5 Commit   6 Append the findings   7 Transition   8 Do not fix   9 Never redesign
```

`builder.md:172-175` says "Commit the plan artifact along with the change", and
§6 then appends findings to a file already committed. Nothing tells the builder
to commit again. This is a live latent bug independent of this ticket — it is
why `dmy.md`'s findings and test steps arrived in `757eaf0`, a separate
after-the-fact commit — and the new step must not be added on the wrong side of
it. Reorder so both artifact writes precede the commit:

```
5 Write the human's test procedure   (new)
6 Append the findings to the artifact (was 6)
7 Commit                              (was 5)
8 Transition, then release your claim (was 7)
9 Do not fix what the review found    (was 8)
10 Never redesign                     (was 9)
```

**Cross-references need no changes.** `grep -n '§' docs/claude/agents/builder.md`
returns exactly two hits — `builder.md:190` ("the toggle from §1") and
`builder.md:209` ("§3 already covers that"). Both point at §1 and §3, which do
not move. Nothing outside `builder.md` references a builder step number.

**New §5, in full:**

```markdown
## 5. Write the human's test procedure

The artifact you were handed has a `## How to test this` section holding the
planner's stub. Replace it — heading kept, everything between that heading and
the next `##` deleted and rewritten. If the heading is not there at all (plans
written before this was required), add it directly after the metadata block,
above `## Current behaviour`.

Edit the copy **in your own worktree** at `docs/plans/<id>.md`. If it is not
there, copy it in from `<main worktree>/docs/plans/<id>.md` first — the planner
writes it to the main worktree without committing, so a fresh build worktree
will not have it. §7 commits it from here.

Write it **to Steve**, in the imperative. You have the fixtures in hand right
now and nobody downstream does; in twenty minutes this environment is gone and
the reviewer cannot reconstruct it. `docs/plans/dmy.md` is the worked example.

**Leave the fixtures behind, and name their paths.** A binary built from this
branch, at a stated absolute path. Any demo repo, scratch checkout or seed data
you created, left in place, with its path. If you built it to test, do not
delete it.

**Numbered steps, in the imperative, with expected output quoted verbatim** in
a fenced block, so a mismatch is obvious to someone who has never seen it work.
"The builder should confirm X" is the exact failure this step exists to
prevent — if a step reads as an instruction to you, rewrite it as an
instruction to Steve.

**A cleanup step**, last, naming exactly what to delete and any config the test
wrote.

**What testing will not reveal.** Close with it, plainly. Most review findings
are code-reading findings with no reproducible symptom, and a reviewer who
walks the happy path and sees it work will otherwise read them as noise. Name
the ones worth knowing while testing.

If no fixture is possible — a TUI interaction, a change needing production
data, a race you cannot force — say so and say **why**, then state what you did
to convince yourself it works and how to reproduce that. "No fixture possible,
here is why" is an acceptable answer. Silence is not, and neither is a
procedure you did not actually run.

Keep it near the top and keep it short. This section is rendered on its own at
the triage gate, ahead of the diff; it is the first thing Steve reads and the
only part of the artifact he is guaranteed to see.
```

**In §7 (Commit), after "Commit the plan artifact along with the change",** add:

```markdown
Commit the artifact **once**, in the same commit as the change, with §5 and §6
already written into it. If you have to correct it afterwards, use
`git commit --amend --no-edit` — never a second commit. The web UI resolves a
triage ticket to the *most recent* commit on the branch touching
`docs/plans/<id>.md`, so a follow-up doc commit replaces the diff Steve reviews
with a diff containing only the plan document, and the code change disappears
from the gate.
```

**In §8 (Transition), before the `hive todo state` block,** add:

```markdown
Do not move the ticket while `How to test this` still holds the planner's stub.
A ticket at triage with an unfilled stub is a gate the human cannot pass.
```

**In the frontmatter `description:` (`builder.md:3`)**, which enumerates the
pipeline in prose, insert the new duty: after "then a parallel review fan-out,"
add "then the human's test procedure,".

#### A4. `docs/claude/README.md`

`README.md:78-84` mirrors the frontmatter description. Update the
`agents/builder.md` bullet to name the new duty in the same words. Update the
`templates/plan-artifact.md` bullet (`README.md:87`) to note that the plan now
opens with the builder's test procedure.

---

### Tier B — surface it at the triage gate

#### B1. `cmd_serve.go`

Add immediately after `planPath` (`cmd_serve.go:239-248`):

```go
// howToTest returns the body of the "## How to test this" section of a plan
// document — everything between that heading and the next level-2 heading,
// trimmed. It returns "" when there is no such section and when the section
// still holds the planner's stub, because an unfilled stub is the same absence
// as a missing heading and the gate should say so either way.
func howToTest(plan string) string
```

Precise behaviour:

- Scan line by line, tracking fenced code blocks: a line whose trimmed form
  starts with ` ``` ` toggles "inside a fence". **Both the heading match and
  the terminator match are skipped while inside a fence.** A plan that quotes
  the template inside a fence — this document does, and so will any re-plan —
  must not match it.
- **Heading match:** the line, with trailing whitespace trimmed, equals
  `## How to test this`, or begins with `## How to test this ` (that trailing
  space is required, so `## How to test this (triage step)` matches and
  `## How to test this section is missing` does not, while
  `## How to test thisxyz` cannot match either). Leading whitespace is not
  allowed. Case-sensitive. Only the **first** match is used.
- **Terminator:** a line beginning, at column zero, with `## ` or `# `
  (hash-hash-space or hash-space). `### ` is not a terminator — a subsection
  belongs to the section. A `---` thematic break is not a terminator. A line of
  `##` with no following space is not a terminator.
- End of input terminates.
- `strings.TrimSpace` the collected body.
- If the trimmed body contains the substring `Not built yet`, return `""`.

Add immediately after it:

```go
// planAtCommit reads a ticket's plan document as it stood in the build's own
// commit. The main worktree's copy is the planner's — the builder edits and
// commits the copy in its own worktree (builder.md §5), so the working-tree
// file at triage is stale and does not carry the procedure the builder wrote.
func planAtCommit(repoPath, commit, id string) string
```

Implementation: `runGit(mainWorktree(repoPath), "show", commit+":docs/plans/"+id+".md")`.
Do not trim the result — `runGit` (`cmd_serve.go:539-542`) returns raw stdout
and the file body is wanted verbatim. On error return `""`; a plan missing from
the commit is an expected state, not a server fault, so do not surface the git
error.

In `apiBuild` (`cmd_serve.go:544-564`), after the `branch == ""` early return,
add **one** field to the existing `writeJSON` map, leaving every existing field
including `hash` byte-identical:

```go
"howto": howToTest(planAtCommit(repo.Path, commit, id)),
```

`hash` stays `hashOf([]byte(diff))`. The howto must **not** enter the hash: the
reviewed artifact at triage is the diff, the plan document is inside that diff,
so the section is already covered transitively. Adding it would break
`apiReview`'s re-hash (`cmd_serve.go:296-324`).

Do not touch `apiPlan`, `apiReview`, `checkVerdict`, `reviewDoc`,
`announceReview`, or `buildFor`.

#### B2. `web/app.js`

In `renderReader` (`app.js:219-240`) replace:

```js
document.getElementById("rmain").innerHTML =
  isBuild ? renderDiff() : (mode==="src" ? renderSource() : renderRendered());
```

with:

```js
document.getElementById("rmain").innerHTML =
  isBuild ? (renderHowto() + renderDiff())
          : (mode==="src" ? renderSource() : renderRendered());
```

Add beside `renderDiff` (`app.js:170-182`):

```js
function renderHowto(){
  const h=PLANS[openPlan]&&PLANS[openPlan].howto;
  if(!h) return `<div class="howto none"><h3>How to test this</h3>`+
    `<p>The plan carries no <code>How to test this</code> section. `+
    `The build did not say how to exercise it.</p></div>`;
  return `<div class="howto"><h3>How to test this</h3><div class="rendered">`+
    mdBlocks(h).map(b=>b.html).join("")+`</div></div>`;
}
```

Two details that matter:

- `mdBlocks` is reused **for its html only**; its `{start,end}` line ranges are
  discarded. The comment button and line number are added by `renderRendered`
  (`app.js:212-213`), not by `mdBlocks`, so the panel emits no `data-line` and
  no `.blk`. Comments at triage continue to attach to diff lines exactly as
  today. Do not wrap panel content in `.blk`.
- The inner `.rendered` wrapper is required: all plan typography
  (`index.html:155-163`) is scoped to `.rendered`, and without it the panel's
  fenced blocks and inline code render unstyled. `.rendered` carries only
  typography, no layout, so nesting it is safe.

#### B3. `web/index.html`

Add beside the `.diff` rules (`index.html:180-188`):

```css
.howto{background:var(--surface);border:1px solid var(--hairline);
  border-left:3px solid var(--you-bg);border-radius:8px;
  padding:14px 18px;margin:0 0 22px}
.howto h3{margin:0 0 8px;font-size:15px}
.howto .rendered h2{border-top:none;padding-top:0;margin-top:14px}
.howto.none{border-left-color:var(--ink-faint);color:var(--ink-faint)}
```

All four variables exist at `:root` (`index.html:10-12`) and in both dark
scopes (`index.html:19-21,28-30`). Use them; introduce no colour literals.

---

### Tier C — refuse the transition, not just the reader

The empty-section notice in B2 tells Steve about a failure at the one moment he
can do nothing about it. The party who can fix it is the builder, and it
already passes through a checkpoint: `runTodoState` (`cmd_todo.go:404-451`),
which every builder calls (`builder.md:186-190`). Enforce there.

#### C1. `cmd_todo.go`

Add:

```go
// planNeedsTestSteps reports whether a ticket's plan document is missing the
// human's test procedure — no "How to test this" section, or one still holding
// the planner's stub. It answers false when no plan document can be found:
// a ticket without a plan is a different problem, and this check must never be
// the thing that wedges the pipeline.
func planNeedsTestSteps(id string) bool
```

Resolution order for the document, first hit wins:

1. `<git rev-parse --show-toplevel from todoCwd()>/docs/plans/<id>.md`
2. `<mainWorktree(todoCwd())>/docs/plans/<id>.md`

The builder's own worktree copy is checked first because that is the one it
edits (A3). Read it, call `howToTest` (same package, added in B1), and return
true when the result is `""`. Return false on any read error or missing file.

In `runTodoState`, inside the existing `mutateOne` closure and immediately
after the existing backwards-move check (`cmd_todo.go:435-439`), add a second
refusal in the same shape:

```go
if want == StateTriage && strings.TrimSpace(note) == "" && planNeedsTestSteps(ts[i].ID) {
    refused = fmt.Sprintf("%s has no filled-in \"How to test this\" section in "+
        "docs/plans/%s.md — write the human's test procedure first, or pass "+
        "--note to move it anyway", ts[i].ID, ts[i].ID)
    return ts, ""
}
```

`--note` is the override, matching the existing escape hatch for backwards
moves. Update `todoStateUsage` (`cmd_todo.go:397-399`) to say so in one line.

This is a refusal, not a warning, and it can block a build. That is deliberate
and it is why `--note` exists — but see **Q4**.

---

### Tests

`cmd_serve_test.go`, in the file's existing style: `cases := []struct{…}` with
`t.Run`, no assertion library (`TestReviewVerdictRules`, `cmd_serve_test.go:13-39`).

```go
func TestHowToTestExtractsTheSection(t *testing.T)
```

Rows `{name, plan, want string}`:

- heading, two paragraphs, then `## Current behaviour` → the two paragraphs,
  trimmed, no heading.
- heading with trailing text `## How to test this (triage step)` → same.
- `## How to test this section is missing` → `""` (no trailing-space match).
- no such heading → `""`.
- body is `_Not built yet. The builder fills this in before moving the ticket
  to triage._` → `""`.
- body contains a `### ` subsection → included, not a terminator.
- body contains a fence whose content has a line starting `## ` → included, not
  a terminator.
- **the only `## How to test this` in the document is inside a fence** → `""`.
  This is the regression that protects plans quoting the template.
- section runs to end of input → body to end, trimmed.
- two headings → only the first used.
- `---` inside the body → not a terminator.

```go
func TestHowToTestOnTheWorkedExample(t *testing.T)
```

An inline `dmy.md`-shaped document — inline, **not** read from
`docs/plans/dmy.md`, which is a mutable artifact and would couple the test to
it. Assert the result starts with "A demo repo is already set up" and contains
"What this does not cover".

`cmd_todo_test.go` (create if absent, same style):

```go
func TestPlanNeedsTestSteps(t *testing.T)
```

Write plan documents into `t.TempDir()` and check: filled section → false;
stub → true; no heading → true; no file at all → false.

## Verification

```bash
gofmt -l .                       # must print nothing (covers the test files too)
go vet ./...                     # must print nothing
go test ./... 2>&1 | tail -20    # ok  github.com/stevenlawton/hive
go build -o /tmp/hive-qxy .      # exit 0
```

Template — the new section must be the **first** `##`:

```bash
grep -n '^## ' docs/claude/templates/plan-artifact.md | head -1
# expect: 8:## How to test this
```

Builder steps renumbered with no gaps, repeats or drops:

```bash
grep -n '^## [0-9]' docs/claude/agents/builder.md
# expect 1..10 in order; §5 is "Write the human's test procedure",
# §7 is "Commit"
```

Cross-references still point at steps that did not move:

```bash
grep -n '§' docs/claude/agents/builder.md
# expect exactly two hits, §1 and §3
```

**End-to-end — this is the step that catches the ordering bug, and none of the
above do.** With a ticket actually sitting at `triage` on an unmerged branch:

```bash
/tmp/hive-qxy serve &
curl -s -H "Cookie: $TOKEN" localhost:<port>/api/build/hive/<id> | jq -r '.howto' | head -20
```

Must print the builder's procedure. If it prints empty, the artifact write is
landing on the wrong side of the commit and Tier B is inert — that is finding 1
below, and it is the whole reason the step order in A3 is what it is.

Then open the gate in a browser and confirm the panel renders above the diff,
carries no line numbers or comment buttons, and that clicking a diff line still
opens a composer against the right line.

Tier C:

```bash
/tmp/hive-qxy todo state <a ticket whose plan has a stub> triage
# expect a refusal naming the ticket and docs/plans/<id>.md, exit non-zero
/tmp/hive-qxy todo state <same> triage --note "no fixture possible, TUI change"
# expect it to move
```

**Not a verification step:** the `[ -s ~/.claude/... ]` symlink loop from
`docs/claude/README.md:40-48`. Those symlinks resolve to the **main** worktree
while the build happens on a branch, so the loop is green whether or not these
edits exist. It checks the symlinks, not this change. Run it after merging to
main if you want, but it proves nothing here.

## Blast radius

**The prompt files are live box-wide the moment they reach main.** `~/.claude`
symlinks into `/home/steve/repos/workspace`, so merging changes every planner
and builder in every worktree and every repo on this machine, with no install
step and no version pin. There is no staged rollout. A malformed `builder.md`
is a box-wide outage of the build pipeline. Sessions in other worktrees read
the symlinked main copy, not their own checkout, so they pick the change up on
merge regardless of their branch.

**Exactly one commit may touch `docs/plans/<id>.md` per build.** `buildFor`
(`cmd_serve.go:526-527`) resolves a triage ticket with `git log -1 … --
docs/plans/<id>.md` — the newest such commit. A doc-only fix-up commit becomes
"the build commit" and Steve reviews a diff with no code in it, at a changed
hash that 409s any in-flight review. `757eaf0` against `dmy.md` is exactly such
a commit; had `dmy` still been unmerged, that is what the gate would have shown.
A3's amend rule is what keeps this from happening, and it is load-bearing.

**Every plan-review now opens with `_Not built yet._`** That is the gate where
the plan document genuinely is rendered, so the first block Steve sees becomes a
stub. Accepted: it is one italic line, and it states the pipeline stage
honestly. If it grates, the alternative is for the planner to omit the heading
and the builder to insert it — rejected, because then nothing reminds the
builder the section exists and `wdd.md`'s situation becomes permanent.

**Plans already in flight.** `docs/plans/wdd.md` (untracked, no heading) will
hit the A3 fallback path — its builder must add the heading rather than replace
a stub. Under Tier C it will also be refused a `triage` transition until it
does. `docs/plans/dmy.md` already carries a filled section with a trailing
`(triage step)`; B1's prefix rule covers it. Sibling planners are writing
`hwq.md`, `jzq.md` and `kdx.md` concurrently against the **old** template —
they will need the stub adding, or their builders will use the fallback path.

**`/ship` is deliberately not covered.** An earlier draft added the duty to
`ship.md` §8. Cut: `/ship` writes its plan in `/pickup`'s shape — `🐞 the bug`,
`📊 current status`, `🛠️ plan` (`ship.md:97-101`) — not from
`plan-artifact.md`, so there is no `## How to test this` heading to fill and
the instruction would have had no target. `/ship` also never sets state
`triage`, so the Tier B panel never sees its tickets, and its gate is
conversational with the human in the room and able to ask. The gap is real but
small; closing it means giving `/ship` the template shape, which is a separate
ticket.

**The `hash` contract is unchanged.** `apiReview` re-derives the diff and
re-hashes it (`cmd_serve.go:296-324`); a display-only response field is outside
that path. 409 behaviour is untouched.

**Comment line numbers do not drift.** `renderDiff` numbers from `p.text`
independently (`app.js:170-182`) and `reviewDoc` quotes `lines[c.line-1]` from
`p.text` (`app.js:242-243`). Prepending a panel to `#rmain` cannot shift them.
Verified, not assumed.

**No new XSS surface.** `mdBlocks` routes every text path through `esc`/`inl`
(`app.js:60-61,76,79`), and `renderHowto`'s fallback is a static literal. The
plan document is local, and the UI is token-authenticated on localhost.

**`mdBlocks` is a hand-rolled markdown subset** (`app.js:66-99`): fences,
`#`–`####`, rules, lists, blockquotes, paragraphs; inline handles only backtick
code and `**bold**`. No links, italics or tables. A procedure using tables will
render as literal text. `dmy.md`'s section uses only paragraphs, fences and
bold, and A3's step text does not encourage tables.

**Tier C can block a build.** A builder that cannot fill the section returns
`FAILED` rather than moving the ticket, so a bug in `planNeedsTestSteps` — a
path resolution that finds the wrong file, say — stalls the pipeline for every
ticket at once. The `--note` override and the false-on-not-found rule are the
containment. See Q4.

**Nearby defects deliberately untouched.** (1) `app.js:156` renders a
hardcoded "Review the plan" button gated on `t.hasPlan` for **any** state,
including `triage` — the same mislabelling `5362d1d` fixed in `renderGates`
(`app.js:117`) but not here. (2) `apiReview` never checks `post.Kind` against
the ticket's actual state, so a `kind:"plan"` review can be recorded against a
`triage` ticket. (3) Review submits are not idempotent. (2) and (3) are named
in `docs/superpowers/specs/2026-08-26-hive-web-design.md:213-232`. All three
want their own tickets. None is caused or worsened here, and the builder should
not fix them.

**`docs/superpowers/plans/2026-08-21-backlog-pipeline-agents.md:40-78` embeds a
copy of the old template.** Left alone on purpose: it is a completed plan and a
historical record of what was built that day, not a live spec.

**Plan length is not addressed.** Plans run 618 and 1170 lines and this adds a
section. The ticket's point that "anything not near the top is effectively
invisible" stays true of the other seven sections at `plan-review`.

## Critic findings

Two `plan-critic` agents ran against the first draft. Both independently
returned the same blocking finding, and it was a real one.

**Confirmed and fixed — the procedure never reached the commit that the reader
displays.** The draft inserted the new builder step *between* §5 (Commit) and
§6, while `planAtCommit` reads the plan **as of the build commit**. The builder
would have committed, then written the section, and `howToTest` would have
returned `""` on every ticket — the panel rendering "The build did not say how
to exercise it" forever, with all of Tier B inert and the draft's pure-string
test suite entirely blind to it. Both critics verified it independently; the
second supplied the proof, `git show 6c88153:docs/plans/dmy.md` has no `How to
test this` heading because it arrived two commits later in `757eaf0`. Fixed by
reordering `builder.md` so both artifact writes precede the commit, which also
repairs the same latent defect in the existing `## Review findings` step. This
finding alone justified the critique pass.

**Confirmed and fixed — a fix-up commit would replace the reviewed diff.** Once
the ordering is discussed, the obvious "fix" is a second commit for the
artifact. `buildFor` takes the *newest* commit touching `docs/plans/<id>.md`
(`cmd_serve.go:526-527`), so that commit becomes the build, and Steve reviews a
docs-only diff with the code change absent. The draft's Blast radius never
mentioned `buildFor`'s selection rule. Now stated as a rule in A3 (§7: amend,
never a second commit) and in Blast radius.

**Confirmed and fixed — the stub leaked agent instructions into the human's
panel.** The draft's stub was two paragraphs, the first being "**The builder
fills this in, not the planner.**". A builder replacing only the italic line —
which the draft's own wording invited, "leave the stub line below in place" —
would have shipped a triage panel opening with an instruction addressed to an
agent. Stub cut to one line; the ownership rule moved into `planner.md`; the
builder step now says to delete everything between the heading and the next
`##`.

**Confirmed and fixed — which copy of the artifact the builder edits was never
stated.** `builder.md:75` sends the builder to read the *main worktree* copy;
§6 says write to `docs/plans/<id>.md` unqualified. Since the planner leaves the
file untracked in main, a fresh build worktree does not have it at all. The
draft's `planAtCommit` comment asserted the builder edits its own worktree copy
— an assumption no prompt file actually stated. A3 now says it, and says to
copy the file in when missing.

**Confirmed and fixed — the missing-heading fallback existed only in Blast
radius.** The draft noted `wdd.md` has no heading and said the step "must
tolerate" it, but the step text never said so. An executor writing the contract
literally would have produced a replace-only step. Now in the step text.

**Confirmed and fixed — `howToTest`'s fence-awareness was specified for the
terminator but not for the heading match.** This document contains
`## How to test this` inside a ```markdown fence, and so will any re-plan
quoting the template. In the draft only the accident of "first match wins"
prevented a false match. Both the match and the terminator are now fence-aware,
and there is a test row for exactly this.

**Confirmed and fixed — the renumbering instruction contradicted itself.** It
told the executor to "update" two cross-references and then said both were
unaffected, and referred to a "§7 cross-reference" that lives in §8. Replaced
with a flat statement that no cross-reference changes, plus the grep that
proves it.

**Confirmed and fixed — verification could not fail on anything that mattered.**
Every test row exercised a pure string function; nothing covered `planAtCommit`,
the `apiBuild` field, or the panel, so finding 1 passed the entire suite. The
symlink loop resolves to the main worktree and is green whether or not the
branch's edits exist or are correct — it verified nothing and is now explicitly
labelled as not a verification step. `gofmt -l cmd_serve.go` omitted the test
file it also changes, now `gofmt -l .`. An end-to-end `curl … | jq -r .howto`
step was added, and it is the one that would have caught finding 1.

**Accepted with a change of design — the enforcement point was wrong.** The
draft called the panel's empty-state notice "the enforcement mechanism". One
critic pointed out this penalises Steve at the moment he has least recourse,
while `runTodoState` (`cmd_todo.go:404-451`) is a checkpoint every builder
already passes through and can be made to refuse in ~10 lines against the party
who can actually fix it. That is right, and it is now Tier C. The panel notice
is kept as a fallback for tickets that reach `triage` by other routes, not as
the primary enforcement.

**Accepted — `/ship` was scope creep against a non-existent target.** One
critic showed `ship.md:97-101` writes its plan in `/pickup`'s shape, so there
is no `## How to test this` heading for the instruction to fill, and the
draft's own verification (`grep -c … >= 1`) would have passed regardless of
whether any of it cohered. Cut from the contract, recorded in Blast radius.

**Where a critic was wrong.** One suggested a zero-Go alternative: since the
plan document is already inside the diff the browser holds as `p.text`,
`renderHowto` could strip `+` prefixes and lift the section in JS, needing no
`planAtCommit` and being immune to finding 1. It is a fair challenge and it
deserved the answer the draft did not give, but it does not win. The plan
document only appears in `p.text` when it is *added or modified* in that commit,
so the section vanishes from any diff rendered with `-M`, from a rebase, or
from any future change to how the artifact is committed; the `+`-stripping
would have to reconstruct markdown from diff syntax and would break on any
context line; and it would silently show the *old* section for a plan modified
rather than added. Reading the file at the commit is unambiguous. It is also
not immune to finding 1 — it reads the same commit — so the ordering fix is
needed either way. Recorded because the option is genuinely cheaper and a later
reader deserves to know it was considered rather than missed.

Both critics separately verified and endorsed the draft's central factual
claim — that the triage gate renders a diff, contradicting the ticket's stated
premise — and spot-checked its `file:line` anchors without finding an incorrect
one.

## Open questions

**Q1 — How much of this is in scope: A, A+B, or A+B+C?**

The ticket lists the web change as item 4 and calls it optional, on the stated
premise that triage opens the plan document. `app.js:285` and `cmd_serve.go:544`
show it opens the branch diff instead — the premise was true at `d2a104a` and
superseded by `5362d1d` the following day, so the ticket is stale rather than
wrong.

- **(a) Tier A only.** Cheapest, no Go. Genuinely fixes authorship and
  ownership, and improves the `plan-review` gate and the permanent record. But
  at triage the procedure stays invisible — buried in the diff as unrendered
  `+` lines — so **the exact thing Steve hit is not fixed**. He would have to
  know to open the plan file outside the web UI.
- **(b) A+B.** Adds ~60 lines of Go plus tests, ~15 of JS, ~6 of CSS. Fixes the
  observed symptom.
- **(c) A+B+C.** Adds ~15 more lines of Go plus a test, and makes the omission
  fail at the builder rather than at Steve.

**I would take (c)**, on the reasoning that the ticket is about a gate that
stalls, and a plan that leaves the stall detectable only by the person stalled
has not finished the job. **(b) is the safe answer** if Tier C's refusal feels
too sharp — see Q4. **(a) is not enough**: it does not fix the reported
failure, which is the whole ticket.

Each tier is independently landable in order; cutting C, or B and C, requires
no edits to what remains.

**Q2 — Is a runnable fixture required, or is stating what you did enough?**

The ticket's own open question, verbatim: "should the builder be REQUIRED to
leave a runnable fixture, or is that overreach for changes where setup is
trivial or impossible (a TUI interaction, a change needing production data)?"

The contract implements the ticket's own suggested shape: **required to state
what it did to test it and how to reproduce that, with "no fixture possible,
here is why" an acceptable answer.** A hard requirement applies only where a
fixture already exists — "if you built it to test, do not delete it."

- **Harder:** a fixture is always required; "no fixture possible" is not
  acceptable and the builder must construct one or return `FAILED`. Would have
  caught `dmy.md`, but blocks TUI work outright.
- **Softer:** the section is advisory. That is effectively the status quo,
  which produced this ticket.

Taken as suggested because it is Steve's own framing of the compromise and the
harder rule has an obvious class of changes it cannot serve. Flagged rather
than assumed, because the ticket says "Steve to steer" and marks everything
above that line as the recorder's elaboration, not his ruling.

**Q3 — Should fixture cleanup be the builder's job or the reviewer's?**

The contract has the builder write a cleanup step for the human to run, which
is what `dmy.md`'s step 4 does by hand. The alternative — the builder cleans up
on ticket close — is tidier but needs a hook that does not exist and would
delete the fixture before a late reviewer got to it.

I would keep it as contracted. It costs one line and it means fixtures stop
accumulating in `/tmp` and `~/repos`. Raised only because nothing currently
owns fixture lifetime and this change starts creating them deliberately.

**Q4 — Should Tier C refuse the transition, or only warn?**

As contracted, `hive todo state <id> triage` **fails** when the plan still holds
the stub, overridable with `--note`. That puts the cost on the builder, who can
fix it, instead of on Steve, who cannot.

The risk is real: it is a new way for the pipeline to stall. A bug in
`planNeedsTestSteps` — resolving to the wrong worktree's copy, say — would
block every build at once. The containments are the `--note` override, the
false-on-not-found rule, and the fact that the builder returns `FAILED`
visibly rather than hanging.

- **Warn instead:** print the refusal text to stderr and move the ticket
  anyway. Zero stall risk, but a warning in a subagent's scrollback that
  nobody reads is indistinguishable from silence, which is the status quo.
- **Refuse without an override:** strictly better enforcement, but no escape
  for the TUI and production-data cases Q2 exists to accommodate.

I would keep the refusal with `--note`, because it mirrors the override
semantics already in `runTodoState` for backwards moves and so adds no new
concept. But this is the one part of the plan that can break the build
pipeline for everything, not just for this ticket, and that deserves a
deliberate yes.
