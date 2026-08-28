# Stamp repo on bus announcements

**Ticket:** jzq
**Repo:** hive (github.com/stevenlawton/hive)
**Base commit:** 87acdeb0ae82594a1d3e69fa0c1dbd19e4f251f2
**Written:** 2026-08-27

## Current behaviour

One global append-only log at `~/.config/hive/bus.jsonl` carries every session
on the machine, across every repo. All line anchors below were re-verified at
the base commit after the critic pass.

- `Announcement` (`bus/types.go:23-33`) has `ID`, `From`, `At`, `Kind`,
  `Headline`, `Body`, `Touches`, `ReplyTo`, `Auto`. **No repo field.**
- `Bus.Announce` (`bus/bus.go:45-62`) is the single write choke point. It runs
  `CheckAutoVerb`, `foldHeadline`, then unconditionally stamps `ID`, `From`
  (from `b.Self`), `At` and `Auto`, then appends.
- `bus.DetectSender()` (`bus/sender.go:16-37`) produces the sender id:
  `$HIVE_SENDER` → tmux session name with `hive-`/`rc-` stripped → `wt:` +
  basename of cwd → `wt:unknown`.
- `Store.load()` (`bus/store.go:62-75`) decodes JSONL with `encoding/json` and
  silently `continue`s past lines that fail to unmarshal. Unknown fields are
  ignored; missing fields zero-value. No schema version, no migration path.
- **Five** independent renderers of an announcement, sharing no formatter:
  - `digestLine` (`cmd_bus.go:274-281`), used by `hive bus list` and the
    injected inbox digest. The format at `cmd_bus.go:280` is
    `"  [%s] %s · %s%s · %s %s"` over `ID`, age, `From`, `AutoMarker()`, icon,
    `ShortHeadline()`.
  - `callList` (`bus/mcp.go:339-363`), which hand-duplicates that format at
    `bus/mcp.go:354` with a clock-time age instead of a relative one.
  - `printFull` (`cmd_bus.go:355-371`), `from:` line at `cmd_bus.go:360`.
  - `callRead` (`bus/mcp.go:366-392`), `from:` line at `bus/mcp.go:380`.
  - **The TUI bus pane**, `view.go:930`:
    `from := busFromStyle.Render(msg.From + msg.AutoMarker())`.
- The "inbox" is not a TUI pane. It is `hive bus inbox` (`cmd_bus.go:160-217`),
  run by the SessionStart / UserPromptSubmit / PostToolUse hooks installed at
  `bus/install.go:210,249,259`. Note there are **two** PostToolUse hooks —
  `bus inbox --posttooluse` on all tools, and `bus todo-hook` on TodoWrite —
  and `cmd_bus_todo.go:58` also goes through `openBus()`.
  `busInboxCmd` filters out the reader's own messages by `msg.From == b.Self`
  and hands the rest to `buildInboxDigest` (`cmd_bus.go:294-326`), which keeps
  at most `maxDigestMessages = 30` (or `firstContactTail = 10` on first
  contact), renders them in one chronological block, and appends a fixed
  trailer whose first line (`cmd_bus.go:321`) reads:

  ```
  The bus is machine-wide. Judge by sender: same-repo worktrees are coordination — act and reply.
  ```
- The guidance every Claude reads is generated at `bus/install.go:100-102` and
  says outright that the sender id "names the worktree, and **by convention**
  the repo it belongs to". `bus/install.go:118-120` tells senders to name their
  repo in prose if the sender id does not make it obvious.
- Announce call sites: `cmd_bus.go:125` (all CLI lifecycle verbs and `reply`),
  `cmd_bus_todo.go:108,116` (the TodoWrite hook), `bus/mcp.go:306,328` (the MCP
  tools), `model.go:2049` (the TUI compose box). `bus/parse.go:22` constructs an
  `Announcement` too, but that is deserialisation, not a send.
- Bus construction, exhaustively: `cmd_bus.go:91` `bus.Open(bus.DetectSender())`,
  `bus/mcp.go:20` the same inside `ServeMCP()`, and `model.go:196`
  `bus.Open("steve")` for the TUI. `bus_runtime.go` constructs no `Bus`.
- `bus/responder.go:59-64` spawns `claude -p` with `cmd.Dir = opts.Peer.Path`
  and `cmd.Env = append(cmd.Environ(), "HIVE_SENDER="+opts.Peer.Name,
  AutoResponderEnv+"=1")`. That subprocess shells back out to `hive bus reply`,
  re-entering `cmd_bus.go:125` with the peer's worktree as cwd.
