# Backlog States Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give hive tasks a pipeline state (`@plan-review` / `@ready` / `@triage`), the verbs to move between them, and a way to recover claims held by dead sessions.

**Architecture:** State is a new token on the trailing marker comment `parseTodoMarker` already parses, so old lines keep parsing and older hives keep reading new files. State is orthogonal to `Claim` (who holds it) and to the box character (open/done/deferred). All work is in the existing `todo.go` model layer plus CLI verbs in `cmd_todo.go`; the drawer gets a display change only.

**Tech Stack:** Go, standard library only. Tests are plain `testing` package, table-free, matching the existing `cmd_todo_*_test.go` style.

**Spec:** `docs/superpowers/specs/2026-08-21-backlog-pipeline-design.md`

## Global Constraints

- Go module: `hive`, package `main`, no new dependencies.
- Marker tokens must stay order-independent and forwards-tolerant: unrecognised tokens are ignored on read (`todo.go:182-197`), and that property must survive every change here.
- `docs/TODO.md` is a shared file every worktree reads. Any format change must leave lines written by the *previous* hive parseable, and lines written by the *new* hive readable by the previous one (unknown tokens ignored).
- Test helper `chdir(t, dir)` lives at `todo_store_test.go:169`; tests use `t.TempDir()` and drive the `runTodoX` functions by return code.
- State values are exactly: `""` (unrefined), `"plan-review"`, `"ready"`, `"triage"`. Nothing else is valid.
- Run the full suite with `go test ./...` before every commit.

---

### Task 1: `State` on `Todo`, round-tripping through the marker

**Files:**
- Modify: `todo.go:21-29` (struct), `todo.go:187-197` (`parseTodoMarker`), `todo.go:219-226` (`parseTodoLine`), `todo.go:310-322` (`marker`)
- Test: `todo_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces: `Todo.State string`; constants `StateUnrefined`, `StatePlanReview`, `StateReady`, `StateTriage`; `validTodoState(s string) bool`; `stateRank(s string) int`; changed signature `parseTodoMarker(inner string) (claim, id, state string, ok bool)`.

- [ ] **Step 1: Write the failing test**

Append to `todo_test.go`:

```go
func TestTodoStateRoundTrips(t *testing.T) {
	line := "- [~] **fix the parser** - it eats flags <!-- @split-1 id:lxg state:plan-review -->"
	got, ok := parseTodoLine(line)
	if !ok {
		t.Fatal("line did not parse")
	}
	if got.State != StatePlanReview {
		t.Errorf("state: got %q, want %q", got.State, StatePlanReview)
	}
	if got.Claim != "split-1" || got.ID != "lxg" {
		t.Errorf("claim/id lost: %q %q", got.Claim, got.ID)
	}

	out := formatTodos([]Todo{got})
	if !strings.Contains(out, "state:plan-review") {
		t.Errorf("state not written back:\n%s", out)
	}
	if !strings.Contains(out, "id:lxg") || !strings.Contains(out, "@split-1") {
		t.Errorf("claim/id not written back:\n%s", out)
	}
}

// A stateless task must not grow an empty token — every worktree diffs this file.
func TestTodoWithoutStateWritesNoStateToken(t *testing.T) {
	out := formatTodos([]Todo{{Subject: "plain", ID: "abc"}})
	if strings.Contains(out, "state:") {
		t.Errorf("unexpected state token:\n%s", out)
	}
}

// Forwards tolerance: a marker from a newer hive must still yield what we know.
func TestUnknownMarkerTokensAreIgnored(t *testing.T) {
	got, ok := parseTodoLine("- [ ] **x** <!-- id:abc state:ready future:42 -->")
	if !ok {
		t.Fatal("line did not parse")
	}
	if got.ID != "abc" || got.State != StateReady {
		t.Errorf("got id=%q state=%q", got.ID, got.State)
	}
}
```

Ensure `todo_test.go` imports `strings` and `testing`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./... -run 'TestTodoState|TestTodoWithout|TestUnknownMarker' 2>&1 | head -20`
Expected: FAIL to compile — `undefined: StatePlanReview`, `got.State undefined`.

- [ ] **Step 3: Write minimal implementation**

In `todo.go`, add to the `Todo` struct after `ID`:

```go
	State       string // pipeline stage: "" unrefined | plan-review | ready | triage
```

Add below the existing `const` block:

```go
// Pipeline states. A task's state is where the work has got to; it is
// orthogonal to Claim (who holds it) and to the box (open/done/deferred).
const (
	StateUnrefined  = ""
	StatePlanReview = "plan-review"
	StateReady      = "ready"
	StateTriage     = "triage"
)

func validTodoState(s string) bool {
	switch s {
	case StateUnrefined, StatePlanReview, StateReady, StateTriage:
		return true
	}
	return false
}

// stateRank orders the pipeline so a backwards move can be detected.
func stateRank(s string) int {
	switch s {
	case StatePlanReview:
		return 1
	case StateReady:
		return 2
	case StateTriage:
		return 3
	default:
		return 0
	}
}
```

Change `parseTodoMarker` to:

```go
func parseTodoMarker(inner string) (claim, id, state string, ok bool) {
	for _, tok := range strings.Fields(inner) {
		switch {
		case strings.HasPrefix(tok, "@"):
			claim, ok = tok[1:], true
		case strings.HasPrefix(tok, "id:"):
			id, ok = tok[3:], true
		case strings.HasPrefix(tok, "state:"):
			state, ok = tok[6:], true
		}
	}
	return claim, id, state, ok
}
```

Update its one caller in `parseTodoLine`:

```go
			if claim, id, state, ok := parseTodoMarker(rest[i+4 : i+j]); ok {
				t.Claim, t.ID, t.State = claim, id, state
				rest = strings.TrimSpace(rest[:i])
			}
```

In `marker()`, after the `id:` token and before the length check:

```go
	if t.State != "" && !t.Done {
		toks = append(toks, "state:"+t.State)
	}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./...`
Expected: PASS, whole suite.

- [ ] **Step 5: Commit**

```bash
git add todo.go todo_test.go
git commit -m "todo: carry a pipeline state on the task marker"
```

---

### Task 2: `hive todo state` verb

**Files:**
- Modify: `todo.go` (add `setTodoState`), `cmd_todo.go:19-46` (verb switch), `cmd_todo.go` (add `runTodoState`)
- Test: `cmd_todo_state_test.go` (create)

**Interfaces:**
- Consumes: `StatePlanReview`, `StateReady`, `StateTriage`, `validTodoState`, `stateRank` from Task 1.
- Produces: `setTodoState(todos []Todo, i int, state, note string) ([]Todo, bool)`; CLI `hive todo state <ref> <state|clear> [--note <text>]`; `runTodoState(args []string) int`.

- [ ] **Step 1: Write the failing test**

Create `cmd_todo_state_test.go`:

```go
package main

import (
	"strings"
	"testing"
)

func TestStateVerbMovesForward(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	if rc := runTodoAdd([]string{"fix the parser - it eats flags"}); rc != 0 {
		t.Fatalf("add returned %d", rc)
	}
	id := loadTodos(dir)[0].ID

	if rc := runTodoState([]string{id, "plan-review"}); rc != 0 {
		t.Fatalf("state returned %d", rc)
	}
	if got := loadTodos(dir)[0].State; got != StatePlanReview {
		t.Errorf("state: got %q, want %q", got, StatePlanReview)
	}
}

// Going backwards without saying why produces a ticket that gets replanned
// identically, so the note is required rather than encouraged.
func TestBackwardsMoveRequiresANote(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	runTodoAdd([]string{"fix the parser"})
	id := loadTodos(dir)[0].ID
	runTodoState([]string{id, "ready"})

	if rc := runTodoState([]string{id, "plan-review"}); rc == 0 {
		t.Fatal("backwards move without a note should fail")
	}
	if got := loadTodos(dir)[0].State; got != StateReady {
		t.Errorf("state changed despite the error: %q", got)
	}

	if rc := runTodoState([]string{id, "plan-review", "--note", "contract missed the retry path"}); rc != 0 {
		t.Fatalf("state with note returned %d", rc)
	}
	after := loadTodos(dir)[0]
	if after.State != StatePlanReview {
		t.Errorf("state: got %q", after.State)
	}
	if !strings.Contains(after.Description, "contract missed the retry path") {
		t.Errorf("note not recorded: %q", after.Description)
	}
}

func TestStateVerbRejectsUnknownState(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	runTodoAdd([]string{"fix the parser"})
	id := loadTodos(dir)[0].ID

	if rc := runTodoState([]string{id, "banana"}); rc == 0 {
		t.Fatal("unknown state should be refused")
	}
}

func TestStateVerbClears(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	runTodoAdd([]string{"fix the parser"})
	id := loadTodos(dir)[0].ID
	runTodoState([]string{id, "triage"})

	if rc := runTodoState([]string{id, "clear", "--note", "starting over"}); rc != 0 {
		t.Fatalf("clear returned %d", rc)
	}
	if got := loadTodos(dir)[0].State; got != StateUnrefined {
		t.Errorf("state: got %q, want empty", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./... -run TestState 2>&1 | head -20`
