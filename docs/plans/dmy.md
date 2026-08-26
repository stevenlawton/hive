# Worktree creation never bootstraps the new checkout

**Ticket:** dmy
**Repo:** hive (github.com/stevenlawton/hive)
**Base commit:** 9f4302d64f48feaaf1d09f8e46702fa835be3bad
**Written:** 2026-08-26

> **This is a rewrite.** The first draft chose option (a) — per-workspace YAML
> listing files-to-copy and commands-to-run. Steve answered Q1 with **(b): a
> repo-local `wt-init` script, no-op when absent**, and said the plan must be
> rewritten rather than patched. The config-based design is gone. What survives
> is the root-cause research and the rejected-alternatives reasoning, which is
> still load-bearing.

## Current behaviour

`createWorktree` is the single creation path for every worktree hive makes.

`worktree.go:177` — `func (m *model) createWorktree() tea.Cmd`. It runs
synchronously on the Bubbletea update goroutine (every git/tmux call is a
blocking `exec.Command`; the function always `return nil` and never defers work
into the `tea.Cmd` it claims to return). In order:

- `worktree.go:178-199` — read branch and prompt from `m.wtFields`, resolve
  `parent *repoItem` by scanning `m.items` for `DirName == m.wtParent`.
- `worktree.go:202` — `wtDir := filepath.Join(parent.repo.Path, ".worktrees", branch)`.
- `worktree.go:203-213` — `git -C <parent> worktree add -b <branch> <wtDir>`,
  falling back at `worktree.go:206` to `git -C <parent> worktree add <wtDir> <branch>`
  when the branch already exists. Only a second failure sets `m.err` and bails.
- `worktree.go:215-221` — `sessionName := TmuxSessionName(m.wtParent+"-wt-"+branch, false)`
  then `TmuxNewSession(sessionName, wtDir)`. A detached session with **no**
  initial command; its cwd is `wtDir`.
- `worktree.go:223-237` — build `args` (`--permission-mode bypassPermissions`
  when yolo, plus `shellQuote(prompt)`), then the one and only
  `TmuxSendKeys(sessionName, claudeCommand(args))`. Its error is discarded.
- `worktree.go:239-270` — append a `Repo{IsWorktree: true, ...}` /
  `repoItem{status: statusClaude}` to `m.items` and return to the manager or
  attach as a split.

Between the `git worktree add` at 203-213 and the claude launch at 237 there is
**nothing**: no file copy, no install, no build.

Confirmed by grep across the tracked Go sources: `composer`, `npm`,
`node_modules`, `vendor/`, `post_create`, `wt-init` and `.hive/` have **zero**
hits. There is no `copyFile` helper (the only `io.Copy` is `ui/attach.go:40`, a
PTY stream) and no per-repo script convention of any kind. There is also **no
test that calls `createWorktree` at all** — `worktree_test.go`,
`worktree_split_test.go` and `chord_worktree_test.go` exercise helpers around
it, never the function itself.

Three entry points, all inheriting the gap:

- `worktree.go:80` — `handleWorktreeKey`, `ctrl+s` / `ctrl+enter`.
- `worktree.go:120` — same handler, plain `enter`.
- `model.go:1442` — `ChordNextWorker` (`ctrl+space g`). **No modal**: it fills
  `m.wtFields` programmatically from `nextSplitBranch(...)` and the
  `nextWorkerPrompt` `/next` constant (`worktree.go:139`) and calls
  `createWorktree()` directly. Nothing on this path can prompt a human, which
  constrains the design below.

### The relevant config surface

`WorkspaceConfig` (`config.go:28-37`) carries only cosmetic/session-flavour
fields:

```go
type WorkspaceConfig struct {
	Name        string `yaml:"name,omitempty"`
	Short       string `yaml:"short,omitempty"`
	Color       string `yaml:"color,omitempty"`
	Description string `yaml:"description,omitempty"`
	Yolo        bool   `yaml:"yolo,omitempty"`
	Remote      bool   `yaml:"remote,omitempty"`
	Favourite   bool   `yaml:"favourite,omitempty"`
	Collection  bool   `yaml:"collection,omitempty"`
}
```

`Yolo` is the precedent this plan imitates. It is a per-workspace boolean that
reaches runtime through **five** places:

- decoded automatically — workspace values go through
  `val.Content[j+1].Decode(&ws)` at `config.go:141`, a plain struct decode, so a
  **new yaml-tagged field needs no decoder change**. (The `switch` at
  `config.go:123-146` enumerates *top-level* keys only; this plan adds none.)
- merged in `mergeWorkspace` (`config.go:244-263`) — `out.Yolo = a.Yolo || b.Yolo`.
- copied onto the runtime `Repo` (`config.go:285-301`) by `applyWorkspaceConfig`
  (`config.go:311-324`) — `repo.Yolo = ws.Yolo`.
- **read and written by the `E` edit panel** — `edit.go:58`
  (`m.editToggles = []bool{repo.Yolo, repo.Remote, repo.Favourite, repo.IsCollection}`),
  `edit.go:92` and `edit.go:103`, then `SaveConfig(m.cfgPath, m.cfg)` at
  `edit.go:108`.
- labelled in the panel at `view.go:596` —
  `toggleLabels := []string{"Yolo", "Remote", "Favourite", "Collection"}`.

The first draft of this plan claimed the field reached runtime "through three
places and no others". That was wrong, and the two `edit.go` sites turn out to
be the decisive ones — see Critic findings.

`createWorktree` already holds `parent *repoItem`, so `parent.repo.<field>` is
in scope with no new plumbing.

## Root cause

This is a missing feature, not a regression. Worktree creation was built to
produce a *git* checkout, and `git worktree add` by construction materialises
only tracked content. Every gitignored-but-required artefact — `.env`,
`vendor/`, `node_modules/`, `public/build/` — is absent from the new checkout by
design of the tool hive is driving, and hive adds nothing on top.

Confirmed on the reporting repo, `/home/steve/repos/he-events`:

```
.gitignore:9:.env	.env
.gitignore:2:/node_modules	node_modules
.gitignore:8:/vendor	vendor
.gitignore:3:/public/build	public/build
```

`git ls-files --error-unmatch .env` fails there — `.env` is untracked, so a
fresh worktree cannot have it. The originating diagnosis is bus message
`msg_6a7c9a0bb66ca92ffc5e` (`wt:workspace` auto-responder, 2026-08-12), replying
to `msg_6a7c99e98d26ebaf8873` from `wt:he-events-wt-split-1`: "composer test
silently warned (missing .env) and 61 unrelated tests failed on 'Vite manifest
not found' until composer install / npm install / npm run build were run."

For the feature to exist, two things are missing: **a place to declare what a
worktree of a given repo needs**, and **a step in `createWorktree` that acts on
it**. Q1 settles the first as a repo-local script.

### Why reinstall rather than share the parent's dependency trees

Kept from the first draft because it is the reason the recipe is *commands*
rather than *copying*, and nobody should "optimise" it away later.