- `bus_runtime.go:96` suppresses self-response by comparing `peer.Name ==
  msg.From`, and `peerFromRepo` (`bus_runtime.go:161`) builds `"wt:" + name`
  with no repo. This is why the repo must be a **new field** and not folded
  into `From` — see Root cause.
- No `HIVE_REPO` env var is read anywhere; `grep -rn HIVE_REPO` returns nothing.
- `todo.go:84-95` already has `mainWorktree(repoPath string) string`, which
  parses `git worktree list --porcelain` for the first `worktree ` line. It is
  in package `main`.
- `web/` contains no bus code; there is no JS/TS mirror of `Announcement`.

## Root cause

Relevance on a machine-wide bus is currently inferred by the reader from the
`wt:<name>` sender prefix. That only works while worktree names happen to carry
the repo — `wt:split-3` and `wt:auth-fix` say nothing about which project they
belong to, and `DetectSender`'s fallbacks (`wt:` + cwd basename, `wt:unknown`)
make it worse. The information exists at send time and is thrown away, so every
reader downstream is guessing.

Two things the obvious implementations get wrong, both verified empirically at
the base commit:

**1. The ticket's proposed capture is wrong for this repo's own topology.**
`basename $(git rev-parse --show-toplevel)` must not be implemented as written.
From `/home/steve/repos/workspace/.worktrees/split-3`:

```
$ git rev-parse --show-toplevel
/home/steve/repos/workspace/.worktrees/split-3          → basename "split-3"
$ git worktree list --porcelain | head -1
worktree /home/steve/repos/workspace                     → basename "workspace"
```

`--show-toplevel` returns the *current worktree* root, so every worktree of one
repo would get a different repo name — which defeats the whole feature, since
same-repo worktrees are exactly the peers the field exists to group. I confirmed
in a scratch repo that `git worktree list --porcelain` lists the **main**
worktree first even when run from a linked worktree, and that it exits 0 with
the main worktree listed in a repo with no commits.

**2. The repo must not be folded into `From`.** Making `DetectSender` return
`wt:split-3@workspace` would be a smaller change — no schema field, no renderer
edits — and is wrong: `bus_runtime.go:96` suppresses self-response by comparing
`peer.Name == msg.From`, and `peerFromRepo` (`bus_runtime.go:161`) builds
`peer.Name` as `"wt:" + name` with no repo. A session posting as
`wt:foo@workspace` would fail that comparison and its own worktree's
auto-responder would fire `claude -p` at its own announcement. Identity and
display must stay separate fields.

## The contract

### 1. `bus/types.go` — add the field and one shared accessor

Add to the `Announcement` struct, after `ReplyTo` and before `Auto`:

```go
	Repo     string    `json:"repo,omitempty"`     // repo the sender was working in; "" on messages predating the stamp
```

Add, immediately after `AutoMarker()`:

```go
// Origin renders the sender together with the repo it announced from, which is
// what tells a reader whether a message is their coordination or someone
// else's. Messages written before the repo was stamped have none, and render
// exactly as they always did.
func (a Announcement) Origin() string {
	if a.Repo == "" {
		return a.From
	}
	return a.From + "@" + a.Repo
}
```

Update the doc comment on `Announcement` to mention `Repo`.

### 2. `bus/sender.go` — `DetectRepo`, and a shared `MainWorktree`

`todo.go:84-95`'s `mainWorktree` already parses `git worktree list --porcelain`
for this. Package `bus` cannot import package `main`, so rather than fork the
logic, **move it into `bus` and have `todo.go` delegate**. Add to
`bus/sender.go`:

```go
// MainWorktree returns the path of the repo's main worktree — the one that
// every linked worktree shares. `repoPath` may be any worktree of the repo;
// pass "" to use the process's working directory. Returns "" if `repoPath` is
// not inside a git repo.
//
// Deliberately not `git rev-parse --show-toplevel`, which returns the *current*
// worktree and would give every worktree of one repo a different identity.
func MainWorktree(repoPath string) string {
	args := []string{"worktree", "list", "--porcelain"}
	if repoPath != "" {
		args = append([]string{"-C", repoPath}, args...)
	}
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		if rest, ok := strings.CutPrefix(line, "worktree "); ok {
			return strings.TrimSpace(rest)
		}
	}
	return ""
}

// DetectRepo returns the repo name to stamp on outgoing announcements, or ""
// when the caller is not inside a git repo.
//
// Priority:
//  1. $HIVE_REPO env var — explicit override, always wins
//  2. basename of the repo's main worktree, with any ".git" suffix trimmed so
//     a bare repo does not announce itself as "foo.git"
//  3. "" — not a git repo; the message renders without a repo, as old ones do
func DetectRepo() string {
	if v := strings.TrimSpace(os.Getenv("HIVE_REPO")); v != "" {
		return v
	}
	main := MainWorktree("")
	if main == "" {
		return ""
	}
	return strings.TrimSuffix(filepath.Base(main), ".git")
}
```

