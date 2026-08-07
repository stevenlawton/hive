# TODO Concurrency Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make hive's shared `TODO.md` safe for concurrent sessions by giving every task a stable id and routing every mutation through a single locked, atomic read-modify-write.

**Architecture:** Tasks gain a three-consonant `ID` persisted in the existing trailing HTML comment. A new `withTodos(repoPath, mutate)` helper takes an exclusive `flock` on an out-of-repo lock file, re-reads the list from disk, applies the caller's delta, and writes back via temp-file-plus-rename. Callers resolve which task they mean *inside* that closure, against the fresh list, so a peer's concurrent edit can neither be clobbered nor cause the wrong task to be hit.

**Tech Stack:** Go 1.25.6, stdlib only (`crypto/rand`, `crypto/sha256`, `syscall`, `os`), bubbletea v2 for the TUI.

**Spec:** `docs/superpowers/specs/2026-08-07-todo-concurrency-design.md`

## Global Constraints

- **No new module dependencies.** Everything needed is stdlib. Do not add to `go.mod`.
- **Go 1.25.6** (per `go.mod`).
- **Unix-only is accepted.** `syscall.Flock` does not exist on Windows. Do not add build tags or a Windows shim.
- **Id alphabet is exactly `bcdfghjklmnpqrstvwxyz`** — lowercase consonants, 21 characters, no digits and no vowels. The absence of digits is load-bearing: it is what makes an id unambiguously distinguishable from a positional argument.
- **`go test ./...` must be green at the end of every task.** Baseline is currently green.
- **Comment style:** this repo's `CLAUDE.md` forbids comments that narrate a change ("now uses…", "reverted…", "this fixes…"). Comment only genuinely non-obvious code. Match the existing density in `todo.go`, which is fairly heavily commented at the function level.
- **Content outside the `TASKS:BEGIN`/`TASKS:END` markers must never be altered.** `replaceBlock` already guarantees this; do not bypass it.

## File Structure

| File | Responsibility | Change |
|---|---|---|
| `todo.go` | Pure model: parse, format, mutate in memory | Modify — add `ID`, marker grammar, id generation, ref resolution |
| `todo_store.go` | **New.** Persistence: locking, fresh read, atomic write | Create |
| `todo_test.go` | Pure-function tests | Modify — add id cases |
| `todo_store_test.go` | **New.** Filesystem/concurrency tests | Create |
| `cmd_todo.go` | `hive todo` CLI verbs | Modify — route through `withTodos`, resolve by id |
| `drawer.go` | TUI drawer key handling | Modify — id-addressed deltas, cursor by id |
| `model.go:117` | Drawer state fields | Modify — `drawerEditIdx int` → `drawerEditID string` + `drawerAdding bool` |
| `drawer_test.go` | **New.** Drawer delta tests | Create |
| `docs/claude-commands/todo.md` | `/todo` skill doc | Modify — instruct ids |

`withTodos` goes in a new `todo_store.go` rather than into `todo.go` because `todo.go` is already 455 lines of pure model code with no I/O beyond `loadTodos`/`saveTodos`; locking and atomic replacement are a separate responsibility with a separate test style (`t.TempDir`, goroutines).

---

### Task 1: Id field and marker-comment grammar

Teach the parser and formatter to carry an `ID` in the trailing `<!-- ... -->` comment, alongside the existing claim.

**Files:**
- Modify: `todo.go:19-26` (struct), `todo.go:213-222` (parse), `todo.go:256-265` (format)
- Test: `todo_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces: `Todo.ID string`; `parseTodoMarker(inner string) (claim, id string, ok bool)`; `func (t Todo) marker() string`.

- [ ] **Step 1: Write the failing test**

Add to `todo_test.go`. Do **not** modify the existing `TestParseTodoLine` table — its rows are positional struct literals and adding a field would require rewriting all ten. A separate function keeps the legacy-shape coverage intact.

```go
func TestParseTodoLineMarker(t *testing.T) {
	cases := []struct {
		line    string
		claim   string
		id      string
		subject string
		desc    string
	}{
		{"- [ ] **subj** - desc <!-- id:kdx -->", "", "kdx", "subj", "desc"},
		{"- [~] **subj** - desc <!-- @split-1 id:kdx -->", "split-1", "kdx", "subj", "desc"},
		{"- [~] **subj** - desc <!-- id:kdx @split-1 -->", "split-1", "kdx", "subj", "desc"},
		{"- [~] **subj** - desc <!-- @split-1 -->", "split-1", "", "subj", "desc"},
		{"- [x] **subj** - desc <!-- id:kdx -->", "", "kdx", "subj", "desc"},
		{"- [ ] **subj** - desc <!-- @split-1 id:kdx future:9 -->", "split-1", "kdx", "subj", "desc"},
		{"- [ ] **subj** - desc <!-- just a note -->", "", "", "subj", "desc <!-- just a note -->"},
	}
	for _, c := range cases {
		got, ok := parseTodoLine(c.line)
		if !ok {
			t.Errorf("parseTodoLine(%q) failed to parse", c.line)
			continue
		}
		if got.Claim != c.claim || got.ID != c.id || got.Subject != c.subject || got.Description != c.desc {
			t.Errorf("parseTodoLine(%q) = claim=%q id=%q subj=%q desc=%q; want claim=%q id=%q subj=%q desc=%q",
				c.line, got.Claim, got.ID, got.Subject, got.Description, c.claim, c.id, c.subject, c.desc)
		}
	}
}