- **Symlinking `vendor/` is actively wrong.**
  `he-events/vendor/composer/autoload_psr4.php` computes
  `$vendorDir = dirname(__DIR__); $baseDir = dirname($vendorDir);` and PHP's
  `__DIR__` resolves symlinks — so a symlinked `vendor/` inside a worktree makes
  `$baseDir` the **parent** checkout. The worktree would silently autoload and
  test the parent's `app/` classes. A green suite would be meaningless.
- **Copying the trees is rejected.** 156M `vendor/` plus 77M `node_modules/` on
  ext4 with no reflink support, and `node_modules/.bin` is entirely relative
  symlinks.

Running the installers is the only correct option. `he-events/.gitignore:38`
ignores `/.worktrees/`, so nothing a script does inside a worktree dirties the
parent's working tree.

## The contract

Six files change — `config.go`, `edit.go`, `view.go`, `worktree.go`,
`config.example.yaml`, `README.md` — plus one new source file and two test
files.

The design in one sentence: **if the parent checkout contains
`scripts/wt-init.sh`, and its workspace has `worktree_init` enabled, hive
prepends `bash '<parent>/scripts/wt-init.sh' '<parent>' '<branch>'` to the
single line it types into the new worktree's tmux pane before `claude`;
otherwise it types what it types today.**

Two properties are load-bearing and must survive any refactor:

- **The script is read from the PARENT checkout, never from the new worktree.**
  Trust is granted per repo; if hive ran the worktree's copy, that trust would
  silently extend to whatever `scripts/wt-init.sh` exists on any branch checked
  out via the pre-existing-branch fallback at `worktree.go:206` — a fetched PR
  or fork branch nobody read. Both critics found this independently. It also
  fixes a second bug: an old branch that predates the script would otherwise
  silently no-op, in exactly the case where bootstrapping matters most.
- **One `TmuxSendKeys` call, one line.** See §3.

### 1. `config.go` — a single opt-in boolean

Four one-line additions, each mirroring `Yolo`.

**(a)** In `WorkspaceConfig` (`config.go:28-37`), after the `Yolo` field:

```go
	WorktreeInit bool   `yaml:"worktree_init,omitempty"`
```

**(b)** In `Repo` (`config.go:285-301`), after the `Yolo bool` field:

```go
	WorktreeInit   bool
```

**(c)** In `mergeWorkspace` (`config.go:244-263`), beside the other booleans:

```go
	out.WorktreeInit = a.WorktreeInit || b.WorktreeInit
```

**(d)** In `applyWorkspaceConfig` (`config.go:311-324`), beside `repo.Yolo`:

```go
	repo.WorktreeInit = ws.WorktreeInit
```

Do **not** touch `decodeConfigNode` — workspace entries are struct-decoded at
`config.go:141` and pick the field up for free. Adding a `case` there would be
wrong; that switch is for top-level keys only.

### 2. `edit.go` + `view.go` — make the flag settable

Without this the flag is undeliverable. Evidence: Steve's real config
(`~/.config/hive/config.yaml`) has **106 workspace entries**, and the only keys
that appear anywhere in it are `name`, `short`, `color`, `description`,
`remote`, `favourite`, `collection` — precisely the set the `E` panel writes.
**`yolo` appears zero times**, despite being supported since it was added. A
YAML-only flag would be the first key in that file that is not editor-written.

Five edits, all mechanical:

**(a)** `edit.go:8-18` const block — insert `editToggleWorktreeInit` after
`editToggleCollection` and bump the count:

```go
	editToggleCollection   = 7
	editToggleWorktreeInit = 8
	editFieldCount         = 9
```

**(b)** `edit.go:58` — append to the toggle slice:

```go
	m.editToggles = []bool{repo.Yolo, repo.Remote, repo.Favourite, repo.IsCollection, repo.WorktreeInit}
```

**(c)** `edit.go:92-95` block — add:

```go
		item.repo.WorktreeInit = m.editToggles[4]
```

**(d)** `edit.go:103-106` block — add:

```go
		ws.WorktreeInit = item.repo.WorktreeInit
```

**(e)** `view.go:596` — extend the labels:

```go
	toggleLabels := []string{"Yolo", "Remote", "Favourite", "Collection", "Worktree init"}
```

The loop at `view.go:597` uses `focusIdx := editToggleYolo + i`, so it picks the
new toggle up with no further change. Check the panel's height/bounds arithmetic
around `view.go:594-610` still fits nine fields; if the panel is fixed-height,
grow it by one line rather than dropping the toggle.

### 3. `worktree_init.go` — new file

Package `main`. Imports: **`os` and `path/filepath` only.** Do not import
`strings` (`shellQuote` lives in `worktree.go:418` and is package-scoped) and do
not import `fmt` unless the implementation actually calls it — Go rejects unused
imports and the first draft's import list would not have compiled.

```go
// worktreeInitScriptRel is the repo-relative path hive looks for. It follows
// the scripts/gate.sh convention used by he-events and stevenlawton.com: a
// repo-local script found by name, whose absence is not an error.
const worktreeInitScriptRel = "scripts/wt-init.sh"

// hasWorktreeInitScript reports whether repoPath contains a wt-init script.
// It requires a regular file; a directory, a dangling symlink, or an absent
// path all report false.
//
// The executable bit is deliberately NOT checked: hive invokes the script as
// `bash <path>`, so the bit is irrelevant, and requiring it would make the
// feature fail silently on a checkout where it was lost.
//
// Callers MUST pass the PARENT checkout path, never the new worktree — see
// "The contract" above.
func hasWorktreeInitScript(repoPath string) bool

// worktreeLaunch is the input to worktreeLaunchLine. A struct rather than
// positional parameters because the call site in createWorktree is not covered
// by any test, so a swapped pair of adjacent strings would compile, pass the
// suite, and ship.
type worktreeLaunch struct {
	ScriptPresent bool   // parent has scripts/wt-init.sh
	Enabled       bool   // workspace has worktree_init set
	ParentPath    string // absolute path of the parent checkout
	Branch        string // new branch name
	DirName       string // workspace dir name, for the notice text only
	ClaudeCmd     string // fully-built claude invocation
}

// worktreeLaunchLine builds the single line hive types into the new worktree's
// tmux pane. ClaudeCmd always ends the line.
//
//	present && enabled  -> "bash '<parent>/scripts/wt-init.sh' '<parent>' '<branch>' ; <ClaudeCmd>"
//	present && !enabled -> "echo '<notice>' ; <ClaudeCmd>"
//	!present            -> ClaudeCmd, returned unchanged
func worktreeLaunchLine(w worktreeLaunch) string
```

Implementation notes the builder must honour:

- `hasWorktreeInitScript` uses the codebase's ubiquitous pattern —
  `fi, err := os.Stat(filepath.Join(repoPath, worktreeInitScriptRel))`, then
  `err == nil && fi.Mode().IsRegular()`. `os.Stat` follows symlinks, which is
  what we want. There is no existing file-check helper to reuse; every call site
  repeats `os.Stat` raw (`worktree.go:47,170,291,354`, `config.go:64`,
  `scratch.go:86`, …).