`bus/sender.go` already imports `os`, `os/exec`, `path/filepath`, `strings`. No
import changes.

Then rewrite `todo.go:84-95` to delegate, preserving its existing contract of
returning `repoPath` (not `""`) when git fails:

```go
func mainWorktree(repoPath string) string {
	if main := bus.MainWorktree(repoPath); main != "" {
		return main
	}
	return repoPath
}
```

`todo.go` must import `"github.com/stevenlawton/hive/bus"`. If that leaves
`os/exec` or another import unused in `todo.go`, drop it — `go build` will say.

### 3. `bus/bus.go` — carry it on the Bus, stamp it in Announce

Add an exported field to `Bus`, directly after `Self`:

```go
	// Repo names the repo this participant is working in, stamped on every
	// outgoing message so readers can tell coordination from chatter. Empty
	// for participants that are not repo-scoped (the Hive UI itself).
	Repo string
```

In `Announce`, alongside the existing stamps, directly after `msg.From = b.Self`:

```go
	msg.Repo = b.Repo
```

Add, next to `Open`:

```go
// OpenSession opens the default store for a repo-scoped participant — a Claude
// session in a worktree. Unlike Open it detects and stamps the repo, so every
// message it sends carries its origin. The repo is resolved once, at open time.
func OpenSession(self string) (*Bus, error) {
	b, err := Open(self)
	if err != nil {
		return nil, err
	}
	b.Repo = DetectRepo()
	return b, nil
}
```

