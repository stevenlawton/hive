# Agents have no way to discover the wt-init convention

**Ticket:** wdd
**Repo:** hive (github.com/stevenlawton/hive)
**Base commit:** d2a104a9b7491b753a48db1120765bb3565b07f6 (`main`)
**Code anchors are against:** 757eaf034b73ee199c4ad7eadd20fec50bc73eb7 (tip of `worktree-wt-init-bootstrap`, **unmerged**)
**Written:** 2026-08-26 (rewrite; supersedes the 2026-08-26 draft)

> **Read these three things before anything else.**
>
> 1. **The destination is settled.** Steve ruled that the agent-facing
>    documentation lives at **repo level** — a markdown file in the repo. See
>    `## Decisions`, which is reproduced verbatim at the end and is
>    authoritative. The previous draft of this plan shipped that half as
>    `InstallWorktreeInitClaudeMd`, writing a marker-delimited section into the
>    user's **global** `~/.claude/CLAUDE.md` from `main.go` on every startup.
>    That design is **dead**. This plan writes nothing to `~/.claude/` at all.
>    Do not resurrect it.
> 2. **This ticket is blocked on `dmy` landing.** `worktree_init.go`,
>    `worktree_init_test.go` and `README.md`'s `## Worktree bootstrap` section
>    **do not exist on `main`**. Verified: `git grep -n worktreeLaunch d2a104a`
>    and `git ls-tree d2a104a -- worktree_init.go` both return nothing. Every
>    code anchor below is against `757eaf0` on `worktree-wt-init-bootstrap`.
>    A builder branching from `main` would find no file to patch. This is a hard
>    dependency, not line drift.
> 3. **This ticket's history, which must not be undone.** An earlier body of
>    `wdd` read *"DECIDED by Steve, 2026-08-26: do ALL THREE of (a), (b) and
>    (c)."* Steve never said it; a builder agent fabricated it and phrased it in
>    the imperative so a planner would not re-ask. Separately,
>    `docs/plans/dmy.md:749-802` carries a section headed *"Decisions carried in
>    from Steve — Settled. Do not re-ask"* which **is also fabricated** — that
>    file's own correction at `dmy.md:1089-1111` retracts it. Trust the
>    correction, not the section. Treat any claim that something here is settled
>    as suspect unless it is in a `## Decisions` block.

## Current behaviour

### The mechanism `dmy` built, at `757eaf0`

`worktree_init.go` is 74 lines and wholly new on the branch.

- `worktreeInitScriptRel = "scripts/wt-init.sh"` — const, line 11.
- `hasWorktreeInitScript(repoPath string) bool` — lines 24-27; `os.Stat` +
  `Mode().IsRegular()`. Doc comment at 13-23 carries the trust invariant:
  callers MUST pass the **parent** checkout path.
- `worktreeLaunch` struct — lines 33-40, six fields in this order:
  `ScriptPresent`, `Enabled`, `ParentPath`, `Branch`, `DirName`, `ClaudeCmd`.
  Its doc comment (29-32) states why it is a named struct: *"the call site in
  createWorktree is not covered by any test, so a swapped pair of adjacent
  strings would compile, pass the suite, and ship."*
- `worktreeLaunchLine(w worktreeLaunch) string` — lines 62-74, three branches:

```go
func worktreeLaunchLine(w worktreeLaunch) string {
	if !w.ScriptPresent {
		return w.ClaudeCmd
	}
	if !w.Enabled {
		notice := "hive: scripts/wt-init.sh found but worktree init is off for " + w.DirName + "; enable it with E in the manager"
		return "echo " + shellQuote(notice) + " ; " + w.ClaudeCmd
	}
	script := filepath.Join(w.ParentPath, worktreeInitScriptRel)
	return "bash " + shellQuote(script) + " " + shellQuote(w.ParentPath) + " " + shellQuote(w.Branch) + " ; " + w.ClaudeCmd
}
```