- The script path is built by joining `ParentPath` with
  `worktreeInitScriptRel` and passing the result through `shellQuote`. It is
  **absolute**, which sidesteps the fact that `branch` is unsanitised user input
  joined into `wtDir` (`worktree.go:202`) — a branch named `feature/foo` nests
  the worktree an extra level deep, so a relative path would be wrong.
- `ParentPath` and `Branch` go through the existing `shellQuote`
  (`worktree.go:418-419`, POSIX single-quoting with `'\''` escaping), passed as
  `$1` and `$2`.
- The separator before `ClaudeCmd` is `" ; "` — see Open question 2.
- The notice text, exactly (no em-dash, no config path — the toggle from §2 is
  now the way to set it):

  ```
  hive: scripts/wt-init.sh found but worktree init is off for <DirName>; enable it with E in the manager
  ```

  Pass the whole notice through `shellQuote` so the `:` and `;` cannot break the
  shell line. **Note the notice itself contains a `;`** — this is precisely why
  it must be quoted, and why the tmux invariant below matters.
- `worktreeLaunchLine` must return `ClaudeCmd` **byte-identical and untouched**
  when `ScriptPresent` is false. That is the no-op-if-absent guarantee, asserted
  directly by a test below.

**Two invariants to write into the code as comments**, because both are
currently true by luck rather than construction:

1. **One `TmuxSendKeys`, one line.** Because bash receives the whole `a ; b`
   list in a single read, it parses `claude` into its command list *before*
   running the bootstrap. A four-minute `composer install` therefore does not
   leave the claude invocation sitting in the tty input buffer. Splitting this
   into two `TmuxSendKeys` calls would reintroduce exactly that race. (The
   `TmuxNewSession`→`TmuxSendKeys` race is pre-existing and proven fine in
   practice — `scratch.go:119` and `session.go:245` do the same today.)
2. **tmux argv and the trailing `;`.** `tmuxSendKeysArgs` (`tmux.go:95-97`)
   passes the command as one argv element, and tmux treats an argument *ending*
   in an unescaped `;` as a command terminator. This change introduces the first
   `;` ever to appear in a `send-keys` argument in this codebase. It is safe
   only because the line always ends with `ClaudeCmd`, which never ends in `;`.
   Say so in a comment.

### 4. `worktree.go` — wire it in

Replace the single line at `worktree.go:237`:

```go
	TmuxSendKeys(sessionName, claudeCommand(args))
```

with:

```go
	TmuxSendKeys(sessionName, worktreeLaunchLine(worktreeLaunch{
		ScriptPresent: hasWorktreeInitScript(parent.repo.Path),
		Enabled:       parent.repo.WorktreeInit,
		ParentPath:    parent.repo.Path,
		Branch:        branch,
		DirName:       parent.repo.DirName,
		ClaudeCmd:     claudeCommand(args),
	}))
```

That is the **entire** change to `worktree.go`. Specifically:

- **Do not add `WorktreeInit` to the `wtRepo` literal** at `worktree.go:240-250`.
  The first draft did, "for consistency with `Yolo`". It is dead state:
  `DiscoverWorktrees` (`worktree.go:294`) does not set it, so it would be `true`
  this session and `false` after a restart, and nothing ever reads a worktree's
  own `WorktreeInit` — `openWorktreePanel` (`worktree.go:57`) refuses
  `item.repo.IsWorktree` outright.
- **Nothing new can set `m.err`.** `m.err` is never cleared anywhere
  (`grep -rn "m\.err = nil" *.go` returns nothing), so it persists in the status
  bar (`view.go:448`, `model.go:2008`) for the life of the process. Every
  existing `m.err` write is a hard failure; a missing or failing wt-init script
  is not one.
- **No new failure path, no early return.** By the time this line runs the git
  worktree already exists on disk; an early return would strand a checkout with
  no session and no entry in `m.items`.
- **Do not add a tmux seam.** The superseded draft proposed package-level
  `var tmuxSendKeys = TmuxSendKeys` indirection so `createWorktree` could be
  driven in tests. Rejected: constructing a `model` for `createWorktree`
  requires building `m.wtFields` textinputs and a populated `m.items`, and the
  test still would not exercise real tmux. The struct parameter above plus the
  mechanical wiring check in Verification cover the same failure at lower cost.
- **`TmuxNewSessionWithCmd` (`tmux.go:87-93`, used at `edit.go:114`) was
  considered and rejected.** It would run the line as the session's initial
  command and remove the send-keys race entirely — but the pane then dies when
  claude exits, instead of dropping to a shell in the worktree, which is a
  behaviour change nobody asked for.
- No import change in `worktree.go`.

### 5. `config.example.yaml`

Append to the `my-project` block **only** — its `favourite: true` is **line
24**; there are further `favourite: true` lines at 30 and 35, so anchor on the
line number or on the `my-project` block, not the bare string.

Ship it **commented out**. This is the one key in the file that causes code to
execute, and `config.example.yaml` is a block people copy wholesale:

```yaml
    # yolo: true                 # Start claude with bypassPermissions
    # worktree_init: true        # Run this repo's scripts/wt-init.sh in each new
                                 # worktree before claude starts. Off by default:
                                 # it executes a script the repo controls, so it
                                 # is opt-in per workspace. Toggle it with E.
```

The `yolo` line is included because the example currently documents neither
`yolo` nor `description`, and `worktree_init` is meaningless as "the same idea
as yolo" if yolo is undocumented.

### 6. `README.md` — document the convention

Insert a new `## Worktree bootstrap` section **between the end of the
Workspace-view keybinding table and `## Claude Code Plugin` at `README.md:86`**.
That is the only prose seam; the worktree references at `README.md:11` and
`README.md:58/76-77` are a feature bullet and two table rows, not insertion
points.

Content:

- `git worktree add` materialises tracked files only, so a fresh worktree of a
  PHP/JS project has no `.env`, `vendor/`, `node_modules/` or `public/build/`.
- If a repo has `scripts/wt-init.sh` **and** its workspace has `Worktree init`
  enabled (`E` in the manager, or `worktree_init: true` in config), hive runs it
  before starting claude, in the visible tmux pane.
- **hive runs the parent checkout's copy of the script**, with the new worktree
  as the working directory. State this explicitly and state why: the trust flag
  is per repo, so the script that runs is the one in the checkout you reviewed,
  not one that arrived on a fetched branch.
- Consequence for script authors: `$PWD` is the new worktree, but
  `${BASH_SOURCE[0]}` points into the **parent**. Do not use `dirname
  "${BASH_SOURCE[0]}"` to locate the worktree — that is the gate.sh idiom and it
  is wrong here.
- It is passed `$1` = absolute path of the parent checkout, `$2` = the new
  branch name.
- Claude starts even if the script fails.
- Without the script, hive behaves exactly as before; with the script but
  without the flag, hive prints a one-line notice into the pane and runs nothing.

Include this copy-pasteable example:

```bash
#!/usr/bin/env bash
#
# Prepare a fresh git worktree for work. Run by hive immediately after
# `git worktree add`, from the new worktree, before claude starts.
#
#   $1  absolute path of the parent checkout
#   $2  the new branch name
#
# NOTE: $PWD is the new worktree; BASH_SOURCE points into the parent.
#
# git worktree add materialises tracked files only, so everything gitignored
# but required has to be recreated here.

set -euo pipefail

parent="${1:?parent checkout path required}"

if [ -f "$parent/.env" ] && [ ! -f .env ]; then
	cp "$parent/.env" .env
fi

composer install
npm ci
npm run build
```