`Open` is left alone. `model.go:196` opens as `"steve"` from wherever the Hive
binary was launched; stamping that cwd would be arbitrary, and the human is
explicitly repo-agnostic (`bus/install.go:114`: "From Steve — always yours,
whatever repo it names"). Detection is *not* done inside `Announce`, because
that would fire a git subprocess on every write and make `Announce` untestable
without a real repo on disk.

### 4. Switch the repo-scoped entry points to `OpenSession`

- `cmd_bus.go:90-92`:

  ```go
  func openBus() (*bus.Bus, error) {
  	return bus.OpenSession(bus.DetectSender())
  }
  ```

  This covers every CLI verb, `hive bus inbox`, and `cmd_bus_todo.go:58`.
- `bus/mcp.go:20` — change `Open(DetectSender())` to
  `OpenSession(DetectSender())`. Note the consequence and accept it: `ServeMCP`
  resolves the repo **once**, from whatever cwd Claude Code launched the MCP
  server in. That is the session's cwd in normal use. If an MCP server is ever
  started outside a worktree, that session's MCP-posted messages will be
  unstamped while its CLI-posted ones are stamped. Add a one-line comment at the
  call site saying so.
- `model.go:196` — unchanged.

### 5. `bus/responder.go` — do not leak the parent's `HIVE_REPO`

At `bus/responder.go:61-64`, add a third entry so an inherited `HIVE_REPO` in
the Hive process's environment cannot override the peer's own detection:

```go
	cmd.Env = append(cmd.Environ(),
		"HIVE_SENDER="+opts.Peer.Name,
		"HIVE_REPO=", // the responder detects from cmd.Dir; never inherit ours
		AutoResponderEnv+"=1",
	)
```

I verified by running it that Go's `exec` env dedup keeps the **last**
occurrence, so this does neutralise an inherited value, and `DetectRepo`'s
`TrimSpace != ""` guard then treats it as unset and falls through to git in
`cmd.Dir`. No unit test is specified for this — it needs a real subprocess and
defends only against a stray shell export.

### 6. Rendering — one accessor, five call sites

- `cmd_bus.go:280` in `digestLine`: replace `msg.From` with `msg.Origin()`.
  Nothing else in the format string changes.
- `bus/mcp.go:354` in `callList`: replace `m.From` with `m.Origin()`.
- `view.go:930` in the TUI bus pane: replace `msg.From` with `msg.Origin()`, so
  the line becomes
  `from := busFromStyle.Render(msg.Origin() + msg.AutoMarker())`. This is the
  human's own view of a machine-wide bus and needs the label most.
- `cmd_bus.go:360` in `printFull`: after the `from:` line, add

  ```go
  	if msg.Repo != "" {
  		fmt.Printf("repo:     %s\n", msg.Repo)
  	}
  ```
- `bus/mcp.go:380` in `callRead`: after the `from:` line, add

  ```go
  	if msg.Repo != "" {
  		fmt.Fprintf(&sb, "repo:     %s\n", msg.Repo)
  	}
  ```

`bus/responder.go:107`'s `headline:` line is deliberately left alone: it is
prompt text about the one message the responder is answering, and the sender is
already named elsewhere in that prompt.

### 7. `cmd_bus.go` — protect same-repo messages from elision

**Rank the elision budget, not the reading order.** The digest stays a single
chronological block; what changes is *which* messages survive the 30-message
cap. `Origin()` already puts the discriminator on every line, so section
headings would only duplicate it — and grouping would break threading, because
`digestLine` renders a reply as `💬→msg_abc` (`cmd_bus.go:276-278`) and relies
on the parent being nearby in the same chronological run.

Change the signature:

```go
func buildInboxDigest(unseen []bus.Announcement, firstContact bool, selfRepo string) string
```

Add above it:

```go
// splitByRepo partitions messages into the reader's own repo and everyone
// else's, preserving chronological order within each group. A message with no
// repo — every message written before the stamp shipped — cannot be claimed as
// same-repo and lands in `theirs`; it is still shown, it just does not get
// priority when the cap bites.
func splitByRepo(msgs []bus.Announcement, selfRepo string) (mine, theirs []bus.Announcement) {
	for _, msg := range msgs {
		if msg.Repo != "" && msg.Repo == selfRepo {
			mine = append(mine, msg)
		} else {
			theirs = append(theirs, msg)
		}
	}
	return mine, theirs
}
```

Rewrite the body of `buildInboxDigest` to:

1. `limit := maxDigestMessages`; `if firstContact { limit = firstContactTail }`.
2. `total := len(unseen)`.
3. Select the survivors:
   - If `selfRepo == ""`, keep today's behaviour exactly:
     `shown := unseen`; `if total > limit { shown = unseen[total-limit:] }`.
   - Otherwise `mine, theirs := splitByRepo(unseen, selfRepo)`, then budget:
     - `if len(mine) > limit { mine = mine[len(mine)-limit:] }`
     - `remaining := limit - len(mine)` (never negative after the line above)
     - `if len(theirs) > remaining { theirs = theirs[len(theirs)-remaining:] }`
     - Re-emit in the original order rather than concatenating, so chronology
       and threading survive:

       ```go
       keep := make(map[string]bool, len(mine)+len(theirs))
       for _, m := range mine {
       	keep[m.ID] = true
       }
       for _, m := range theirs {
       	keep[m.ID] = true
       }
       shown := make([]bus.Announcement, 0, len(keep))
       for _, m := range unseen {
       	if keep[m.ID] {
       		shown = append(shown, m)
       	}
       }
       ```
4. `elided := total - len(shown)`.
5. Headers — the three `fmt.Fprintf` calls at `cmd_bus.go:307-312`. Keep the
   leading `"📬 %d new bus announcement(s) since your last check"` text exactly,
   because `cmd_bus_test.go:49,152` assert on it, but replace the "most recent"
   wording, which the budget makes untrue (the survivors are no longer simply
   the newest N):
   - first contact: `"📬 First check in from this worktree — showing %d of %d bus announcement(s):\n\n"`, args `len(shown), total`
   - elided: `"📬 %d new bus announcement(s) since your last check, showing %d:\n\n"`, args `total, len(shown)`
   - default: unchanged, `"📬 %d new bus announcement(s) since your last check:\n\n"`, arg `total`
6. Loop over `shown` (not `unseen`) emitting `digestLine`.
7. Elision note at `cmd_bus.go:321`: the withheld messages are no longer
   necessarily the older ones, so change

   ```
   \n%d older message(s) not shown — `hive bus list -n %d` if you need them.\n
   ```

   to

   ```
   \n%d message(s) not shown — `hive bus list -n %d` if you need them.\n
   ```

   args unchanged (`elided, total`). **This breaks
   `TestBuildInboxDigestGivesFirstContactAShortTail`
   (`cmd_bus_test.go:139`), which asserts `"390 older"`.** Update that assertion
   to `"390 message(s) not shown"`. That is the only intended change to an
   existing assertion.
8. Trailer first line (`cmd_bus.go:321` block) — replace the single line

   ```
   The bus is machine-wide. Judge by sender: same-repo worktrees are coordination — act and reply.
   ```

   with two lines:

   ```
   The bus is machine-wide. Senders show as wt:<worktree>@<repo>; your own repo is coordination — act and reply.
   A sender with no @repo predates the stamp — judge it by its name, and treat anything from steve as yours.
   ```

   The remaining trailer lines are unchanged.

Update the caller at `cmd_bus.go:209`:

```go
	digest := buildInboxDigest(unseen, !resolved, b.Repo)
```

### 8. `bus/install.go` — correct the guidance it generates

In the CLAUDE.md block, replace `bus/install.go:100-102`:

```
One bus carries every session on this machine, across every repo. The sender
id (`wt:<name>`) names the worktree, and by convention the repo it
belongs to. Read every message through that:
```

with

```
One bus carries every session on this machine, across every repo. Each sender
shows as `wt:<worktree>@<repo>` — the repo is stamped at send time, so it is
authoritative, not a naming convention. A sender with no `@repo` predates the
stamp; judge it by its name. Read every message through that:
```

Replace the paragraph at `bus/install.go:118-120` ("When you announce, the
audience is machine-wide. If your sender id doesn't make your repo obvious, say
which repo or area you're in, so peers can filter you the same way.") with:

```
When you announce, the audience is machine-wide, and your repo is stamped for
you — you no longer need to name it in prose.
```

In `bus/mcp.go:187` (`hive_bus_list`), replace "The bus is machine-wide, so the
sender id (wt:<name>) is what tells you which repo a message belongs to." with
"The bus is machine-wide; each sender shows as wt:<worktree>@<repo>, and a
sender with no @repo predates the stamp."

In `bus/mcp.go:200` (`hive_bus_read`), the sentence is different — it reads
"Check the sender's repo first — a message from another repo is worth reading
only for technical content that applies to your own stack." Replace only
"Check the sender's repo first" with "Check the `@repo` on the sender first".

### 9. `docs/bus.md` — keep the reference honest

- The struct listing at `docs/bus.md:92-102` is already missing `Auto`. Add both:

  ```go
    Repo     string    // main-worktree dir name; "" on messages predating the stamp
    Auto     bool      // posted by the bus auto-responder
  ```
- After the "Sender identity" section (`docs/bus.md:158-168`), add a "Repo
  identity" section: `DetectRepo`'s priority (`$HIVE_REPO`, else basename of the
  main worktree with `.git` trimmed, else ""), and state in these words that
  **the label is the checkout directory name of the main worktree, not the forge
  repo name** — chosen because it is offline, cheap, and needs no remote, unlike
  `repo_key.go`'s remote-URL identity. Note `$HIVE_REPO` exists precisely for
  when the directory name is not the name you want.
- `docs/bus.md:167` ("The Hive UI uses the hard-coded sender `steve`") — add
  that the UI stamps no repo, deliberately, so the asymmetry does not read as a
  bug.
- The "Why global broadcast and not topic/repo scoping?" rationale
  (`docs/bus.md:341`) stays. Add one sentence: the repo stamp is a *hint for the
  reader and a tiebreak when the digest is capped*, not a routing key — nothing
  is ever withheld from anyone because of it.

### 10. Tests

New file `bus/sender_test.go`:

```go
func TestDetectRepoPrefersTheEnvOverride(t *testing.T)
func TestDetectRepoNamesTheMainWorktreeNotTheLinkedOne(t *testing.T)
func TestDetectRepoIsEmptyOutsideAGitRepo(t *testing.T)
func TestDetectRepoTrimsABareRepoSuffix(t *testing.T)
```

`TestDetectRepoNamesTheMainWorktreeNotTheLinkedOne` is the load-bearing one and
must fail against a `--show-toplevel` implementation. Recipe, verified to run:

```go
if _, err := exec.LookPath("git"); err != nil {
	t.Skip("git not available")
}
t.Setenv("HIVE_REPO", "")
dir := t.TempDir()
run := func(args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}
proj := filepath.Join(dir, "myproj")
run("init", "-q", proj) // NOT `git -C proj init` — proj does not exist yet
run("-C", proj, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "--allow-empty", "-m", "init")
run("-C", proj, "worktree", "add", "-q", filepath.Join(dir, "wt-feature"), "-b", "feature")
t.Chdir(filepath.Join(dir, "wt-feature"))
if got := DetectRepo(); got != "myproj" {
	t.Errorf("DetectRepo() = %q, want %q", got, "myproj")
}
```

`t.Chdir` is available on go 1.25.6 (`go.mod`) and marks the test non-parallel.
`TestDetectRepoIsEmptyOutsideAGitRepo` uses `t.Chdir(t.TempDir())` and must also
`t.Setenv("HIVE_REPO", "")`; it is safe only if the temp dir is not inside a
repo, which `t.TempDir()` guarantees under `/tmp`.

New tests in `bus/headline_test.go` (it already has the `testBus`/`announce`
helpers at lines 9-24; `testBus` returns `New(store, "wt:test")` with no repo):

```go
func TestAnnounceStampsTheBusRepo(t *testing.T)            // set b.Repo = "workspace"; announce; want got.Repo == "workspace"
func TestAnnounceLeavesRepoEmptyWhenTheBusHasNone(t *testing.T)
func TestOriginFallsBackToTheSenderWhenRepoIsEmpty(t *testing.T)
func TestOriginJoinsSenderAndRepo(t *testing.T)            // want "wt:split-3@workspace"
```

New tests in `cmd_bus_test.go` (reuse `manyAnnouncements` at :102 and
`digestLineCount` at :115, which keys on the `"  [m"` prefix):

```go
func TestDigestLineShowsTheRepo(t *testing.T)                     // contains "wt:a@workspace"
func TestDigestLineIsUnchangedForAMessageWithNoRepo(t *testing.T)  // exact-equal to today's string
func TestInboxDigestKeepsSameRepoMessagesWhenEliding(t *testing.T) // 40 foreign + 5 same-repo, selfRepo set: all 5 same-repo headlines present, 30 lines total
func TestInboxDigestStaysChronological(t *testing.T)               // interleaved repos: line order matches input order
func TestInboxDigestIgnoresRankingWhenTheReadersRepoIsUnknown(t *testing.T) // selfRepo == "", byte-identical to the pre-change tail behaviour
func TestInboxDigestTreatsAnUnstampedMessageAsForeign(t *testing.T)
```

Existing calls to `buildInboxDigest` in `cmd_bus_test.go` (lines 47, 128, 147,
158 and any others `go build` flags) gain a third argument `""`, which preserves
their current expectations exactly — except the one assertion change named in
§7.7.

## Verification

```bash
cd /home/steve/repos/workspace   # or the build worktree
gofmt -l . && go vet ./... && go build ./... && go test ./...
```

Success: `gofmt -l` prints nothing, `go vet` and `go build` are silent, and
`go test ./...` reports `ok` for both `github.com/stevenlawton/hive` and
`github.com/stevenlawton/hive/bus` with no `FAIL`.

Red-first check for the load-bearing test — with `DetectRepo` temporarily
implemented as `filepath.Base(git rev-parse --show-toplevel)`,
`go test ./bus -run TestDetectRepoNamesTheMainWorktreeNotTheLinkedOne` must fail
with `DetectRepo() = "wt-feature", want "myproj"`.

Backward-compatibility check against the real log (read-only; `--peek` does not
advance the cursor):

```bash
go run . bus list -n 20
go run . bus inbox --peek
```

Success: every pre-existing message renders exactly as before — bare
`wt:<name>`, no `@`, no crash — and the digest's message count is unchanged.

End-to-end check that the stamp is captured and names the *main* worktree:

```bash
cd /home/steve/repos/workspace/.worktrees/split-3
HIVE_SENDER=wt:jzq-check go run . bus fyi "jzq smoke test"
tail -1 ~/.config/hive/bus.jsonl | grep -o '"repo":"[^"]*"'
go run . bus list -n 1
```

Success: the first prints `"repo":"workspace"` — the main worktree's basename,
**not** `"split-3"` — and the second shows `wt:jzq-check@workspace`.

Override check:

```bash
HIVE_SENDER=wt:jzq-check HIVE_REPO=hive go run . bus fyi "jzq override test"
tail -1 ~/.config/hive/bus.jsonl | grep -o '"repo":"[^"]*"'
```

Success: prints `"repo":"hive"`.

## Blast radius

- **Persisted data.** `bus.jsonl` gains one optional key. `Store.load`
  (`bus/store.go:62-75`) uses plain `encoding/json`, so new records read by an
  older binary drop `repo` silently, and old records read by the new binary get
  `Repo == ""`. Additive and optional in both directions; no migration, no
  version bump. Every render path guards on `Repo != ""`.
- **Reader-side transition window.** `bus.jsonl` is append-only and never
  rewritten, so on the day this ships *every* message already in a reader's
  backlog is unstamped, including from same-repo peers. Because §7 ranks only
  the elision budget and never hides or re-orders anything, the worst case is
  that an unstamped same-repo message loses a tiebreak when the 30-cap bites —
  it is still shown, in place. The trailer and the generated CLAUDE.md both say
  explicitly that a missing `@repo` means "predates the stamp, judge by name",
  so the reader is told rather than misled. This is the reason the two-section
  layout was rejected: it would have filed every legacy message, and every
  message from steve, under an "Other repos" heading that asserts they are not
  the reader's coordination.
- **`buildInboxDigest` signature change** breaks compilation of
  `cmd_bus_test.go` until the third argument is added — intentional, so no
  caller is silently missed.
- **Five renderers**, all listed in §6. `Origin()` unifies three of them
  (`digestLine`, `callList`, `view.go:930`); the other two gain an explicit
  `repo:` line. `bus/responder.go:107` is deliberately excluded.
- **`bus_runtime.go:96`** compares `peer.Name == msg.From` to suppress
  self-response. Untouched, and the reason `From` keeps its current shape.
- **`todo.go:84`'s `mainWorktree`** becomes a thin delegate to
  `bus.MainWorktree`. Its callers (`repo_key.go:23,29,63,70,142`) see no
  behaviour change: `bus.MainWorktree` returns `""` where the old code returned
  `repoPath`, and the delegate restores that. `repo_key_test.go`'s
  `TestTodoStorePathSharedAcrossWorktrees` is the regression guard.
- **Hot path cost.** Both PostToolUse hooks (`bus/install.go:249,259`) go
  through `openBus()`, so up to **two** extra `git worktree list --porcelain`
  subprocesses per tool call, on top of `DetectSender`'s existing tmux spawn.
  `git worktree list --porcelain` reads `.git/worktrees/` and does no object
  I/O, so it is cheap — but see Open question 2.
- **`repo_key.go`'s `repoIdentity`/`repoKey` is a different system** (durable,
  hashed, remote-URL-based identity for the backlog store). Deliberately not
  reused: the bus wants a short human-readable label in a digest line, not an
  8-hex key, and it must work offline with no remote. The two may disagree if a
  repo is renamed on disk; acceptable for a display hint.