Expected: FAIL to compile — `undefined: runTodoState`.

- [ ] **Step 3: Write minimal implementation**

In `todo.go`, after `deferTodo`:

```go
// setTodoState moves task i to state. A note is appended to the description so
// a ticket sent backwards carries the reason with it — without one it is simply
// planned again the same way. Returns false for an out-of-range index or an
// unknown state.
func setTodoState(todos []Todo, i int, state, note string) ([]Todo, bool) {
	if i < 0 || i >= len(todos) || !validTodoState(state) {
		return todos, false
	}
	todos[i].State = state
	if note = strings.TrimSpace(note); note != "" {
		if todos[i].Description == "" {
			todos[i].Description = note
		} else {
			todos[i].Description += " ↩ " + note
		}
	}
	return todos, true
}
```

In `cmd_todo.go`, add to the switch after the `defer` case:

```go
	case "state":
		return runTodoState(args[1:])
```

and extend the usage line in the `default` case to include `state`.

Add the verb implementation near `runTodoDefer`:

```go
const todoStateUsage = `usage: hive todo state <ref> <state> [--note <text>]

States: plan-review | ready | triage | clear (back to unrefined).
Moving a task backwards requires --note explaining why.`

// runTodoState moves a task through the pipeline. Machine transitions are
// written by the worker that finished a stage; human ones come from the drawer
// or from here.
func runTodoState(args []string) int {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, todoStateUsage)
		return 1
	}
	ref, want := args[0], args[1]
	if want == "clear" {
		want = StateUnrefined
	}
	if !validTodoState(want) {
		fmt.Fprintf(os.Stderr, "error: unknown state %q\n\n%s\n", args[1], todoStateUsage)
		return 1
	}

	note := ""
	for i := 2; i < len(args); i++ {
		switch args[i] {
		case "-n", "--note":
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "error: %s needs a value\n", args[i])
				return 1
			}
			note = args[i+1]
			i++
		default:
			fmt.Fprintf(os.Stderr, "error: unexpected argument %q\n\n%s\n", args[i], todoStateUsage)
			return 1
		}
	}

	var refused string
	rc := mutateOne(todoCwd(), ref, func(ts []Todo, i int) ([]Todo, string) {
		if stateRank(want) < stateRank(ts[i].State) && strings.TrimSpace(note) == "" {
			refused = fmt.Sprintf("moving %s back to %q needs --note explaining why", ts[i].ID, want)
			return ts, ""
		}
		out, ok := setTodoState(ts, i, want, note)
		if !ok {
			refused = "could not set state"
			return ts, ""
		}
		label := want
		if label == StateUnrefined {
			label = "unrefined"
		}
		return out, fmt.Sprintf("%s → %s", out[i].ID, label)
	})
	if refused != "" {
		fmt.Fprintf(os.Stderr, "error: %s\n", refused)
		return 1
	}
	return rc
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add todo.go cmd_todo.go cmd_todo_state_test.go
git commit -m "todo: add the state verb for pipeline transitions"
```

---

### Task 3: Tasks awaiting a human are not pickable as "next"

**Files:**
- Modify: `todo.go:410-424` (`currentForClaim`)
- Test: `todo_test.go`

**Interfaces:**
- Consumes: `StateUnrefined`, `StateReady`, `Todo.State` from Task 1.
- Produces: `func (t Todo) pickable() bool`.

Without this, `/next` and the statusline offer a ticket that is sitting in a human review queue as the next thing to code, which is exactly wrong: `@plan-review` and `@triage` are waiting on a person, not on a worker.

- [ ] **Step 1: Write the failing test**

Append to `todo_test.go`:

```go
func TestCurrentSkipsTasksAwaitingAHuman(t *testing.T) {
	todos := []Todo{
		{Subject: "awaiting plan review", ID: "aaa", State: StatePlanReview},
		{Subject: "awaiting triage", ID: "bbb", State: StateTriage},
		{Subject: "ready to build", ID: "ccc", State: StateReady},
	}
	got := currentForClaim(todos, "")
	if got == nil {
		t.Fatal("expected the ready task, got nil")
	}
	if got.ID != "ccc" {
		t.Errorf("picked %q (%s), want ccc", got.ID, got.State)
	}
}

func TestCurrentStillPicksUnrefinedWork(t *testing.T) {
	todos := []Todo{{Subject: "fresh", ID: "ddd"}}
	got := currentForClaim(todos, "")
	if got == nil || got.ID != "ddd" {
		t.Fatalf("got %#v, want ddd", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./... -run TestCurrentSkips 2>&1 | head -20`
Expected: FAIL — picked "aaa", want ccc.

- [ ] **Step 3: Write minimal implementation**

In `todo.go`, add above `currentForClaim`:

```go
// pickable reports whether a task is available as the next piece of work. A
// task parked in a human queue (@plan-review, @triage) is waiting on a person,
// so offering it to a worker would jump the gate.
func (t Todo) pickable() bool {
	return !t.Done && !t.Deferred &&
		(t.State == StateUnrefined || t.State == StateReady)
}
```

Rewrite the two loops in `currentForClaim` to use it:

```go
func currentForClaim(todos []Todo, owner string) *Todo {
	if owner != "" {
		for i := range todos {
			if !todos[i].Done && !todos[i].Deferred && todos[i].Claim == owner {
				return &todos[i]
			}
		}
	}
	for i := range todos {
		if todos[i].pickable() && todos[i].Claim == "" {
			return &todos[i]
		}
	}
	return nil
}
```