// A done or deferred task must keep its id — otherwise it cannot be addressed
// to reopen it — while its claim is dropped.
func TestFormatKeepsIDInEveryState(t *testing.T) {
	cases := []struct {
		todo Todo
		want string
	}{
		{Todo{Subject: "s", ID: "kdx"}, "- [ ] **s** <!-- id:kdx -->"},
		{Todo{Subject: "s", ID: "kdx", Claim: "wt"}, "- [~] **s** <!-- @wt id:kdx -->"},
		{Todo{Subject: "s", ID: "kdx", Done: true, Claim: "wt"}, "- [x] **s** <!-- id:kdx -->"},
		{Todo{Subject: "s", ID: "kdx", Deferred: true, Claim: "wt"}, "- [-] **s** <!-- id:kdx -->"},
		{Todo{Subject: "s"}, "- [ ] **s**"},
	}
	for _, c := range cases {
		out := formatTodos([]Todo{c.todo})
		if !strings.Contains(out, c.want) {
			t.Errorf("formatTodos(%+v) = %q; want it to contain %q", c.todo, out, c.want)
		}
	}
}

func TestMarkerRoundTrip(t *testing.T) {
	in := []Todo{
		{Section: "Alpha", Subject: "one", Description: "first", ID: "kdx"},
		{Section: "Alpha", Subject: "two", ID: "mfp", Claim: "split-1"},
		{Section: "Alpha", Subject: "three", ID: "qrz", Done: true},
	}
	got := parseTodos(formatTodos(in))
	if len(got) != len(in) {
		t.Fatalf("round trip produced %d todos, want %d", len(got), len(in))
	}
	for i := range in {
		if got[i].ID != in[i].ID || got[i].Claim != in[i].Claim || got[i].Done != in[i].Done {
			t.Errorf("todo %d round-tripped as %+v, want id=%q claim=%q done=%v",
				i, got[i], in[i].ID, in[i].Claim, in[i].Done)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./... -run 'Marker|FormatKeepsID' -v`
Expected: FAIL — compile error, `got.ID undefined (type Todo has no field or method ID)`.

- [ ] **Step 3: Add the `ID` field**

In `todo.go`, add to the `Todo` struct (after `Claim`):

```go
	Claim       string // branch/worktree that claimed it; "" = unclaimed
	ID          string // stable short id — addresses the task across peers
```

- [ ] **Step 4: Add the marker parser**

In `todo.go`, add above `parseTodoLine`:

```go
// parseTodoMarker reads the trailing "<!-- @owner id:xyz -->" marker. Tokens are
// order-independent and each is optional; unrecognised tokens are ignored so a
// marker written by a newer hive still parses here. ok is false when no token was
// recognised, in which case the caller leaves the comment in the text — a plain
// HTML comment in a description is not ours to eat.
func parseTodoMarker(inner string) (claim, id string, ok bool) {
	for _, tok := range strings.Fields(inner) {
		switch {
		case strings.HasPrefix(tok, "@"):
			claim, ok = tok[1:], true
		case strings.HasPrefix(tok, "id:"):
			id, ok = tok[3:], true
		}
	}
	return claim, id, ok
}
```

- [ ] **Step 5: Use it in `parseTodoLine`**

Replace `todo.go:213-222` (the block commented `// Trailing claim marker: <!-- @owner -->`) with:

```go
	// Trailing marker comment: <!-- @owner id:xyz -->
	if i := strings.LastIndex(rest, "<!--"); i >= 0 {
		if j := strings.Index(rest[i:], "-->"); j >= 0 {
			if claim, id, ok := parseTodoMarker(rest[i+4 : i+j]); ok {
				t.Claim, t.ID = claim, id
				rest = strings.TrimSpace(rest[:i])
			}
		}
	}
```

- [ ] **Step 6: Emit the marker on format**

In `todo.go`, add a method next to `boxChar`:

```go
// marker renders the trailing comment. The id is written in every state — a done
// task that lost its id could not be addressed to reopen it — while the claim is
// only meaningful while the task is live.
func (t Todo) marker() string {
	var toks []string
	if t.Claim != "" && !t.Done && !t.Deferred {
		toks = append(toks, "@"+t.Claim)
	}
	if t.ID != "" {
		toks = append(toks, "id:"+t.ID)
	}
	if len(toks) == 0 {
		return ""
	}
	return "<!-- " + strings.Join(toks, " ") + " -->"
}
```

Then replace the claim-writing lines inside `writeTodo` (`todo.go:261-263`) with:

```go
		if mk := t.marker(); mk != "" {
			b.WriteString(" " + mk)
		}
```

- [ ] **Step 7: Run the full suite**

Run: `go test ./...`
Expected: PASS, all three packages. The pre-existing `TestParseTodoLine` and `TestFormatRoundTrip` must still pass — they cover the no-id and legacy `<!-- @owner -->` shapes.

- [ ] **Step 8: Commit**

```bash
git add todo.go todo_test.go
git commit -m "feat(todo): carry a stable id in the task marker comment"
```

---

### Task 2: Id generation and backfill

**Files:**
- Modify: `todo.go` (new functions + `crypto/rand` import)
- Test: `todo_test.go`

**Interfaces:**
- Consumes: `Todo.ID` from Task 1.
- Produces: `newTodoID(taken map[string]bool) string`; `backfillIDs(todos []Todo) []Todo`.

- [ ] **Step 1: Write the failing test**

```go
func TestBackfillIDsStampsOnlyMissing(t *testing.T) {
	todos := []Todo{
		{Subject: "keeps its id", ID: "kdx"},
		{Subject: "needs one"},
		{Subject: "also needs one"},
	}
	got := backfillIDs(todos)

	if got[0].ID != "kdx" {
		t.Errorf("existing id was overwritten: %q", got[0].ID)
	}
	seen := map[string]bool{}
	for i, td := range got {
		if td.ID == "" {
			t.Fatalf("todo %d still has no id", i)
		}
		if seen[td.ID] {
			t.Errorf("duplicate id %q", td.ID)
		}
		seen[td.ID] = true
		if strings.ContainsAny(td.ID, "0123456789") {
			t.Errorf("id %q contains a digit — ids must never be confusable with a position", td.ID)
		}
	}
}

func TestBackfillIDsIsIdempotent(t *testing.T) {
	first := backfillIDs([]Todo{{Subject: "a"}, {Subject: "b"}})
	second := backfillIDs(append([]Todo{}, first...))
	for i := range first {
		if first[i].ID != second[i].ID {
			t.Errorf("todo %d renumbered on second pass: %q -> %q", i, first[i].ID, second[i].ID)
		}
	}
}

func TestNewTodoIDAvoidsCollisions(t *testing.T) {
	taken := map[string]bool{}
	for i := 0; i < 500; i++ {
		id := newTodoID(taken)
		if taken[id] {
			t.Fatalf("newTodoID returned a taken id %q", id)
		}
		taken[id] = true
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./... -run 'BackfillIDs|NewTodoID' -v`
Expected: FAIL — `undefined: backfillIDs`, `undefined: newTodoID`.

- [ ] **Step 3: Implement generation and backfill**

Add `"crypto/rand"` to `todo.go`'s import block, then:

```go
// idAlphabet is lowercase consonants. No digits, so an id can never be mistaken
// for a positional argument; no vowels, so an id never spells a word.
const idAlphabet = "bcdfghjklmnpqrstvwxyz"

// newTodoID returns a short id absent from taken, widening the id by a character
// if three proves crowded rather than looping forever.
func newTodoID(taken map[string]bool) string {
	for n := 3; ; n++ {
		for attempt := 0; attempt < 100; attempt++ {
			if id := randomID(n); !taken[id] {
				return id
			}
		}
	}
}

// randomID draws n characters from idAlphabet. The modulo bias across 21 symbols
// is irrelevant here — ids only need to not collide, not to be uniform.
func randomID(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b) // crypto/rand.Read panics rather than returning an error
	out := make([]byte, n)
	for i, v := range b {
		out[i] = idAlphabet[int(v)%len(idAlphabet)]
	}
	return string(out)
}

// backfillIDs stamps an id onto every task lacking one, leaving existing ids
// alone. Run on every write, so a hand-edited file heals itself.
func backfillIDs(todos []Todo) []Todo {
	taken := make(map[string]bool, len(todos))
	for _, t := range todos {
		if t.ID != "" {
			taken[t.ID] = true
		}
	}
	for i := range todos {
		if todos[i].ID == "" {
			todos[i].ID = newTodoID(taken)
			taken[todos[i].ID] = true
		}
	}
	return todos
}
```

- [ ] **Step 4: Run the full suite**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add todo.go todo_test.go
git commit -m "feat(todo): generate and backfill stable task ids"
```

---

### Task 3: Reference resolution

**Files:**
- Modify: `todo.go` (new functions + `strconv` import)
- Test: `todo_test.go`

**Interfaces:**
- Consumes: `Todo.ID` from Task 1.
- Produces: `indexByID(todos []Todo, id string) (int, bool)`; `resolveTodoRef(todos []Todo, arg string) (int, bool)`.

- [ ] **Step 1: Write the failing test**

```go
func TestResolveTodoRef(t *testing.T) {
	todos := []Todo{
		{Subject: "first", ID: "kdx"},
		{Subject: "second", ID: "mfp"},
		{Subject: "third", ID: "qrz"},
	}
	cases := []struct {
		arg  string
		want int
		ok   bool
	}{
		{"kdx", 0, true},
		{"KDX", 0, true},  // case-insensitive
		{"qrz", 2, true},
		{"1", 0, true},    // positional fallback
		{"3", 2, true},
		{"0", 0, false},   // positions are 1-based
		{"4", 0, false},   // out of range
		{"zzz", 0, false}, // unknown id
		{"", 0, false},
	}
	for _, c := range cases {
		got, ok := resolveTodoRef(todos, c.arg)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("resolveTodoRef(%q) = %d,%v; want %d,%v", c.arg, got, ok, c.want, c.ok)
		}
	}
}

func TestIndexByID(t *testing.T) {
	todos := []Todo{{Subject: "a", ID: "kdx"}, {Subject: "b"}}
	if i, ok := indexByID(todos, "kdx"); !ok || i != 0 {
		t.Errorf("indexByID(kdx) = %d,%v; want 0,true", i, ok)
	}
	if _, ok := indexByID(todos, ""); ok {
		t.Error("indexByID(\"\") should not match the id-less task")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./... -run 'ResolveTodoRef|IndexByID' -v`
Expected: FAIL — `undefined: resolveTodoRef`, `undefined: indexByID`.

- [ ] **Step 3: Implement**

Add `"strconv"` to `todo.go`'s imports, then:

```go
// indexByID finds a task by exact id, case-insensitively. An empty id never
// matches, so tasks not yet backfilled are not addressable this way.
func indexByID(todos []Todo, id string) (int, bool) {
	if id == "" {
		return 0, false
	}
	lower := strings.ToLower(id)
	for i := range todos {
		if todos[i].ID != "" && strings.ToLower(todos[i].ID) == lower {
			return i, true
		}
	}
	return 0, false
}

// resolveTodoRef maps a CLI argument to an index: an id first, then a 1-based
// position. Ids contain no digits, so the two forms cannot collide. Callers must
// resolve inside withTodos, against the on-disk list — a position read from an
// earlier `list` may point at a different task by now.
func resolveTodoRef(todos []Todo, arg string) (int, bool) {
	arg = strings.TrimSpace(arg)
	if i, ok := indexByID(todos, arg); ok {
		return i, true
	}
	if v, err := strconv.Atoi(arg); err == nil && v >= 1 && v <= len(todos) {
		return v - 1, true
	}
	return 0, false
}
```

- [ ] **Step 4: Run the full suite**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add todo.go todo_test.go
git commit -m "feat(todo): resolve tasks by id, falling back to position"
```

---

### Task 4: Locked, atomic read-modify-write

The core of the plan. After this task the race is fixable; the following two tasks actually fix it at the call sites.

**Files:**
- Create: `todo_store.go`
- Create: `todo_store_test.go`

**Interfaces:**
- Consumes: `backfillIDs` (Task 2); existing `todoFilePath`, `mainWorktree`, `parseTodos`, `extractBlock`, `replaceBlock`, `formatTodos` from `todo.go`.
- Produces: `withTodos(repoPath string, mutate func([]Todo) []Todo) ([]Todo, error)`; `todoLockPath(repoPath string) string`; `writeTodoFile(path, content string) error`; `lockTodos(repoPath string) (func(), error)`.

- [ ] **Step 1: Write the failing test**

Create `todo_store_test.go`:

```go
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// The regression test for the whole plan: N processes-worth of concurrent
// mutations must all survive. Each withTodos call opens the lock file freshly, so
// flock serialises these goroutines exactly as it would separate processes.
func TestWithTodosConcurrentWritersAllSurvive(t *testing.T) {
	dir := t.TempDir()
	const n = 8

	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if _, err := withTodos(dir, func(ts []Todo) []Todo {
				return addTodo(ts, "Tasks", fmt.Sprintf("task-%d", i), "")
			}); err != nil {
				t.Errorf("writer %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()

	got := loadTodos(dir)
	if len(got) != n {
		t.Fatalf("got %d tasks after %d concurrent adds, want %d", len(got), n, n)
	}
	seen := map[string]bool{}
	for _, td := range got {
		seen[td.Subject] = true
		if td.ID == "" {
			t.Errorf("task %q was written without an id", td.Subject)
		}
	}
	for i := 0; i < n; i++ {
		if !seen[fmt.Sprintf("task-%d", i)] {
			t.Errorf("task-%d was lost", i)
		}
	}
}

func TestWithTodosBackfillsBeforeAndAfterMutate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "docs", "TODO.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	// A legacy file: no ids anywhere.
	legacy := tasksBegin + "\n\n### Tasks\n\n- [ ] **existing** - no id here\n" + tasksEnd + "\n"
	if err := os.WriteFile(path, []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}

	var sawExistingID string
	out, err := withTodos(dir, func(ts []Todo) []Todo {
		sawExistingID = ts[0].ID // backfilled before mutate runs
		return addTodo(ts, "Tasks", "fresh", "")
	})
	if err != nil {
		t.Fatal(err)
	}
	if sawExistingID == "" {
		t.Error("mutate saw an id-less pre-existing task; backfill must run before mutate")
	}
	if out[len(out)-1].ID == "" {
		t.Error("the newly added task has no id; backfill must also run after mutate")
	}
}

func TestWithTodosPreservesSurroundingProse(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "docs", "TODO.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	content := "# Open work\n\nIntro prose.\n\n" + tasksBegin + "\n\n### Tasks\n\n- [ ] **one**\n" +
		tasksEnd + "\n\n## Recently completed\n\nHand-kept archive.\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := withTodos(dir, func(ts []Todo) []Todo {
		return addTodo(ts, "Tasks", "two", "")
	}); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Intro prose.", "## Recently completed", "Hand-kept archive."} {
		if !strings.Contains(string(data), want) {
			t.Errorf("content outside the markers was lost: %q missing", want)
		}
	}
}

func TestWriteTodoFileLeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "TODO.md")
	if err := writeTodoFile(path, "hello\n"); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("temp file %q was left behind", e.Name())
		}
	}
}

func TestTodoLockPathIsOutsideTheRepo(t *testing.T) {
	dir := t.TempDir()
	got := todoLockPath(dir)
	if strings.HasPrefix(got, dir) {
		t.Errorf("lock path %q is inside the repo; it would show as untracked and ride deploy rsyncs", got)
	}
	if !strings.HasSuffix(got, ".lock") {
		t.Errorf("lock path %q should end in .lock", got)
	}
}
```

Note on `t.Errorf` inside goroutines: that is permitted; `t.Fatalf` is not, which is why the concurrent test reports and lets `wg.Wait()` finish.

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./... -run 'WithTodos|WriteTodoFile|TodoLockPath' -v`
Expected: FAIL — `undefined: withTodos`, `undefined: writeTodoFile`, `undefined: todoLockPath`.

- [ ] **Step 3: Create `todo_store.go`**

```go
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// withTodos serialises a read-modify-write against a repo's backlog. mutate
// receives the list exactly as it exists on disk, read under an exclusive lock,
// so a caller holding a stale in-memory copy cannot clobber a peer. Ids are
// backfilled before mutate — so it can resolve by id — and again afterwards, so
// anything mutate created is addressable next time.
func withTodos(repoPath string, mutate func([]Todo) []Todo) ([]Todo, error) {
	unlock, lockErr := lockTodos(repoPath)
	defer unlock()
	if lockErr != nil {
		fmt.Fprintf(os.Stderr, "hive: todo lock unavailable (%v) — writing unserialised\n", lockErr)
	}

	path := todoFilePath(repoPath)
	existing := ""
	if data, err := os.ReadFile(path); err == nil {
		existing = string(data)
	}

	todos := backfillIDs(mutate(backfillIDs(parseTodos(extractBlock(existing)))))
	if err := writeTodoFile(path, replaceBlock(existing, formatTodos(todos))); err != nil {
		return todos, err
	}
	return todos, nil
}

// lockTodos takes an exclusive advisory lock for a repo's backlog. The returned
// release is always safe to call. A non-nil error means locking is unavailable
// (some network filesystems); callers proceed unserialised, which is no worse
// than the behaviour before locking existed.
func lockTodos(repoPath string) (func(), error) {
	noop := func() {}
	path := todoLockPath(repoPath)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return noop, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return noop, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return noop, err
	}
	return func() { f.Close() }, nil // closing the fd releases the lock
}

// todoLockPath is the lock for a repo's backlog, keyed by the resolved main
// worktree so every worktree of a repo contends on the same file. It lives
// outside the repo deliberately: a sidecar in docs/ would show up as untracked
// in `git status` and would ride along with deploy rsyncs.
func todoLockPath(repoPath string) string {
	sum := sha256.Sum256([]byte(mainWorktree(repoPath)))
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "hive", "todo-"+hex.EncodeToString(sum[:4])+".lock")
}

// writeTodoFile replaces path atomically. The temp file shares the target's
// directory because a rename is only atomic within one filesystem.
func writeTodoFile(path, content string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".TODO.md.*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Chmod(name, 0o644); err != nil {
		os.Remove(name)
		return err
	}
	return os.Rename(name, path)
}
```

- [ ] **Step 4: Run the new tests**

Run: `go test ./... -run 'WithTodos|WriteTodoFile|TodoLockPath' -v`
Expected: PASS, including `TestWithTodosConcurrentWritersAllSurvive`.

- [ ] **Step 5: Run the full suite**

Run: `go test ./...`
Expected: PASS. `saveTodos` still exists and is still used by `cmd_todo.go` and `drawer.go` — it is removed in Task 5.

- [ ] **Step 6: Commit**

```bash
git add todo_store.go todo_store_test.go
git commit -m "feat(todo): locked, atomic read-modify-write via withTodos"
```

---

### Task 5: Move the CLI onto `withTodos`

**Files:**
- Modify: `cmd_todo.go` (all mutating verbs, `runTodoList`, delete `todoIndex` and `saveAndReport`)
- Modify: `todo.go` — delete `saveTodos` once nothing references it
- Modify: `docs/claude-commands/todo.md`

**Interfaces:**
- Consumes: `withTodos` (Task 4), `resolveTodoRef` (Task 3), `backfillIDs` (Task 2).
- Produces: `mutateOne(cwd, ref string, apply func([]Todo, int) ([]Todo, string)) int`.

- [ ] **Step 1: Write the failing test**

Add to `todo_store_test.go`. These drive the CLI functions directly rather than shelling out, and set the working directory so `todoCwd()` resolves to the temp repo.

```go
// chdir points todoCwd() at dir for the duration of the test.
func chdir(t *testing.T, dir string) {
	t.Helper()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(prev) })
}