- **The generated CLAUDE.md block** is rewritten by `hive bus hook --install`.
  Text changes reach every session on the machine only on the next install; the
  copy currently in `~/.claude/CLAUDE.md` stays stale until then. Cosmetic.
- **Seen cursor** (`cmd_bus.go:179-202`) keys on `bus.SeenKey()`, not on sender
  or repo. Untouched.
- **`web/`** has no bus code; nothing to update.

## Critic findings

Two `plan-critic` runs, one on correctness/executability and one on
scope/design. They converged.

**Taken:**

- *The two-section digest was over-built.* Both critics attacked §7. The scope
  critic showed that section headings duplicate what `Origin()` now puts on
  every line, that grouping would separate a reply (`💬→msg_abc`,
  `cmd_bus.go:276`) from its parent, and that filing every unstamped message and
  every message from steve under "Other repos" contradicts
  `bus/install.go:114`'s "From Steve — always yours". §7 was rewritten to rank
  the *elision budget* only and re-emit survivors in original order. This
  removed the section headers, the trailer's dependence on them, and three of
  the planned tests.
- *A fifth renderer was missed.* Both critics found `view.go:930`
  (`busFromStyle.Render(msg.From + msg.AutoMarker())`) — the TUI bus pane, the
  human's own view. The draft claimed four renderers and enumerated them
  confidently. Added to §6 and to the blast radius.