Note in the README that this is illustrative — `npm ci` versus `npm install`,
and `build` versus `dev`, are per-repo choices.

### 7. Tests

Stdlib only, `package main`, `t.TempDir()`, hand-written `t.Errorf`/`t.Fatalf` —
matching `config_test.go` and `worktree_test.go`. No testify (absent from
`go.mod`), no `testdata/` (none exists).

**New file `worktree_init_test.go`:**

- `TestHasWorktreeInitScript` — one `t.TempDir()` per case:
  - empty dir → `false`.
  - `scripts/` exists but no `wt-init.sh` → `false`.
  - `scripts/wt-init.sh` written as a regular file, mode `0o644` → `true`.
    This case pins "the executable bit is not required".
  - `scripts/wt-init.sh` created as a *directory* → `false`.
- `TestWorktreeLaunchLineNoScript` — the no-op guarantee. With
  `ScriptPresent: false`, assert the result `==` `ClaudeCmd` exactly, not
  `strings.Contains`. Assert it for both `Enabled: true` and `Enabled: false`.
- `TestWorktreeLaunchLineRunsScript` — `ScriptPresent: true, Enabled: true`,
  `ParentPath: "/home/steve/repos/he-events"`, `Branch: "split-1"`,
  `DirName: "he-events"`, `ClaudeCmd: "env -u X claude"`. Assert the result
  equals, exactly:

  ```
  bash '/home/steve/repos/he-events/scripts/wt-init.sh' '/home/steve/repos/he-events' 'split-1' ; env -u X claude
  ```

  Exact equality, because the quoting and the `" ; "` spacing are the contract.
- `TestWorktreeLaunchLineDisabledNotice` — `ScriptPresent: true, Enabled: false`.
  Assert the result does **not** contain `bash ` and does **not** contain
  `wt-init.sh'` as a command (i.e. no invocation), does contain the `DirName`,
  and ends with `ClaudeCmd`. This is the security assertion: a repo-controlled
  script is not executed without the flag.
- `TestWorktreeLaunchLineQuotesHostileInputs` — `ParentPath: "/tmp/it's here"`,
  `Branch: "a'b"`. Assert the line contains `'/tmp/it'\''s here/scripts/wt-init.sh'`
  and `'a'\''b'`, proving a branch name cannot break out of its argument.
- `TestWorktreeLaunchLineEndsWithClaude` — for all three shapes, assert
  `strings.HasSuffix(got, w.ClaudeCmd)`. This pins tmux invariant 2 (the line
  never ends in `;`).

**Additions to `config_test.go`:**

- Extend `TestLoadConfig_ParsesYAML` (`config_test.go:26`) — add
  `worktree_init: true` to the workspace entry and assert it round-trips onto
  `cfg.Workspaces[...].WorktreeInit`. Catches a missing or misspelled yaml tag
  (without the tag, yaml.v3 looks for `worktreeinit` and the key decodes to
  `false`).
- `TestMergeWorkspace_WorktreeInit` — new. Four cases: set on `a` only, on `b`
  only, on both, on neither; assert OR semantics. **The `b`-only case is the
  load-bearing one** — `mergeWorkspace` opens with `out := a` (`config.go:250`),
  so the other three pass even if the new line is missing entirely. Do not let
  a later cleanup delete it as redundant; say so in a comment.
- `TestApplyWorkspaceConfig_WorktreeInit` — new, as its own function (do **not**
  fold it into `TestDiscoverRepos`; the Verification `-run` filter below names
  it, and `go test -run` exits 0 when nothing matches, so a folded-in version
  would silently report success). Assert `repo.WorktreeInit` is true for a
  configured workspace and false for an unconfigured one.

## Verification

Hive has no `Makefile`, no `justfile`, no CI workflow and no `scripts/gate.sh`
of its own (all confirmed absent). Its gate is the plain four-command sequence.
Run from `/home/steve/repos/workspace`. **Baseline confirmed green at
`9f4302d` before this plan was written**, so any failure below is yours.

```bash
gofmt -l .
```
Success = **prints nothing**.

```bash
go build -o /tmp/hive-dmy .
```
Success = exit 0, no output.

```bash
go vet ./...
```
Success = exit 0, no output.

```bash
go test ./...
```
Success = exactly three lines, all `ok`:
`github.com/stevenlawton/hive`, `.../bus`, `.../ui`. (All three have tests;
none reports "no test files".) No `FAIL`.

```bash
go test -count=1 -run 'TestWorktreeLaunchLine|TestHasWorktreeInitScript|TestMergeWorkspace_WorktreeInit|TestApplyWorkspaceConfig_WorktreeInit|TestLoadConfig_ParsesYAML' -v .
```
Success = every named test reports `--- PASS`, and the count of `--- PASS` lines
is **at least 8**. Before the implementation exists this must fail to compile
(`undefined: worktreeLaunchLine`) — that is the red.

**Wiring check — do not skip this.** Every test above calls the new helpers
directly, so a builder who creates `worktree_init.go`, makes all the config
edits, writes every test, and *never edits `worktree.go:237`* still gets a fully
green gate: unused package-level functions and unread struct fields are not
errors in Go. The feature would be entirely absent and every criterion above
met. These three commands close that hole:

```bash
grep -n 'worktreeLaunchLine(worktreeLaunch{' worktree.go
grep -n 'hasWorktreeInitScript(parent.repo.Path)' worktree.go
grep -c 'TmuxSendKeys(sessionName, claudeCommand(args))' worktree.go
```
Success = the first two each print exactly one line inside `createWorktree`, and
the third prints `0` (the old call is gone).

```bash
grep -n 'WorktreeInit' config.go edit.go | wc -l
```
Success = `6` — four in `config.go` (struct field, `Repo` field,
`mergeWorkspace`, `applyWorkspaceConfig`) and two in `edit.go`.

**Manual end-to-end check** (a human step; the builder should report it as
outstanding rather than claim it):

1. Create a worktree of a repo with no `scripts/wt-init.sh` — hive itself
   qualifies — and confirm the pane shows the bare `env -u … claude …` line and
   nothing else, identical to today.
2. In a scratch repo, add `scripts/wt-init.sh` containing
   `echo "wt-init ran: parent=$1 branch=$2 pwd=$PWD"`. Create a worktree with
   the flag off: confirm the pane shows the notice and the echo does **not**
   run. Enable `Worktree init` with `E`, create another worktree: confirm the
   echo runs, `pwd` is the new worktree, and `parent` is the parent checkout.

## Blast radius

- **`createWorktree` is the only caller** of the new code, reached from exactly
  three places: `worktree.go:80`, `worktree.go:120`, `model.go:1442`. All three
  get the same behaviour. The `ChordNextWorker` path cannot prompt a human,
  which is why the design uses a flag rather than a confirmation modal — a modal
  there would either block `ctrl+space g` or be skipped, and skipping is the
  insecure default.