// The bug this whole plan exists to fix: a peer removes a task between the
// caller reading the list and acting on it. Addressing by id must still hit the
// task the caller meant.
func TestCLIDoneByIDSurvivesAPeerShiftingPositions(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	for _, s := range []string{"first", "second", "third"} {
		if rc := runTodoAdd([]string{s}); rc != 0 {
			t.Fatalf("add %q returned %d", s, rc)
		}
	}
	todos := loadTodos(dir)
	target := todos[2].ID // "third", currently position 3

	// A peer removes position 1; "third" is now at position 2.
	if _, err := withTodos(dir, func(ts []Todo) []Todo { return deleteTodo(ts, 0) }); err != nil {
		t.Fatal(err)
	}

	if rc := runTodoSetDone([]string{target}, true); rc != 0 {
		t.Fatalf("done %s returned %d", target, rc)
	}

	for _, td := range loadTodos(dir) {
		if td.ID == target && !td.Done {
			t.Error("the targeted task was not marked done")
		}
		if td.ID != target && td.Done {
			t.Errorf("the wrong task %q was marked done", td.Subject)
		}
	}
}

func TestCLIRejectsUnknownRef(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	if rc := runTodoAdd([]string{"only"}); rc != 0 {
		t.Fatalf("add returned %d", rc)
	}
	if rc := runTodoSetDone([]string{"zzz"}, true); rc == 0 {
		t.Error("done with an unknown id should have failed")
	}
	if rc := runTodoSetDone([]string{"99"}, true); rc == 0 {
		t.Error("done with an out-of-range position should have failed")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./... -run 'TestCLI' -v`
Expected: FAIL — `runTodoSetDone` currently rejects a non-numeric argument via `todoIndex`, so the id form errors out.

- [ ] **Step 3: Add the shared verb helper**

In `cmd_todo.go`, replace `todoIndex` (lines 199-211) and `saveAndReport` (lines 213-220) with:

```go
// mutateOne resolves ref and applies a change under the backlog lock. Resolution
// happens inside the closure, against the list as it is on disk: a position read
// from an earlier `list` may point at a different task by now. apply returns the
// message to print, or "" when it declined and reported the reason itself.
func mutateOne(cwd, ref string, apply func([]Todo, int) ([]Todo, string)) int {
	var msg string
	var missing bool
	_, err := withTodos(cwd, func(ts []Todo) []Todo {
		i, ok := resolveTodoRef(ts, ref)
		if !ok {
			missing = true
			return ts
		}
		out, m := apply(ts, i)
		msg = m
		return out
	})
	switch {
	case missing:
		fmt.Fprintf(os.Stderr, "error: no such task %q (see: hive todo list)\n", ref)
		return 1
	case err != nil:
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	case msg == "":
		return 1
	}
	fmt.Println(msg)
	return 0
}

// todoRef pulls the task reference from a verb's arguments.
func todoRef(args []string) (string, bool) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "error: need a task id or number (see: hive todo list)")
		return "", false
	}
	return args[0], true
}
```

- [ ] **Step 4: Rewrite the mutating verbs**

Replace `runTodoAdd`, `runTodoSetDone`, `runTodoCurrent`, `runTodoDefer`, `runTodoRm`, and `runTodoNormalize` (`cmd_todo.go:84-182`) with:

```go
func runTodoAdd(args []string) int {
	text := strings.TrimSpace(strings.Join(args, " "))
	if text == "" {
		fmt.Fprintln(os.Stderr, "usage: hive todo add <subject — optional description>")
		return 1
	}
	subj, desc := splitSubjectDesc(text)
	cwd := todoCwd()
	todos, err := withTodos(cwd, func(ts []Todo) []Todo {
		return addTodo(ts, defaultSection, subj, desc)
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	fmt.Printf("added %s: %s\n", todos[len(todos)-1].ID, subj)
	return 0
}

func runTodoSetDone(args []string, done bool) int {
	ref, ok := todoRef(args)
	if !ok {
		return 1
	}
	word := "done"
	if !done {
		word = "reopened"
	}
	return mutateOne(todoCwd(), ref, func(ts []Todo, i int) ([]Todo, string) {
		ts[i].Done = done
		if done {
			ts[i].Claim = ""
		}
		return ts, fmt.Sprintf("%s %s: %s", word, ts[i].ID, ts[i].Subject)
	})
}

// runTodoCurrent claims (or releases) a task for this worktree, so parallel
// worktrees don't all grab the same "next" item.
func runTodoCurrent(args []string) int {
	cwd := todoCwd()
	owner := worktreeClaim(cwd)
	if owner == "" {
		fmt.Fprintln(os.Stderr, "error: not in a git worktree — can't claim")
		return 1
	}
	if len(args) > 0 && (args[0] == "clear" || args[0] == "none") {
		if _, err := withTodos(cwd, func(ts []Todo) []Todo {
			return releaseClaim(ts, owner)
		}); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return 1
		}
		fmt.Println("released your claims")
		return 0
	}
	ref, ok := todoRef(args)
	if !ok {
		return 1
	}
	var held string
	rc := mutateOne(cwd, ref, func(ts []Todo, i int) ([]Todo, string) {
		out, changed := claimTodo(ts, i, owner)
		if !changed {
			held = ts[i].Claim
			return ts, ""
		}
		verb := "claimed"
		if out[i].Claim == "" {
			verb = "released"
		}
		return out, fmt.Sprintf("%s %s: %s", verb, out[i].ID, out[i].Subject)
	})
	if held != "" {
		fmt.Fprintf(os.Stderr, "error: claimed by %s\n", held)
	}
	return rc
}

// runTodoDefer toggles the parked state on a task.
func runTodoDefer(args []string) int {
	ref, ok := todoRef(args)
	if !ok {
		return 1
	}
	return mutateOne(todoCwd(), ref, func(ts []Todo, i int) ([]Todo, string) {
		ts = deferTodo(ts, i)
		state := "deferred"
		if !ts[i].Deferred {
			state = "un-deferred"
		}
		return ts, fmt.Sprintf("%s %s: %s", state, ts[i].ID, ts[i].Subject)
	})
}

func runTodoRm(args []string) int {
	ref, ok := todoRef(args)
	if !ok {
		return 1
	}
	return mutateOne(todoCwd(), ref, func(ts []Todo, i int) ([]Todo, string) {
		removed := ts[i].Subject
		return deleteTodo(ts, i), "removed: " + removed
	})
}

// runTodoNormalize re-reads and re-writes the block, cleaning up formatting drift
// and stamping ids onto any task still lacking one.
func runTodoNormalize() int {
	todos, err := withTodos(todoCwd(), func(ts []Todo) []Todo { return ts })
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	fmt.Printf("normalized %d tasks\n", len(todos))
	return 0
}
```

- [ ] **Step 5: Print ids in `list`**

In `runTodoList`, replace the line building `line` (`cmd_todo.go:68`) with:

```go
		handle := t.ID
		if handle == "" {
			handle = strconv.Itoa(i + 1) // not yet stamped; `hive todo normalize` fixes this
		}
		line := fmt.Sprintf("%-4s [%s] %s", handle, t.boxChar(), t.Subject)
```

Add `"strconv"` to `cmd_todo.go`'s imports.

- [ ] **Step 6: Delete `saveTodos`**

`drawer.go:154` is the last remaining caller. Change it to the stopgap below **first**, so the build stays clean:

```go
func (m *model) persistDrawer() {
	if _, err := withTodos(m.drawerRepo, func([]Todo) []Todo { return m.drawerTodos }); err != nil {
		m.err = err
	}
}
```

This is still a whole-array write and does **not** fix the drawer clobber — Task 6 replaces it properly. It exists only so this task compiles and commits independently.

Then delete `saveTodos` from `todo.go` (lines 97-110) and confirm:

Run: `grep -rn 'saveTodos' --include=*.go .`
Expected: no matches.

- [ ] **Step 7: Run the full suite**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 8: Update the `/todo` skill doc**

In `docs/claude-commands/todo.md`, change the command table's `<n>` to `<ref>`, and replace the Rules block (lines 43-47) with:

```markdown
Rules:
- Address tasks by the **id** shown in the left column of `hive todo list` (three
  letters, e.g. `kdx`). Ids are stable — a peer session adding or removing tasks
  never changes them. A positional number still works but is unsafe when other
  worktrees are active, because positions shift.
- Run `hive todo list` to discover ids; you do not need to re-run it before each
  command the way positional numbers required.
- Keep headlines to a short title, not a paragraph.
- If the request is empty, just run `hive todo list`.
- After any change, run `hive todo list` once and show the user the result.
```

Also update the `<n>` description near the end of the file to describe ids, and note that `~/.claude/commands/todo.md` is the live copy — that mirror must be updated by hand, since it lives outside this repo.

- [ ] **Step 9: Commit**

```bash
git add cmd_todo.go todo.go todo_store_test.go docs/claude-commands/todo.md
git commit -m "feat(todo): address CLI tasks by stable id under the backlog lock"
```

---

### Task 6: Move the drawer onto id-addressed deltas

**Files:**
- Modify: `model.go:117`
- Modify: `drawer.go:43-49, 88-95, 104-113, 152-156, 165-255`
- Create: `drawer_test.go`

**Interfaces:**
- Consumes: `withTodos` (Task 4), `indexByID` (Task 3).
- Produces: `func (m *model) applyDrawer(mutate func([]Todo) []Todo)`; `func (m model) cursorTodoID() string`; `func (m *model) restoreCursor(id string)`; `func (m *model) loadDrawerTodos(path string)`.

- [ ] **Step 1: Write the failing test**

Create `drawer_test.go`:

```go
package main

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// newDrawerModel builds a model with the drawer open on a temp repo seeded with
// the given subjects.
func newDrawerModel(t *testing.T, subjects ...string) (model, string) {
	t.Helper()
	dir := t.TempDir()
	for _, s := range subjects {
		if _, err := withTodos(dir, func(ts []Todo) []Todo {
			return addTodo(ts, "Tasks", s, "")
		}); err != nil {
			t.Fatal(err)
		}
	}
	m := model{drawerOpen: true, drawerRepo: dir, drawerClaim: "wt-test"}
	m.loadDrawerTodos(dir)
	return m, dir
}

func press(t *testing.T, m model, r rune) model {
	t.Helper()
	out, _ := m.handleDrawerKey(tea.KeyPressMsg{Code: r, Text: string(r)})
	mm, ok := out.(model)
	if !ok {
		t.Fatalf("handleDrawerKey returned %T, want model", out)
	}
	return mm
}

// The drawer's old whole-array write reverted anything a peer did while its copy
// was in memory. A delta must leave the peer's task alone.
func TestDrawerToggleDoesNotClobberAPeersConcurrentAdd(t *testing.T) {
	m, dir := newDrawerModel(t, "alpha", "beta")
	m.drawerCursor = 0
	targetID := m.drawerTodos[0].ID

	// A peer adds a task while the drawer holds its stale copy.
	if _, err := withTodos(dir, func(ts []Todo) []Todo {
		return addTodo(ts, "Tasks", "peer-task", "")
	}); err != nil {
		t.Fatal(err)
	}

	m = press(t, m, 'x') // toggle done on the cursor

	got := loadTodos(dir)
	if len(got) != 3 {
		t.Fatalf("got %d tasks, want 3 — the peer's add was clobbered", len(got))
	}
	var found bool
	for _, td := range got {
		if td.ID == targetID {
			found = true
			if !td.Done {
				t.Error("the cursor's task was not marked done")
			}
		}
		if td.Subject == "peer-task" && td.Done {
			t.Error("the peer's task was wrongly marked done")
		}
	}
	if !found {
		t.Error("the cursor's task vanished")
	}
}

// A peer inserting above the cursor must not slide the cursor onto a different
// task.
func TestDrawerCursorFollowsItsTaskByID(t *testing.T) {
	m, dir := newDrawerModel(t, "alpha", "beta", "gamma")
	m.drawerCursor = 2
	wantID := m.drawerTodos[2].ID

	if _, err := withTodos(dir, func(ts []Todo) []Todo {
		return append([]Todo{{Section: "Tasks", Subject: "inserted"}}, ts...)
	}); err != nil {
		t.Fatal(err)
	}

	m = press(t, m, 'x')

	if m.drawerTodos[m.drawerCursor].ID != wantID {
		t.Errorf("cursor landed on %q, want the task it was on (%q)",
			m.drawerTodos[m.drawerCursor].ID, wantID)
	}
}

func TestDrawerDeleteClampsCursorWhenTaskIsGone(t *testing.T) {
	m, _ := newDrawerModel(t, "alpha", "beta")
	m.drawerCursor = 1
	m = press(t, m, 'd')

	if len(m.drawerTodos) != 1 {
		t.Fatalf("got %d tasks after delete, want 1", len(m.drawerTodos))
	}
	if m.drawerCursor != 0 {
		t.Errorf("cursor = %d, want 0 after deleting the last row", m.drawerCursor)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./... -run 'TestDrawer' -v`
Expected: FAIL — `undefined: (model).loadDrawerTodos`.

- [ ] **Step 3: Swap the edit-state fields**

In `model.go`, replace line 117:

```go
	drawerEditIdx  int  // -1 = adding a new task; >=0 = editing that task
```

with:

```go
	drawerEditID   string // id being edited; meaningful only when drawerAdding is false
	drawerAdding   bool   // input is an add rather than an edit
```

- [ ] **Step 4: Add the drawer's delta plumbing**

In `drawer.go`, replace `persistDrawer` (lines 152-156) with:

```go
// loadDrawerTodos loads a repo's list for display, stamping ids on any task that
// lacks one so every visible row is addressable by the delta handlers. The write
// is content-identical once a file has been stamped, so it causes no git churn.
func (m *model) loadDrawerTodos(path string) {
	todos, err := withTodos(path, func(ts []Todo) []Todo { return ts })
	if err != nil {
		m.err = err
		todos = loadTodos(path)
	}
	m.drawerTodos = todos
}

// applyDrawer runs a delta against the on-disk list and adopts the result, so a
// peer's concurrent edits survive. The cursor stays on the task it was on, even
// if the peer inserted rows above it.
func (m *model) applyDrawer(mutate func([]Todo) []Todo) {
	id := m.cursorTodoID()
	todos, err := withTodos(m.drawerRepo, mutate)
	if err != nil {
		m.err = err
		return
	}
	m.drawerTodos = todos
	m.restoreCursor(id)
}

func (m model) cursorTodoID() string {
	if m.drawerCursor >= 0 && m.drawerCursor < len(m.drawerTodos) {
		return m.drawerTodos[m.drawerCursor].ID
	}
	return ""
}

// restoreCursor puts the cursor back on id, clamping when it is gone — deleted
// here or by a peer.
func (m *model) restoreCursor(id string) {
	if i, ok := indexByID(m.drawerTodos, id); ok {
		m.drawerCursor = i
		return
	}
	if m.drawerCursor >= len(m.drawerTodos) {
		m.drawerCursor = max(0, len(m.drawerTodos)-1)
	}
}
```

- [ ] **Step 5: Update the three load sites and the edit-state resets**

In `drawer.go`:
- Line 46 (`toggleDrawer`): `m.drawerTodos = loadTodos(path)` → `m.loadDrawerTodos(path)`
- Line 49 (`toggleDrawer`): `m.drawerEditIdx = -1` → `m.drawerEditID, m.drawerAdding = "", false`
- Line 93 (`stopDrawerInput`): `m.drawerEditIdx = -1` → `m.drawerEditID, m.drawerAdding = "", false`
- Line 110 (`reloadDrawerForContext`): `m.drawerTodos = loadTodos(path)` → `m.loadDrawerTodos(path)`

Leave `refreshDrawerFromDisk` (line 122) on plain `loadTodos` — it is a read-only tick and must not write.

- [ ] **Step 6: Rewrite the input-mode enter handler**

Replace the `case "enter":` body inside the `if m.drawerInputOn` block (`drawer.go:168-181`) with:

```go
		case "enter":
			val := strings.TrimSpace(m.drawerInput.Value())
			if val != "" {
				subj, desc := splitSubjectDesc(val)
				if m.drawerAdding {
					section := m.drawerCursorSection()
					todos, err := withTodos(m.drawerRepo, func(ts []Todo) []Todo {
						return addTodo(ts, section, subj, desc)
					})
					if err != nil {
						m.err = err
					} else {
						m.drawerTodos = todos
						m.restoreCursor(todos[len(todos)-1].ID)
					}
				} else {
					editID := m.drawerEditID
					m.applyDrawer(func(ts []Todo) []Todo {
						if i, ok := indexByID(ts, editID); ok {
							ts[i].Subject, ts[i].Description = subj, desc
						}
						return ts
					})
					m.restoreCursor(editID)
				}
			}
			m.stopDrawerInput()
			return m, nil
```

The add branch calls `withTodos` directly rather than `applyDrawer` because it wants the cursor to land on the *new* task, whereas `applyDrawer` deliberately keeps it on the old one.

- [ ] **Step 7: Rewrite the navigation-mode handlers**

Replace the `case "a":`, `case "e":`, and the four mutating cases (`drawer.go:214-253`) with:

```go
	case "a":
		m.drawerInput = newDrawerInput("add: ", "subject — optional description", "")
		m.drawerInputOn = true
		m.drawerEditID, m.drawerAdding = "", true
		return m, m.drawerInput.Focus()
	case "e":
		if m.drawerCursor >= 0 && m.drawerCursor < len(m.drawerTodos) {
			m.drawerInput = newDrawerInput("edit: ", "", todoEditText(m.drawerTodos[m.drawerCursor]))
			m.drawerInputOn = true
			m.drawerEditID, m.drawerAdding = m.drawerTodos[m.drawerCursor].ID, false
			return m, m.drawerInput.Focus()
		}
	case "up", "k":
		if m.drawerCursor > 0 {
			m.drawerCursor--
		}
	case "down", "j":
		if m.drawerCursor < len(m.drawerTodos)-1 {
			m.drawerCursor++
		}
	case "space", " ", "x":
		id := m.cursorTodoID()
		m.applyDrawer(func(ts []Todo) []Todo {
			if i, ok := indexByID(ts, id); ok {
				return toggleTodoDone(ts, i)
			}
			return ts
		})
	case "~", "enter", "c":
		id := m.cursorTodoID()
		var held string
		m.applyDrawer(func(ts []Todo) []Todo {
			i, ok := indexByID(ts, id)
			if !ok {
				return ts
			}
			out, changed := claimTodo(ts, i, m.drawerClaim)
			if !changed {
				held = ts[i].Claim
				return ts
			}
			return out
		})
		if held != "" {
			m.err = fmt.Errorf("task claimed by %s", held)
		}
	case ">":
		id := m.cursorTodoID()
		m.applyDrawer(func(ts []Todo) []Todo {
			if i, ok := indexByID(ts, id); ok {
				return deferTodo(ts, i)
			}
			return ts
		})
	case "d":
		id := m.cursorTodoID()
		m.applyDrawer(func(ts []Todo) []Todo {
			if i, ok := indexByID(ts, id); ok {
				return deleteTodo(ts, i)
			}
			return ts
		})
```

- [ ] **Step 8: Run the drawer tests**

Run: `go test ./... -run 'TestDrawer' -v`
Expected: PASS.

- [ ] **Step 9: Confirm `saveTodos` is fully gone**

Run: `grep -rn 'saveTodos\|drawerEditIdx\|persistDrawer' --include=*.go .`
Expected: no matches. If `saveTodos` was left in `todo.go` at the end of Task 5, delete it now.

- [ ] **Step 10: Run the full suite and build**

Run: `go test ./... && go build -o /tmp/hive-check . && rm /tmp/hive-check`
Expected: PASS, clean build.

- [ ] **Step 11: Commit**

```bash
git add drawer.go model.go drawer_test.go todo.go
git commit -m "feat(todo): drawer writes id-addressed deltas instead of whole-array snapshots"
```

---

## Deviations from the spec

Two refinements surfaced while working the drawer through in detail. Both are
carried into the spec.

1. **`drawerAdding bool` alongside `drawerEditID string`.** The spec replaced
   `drawerEditIdx int` with `drawerEditID string` alone. That overloads `""` to
   mean "adding", which silently converts an edit into an add whenever the task
   being edited has no id yet. A separate boolean keeps the two modes explicit.

2. **`loadDrawerTodos` backfills ids when the drawer opens.** The spec only
   backfilled on write. But every drawer delta addresses its task by id, so a
   legacy row without one would make `x`, `>`, `d` and `c` silently no-op. Opening
   the drawer now stamps ids through `withTodos`. Once a file is stamped the write
   is content-identical, so it produces no git churn.

## Manual verification

After Task 6, verify against a real repo with worktrees. `stevenlawton.com` has three (`main`, `.worktrees/split-1`, `.worktrees/split-2`) and active sessions — **coordinate on the hive bus before touching its `TODO.md`**, since peers are mid-flight there.

1. `hive todo list` — every row shows a three-letter id.
2. `hive todo normalize` — stamps ids on any legacy rows; `git diff docs/TODO.md` shows only added `id:` tokens, and nothing outside the markers changed.
3. From two shells in different worktrees, run `hive todo add one` and `hive todo add two` simultaneously. Both survive.
4. In one shell run `hive todo list`, note an id, have the other shell `hive todo rm 1`, then `hive todo done <id>` in the first. The correct task is marked done.
5. Open the hive drawer, press `a`, and type slowly. From another shell, `hive todo add peer`. Submit the drawer's add. Both tasks are present.

## Rollback

Every task is a standalone commit and the file format is backward compatible in both directions: an older hive reads `id:` markers as an unrecognised trailing comment on the claim, and a newer hive re-stamps ids onto a file an older one wrote. Reverting any commit leaves a readable `TODO.md`.