- *Copy made false by the change.* The correctness critic showed that keeping
  `"most recent %d"` (`cmd_bus.go:310`) and `"%d older message(s) not shown"`
  (`cmd_bus.go:321`) verbatim would make the digest lie about what it withheld,
  once the budget is repo-ranked. Both reworded in §7.5 and §7.7, with the one
  consequent change to an existing assertion called out explicitly.
- *`bus/mcp.go:200` was misquoted.* The draft claimed `:187` and `:200` share a
  sentence; they do not. §8 now quotes each separately.
- *The load-bearing test recipe would not run.* `git -C <dir>/myproj init` fails
  on a directory that does not exist yet. §10 now uses `git init -q <path>`, and
  the whole recipe was executed against git in this environment before being
  written down.
- *Duplicated git-topology parsing.* The draft forked `todo.go:84`'s
  `mainWorktree` into `bus/sender.go` on the grounds that package `bus` cannot
  import package `main`. The scope critic pointed out the fix is to move the
  helper the other way. §2 now exports `bus.MainWorktree` and makes `todo.go`
  delegate.
- *Bare repos would stamp `foo.git`.* Confirmed by the correctness critic.
  `DetectRepo` now trims the `.git` suffix.
- *Hot-path cost was undercounted 2×.* Two PostToolUse hooks go through
  `openBus()`, not one. Corrected in the blast radius.