- **The change ships inert.** No workspace has the flag and no repo under
  `~/repos` (121 directories) has a `wt-init.sh` — verified: grep for
  `wt-init`/`worktree_init` across `~/repos` matches only this plan and
  `docs/TODO.md`. Every existing worktree flow is byte-identical. That is
  deliberate, but it means **the reported he-events symptom is still present
  when this ticket closes** — see Open question 1.
- **`Repo` gains a field.** `Repo` is constructed in `DiscoverRepos`
  (`config.go:326+`), `DiscoverArchived` (`archive.go:123`), `DiscoverWorktrees`
  (`worktree.go:294`), `createWorktree` (`worktree.go:240`) and scratch creation
  (`scratch.go`). All keyed struct literals, so the new field is additive and
  defaults to `false`. `Repo` is not persisted in this shape — layout
  persistence stores tab/pane state — so no on-disk format changes.
- **`WorkspaceConfig` gains a field, and two different paths rewrite the user's
  config.** `LoadConfig`→`cleanupWorkspaces` rewrites via
  `rewriteConfigWithBackup` (`config.go:265-272`, takes a `.bak`), and the `E`
  panel rewrites via a bare `SaveConfig` (`edit.go:108`, **no backup**). Because
  the new field is `omitempty`, a `false` never appears in a rewritten file.
  Because `mergeWorkspace` now ORs it, a `true` survives dedup instead of being
  dropped — omitting that line would be silent data loss for anyone with
  duplicate keys, which is why it has its own test.
- **Editing config by hand while hive is running is unsafe, and this plan does
  not make it worse.** `edit.go:108` marshals the whole in-memory `Config`
  loaded at startup, so any hand-edit made since is lost the next time `E` is
  used on *any* repo. `WorktreeInit` survives an `E` on a *different* repo only
  because `edit.go:98` is read-modify-write on the map entry. This is a
  pre-existing hazard worth its own ticket; it is the reason §2 adds the toggle
  rather than telling users to edit YAML.
- **Collection workspaces.** `WorkspaceConfig.Collection` makes `DiscoverRepos`
  (`config.go:349-373`) synthesise child keys `dirName + "/" + child`. Children
  get `applyWorkspaceConfig` from their *own* entry, so `worktree_init` does
  **not** inherit from a collection parent. That is correct for a trust flag —
  trust is per repo — and should not be "fixed" later without thought.
- **No behaviour is removed and no test encodes the old behaviour**, because no
  test calls `createWorktree` today. The new tests are the first coverage here,
  but they cover the *helpers*; `createWorktree` itself remains untested, which
  is why the wiring greps above are part of the gate. Making `createWorktree`
  testable is a separate refactor, out of scope.
- **Hive itself will not get a `scripts/wt-init.sh`.** A Go worktree needs
  nothing beyond the module cache, so there is no honest one to write. The
  feature cannot be dogfooded in-repo, hence the scratch repo in the manual
  check.