Note the first loop is deliberately unchanged: a task *this worktree already holds* stays your current task whatever its state.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add todo.go todo_test.go
git commit -m "todo: keep review-queue tasks out of next"
```

---

### Task 4: Stamp claims with a time

**Files:**
- Modify: `todo.go` (struct, `parseTodoMarker`, `marker`, `claimTodo`, `releaseClaim`)
- Test: `todo_test.go`

**Interfaces:**
- Consumes: `parseTodoMarker` signature from Task 1.
- Produces: `Todo.Since string` (RFC3339, UTC); package var `nowFunc = time.Now`; changed signature `parseTodoMarker(inner string) (claim, id, state, since string, ok bool)`.

A claim carries no time today, so nothing can tell a live worker from one whose session died an hour ago. Task 5 needs that distinction.

- [ ] **Step 1: Write the failing test**

Append to `todo_test.go`:

```go
func TestClaimStampsAndReleaseClears(t *testing.T) {
	fixed := time.Date(2026, 8, 21, 9, 0, 0, 0, time.UTC)
	old := nowFunc
	nowFunc = func() time.Time { return fixed }
	defer func() { nowFunc = old }()

	todos := []Todo{{Subject: "x", ID: "aaa"}}
	todos, ok := claimTodo(todos, 0, "split-1")
	if !ok {
		t.Fatal("claim refused")
	}
	if todos[0].Since != "2026-08-21T09:00:00Z" {
		t.Errorf("since: got %q", todos[0].Since)
	}

	out := formatTodos(todos)
	if !strings.Contains(out, "since:2026-08-21T09:00:00Z") {
		t.Errorf("since not written:\n%s", out)
	}
	round := parseTodos(out)
	if len(round) != 1 {
		t.Fatalf("parsed %d tasks, want 1", len(round))
	}
	if round[0].Since != todos[0].Since {
		t.Errorf("since did not round-trip: %q", round[0].Since)
	}

	todos = releaseClaim(todos, "split-1")
	if todos[0].Since != "" {
		t.Errorf("since survived release: %q", todos[0].Since)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./... -run TestClaimStamps 2>&1 | head -20`
Expected: FAIL to compile — `undefined: nowFunc`, `todos[0].Since undefined`.

- [ ] **Step 3: Write minimal implementation**

In `todo.go`, add the struct field after `State`:

```go
	Since       string // RFC3339 UTC time the current claim was taken; "" if unclaimed
```

Add below the state constants:

```go
// nowFunc is the clock, swapped out in tests.
var nowFunc = time.Now
```

Extend `parseTodoMarker` with a `since:` case and widen its return:

```go
func parseTodoMarker(inner string) (claim, id, state, since string, ok bool) {
	for _, tok := range strings.Fields(inner) {
		switch {
		case strings.HasPrefix(tok, "@"):
			claim, ok = tok[1:], true
		case strings.HasPrefix(tok, "id:"):
			id, ok = tok[3:], true
		case strings.HasPrefix(tok, "state:"):
			state, ok = tok[6:], true
		case strings.HasPrefix(tok, "since:"):
			since, ok = tok[6:], true
		}
	}
	return claim, id, state, since, ok
}
```

Update the caller in `parseTodoLine`:

```go
			if claim, id, state, since, ok := parseTodoMarker(rest[i+4 : i+j]); ok {
				t.Claim, t.ID, t.State, t.Since = claim, id, state, since
				rest = strings.TrimSpace(rest[:i])
			}
```

In `marker()`, emit it alongside the claim (same condition — it describes the claim):

```go
	if t.Claim != "" && !t.Done && !t.Deferred {
		toks = append(toks, "@"+t.Claim)
		if t.Since != "" {
			toks = append(toks, "since:"+t.Since)
		}
	}
```

Replace the existing claim-token block with the above. In `claimTodo`, set `Since` wherever `Claim` is set and clear it wherever the claim is dropped:

```go
	if todos[i].Deferred {
		todos[i].Deferred = false
		todos[i].Claim = owner
		todos[i].Since = nowFunc().UTC().Format(time.RFC3339)
		return todos, true
	}
	switch todos[i].Claim {
	case owner:
		todos[i].Claim, todos[i].Since = "", ""
	case "":
		todos[i].Claim = owner
		todos[i].Since = nowFunc().UTC().Format(time.RFC3339)
	default:
		return todos, false // held by another worktree
	}
```

In `releaseClaim`, clear both:

```go
		if todos[i].Claim == owner {
			todos[i].Claim, todos[i].Since = "", ""
		}
```

In `toggleTodoDone`, the completing branch clears the claim — clear `Since` there too:

```go
	if todos[i].Done {
		todos[i].Claim, todos[i].Since = "", "" // completing releases any claim
	}
```

And in `deferTodo`'s parking branch:

```go
	if todos[i].Deferred {
		todos[i].Claim, todos[i].Since = "", ""
		todos[i].Done = false
	}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add todo.go todo_test.go
git commit -m "todo: record when a claim was taken"
```

---

### Task 5: `hive todo reap` — release claims nothing is working

**Files:**
- Modify: `todo.go` (add `reapClaims`), `cmd_todo.go` (verb switch, `runTodoReap`)
- Test: `cmd_todo_reap_test.go` (create)

**Interfaces:**
- Consumes: `Todo.Since`, `nowFunc` from Task 4.
- Produces: `reapClaims(todos []Todo, live map[string]bool, cutoff time.Time) ([]Todo, []string)`; `liveWorktreeBranches(repoPath string) map[string]bool`; CLI `hive todo reap [--older-than <dur>]`.

A claim outlives the session that took it. When the orchestrator dies mid-batch, its tickets stay locked and no other worktree can touch them. Reaping releases the claim only — the state marker stays, because the stage the work reached is still true.

- [ ] **Step 1: Write the failing test**

Create `cmd_todo_reap_test.go`:

```go
package main

import (
	"testing"
	"time"
)

func TestReapReleasesDeadAndStaleClaims(t *testing.T) {
	cutoff := time.Date(2026, 8, 21, 10, 0, 0, 0, time.UTC)
	todos := []Todo{
		{Subject: "live worker", ID: "aaa", Claim: "split-1",
			Since: "2026-08-21T09:59:00Z", State: StateReady},
		{Subject: "dead worktree", ID: "bbb", Claim: "split-9",
			Since: "2026-08-21T09:59:00Z", State: StatePlanReview},
		{Subject: "stale but live branch", ID: "ccc", Claim: "split-1",
			Since: "2026-08-21T06:00:00Z", State: StateReady},
		{Subject: "unclaimed", ID: "ddd"},
	}
	live := map[string]bool{"split-1": true}

	got, released := reapClaims(todos, live, cutoff)

	if got[0].Claim != "split-1" {
		t.Errorf("live claim was reaped: %#v", got[0])
	}
	if got[1].Claim != "" || got[2].Claim != "" {
		t.Errorf("dead/stale claims survived: %q %q", got[1].Claim, got[2].Claim)
	}
	if got[1].State != StatePlanReview {
		t.Errorf("reaping must not touch state: %q", got[1].State)
	}
	if got[1].Since != "" || got[2].Since != "" {
		t.Error("since should be cleared with the claim")
	}
	if len(released) != 2 {
		t.Errorf("released %d, want 2: %v", len(released), released)
	}
}

// An unparseable or absent timestamp must not be read as "infinitely old" —
// that would reap a claim taken by a hive too old to stamp one.
func TestReapKeepsClaimsWithNoTimestampWhenBranchIsLive(t *testing.T) {
	cutoff := time.Date(2026, 8, 21, 10, 0, 0, 0, time.UTC)
	todos := []Todo{{Subject: "x", ID: "aaa", Claim: "split-1"}}
	got, released := reapClaims(todos, map[string]bool{"split-1": true}, cutoff)
	if got[0].Claim == "" {
		t.Error("claim with no timestamp on a live branch was reaped")
	}
	if len(released) != 0 {
		t.Errorf("released %v", released)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./... -run TestReap 2>&1 | head -20`
Expected: FAIL to compile — `undefined: reapClaims`.

- [ ] **Step 3: Write minimal implementation**

In `todo.go`, after `releaseClaim`:

```go
// reapClaims releases claims that nothing is working on: those held by a branch
// with no live worktree, and those taken before cutoff. The state marker is left
// alone — the stage the work reached is still true, only the lock is stale.
//
// A claim with no parseable timestamp is kept while its branch is live: hive
// only started stamping claims recently, and treating "no stamp" as "ancient"
// would reap live work.
func reapClaims(todos []Todo, live map[string]bool, cutoff time.Time) ([]Todo, []string) {
	var released []string
	for i := range todos {
		if todos[i].Claim == "" {
			continue
		}
		stale := false
		if !live[todos[i].Claim] {
			stale = true
		} else if ts, err := time.Parse(time.RFC3339, todos[i].Since); err == nil && ts.Before(cutoff) {
			stale = true
		}
		if !stale {
			continue
		}
		released = append(released, todos[i].ID+" (was @"+todos[i].Claim+")")
		todos[i].Claim, todos[i].Since = "", ""
	}
	return todos, released
}

// liveWorktreeBranches lists the branches currently checked out across the
// repo's worktrees — the set of claim owners that could still be working.
func liveWorktreeBranches(repoPath string) map[string]bool {
	live := map[string]bool{}
	out, err := exec.Command("git", "-C", repoPath, "worktree", "list", "--porcelain").Output()
	if err != nil {
		return live
	}
	for _, line := range strings.Split(string(out), "\n") {
		if rest, ok := strings.CutPrefix(line, "branch refs/heads/"); ok {
			live[strings.TrimSpace(rest)] = true
		}
	}
	return live
}
```

In `cmd_todo.go`, add to the switch:

```go
	case "reap":
		return runTodoReap(args[1:])
```

and the implementation:

```go
const todoReapUsage = `usage: hive todo reap [--older-than <duration>]

Releases claims held by a branch with no live worktree, and claims older than
the cutoff (default 4h). States are left untouched.`

func runTodoReap(args []string) int {
	older := 4 * time.Hour
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--older-than":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "error: --older-than needs a value")
				return 1
			}
			d, err := time.ParseDuration(args[i+1])
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: bad duration %q\n", args[i+1])
				return 1
			}
			older = d
			i++
		default:
			fmt.Fprintf(os.Stderr, "error: unexpected argument %q\n\n%s\n", args[i], todoReapUsage)
			return 1
		}
	}

	cwd := todoCwd()
	live := liveWorktreeBranches(mainWorktree(cwd))
	cutoff := nowFunc().UTC().Add(-older)

	var released []string
	if _, err := withTodos(cwd, func(ts []Todo) []Todo {
		out, rel := reapClaims(ts, live, cutoff)
		released = rel
		return out
	}); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	if len(released) == 0 {
		fmt.Println("nothing to reap")
		return 0
	}
	for _, r := range released {
		fmt.Println("released " + r)
	}
	return 0
}
```

Add `"time"` to `cmd_todo.go`'s imports and `"os/exec"` is already imported in `todo.go`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add todo.go cmd_todo.go cmd_todo_reap_test.go
git commit -m "todo: reap claims held by dead workers"
```