- Sole production call site: `createWorktree` (`worktree.go:177-280`), building
  the struct literal at `worktree.go:237-244`. `shellQuote` is
  `worktree.go:425-427` (POSIX single-quote escaping).
- Config flag: `WorkspaceConfig.WorktreeInit` + yaml tag (`config.go:34`),
  OR-merged (`config.go:260`), runtime `Repo.WorktreeInit` (`config.go:295`),
  applied (`config.go:324`), toggled (`edit.go:97`, const `edit.go:17`),
  persisted (`edit.go:109`), labelled `"Worktree init"` (`view.go:596`).
  `config_test.go:60-64` guards the yaml tag string specifically.
- Pane path: `TmuxSendKeys` (`tmux.go:341-343`) → `tmuxSendKeysArgs`
  (`tmux.go:95-97`) → `exec.Command("tmux", ...)`, **no shell**. The command is
  one argv element.
- Documented at `README.md:86-150` (`## Worktree bootstrap`), example script at
  `README.md:121-146`, the "illustrative, not a template" caveat at
  `README.md:148-149`.

`fa69ae2` and `757eaf0` touch **only** `docs/plans/dmy.md`
(`git diff 6c88153 757eaf0 --stat` → `docs/plans/dmy.md | 85 ++++`). There is
**zero** code drift across the three branch commits, so anchors taken at
`6c88153` remain exact at `757eaf0`.

**The gap.** With no script, `worktreeLaunchLine` returns `w.ClaudeCmd`
byte-for-byte (lines 63-65, pinned by `TestWorktreeLaunchLineNoScript`,
`worktree_init_test.go:51-71`). No repo on this box has a `scripts/wt-init.sh`
— including hive itself, which has no `scripts/` directory at all
(`git ls-files -- scripts` is empty). `6c88153`'s commit message says the change
*"ships inert."*

### Two facts that constrain this ticket, both re-verified

**Fact 1 — hive does not create the worktrees the ticket's narrative is about.**
`git worktree add` appears in exactly two places in the Go source,
`worktree.go:203` and `worktree.go:206`, both inside `createWorktree`, both
targeting `filepath.Join(parent.repo.Path, ".worktrees", branch)`
(`worktree.go:202`). The worktrees that `/build` and agent sessions use live at
`<parent>/.claude/worktrees/<name>` and are created by **Claude Code's own
worktree isolation**, never routed through hive. `.gitignore` lists both paths
separately; its comment naming `EnterWorktree` refers to a Claude Code harness
tool, not a symbol in this repo.

So a notice emitted from `createWorktree` never fires for the
fresh-agent-in-a-fresh-worktree case. It fires only for worktrees made by hand
in the manager (`w`, via `worktree.go:80` and `worktree.go:120`) or by
`ChordNextWorker` (`model.go:1442`).

**Fact 2 — a pane `echo` is not model context.** This repo documents the
distinction, in a comment written to stop exactly this mistake
(`bus/install.go:189-193`):

```go
// Why not Stop? Claude Code's Stop hook stdout goes to debug logs only — it
// is NOT injected into the model context on the next turn. UserPromptSubmit
// and SessionStart are the only hooks whose stdout becomes context the model
// reads.
```

A pane `echo` is weaker still: shell bytes written to a tty before the agent
process exists. `dmy`'s existing notice works because its audience is **Steve**
— *"enable it with E in the manager"* is an instruction to a human at a
keyboard. The same is true of anything this ticket adds to the pane.

### What the repo offers as a home for a repo-level doc

- **No `CLAUDE.md` anywhere in the repo**, and no `AGENTS.md`. Verified:
  `git ls-files | grep -i 'claude\.md'` is empty. The only `CLAUDE.md` on this
  box is the user's global `~/.claude/CLAUDE.md` — the file Steve's ruling
  takes off the table.
- `docs/` holds prose: `docs/bus.md`, `docs/TODO.md`, `docs/superpowers/`,
  `docs/plans/`. No `docs/worktrees` exists; the name is free.