- *Line anchors drifted 1–9 lines* in six places. All anchors in this document
  were re-verified against the base commit and corrected.
- *The strongest justification was missing.* Both critics independently noted
  that folding the repo into `From` is the tempting simpler option, and that
  `bus_runtime.go:96`'s `peer.Name == msg.From` self-response check is what
  makes it unsafe. Promoted into Root cause.
- *`ServeMCP` resolves the repo once* from its launch cwd. Now stated at the
  call site in §4.
- *`docs/bus.md`'s struct listing was already missing `Auto`.* Added in §9.
- *The label is `workspace`, not `hive`.* The scope critic was right that a
  reviewer will trip on a field called `Repo` holding a directory basename. §9
  now says so in those words, the plan's own examples were made consistent, and
  it is raised as Open question 1 rather than buried.

**Where a critic was wrong or overruled:**

- The correctness critic asked whether `git worktree list --porcelain` reliably
  lists the main worktree first, and how it behaves in a bare repo, a repo with
  no commits, and outside a repo. It then verified all four itself and confirmed
  the plan's assumption. Not a defect — recorded because it is the load-bearing
  assumption of the whole change.
- The correctness critic doubted §5's `"HIVE_REPO="` env-neutralisation trick.
  It verified it and confirmed the claim; I also ran it independently
  (parent `HIVE_REPO=inherited`, child observed `""`). No change.