---

### Task 6: Show state in the list and the drawer

**Files:**
- Modify: `cmd_todo.go:74-85` (`runTodoList`), `drawer.go:447-459` (`drawerRow`)
- Test: `cmd_todo_state_test.go`

**Interfaces:**
- Consumes: `Todo.State` from Task 1.
- Produces: no new exported surface — display only.

- [ ] **Step 1: Write the failing test**

Append to `cmd_todo_state_test.go`:

```go
func TestDrawerRowShowsState(t *testing.T) {
	row := drawerRow(Todo{Subject: "fix the parser", ID: "lxg", State: StatePlanReview},
		false, "split-1", 80)
	if !strings.Contains(row, "plan-review") {
		t.Errorf("state missing from row: %q", row)
	}
}

func TestDrawerRowOmitsStateWhenDone(t *testing.T) {
	row := drawerRow(Todo{Subject: "fix the parser", ID: "lxg", State: StateTriage, Done: true},
		false, "split-1", 80)
	if strings.Contains(row, "triage") {
		t.Errorf("done task should not show a state: %q", row)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./... -run TestDrawerRow 2>&1 | head -20`
Expected: FAIL — state missing from row.

- [ ] **Step 3: Write minimal implementation**

In `drawer.go`, inside `drawerRow`, extend the tag before `avail` is computed:

```go
	// Tag tasks another worktree holds so they read as taken.
	tag := ""
	if !t.Done && t.Claim != "" && t.Claim != myClaim {
		tag = " 🔒@" + t.Claim
	}
	if !t.Done && t.State != "" {
		tag = " ·" + t.State + tag
	}
```

In `cmd_todo.go`, inside `runTodoList`, after the description is appended and before the claim tag:

```go
		if !t.Done && t.State != "" {
			line += "  ·" + t.State
		}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd_todo.go drawer.go cmd_todo_state_test.go
git commit -m "todo: surface pipeline state in the list and drawer"
```

---

### Task 7: `build_concurrency` workspace config key

**Files:**
- Modify: `config.go:29-37` (`WorkspaceConfig`)
- Test: `config_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces: `WorkspaceConfig.BuildConcurrency int`; `func (w WorkspaceConfig) buildConcurrency() int` returning 3 when unset.

- [ ] **Step 1: Write the failing test**

Append to `config_test.go`:

```go
func TestBuildConcurrencyDefaultsToThree(t *testing.T) {
	if got := (WorkspaceConfig{}).buildConcurrency(); got != 3 {
		t.Errorf("default: got %d, want 3", got)
	}
	if got := (WorkspaceConfig{BuildConcurrency: 5}).buildConcurrency(); got != 5 {
		t.Errorf("configured: got %d, want 5", got)
	}
	if got := (WorkspaceConfig{BuildConcurrency: -1}).buildConcurrency(); got != 3 {
		t.Errorf("negative should fall back to the default, got %d", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./... -run TestBuildConcurrency 2>&1 | head -20`
Expected: FAIL to compile — `buildConcurrency undefined`.

- [ ] **Step 3: Write minimal implementation**

In `config.go`, add to `WorkspaceConfig`:

```go
	BuildConcurrency int    `yaml:"build_concurrency,omitempty"` // parallel /build workers; 0 = default
```

and below the struct:

```go
// buildConcurrency caps how many /build workers run at once. Concurrent builds
// each hold a worktree and a branch, and every one of them lands in the triage
// queue, so the default is deliberately small.
func (w WorkspaceConfig) buildConcurrency() int {
	if w.BuildConcurrency > 0 {
		return w.BuildConcurrency
	}
	return 3
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add config.go config_test.go
git commit -m "config: add build_concurrency per workspace"
```

---

## Manual verification

After Task 7, from a repo with a TODO list:

```bash
go build -o /tmp/hive . && sudo install /tmp/hive /usr/local/bin/hive   # or your usual install
hive todo add "pipeline smoke test - checking states render"
hive todo list                      # new task, no state tag
hive todo state <id> plan-review
hive todo list                      # shows ·plan-review
hive todo state <id> ready
hive todo state <id> plan-review    # refused: needs --note
hive todo state <id> plan-review --note "wrong root cause"
hive todo reap                      # "nothing to reap" on a clean tree
hive todo rm <id>
```

Confirm `git diff docs/TODO.md` shows only the expected line changes — no reflow of untouched tasks.