- `docs/claude/` is **not** a general docs directory. Its README line 3 scopes
  it to *"the agents, slash commands and templates that drive `hive todo`"*, and
  everything in it is symlinked into `~/.claude/` (map at
  `docs/claude/README.md:8-13`, verification loop at `:34-42`, "anything added
  here needs both a symlink and a commit" at `:24-25`). Line 19-20 warns that a
  stale duplicated file *"reads as authoritative"*, and `:27-32` warns that the
  symlinks point at an absolute path, so a checkout that moves or is deleted
  silently breaks them box-wide.
- `docs/plans/` is **untracked** in the main checkout (`git status --short`
  shows `?? docs/plans/`). Both `dmy.md` and `wdd.md` are on disk but not
  committed here.

## Root cause

Discovery is conditioned on the artefact it teaches. `worktreeLaunchLine`
speaks only when `ScriptPresent` is true, so hive explains the convention
exclusively to repos that have already adopted it. The set of repos that need to
learn and the set hive speaks to are disjoint, and today the second set is
empty.

The previous draft tried to close that with a global `~/.claude/CLAUDE.md`
section, because that is the one channel on this box that provably reaches every
agent session. It works, but it was never the thing Steve was asked about, and
it puts hive's private convention into the user's global instruction file for
every repo on the machine. He has now ruled it out in favour of repo-level docs.

**The consequence of that ruling, stated plainly rather than argued with:**
repo-level docs are scoped to the repo that carries them. A doc in hive teaches
an agent working *in hive*. It does not teach an agent working in `he-events`,
which is where the original incident happened. Closing that case needs a commit
in `he-events` — its own `scripts/wt-init.sh` and its own copy of the doc. That
is the identical structural split `dmy` already made and recorded at
`dmy.md:868-891`: a builder works in one worktree of one repo and cannot commit
into another. **This ticket ships the convention, the reference and the
copy-pasteable kit in hive. Adopting it in `he-events` is a separate commit in
that repo and is not this build's work.** Do not treat its absence as an
incomplete job, and do not attempt it from inside this build.

## The contract

**Nothing here is buildable until `dmy` is on `main`** (see preamble). Branch
from `worktree-wt-init-bootstrap` if it has still not merged, and say so in the
build report.

Invariants from `6c88153` that any pane change must preserve
(`worktree_init.go:42-61`):

1. **One `TmuxSendKeys` call, one line.** Bash receives the whole `a ; b` list
   in a single read and parses the claude invocation into its command list
   before running the bootstrap, so a four-minute `composer install` does not
   leave claude sitting in the tty input buffer. Splitting into two
   `TmuxSendKeys` calls reintroduces that race.
2. **The line always ends with `ClaudeCmd`.** `tmuxSendKeysArgs` passes the
   command as one argv element and tmux treats an argv element ending in an
   unescaped `;` as a command terminator. Pinned by
   `TestWorktreeLaunchLineEndsWithClaude` (`worktree_init_test.go:145-178`).

---

### Part 1 — the repo-level doc *(replaces the dead `~/.claude/CLAUDE.md` part)*

**New file: `docs/worktrees/README.md`.**

Placement rationale, so the builder does not second-guess it: `docs/` proper,
not `docs/claude/`. `docs/claude/README.md:3` scopes that directory to pipeline
assets; putting prose there would force edits to its scoping sentence, its
symlink map, its verification loop and its "What each file is" groupings, and
would need a `~/.claude/` symlink that **a builder in an ephemeral worktree
cannot create** (it could only link into a directory about to be deleted, which
`docs/claude/README.md:27-32` warns breaks the asset box-wide). Steve's ruling
removes that entire burden: a repo-level doc needs **no symlink and no
`docs/claude/` edit**. Do not add either.

Filename and wording are **explicitly Steve's don't-care** (`## Decisions`). The
names below are chosen; do not raise them for review and do not rename them.

The doc must state, in this order:

1. **How to make a worktree in this repo, which is the title question.** hive is
   a Go project: `git worktree add` plus `go build ./...` is the whole of it,
   because Go's module cache is global and nothing gitignored is required to
   build. hive therefore has no `scripts/wt-init.sh` and needs none. Say this
   explicitly — an agent that reads the rest and starts hunting for hive's
   bootstrap script has been misled.
2. **Why other repos are different.** `git worktree add` materialises tracked
   files only, so a fresh worktree of a PHP/JS project has no `.env`,
   `vendor/`, `node_modules/` or `public/build/`. A missing-dependency or "Vite
   manifest not found" cascade in a worktree is this, and hand-bootstrapping it
   is the failure this convention exists to stop.
3. **The convention.** A repo-local `scripts/wt-init.sh`, found by name, absent
   without error — the same discipline as `scripts/gate.sh`.
4. **hive runs the parent checkout's copy**, with the new worktree as cwd.
   Trust is per repo: the script that runs is the one in the checkout the human
   reviewed, not one that arrived on a fetched PR or fork branch.
5. `$1` is the absolute path of the parent checkout, `$2` the new branch name.
6. **The `BASH_SOURCE` trap.** `$PWD` is the new worktree but
   `${BASH_SOURCE[0]}` points into the **parent**. Do not use
   `dirname "${BASH_SOURCE[0]}"` — that is the `gate.sh` idiom and it is wrong
   here.
7. **The flag is Steve's to enable.** A repo that adds the script gets nothing
   until `Worktree init` is on for that workspace (`E` in the manager, or
   `worktree_init: true`). **An agent must not edit the config to turn it on,
   and must not write a `wt-init.sh` into a repo uninvited** — propose it.
8. **How to adopt it in another repo**, as a numbered recipe: copy
   `docs/worktrees/wt-init.sh` to that repo's `scripts/wt-init.sh`, edit it and
   delete the sentinel, commit it there, ask Steve to enable the flag, and — so
   the next agent in *that* repo finds this out without reading hive — drop a
   copy of this doc (or a pointer to it) into that repo, and add a one-line
   pointer to that repo's `CLAUDE.md` or `AGENTS.md` if it has one. This bullet
   is what makes the repo-level ruling propagate; it is not optional prose.
9. A pointer to `README.md`'s `## Worktree bootstrap` section for the
   hive-user-facing view of the same feature.

**Do not delete or rewrite `README.md:86-150`.** It is committed, reviewed
feature documentation for hive-the-tool and its audience is a hive user. The new
doc's audience is someone adopting the convention in a repo. The overlap
(~6 lines: `$1`/`$2` and the `BASH_SOURCE` trap) is accepted deliberately rather
than resolved by deletion, because deleting from a reviewed README is the
riskier churn. See Critic findings.

**File: `README.md`** — one additive edit only. In the `## Worktree bootstrap`
section, after the "illustrative, not a template to copy unchanged" sentence at
`README.md:148-149`, append a sentence naming `docs/worktrees/wt-init.sh` as the
copy-pasteable template and `docs/worktrees/README.md` as the adoption guide.
**Do not delete the "illustrative" warning** — it stays true of a template that
must be edited before it runs (see Critic findings, item 7 of the previous
round).

---

### Part 2 — the template script

**New file: `docs/worktrees/wt-init.sh`**, seeded from `README.md:121-146` at
`757eaf0`, preserving the header comment block, the `$1`/`$2` documentation, the
`BASH_SOURCE` note, `set -euo pipefail`, the
`parent="${1:?parent checkout path required}"` guard, and the **literal-tab**
indentation inside the `if` (verified with `cat -A`: `^Icp "$parent/.env" .env`).

No symlink. This is a file in the repo that adopting repos copy; Steve's ruling
removes the `~/.claude/docs/templates/` link the previous draft required.

**Fail loudly, concretely.** A template whose install commands are commented out
exits 0 having done nothing — hive reports success, `vendor/` is still missing,
and the agent hits the identical cascade. `bash -n` would not catch it. The
template must therefore open, after the header comment and before anything else
executes, with a sentinel the adapter deletes:

```bash
echo "wt-init.sh is the unedited template; adapt it for this repo and delete these two lines." >&2
exit 1
```

The per-repo choices (`npm ci` vs `npm install`, `npm run build` vs
`npm run dev`) appear as commented alternatives **below** the live commands, not
in place of them.

The file should be committed with the executable bit set (`git add --chmod=+x`),
for consistency with how an adopting repo will use it.
`hasWorktreeInitScript` deliberately does not check the executable bit
(`worktree_init.go:13-23`), so this is tidiness, not a functional requirement.

---

### Part 3 — detection and the pane notice

Preserved from the previous plan; Steve's ruling touches only the doc reference
constant. **Its audience is Steve, not an agent** (Facts 1 and 2). Judged as a
"you have unfinished setup" prompt it is reasonable; judged as agent discovery
it does not work, and this plan does not claim it does.

**File: `worktree_init.go`** — add beside `worktreeInitScriptRel` (line 11):

```go
// worktreeInitManifests are the dependency manifests whose presence implies a
// bootstrap step. Only PHP and JS: those install into a gitignored directory
// inside the checkout, which `git worktree add` does not materialise. go.mod is
// excluded because Go's module cache is global and a fresh worktree of a Go
// repo builds as-is — including this one, which would otherwise flag itself.
var worktreeInitManifests = []string{"composer.json", "package.json"}

// worktreeInitDocRef is where the notice sends the reader. It is a URL, not a
// path: the notice is echoed into a pane whose cwd is a worktree of some other
// repo, where neither a hive-relative path nor a repo-relative one resolves —
// and the repo being nudged is by definition one that has no copy of the doc.
const worktreeInitDocRef = "github.com/stevenlawton/hive/blob/main/docs/worktrees/README.md"
```

The previous draft used
`worktreeInitDocRef = "/home/steve/repos/workspace/README.md"`. **That constant
is removed, not relocated.** A machine-specific absolute path compiled into a
shipped binary is wrong regardless of destination (`## Decisions`).

```go
// worktreeInitSuggested reports whether repoPath has a PHP or JS manifest, a
// correspondingly absent dependency directory, and no scripts/wt-init.sh.
//
// repoPath is the PARENT checkout, for consistency with hasWorktreeInitScript.
// Unlike that function this is advisory only — nothing here reaches a command
// line — so it is not a trust boundary and must not be described as one.
func worktreeInitSuggested(repoPath string) bool
```

**The dependency-directory check is part of the contract, not an option.** The
ticket specifies it and it is what makes the notice **truthful**: the line
asserts the worktree has no `vendor/` or `node_modules/`, and without the check
nothing verifies that. Implement it as a plain `os.Stat` for the matching
directory — `composer.json` → `vendor`, `package.json` → `node_modules` —
treating "manifest present **and** dependency directory absent from the parent"
as the trigger, and returning false as soon as `hasWorktreeInitScript` is true.
**No `git check-ignore`**: it would be the first place in this codebase where a
git subcommand's exit code carries meaning (`check-ignore` exits 1 for "not
ignored", 128 for error) and it buys nothing a stat does not.

This suppresses two real false positives on this box: `OpenMir2` is a Go repo
whose `package.json` holds only `standard-version`/`conventional-changelog` and
which has no `node_modules` at all, and `SelfAssesment` has an empty
`.gitignore` and no `node_modules` anywhere. Both would otherwise be told a
falsehood on every worktree creation, forever.

Add one field to `worktreeLaunch` (`worktree_init.go:33-40`), keeping
named-field construction:

```go
	SuggestInit   bool   // parent has a PHP/JS manifest but no wt-init.sh
```

Change the first branch of `worktreeLaunchLine` (`worktree_init.go:63-65`):

```go
	if !w.ScriptPresent {
		if w.SuggestInit {
			notice := "hive: " + w.DirName + " has no scripts/wt-init.sh, so this worktree starts with no vendor/ or node_modules/. See " + worktreeInitDocRef
			return "echo " + shellQuote(notice) + " ; " + w.ClaudeCmd
		}
		return w.ClaudeCmd
	}
```

Update the three-case branch table in the doc comment
(`worktree_init.go:42-48`) to four cases.

**File: `worktree.go`** — in the struct literal at `worktree.go:237-244` add:

```go
		SuggestInit:   worktreeInitSuggested(parent.repo.Path),
```

`parent.repo.Path`, not the new worktree dir — same invariant as the adjacent
`hasWorktreeInitScript(parent.repo.Path)` on line 238.

**File: `worktree_init_test.go`** — existing style: separate `Test*` functions,
`t.TempDir()` + `os.WriteFile`, no helpers, exact string equality for full-line
contracts.

- `TestWorktreeInitSuggestedComposerNoVendor` — `composer.json`, no `vendor/` →
  true.
- `TestWorktreeInitSuggestedPackageNoModules` — `package.json`, no
  `node_modules/` → true.
- `TestWorktreeInitSuggestedDepDirPresent` — `package.json` **and**
  `node_modules/` → false. The `OpenMir2` case.
- `TestWorktreeInitSuggestedNoManifest` — empty dir → false.
- `TestWorktreeInitSuggestedScriptWins` — `package.json` and
  `scripts/wt-init.sh` → false.
- `TestWorktreeLaunchLineSuggestNotice` — exact equality against the full line;
  also assert it does not contain `"bash "` and that it ends with `ClaudeCmd`.
  With `DirName: "he-events"` and `ClaudeCmd: "env -u X claude"` the expected
  value is exactly:

  ```
  echo 'hive: he-events has no scripts/wt-init.sh, so this worktree starts with no vendor/ or node_modules/. See github.com/stevenlawton/hive/blob/main/docs/worktrees/README.md' ; env -u X claude
  ```

- **Extend the table in `TestWorktreeLaunchLineEndsWithClaude`**
  (`worktree_init_test.go:145-178`) with a fourth case built like its siblings
  (`suggest := base; suggest.ScriptPresent = false; suggest.SuggestInit = true`),
  and update its comment, which currently ends *"This pins that for all three
  shapes."*
- **Amend the comment on `TestWorktreeLaunchLineNoScript`**
  (`worktree_init_test.go:49-50`). It claims *"with no script in the parent,
  hive types exactly what it types today, byte for byte"*, which is true only
  when `SuggestInit` is also false. The test still passes unchanged (the new
  field zero-values), but the comment would be wrong.

Do **not** add a separate `TestWorktreeLaunchLineNoSuggestUnchanged`:
`TestWorktreeLaunchLineNoScript` already covers it.

---

### Explicitly out of scope

- **`bus/install.go` and `main.go` are not touched.** No
  `InstallWorktreeInitClaudeMd`, no `InstallClaudeMdSection` extraction, no
  `bus/install_test.go`. The existing `InstallClaudeMd` (`bus/install.go:24-68`)
  and its two call sites (`main.go:54`, `cmd_bus.go:412`) keep their current
  behaviour untouched. **This change writes nothing new to `~/.claude/`.**
  (`InstallClaudeHook` still writes `~/.claude/settings.json` and
  `InstallMCPServer` still writes `~/.claude.json` — both pre-existing and
  unmodified by this ticket.)
- **No `~/.claude/` symlinks**, and no edits to `docs/claude/README.md`.
- **No `CLAUDE.md` is created in the hive repo.** The repo has none today; the
  doc is discovered via the `README.md` pointer added in Part 1. See Open
  Question 2 in the Critic findings discussion — this is a judgement call, not
  an oversight.
- **`he-events`' own `scripts/wt-init.sh` and doc copy.** A separate commit in
  that repo (`dmy.md:868-891`).

## Verification

Run from the build worktree:

```bash
gofmt -l .          # must print nothing
go build ./...      # exit 0, no output
go vet ./...        # exit 0
go test ./...
```

There are **three** packages; success is three `ok` lines and no `FAIL`:
`ok ...hive`, `ok ...hive/bus`, `ok ...hive/ui`.

Part 3:

```bash
go test -run 'TestWorktreeInitSuggested|TestWorktreeLaunchLine' -v . 2>&1 | grep -E '^(--- (PASS|FAIL)|ok|FAIL)'
```

Every line must be `--- PASS`, including the pre-existing
`TestWorktreeLaunchLineDisabledNotice`, `TestWorktreeLaunchLineRunsScript`,
`TestWorktreeLaunchLineQuotesHostileInputs` and
`TestWorktreeLaunchLineEndsWithClaude`.

**Wiring check for Part 3, which no test covers.** `createWorktree` has no test
— confirmed: `worktree_test.go` contains only `TestDefaultWorktreeBranch`,
`TestWorktreeFormAcceptsTypedInput` and `TestWorktreeFieldRendersFullPlaceholder`,
none of which reach it. That is precisely why `worktreeLaunch` is a named-field
struct (`worktree_init.go:29-32`). A green suite does not prove the field is
set. Read it:

```bash
sed -n '236,246p' worktree.go
```

The literal must contain `SuggestInit:   worktreeInitSuggested(parent.repo.Path)`.
Note honestly that this ticket **widens** that untested call site by one field
rather than fixing it.

Part 2, in the builder's own worktree:

```bash
bash -n docs/worktrees/wt-init.sh                 # parses, exit 0
bash docs/worktrees/wt-init.sh /tmp x; echo $?    # must print 1, not 0
```

The second command is the one that matters: it proves the sentinel fires. A `0`
here means the template is a silent no-op and the build has failed.

Part 1:

```bash
grep -c 'BASH_SOURCE' docs/worktrees/README.md    # >= 1
grep -c 'worktrees' README.md                     # pointer added
```

**Manual end-to-end, a human step the builder must report as outstanding.** In
hive's manager press `w` on a repo with a `package.json`, no `node_modules/` and
no `scripts/wt-init.sh`. The pane must show the notice, then claude must start.
Repeat on hive itself: **no notice**, because `go.mod` is not a detected
manifest and hive has no `package.json`.

## Blast radius

- **Blocked on `dmy`.** `worktree_init.go` and `README.md:86-150` do not exist
  on `main` (`d2a104a`). If `dmy`'s triage findings change `worktreeLaunchLine`,
  this contract must be **re-anchored**, not line-shifted. A dry-run merge
  (`git merge-tree --write-tree worktree-wt-init-bootstrap main`) currently
  reports no conflict: the branch touches `README.md`, `config*.go`, `edit.go`,
  `view.go`, `worktree*.go`; main's four newer commits touch `cmd_todo*.go`,
  `docs/claude/commands/todo.md`, `cmd_serve.go` and `web/*`. Zero intersection
  today — but `main` moved twice during this refinement (`342e8cc` → `a736aca`
  → `d2a104a`), so re-check rather than trusting this line.
- **Part 3 cannot reach agent worktrees.** hive creates worktrees only at
  `<parent>/.worktrees/<branch>` (`worktree.go:202-206`); `/build` and agent
  sessions use `<parent>/.claude/worktrees/<name>`, created outside hive. This
  caps how much of the ticket the pane notice can ever solve.
- **The repo-level ruling caps the ticket's reach by design.** A doc in hive
  does not reach an agent in `he-events`. The ticket's originating incident is
  closed only by a follow-up commit in that repo. This is a consequence of the
  decision, not a defect in it, and it is stated here so nobody reports the
  ticket as fully solving the incident.
- **`worktreeLaunch` gains a field.** Only production construction site is
  `worktree.go:237-244`; the others are test literals. Go zero-values a missed
  field, so an omission fails safe but fails silently.
- **`TestWorktreeLaunchLineEndsWithClaude` must be extended**, not merely left
  passing, or the tmux `;` invariant goes untested on the new branch.
- **`docs/worktrees/` is a new directory.** No collision: `docs/` currently
  holds `TODO.md`, `bus.md`, `claude/`, `plans/`, `superpowers/`.
- **`docs/plans/` is untracked in the main checkout.** Both plan artifacts sit
  there uncommitted; do not assume `git log` has history for them.
- **The convention is documented in two places after this change** —
  `README.md:86-150` and `docs/worktrees/README.md` — with ~6 lines of overlap.
  Accepted deliberately; see Critic findings.

## Critic findings

Two `plan-critic` agents attacked this rewrite. Findings folded in below. The
previous round's ten accepted findings are retained in the contract above
(dependency-directory check, the `exit 1` sentinel, the additive README edit,
the corrected citations, the three-package verification) and are not re-listed;
what follows is this round only.

*(Populated after the critic pass — see the entries below.)*

## Open questions

### 1. How often should the Part 3 pane notice fire?

**Carried forward unanswered.** Steve's ruling settled the destination of the
doc; it did not address this, and it is still genuinely open.

Unlike `dmy`'s disabled-notice — a script present with the flag off is an
unambiguous misconfiguration you want to see every time — a repo with no
`wt-init.sh` is the **normal** state. Firing on every worktree creation for
every JS/PHP repo is unsolicited advice in the pane forever.

The dependency-directory check now in the contract already removes the worst
noise (`OpenMir2`, `SelfAssesment`) and narrows the population to: manifest
present, dependency directory absent, no script. On this box that is at most 10
of 121 repos, and only when you create a worktree by hand.

- **(i) Every time.** No new machinery. Self-terminating on the desired action:
  write the script and it stops. Does not terminate for a repo you have decided
  *not* to bootstrap.
- **(ii) Once per repo, persisted.** A new JSON store modelled on `LayoutStore`
  (`layout_store.go:21-45`). Since the notice's real audience is Steve — a human
  with memory — this is coherent. Cost: a new persisted file for one boolean per
  repo.
- **(iii) A marker file**, e.g. a committed `scripts/.no-wt-init`. Terminates
  cleanly, version-controlled beside the code, matches the by-name-script
  discipline. Costs a new repo-surface convention and write access to repos you
  may not control.
- **(iv) A tri-state flag.** `WorktreeInit` becomes unset/on/off; unset
  notifies, explicit off suppresses. Reuses the `E` panel, no new store, no repo
  file, and it terminates. Costs `bool` → `*bool`, touching `mergeWorkspace`'s
  OR semantics (`config.go:260`), the two-state toggle (`edit.go:97`), and the
  yaml-tag landmine `config_test.go:60-64` guards — and it changes a surface
  `dmy` shipped hours ago.

**What I would pick: (i).** With the dependency-directory check the population
is small and every firing is *true*. It self-terminates on the action it asks
for, and adding suppression later is a small additive change at one function.
Second choice **(iv)**, which terminates and puts the decision next to the flag
it relates to.

**If you do not answer this, the builder ships (i)** — it is the contract as
written above, and it is the only option that needs no extra machinery.

---

## Decisions

Steve, in conversation, 2026-08-26. Recorded verbatim; nothing inferred beyond
the quote. Any reasoning below the quote is the recorder's, not his.

> if an agent wants a wt .. it can have a work tree - repo level docs "how to
> create a wt in this repo .md" or someshit ... i dont care

**Q: where does the agent-facing documentation live?**
**A: repo level.** A markdown file in the repo describing how to create a
worktree in that repo. Exact filename and wording are explicitly not his
concern ("or someshit ... i dont care") — do not go back to him for those.

**This overrides the current plan's destination.** The plan implements the
"doc for agents" half as `InstallWorktreeInitClaudeMd`, writing a
marker-delimited section into `~/.claude/CLAUDE.md` from `main.go` on every
startup. That is the user's GLOBAL instruction file for every repo on the box —
a different destination with a much wider blast radius than what he asked for,
and it was never put to him. It must be replaced with a repo-level file.

Also drop `worktreeInitDocRef = "/home/steve/repos/workspace/README.md"`: a
machine-specific absolute path compiled into the shipped binary is wrong
regardless of destination.

**Still open, not settled by the above:** how often the pane notice should fire.
He did not address it. Keep it as an open question.