- The correctness critic said the four open questions "must be converted to
  decisions before handover" because the builder cannot ask. Partly overruled.
  Two of the four (`@` as separator; branch as well as repo) were genuine
  judgement calls with an obvious answer and are now decisions written into the
  contract. The other two are real product choices with a user-visible
  consequence and stay as open questions — a plan that reaches review carrying
  a question the human answers in a minute is cheaper than a builder that
  guesses. Both carry a stated recommendation, so the plan is executable as
  written if nobody answers.
- The scope critic suggested excluding `From == "steve"` from the elision
  budget as an alternative to dropping the section headers. Not taken — dropping
  the headers dissolves the problem entirely, and a special case for one
  hard-coded sender id in the ranking logic would be worse than the disease.

**Decisions made in response, recorded so they are not re-litigated:**

- Separator is `@`, giving `wt:split-3@workspace`. `DetectSender`
  (`bus/sender.go:16-37`) never produces an `@` in any of its four branches, so
  the only way to make the line ambiguous is to set `$HIVE_SENDER` to something
  containing one. One character matters in a line already carrying an id, an
  age, an icon and a 160-char headline.
- Branch is **not** stamped, only repo. It would double the label's width for
  information a reader can get from `hive bus read`, and it is not what the
  ticket asks for.

## Open questions

1. **`@workspace` or `@hive`?** The label is the main worktree's directory
   basename. On this machine the hive repo is checked out at
   `~/repos/workspace`, so every message from it will read
   `wt:split-3@workspace` — not `@hive`, which is what the project is actually
   called. Options: (a) ship the directory basename and set
   `HIVE_REPO=hive` in the worktrees where the directory name is not the name
   you want; (b) prefer `repo_key.go:41`'s `normalizeRemote` on
   `git remote get-url origin` and take the last path segment
   (`stevenlawton/hive` → `hive`), falling back to the directory basename when
   there is no remote. **I would ship (a)**: it is offline, needs no network or
   remote, costs one cheap git call, and `$HIVE_REPO` already exists as the
   escape hatch — whereas (b) adds a second git subprocess to a per-tool-call
   hot path to fix a naming mismatch that is local to one repo. But this is the
   single most visible consequence of the ticket, it will appear on every line
   of every digest on this machine, and changing it later churns stored data
   that cannot be rewritten. Worth thirty seconds.

2. **Memoise `DetectRepo`?** It costs one `git worktree list --porcelain`
   subprocess per `openBus()`, and both PostToolUse hooks call it, so up to two
   per tool call of every session. `repo_key.go:62`'s comment shows this
   codebase already deemed exactly this class of cost worth a memo file.
   Options: (a) leave it — the command touches only `.git/worktrees/`, does no
   object I/O, and the hook already pays for a process spawn, a JSON parse and a
   file read; (b) memoise in `$XDG_RUNTIME_DIR/hive/` keyed on cwd, mirroring
   `repoKeyMemoPath` (`repo_key.go:78`). **I would ship (a)** and memoise only
   if the hook shows up as slow in practice, because a cwd-keyed memo introduces
   a staleness class (worktree moved, removed, or re-created) for a value that
   is already cheap to compute. Say if you would rather it were memoised from
   the start — it is a self-contained addition to §2 and does not disturb
   anything else in the contract.