- **Concurrent work in this worktree.** Another session is moving the todo store
  (`todo_store.go`, `repo_key.go`, `cmd_todo*.go`, `docs/TODO.md`) and HEAD has
  already moved from `1bb0614` (the superseded draft's base) to `9f4302d`. Those
  files do not overlap with the six this plan touches, but the builder must
  re-verify every anchor against HEAD before editing. One critic already found
  `Repo`/`applyWorkspaceConfig` reported as `config.go:284`/`310` rather than
  `285`/`311` depending on how the struct comment is counted — anchor on the
  symbol, not the number.

## Critic findings

Two `plan-critic` agents attacked the draft independently. Both confirmed the
research and the anchors; both rejected parts of the design.

**Accepted, and the reason the design changed:**

1. **Trust was keyed on the repo, execution on the branch.** Both critics found
   this independently and it was the most serious finding. The draft looked for
   the script in `wtDir`, so `worktree_init: true` on he-events would have
   authorised whatever `scripts/wt-init.sh` arrived on any branch checked out
   through the pre-existing-branch fallback (`worktree.go:206`) — a fetched PR
   or fork branch nobody read. Reviewing someone's branch in an isolated
   worktree is a *main* use of worktrees, so this was not a corner case. The
   draft's §3 argued for `wtDir` on correctness grounds and never engaged trust,
   directly contradicting its own security section. **Changed: hive now reads
   and runs the parent checkout's copy.** This also fixed an unnoticed bug — an
   old branch predating the script would have silently no-opped.
2. **The gate could not detect an unwired feature.** All tests call the helpers
   directly, and unused functions/fields are legal Go, so a builder could skip
   `worktree.go:237` entirely and get a fully green gate. **Changed: the wiring
   greps are now part of Verification**, with exact expected output.
3. **The opt-in flag was unsettable in practice.** Critic 2 checked the real
   config: 106 workspace entries, and `yolo` — the precedent this plan claims to
   imitate — appears **zero times**. Every key present is one the `E` panel
   writes. A YAML-only flag would have been the first of its kind, and the
   draft's predicted outcome was "nothing changes and the switch never gets
   thrown". **Changed: §2 adds the `E` toggle.** Critic 2 also corrected the
   draft's claim that `Yolo` reaches runtime "through three places and no
   others" — `edit.go:92` and `edit.go:103` are a fourth and fifth.
4. **Six positional parameters, two adjacent bools and three adjacent strings,
   at a call site no test covers.** **Changed: `worktreeLaunch` struct.**
5. **Dead state.** `WorktreeInit` on the `wtRepo` literal is never read and
   would be `true` this session, `false` after a restart, since
   `DiscoverWorktrees` (`worktree.go:294`) does not set it. **Changed: removed**,
   and the `Repo` construction-site list corrected to include `archive.go:123`
   and `worktree.go:294`.
6. **The import list would not have compiled** — `strings` was listed but
   unused. **Changed.** Likewise the README anchor (now `README.md:86`), the
   test-placement ambiguity (`go test -run` exits 0 on no match, so "new or
   folded in" could have reported false success), the `mergeWorkspace` note that
   only the `b`-only case is load-bearing, `config.example.yaml` now shipping the
   flag commented out, "~40 repos" corrected to 121 directories / 106 configured
   workspaces, and the expected `go test` output corrected from "no test files"
   to three `ok` lines.
7. **Undocumented invariants.** Critic 2 confirmed the single-line-with-`;`
   shape is correct — bash parses the whole list in one read, so claude does not
   sit in the tty buffer during a long install — but noted the plan relied on
   this by accident, and that this change introduces the first `;` ever to
   appear in a `send-keys` argument (tmux treats a trailing unescaped `;` as a
   command terminator). **Both are now written into §3 as invariants with
   comments and a `TestWorktreeLaunchLineEndsWithClaude` assertion.**

**Rejected, with reasons:**

- **Critic 1 wanted the disabled-notice branch deleted** as a third code shape
  existing only to say "you forgot a config line", and objected that it fires on
  every worktree creation forever. Kept, but rewritten. Critic 1 was reasoning
  against the draft's version, which pointed at hand-editing a YAML path —
  advice that Critic 2 independently showed to be both unprecedented and unsafe.
  With §2's toggle the notice is actionable in two keystrokes, and it is the only
  thing standing between "added a script, nothing happened" and a silent
  mystery. The permanent-noise objection is real but proportionate: a repo with
  a bootstrap script and the flag off is a misconfiguration you want to see.
- **Critic 1 offered a `var tmuxSendKeys = TmuxSendKeys` seam** as the
  alternative fix for finding 2. Rejected on cost: driving `createWorktree` in a
  test means building `m.wtFields` textinputs and a populated `m.items`, and
  still would not exercise real tmux. The struct parameter plus the wiring greps
  close the same hole for far less.
- **Critic 2 floated dropping the flag entirely** and letting script-presence be
  the opt-in. Rejected: Steve's decision note explicitly forbids quietly
  shipping the auto-execute. The flag stays; §2 is what makes it real.
- **Critic 2 asked why not `TmuxNewSessionWithCmd`** (`tmux.go:87-93`). A fair
  question — it removes the send-keys race — but the pane then dies when claude
  exits instead of dropping to a shell in the worktree. Now recorded as a
  rejected alternative in §4 rather than left unmentioned.

**One correction to a critic.** Critic 1 implied the example script's
`[ -f x ] && [ ! -f y ] && cp` guard would abort under `set -e` when the middle
test fails. Checked empirically: bash exempts every command in an `&&` list
except the last, and the list's own non-zero status does not trigger `set -e`;
the script continued and exited 0. The guard was safe. It was rewritten as an
explicit `if` anyway, because the reader should not have to know that rule.

## Decisions carried in from Steve

Settled 2026-08-26. Do not re-ask.

**Q1 — per-workspace config, or a repo-local script? → (b), the repo-local
script**, no-op when the repo does not have one.

Steve's supporting evidence:

- **This is an existing discipline.** `scripts/gate.sh` is already a
  repo-specific script that tooling finds by name and shrugs off when absent —
  `docs/claude/agents/builder.md:129-136` says outright: *"If there is no gate
  script, use the repo's plain test command."* Hence `scripts/wt-init.sh` rather
  than `.hive/bootstrap.sh`. Verified against `he-events/scripts/gate.sh` and
  `stevenlawton.com/scripts/gate.sh`, which both open with `#!/usr/bin/env
  bash`, a prose header, an argument list and `set -euo pipefail`; both repos'
  `scripts/` dirs already hold a dozen-plus by-name scripts, so one more is
  idiomatic. Note hive itself has no `scripts/` dir, so the convention is cited
  from the sibling repos, not this one.
- The draft's first objection to (b) — *"it does not work on repos you do not
  control"* — is answered by no-op-if-absent: those repos behave exactly as they
  do today. No regression, just no benefit.

**The auto-execute objection, which Steve left open and told this plan to
resolve: resolved by a per-repo opt-in, made real by an `E` toggle.**

The objection: hive would auto-execute a repo-controlled script on every
worktree creation, including for a repo cloned minutes ago and never read.
(Steve wrote "~40 directories under `~/repos`"; it is actually 121, which
strengthens the point.) He noted this differs in kind from `gate.sh`, which an
agent reads before running and which fires because someone asked for tests.

Steve sanctioned three mitigations — a per-repo opt-in, an allowlist, or relying
on `TmuxSendKeys` visibility. This plan takes the **per-repo opt-in**, which is
also the allowlist, and keeps visibility as a second layer:

- **Visibility alone is not a mitigation.** By the time the line is visible it
  has already been typed and executed. The human watches it run; they do not get
  to decline. Good for diagnosis, worthless for prevention.
- **A confirmation modal cannot work.** `ChordNextWorker` (`model.go:1442`)
  creates worktrees with no modal, so a prompt would either block `ctrl+space g`
  or be bypassed there — and a mitigation the automated path skips is not one.
- **It preserves Q1's intent.** The *recipe* still lives in the repo,
  version-controlled beside the code it describes. Config carries one bit —
  trust — not the recipe. The config surface Steve wanted gone stays gone.
- **The flag must be reachable from the UI or it does not exist.** See §2 and
  Critic finding 3: 106 workspace entries, zero YAML-only keys.
- **Trust is keyed to the checkout the human reviewed**, which is why the
  parent's script runs and the worktree's is ignored. Without that, the flag
  would grant execution to arbitrary fetched branches and would be theatre.

The consequence, stated plainly: a repo that adds a `wt-init.sh` gets nothing
until the flag is on. The notice exists so that this is discovered in the pane
in seconds.

## Open questions

**1. Should this ticket also make the reported he-events symptom go away?**

The change ships inert, so `he-events` — the repo that produced the incident —
still creates bare worktrees when this ticket closes. Making the symptom go away
needs two things **outside this repo**:

- `he-events/scripts/wt-init.sh` — a new file in a different repo, needing its
  own commit and review.
- The `Worktree init` flag on for `he-events` — one `E` keystroke, or a line in
  `~/.config/hive/config.yaml`, which is outside any repo and uncommittable.

Options: (a) hive-repo change only; Steve adopts he-events himself afterwards.
(b) The builder also writes `he-events/scripts/wt-init.sh`, committed in that
repo, leaving the flag to Steve. (c) Both, including editing the real config.

I would pick **(a)**. A builder editing files outside the repo it is building in
is unreviewable and cannot be captured in the commit, and the recipe is Steve's
call — `npm ci` versus `npm install` depends on whether the lockfile is
authoritative, and whether a worktree you are actively editing wants
`npm run build` or `npm run dev` is not obvious from outside. The README example
in §6 is a copy-pasteable starting point, which is most of (b)'s value without
the unreviewable edit. But the ticket then closes with the reported symptom
present, which belongs in the commit message rather than being discovered later.
If you prefer (b), file a second ticket against he-events rather than widening
the blast radius here.

**2. `;` or `&&` between the wt-init script and the claude launch?**

The contract produces `bash …/wt-init.sh … ; claude …`, so claude starts even if
the script fails.

Options: (a) `;` — claude always starts, the failure visible in the scrollback
above it. (b) `&&` — claude only starts on a clean bootstrap. (c) a config knob.

I would pick **(a)**, as written: a worktree with a failed bootstrap is exactly
when you most want an agent in the pane that can read the error and fix it, and
under (b) the `ChordNextWorker` path would produce a worker session that never
starts while `m.items` still shows `statusClaude` (`worktree.go:254`, set
unconditionally the moment `TmuxSendKeys` returns). (c) is a knob nobody sets.

**Correction to the draft's reasoning, which a critic caught:** the draft
justified (a) partly by claiming "claude always starts" under `;`. That is
false. Ctrl-C during a long `composer install` makes interactive bash abandon
the rest of the list, so claude does not start — under either separator. Since
Ctrl-C on a slow install is the most likely human action, the two options do not
actually differ on that axis, and the stale-`statusClaude` symptom is reachable
either way. (a) is still right, but on the "an agent should be there to read the
error" argument alone. If you want the stale status fixed, that is its own
ticket.

Note the example script uses `set -euo pipefail`, so the *script* stops at its
first failing command; (a) only governs whether claude starts afterwards.

---

## Decisions — round two

Settled by Steve, 2026-08-26, answering the two open questions above. These
close the plan-review gate; the ticket is now `ready`.

**Q2 → (a), `;`.** As already written in the contract. No change needed.

**Q1 → (b): also write `he-events/scripts/wt-init.sh`, committed in that repo,
leaving the config flag to Steve.**

This overrides the plan's recommendation of (a). The plan suggested that if (b)
were chosen it should become a second ticket; Steve chose (b) as stated in the
option instead, so the deliverable stands.

**A structural constraint on executing it, which the builder cannot resolve
itself.** A `builder` works in one worktree of one repo: `builder.md` §5 commits
on *this worktree's branch*, and the harness's worktree isolation refuses git
operations aimed outside it. So (b) cannot be one build. It is:

1. **This build** — the hive change, exactly as the contract below specifies.
   Unchanged by either answer.
2. **A separate commit in `he-events`** — `scripts/wt-init.sh`, derived from the
   README example in §6, tuned to that repo (`composer install`, `npm ci`,
   `npm run build`, copy `.env`). Not this builder's work.

Do not attempt (2) from inside this build, and do not treat its absence as an
incomplete job. Item 1 is the whole of this ticket.

**What the commit message must say.** The reported he-events symptom is still
present when this lands, because the config flag is off and only Steve can turn
it on. Say so in the commit rather than leaving it to be discovered.

---

## Review findings

Recorded at build time by two reviewers run in parallel against the finished
diff: `review-router` (which routed to `go-reviewer`) on the code, and
`plan-critic` in conformance mode on the diff-versus-contract question. The
Confirmed/Suspected split is preserved exactly as the reviewers reported it.
**None of these were fixed** — triage is a human decision, and a finding
silently fixed is one nobody ever evaluated.

### Red verification

The TDD red was verified by the builder, not merely asserted. The production
code was reconstructed in a scratch copy at its stub state (HEAD `config.go`,
`edit.go`, `view.go`, `worktree.go`, plus untagged `WorktreeInit` fields and a
`worktreeLaunchLine` returning `""`) and the new tests run against it. All eight
test functions failed on **assertions**, not compile or import errors, and each
failure matched the contract's specified behaviour — for example
`worktree_init_test.go:85: launch line mismatch: got "" want "bash '/home/steve/repos/he-events/scripts/wt-init.sh' '/home/steve/repos/he-events' 'split-1' ; env -u X claude"`
and `config_test.go:184: set on b only: mergeWorkspace(...).WorktreeInit = false, want true`.

### Conformance (plan-critic) — Confirmed

**MATCHES CONTRACT.** All of sections 1-7 executed faithfully; every explicit
prohibition in section 4 held (`grep -n WorktreeInit worktree.go` returns
nothing, so no dead state on the `wtRepo` literal; no new `m.err` write; no new
return; no tmux seam; no import change). The notice text is byte-for-byte the
plan's. The height/bounds question section 2 asked the builder to check was
resolved correctly: `viewEdit` (`view.go:576-626`) builds a plain string with no
`SetSize` and no `m.height` arithmetic, so the panel is not fixed-height and
nine fields fit unchanged. Nothing in the diff strays outside the hive repo.

- **The plan's own Verification line is wrong, not the code.**
  `grep -n 'WorktreeInit' config.go edit.go | wc -l` expects `6`, but the tree
  produces `8`. Section 2 mandates four `WorktreeInit`-bearing lines in
  `edit.go` (the const, the toggles slice, and the two `saveEditPanel` writes),
  not the two its parenthetical claims. `8` is the value that proves section 2
  was executed; `6` would have meant two of the five edits were skipped.
  **Action: fix the plan line, not the code.**

### Conformance (plan-critic) — Suspected

- Trivial over-build in `README.md`: one sentence the section 6 bullet list does
  not ask for — "It also means an old branch that predates the script still gets
  bootstrapped." It restates a benefit the plan's own "Two properties" paragraph
  claims, so it is in spirit.
- `model.go:140`'s stale comment `editToggles []bool // remote, favourite, collection`
  already omitted `yolo` before this change and still omits `worktree init`. The
  plan did not list it; leaving it alone was the conformant choice.

### Code review (go-reviewer) — verified as holding

Recorded because these are the properties the design rests on:

- **Shell quoting is complete.** Every interpolated value at
  `worktree_init.go:69` and `:73` goes through `shellQuote` — the joined script
  path, `ParentPath`, `Branch`, and the whole notice string including its
  deliberate `;`. No unquoted interpolation found.
- **The tmux `;` invariant holds today.** tmux treats `;` as a terminator only
  as the *last* character of an argv element, and there is no shell in the path
  (`tmuxRun` is `exec.Command("tmux", args...)`, `tmux.go:326`). All three
  shapes end with `ClaudeCmd`.
- **TOCTOU is not exploitable by the untrusted branch.** The stat and the
  execution both target `<parent>/scripts/wt-init.sh`, and `git worktree add`
  cannot write into the parent's `scripts/`.

### Code review (go-reviewer) — Confirmed

1. **`worktree_init.go:25` — `os.Stat` follows symlinks, so the executed content
   is not necessarily the parent's content.** The doc comment enumerates what
   reports false ("a directory, a dangling symlink, or an absent path") but omits
   the case that matters: a symlink to a *live* regular file resolves,
   `IsRegular()` is true, and `bash` follows it. A parent checkout containing
   `scripts/wt-init.sh` pointing into `.worktrees/<branch>/setup.sh` would route
   execution to worktree-controlled content. The reviewer's own severity note:
   this requires the parent checkout itself to be hostile, which already breaks
   the per-repo trust model, so it is an invariant-precision defect rather than a
   live exploit. Suggested fix: `os.Lstat`, rejecting symlinks outright, or
   `filepath.EvalSymlinks` with the result required to stay under `repoPath`.

2. **`edit.go:22` — the new "Worktree init" toggle is offered on worktree items,
   where it silently does nothing and persists a phantom grant.**
   `openEditPanel` rejects only `item.isTGSession`, so pressing `E` on a worktree
   row shows the checkbox; `saveEditPanel` then writes
   `workspaces["proj-wt-split-1"].worktree_init: true` to config (`edit.go:97`,
   `:109`). Nothing reads it — worktree `Repo`s are built at `model.go:170` and
   `worktree.go:247` without ever calling `applyWorkspaceConfig`, and
   `reloadItems` (`model.go:400`) rebuilds from `DiscoverRepos` +
   `DiscoverScratches` only. The user ticks a security-relevant box, sees it
   tick, and the next worktree still prints the "off for ..." notice.
   Fail-closed, so not dangerous, but misleading in exactly the place where
   clarity is the point. Suggested fix: skip the toggle (or the whole panel)
   when `repo.IsWorktree`.

3. **`worktree_init_test.go:145` — `TestWorktreeLaunchLineEndsWithClaude` claims
   to pin the tmux `;` invariant but asserts a proxy that cannot detect a
   violation of it.** The invariant is "the argv element must not end in an
   unescaped `;`"; the test asserts `strings.HasSuffix(got, c.w.ClaudeCmd)`. Set
   `ClaudeCmd: "claude ;"` and the produced line ends in `;` — tmux strips it,
   treats it as a command terminator, then tries to execute a command named
   `Enter`; the pane gets the text with no newline and claude never starts. The
   test passes. Suggested fix: add a `strings.HasSuffix(got, ";")` check to the
   same loop, plus a case whose `ClaudeCmd` ends in `;`.

4. **`edit.go:17` — `editToggleWorktreeInit` is declared and never referenced,
   and the toggle ordering is duplicated across three files with nothing linking
   them.** `editToggleRemote`/`Favourite`/`Collection` are equally unused. The
   real ordering lives in three independent literals: `edit.go:59`,
   `edit.go:93-97`, and `view.go:596`. A swapped pair compiles, passes the suite,
   and silently writes Favourite into Collection. Separately, nothing enforces
   `len(m.editToggles) == editFieldCount - editToggleYolo`; `handleEditKey`
   indexes `m.editToggles[m.editFocus-editToggleYolo]` (`edit.go:143`, `:174`).
   In bounds today, but bumping `editFieldCount` to 10 without updating
   `edit.go:59` gives a TUI panic on Space. Suggested fix: one table of
   `{label, get, set}` driving all three sites, or at minimum a test asserting
   the length relation.

5. **`worktree_init_test.go:12` — `TestHasWorktreeInitScript` does not test the
   symlink cases its own doc comment promises.** `worktree_init.go:14-15` states
   dangling symlinks report false; no test covers that, and none covers a
   symlink to a regular file (finding 1).

6. **`worktree_init_test.go:92` — the disabled path is the only shape not pinned
   by exact equality, despite being the security-relevant one.** It uses
   `Contains(got, "bash ")`, whose negative would not catch `sh '<path>'`,
   `/bin/bash '<path>'`, a tab after `bash`, or a `.` source invocation. Its two
   siblings both use exact equality. Suggested fix: assert the full expected
   string, as they do.

7. **`worktree.go:261` — the new item is marked `status: statusClaude` the
   instant the line is typed, which is now a lie for the duration of the
   bootstrap.** With the flag on and a `composer install`/`npm ci` script, the
   manager shows the worktree as running claude for minutes while the pane is
   still bootstrapping; a `/next` worker split (`model.go:1423`) looks ready to
   take work before claude exists. The contract's Open question 2 anticipated a
   stale-`statusClaude` symptom and said "that is its own ticket".

8. **`worktree.go:237` — the `TmuxSendKeys` error is discarded.** A pre-existing
   pattern (`scratch.go:119`, `session.go:245`), but the stakes rose: if
   send-keys fails, the worktree is on disk, the tmux session exists, the item is
   appended with `statusClaude`, and neither the bootstrap nor claude ever runs,
   with no error surfaced. Note this collides with the contract's section 4
   prohibition "nothing new can set `m.err`", so it is a genuine triage
   decision, not a clear-cut fix.

9. **`model.go:140-141` — comments describing `editToggles` and `editFocus` were
   already wrong and this change widens the gap.**
   `// remote, favourite, collection` is now five entries starting at yolo, and
   `// (0-2 = text, 3-5 = toggles)` is now 0-3 text, 4-8 toggles. These are the
   map a reader uses to audit exactly the index coupling in finding 4.

### Code review (go-reviewer) — Suspected

- **A. `config.go:84` — a relative `repos_dir` yields a relative script path
  typed into a pane whose cwd is the worktree.** `LoadConfig` expands `~/` but
  never calls `filepath.Abs`, so with `repos_dir: work/repos` the emitted line is
  `bash 'work/repos/proj/scripts/wt-init.sh' ...`, resolved against
  `<parent>/.worktrees/<branch>` — file not found, and the `$1` the script
  receives is wrong too. Suspected only because the reviewer could not confirm
  anyone configures a relative `repos_dir`. It bears directly on the contract's
  section 3 claim that an absolute path "sidesteps" the unsanitised-branch
  problem. Suggested fix: `filepath.Abs` after the `~/` expansion.
- **B. `worktree.go:206` — `Branch` reaches git in option position on the
  fallback path.** `git worktree add <dir> <branch>` puts unvalidated user text
  where git parses a leading `-` as a flag. Pre-existing, not introduced by this
  change, but it is the same unsanitised `Branch` the review asked about, on the
  path that exists specifically to check out a pre-existing (untrusted) branch.
  Suggested fix: `--` before the operands, plus `git check-ref-format --branch`.
- **C. `worktree.go:143` — `activeTabRepo` falls through to returning the
  worktree itself when the parent is absent from `m.items`, which would make
  `ParentPath` a worktree path.** Harmless *today* only because worktree `Repo`s
  always have `WorktreeInit == false` — finding 2's mechanism working in our
  favour. It becomes the exact worktree-content-execution hole the design
  forbids the moment someone copies `WorktreeInit` onto the `wtRepo` literal at
  `worktree.go:247` the way `Yolo` already is at `:256`: a natural-looking
  one-line change, which the contract forbade for a *different* reason (dead
  state). Suggested guard: `if parent.repo.IsWorktree { ScriptPresent = false }`,
  so the safety does not rest on an omission.
- **D. `worktree.go:425` — `shellQuote` does not neutralise newlines**, and tmux
  send-keys converts a raw newline into an Enter keypress. `Branch` cannot
  contain one (single-line `textinput`); a directory name theoretically can.
  Very low priority, not tested.

### Outstanding, not a diff defect

The **manual end-to-end check** in the Verification section is a human step and
was **not** performed — reported as outstanding, per the plan's own instruction
to report it rather than claim it. It needs: (1) a worktree of a repo with no
`scripts/wt-init.sh` (hive itself qualifies), confirming the pane shows the bare
claude line and nothing else; (2) a scratch repo with a `wt-init.sh`, confirming
the notice appears and the script does not run with the flag off, and that with
the flag on the script runs with `pwd` = the new worktree and `$1` = the parent.

---

## Decision provenance — corrected 2026-08-26

The section above headed "Decisions carried in from Steve — Settled 2026-08-26.
Do not re-ask." has **no verifiable source**. Checked: the todo-store note
entered via commit 96f4cbb, a bulk import of 253 tasks authored by Steve because
he ran the import; every bus message mentioning wt-init is agent-authored; the
section itself cites only technical reasoning and points nowhere. It should not
have been written as a settled decision and must not be cited as one.

**What Steve actually said, in conversation on 2026-08-26, verbatim:**

> a standard way to init a workspace ... whatever or however - i dont care

That is the whole of it. It expresses no preference between a repo-local script
and per-workspace config, and it attaches no reasoning. The shipped design — a
repo-local `scripts/wt-init.sh`, no-op when absent — satisfies it, so the build
stands on this instruction rather than on the unverified section above.

Nothing else in that section is confirmed. In particular the elaborate
justification for the opt-in flag and the parent-checkout trust rule remains
agent-authored reasoning. Both are defensible on their technical merits, which
are argued in the contract and in Critic findings, but neither should be
attributed to Steve.
