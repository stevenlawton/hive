# Backlog Store Out of the Repos (P1) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move hive's per-repo backlog out of each repo's `docs/TODO.md` and into a hive-owned store under `$XDG_DATA_HOME/hive/todos/`, importing existing tasks with their ids intact.

**Architecture:** A repo is identified by the most durable thing available — normalized git remote, else first-commit SHA, else main-worktree path — and that identity hashes into a store filename. The markdown format is unchanged; only the path moves. The first time hive touches a repo with no store, it imports that repo's `TASKS:BEGIN/END` block wholesale inside the existing flock.

**Tech Stack:** Go (package `main`, module `github.com/stevenlawton/hive`), stdlib only. `syscall.Flock` for locking, `os/exec` shelling to `git`. Tests are standard `go test`.

**Spec:** `docs/superpowers/specs/2026-08-25-todo-store-out-of-repo-design.md`

## Global Constraints

- **The CLI surface does not change.** All twelve verbs (`list`, `claim`, `show`, `done`, `edit`, `state`, `reap`, `add`, `statusline`, `rm`, `reopen`, `defer`) keep their arguments and output shape. The only permitted output change is Task 8.
- **The file format does not change.** `parseTodos`, `formatTodos`, `extractBlock`, `replaceBlock`, `blockBounds`, `stripSyncLine` are not edited in this plan.
- **Ids are never reminted during import.** Sessions hold live claims addressed by id.
- **Tests must never write to the real `$HOME`.** Every test touching the store uses `newTestRepo` from Task 1.
- **`hive todo statusline` writes only the status line to stdout.** Any notice goes to stderr.
- Store root: `$XDG_DATA_HOME/hive/todos/`, defaulting to `~/.local/share/hive/todos/`.
- Lock root stays `$XDG_RUNTIME_DIR/hive/` (unchanged behaviour, re-keyed in Task 5).
- Unix only, as hive already is.

---

### Task 1: Test isolation helper

Every later task writes to a store outside the repo. Without this, running the
suite pollutes the developer's real `~/.local/share/hive`. This lands first so
nothing after it can leak.

**Files:**
- Modify: `todo_store_test.go:169` (the existing `chdir` helper lives here; add beside it)
- Modify: `todo_store_test.go`, `cmd_todo_edit_test.go`, `cmd_todo_state_test.go`, `drawer_test.go` — replace `t.TempDir()` repo fixtures with `newTestRepo(t)`

**Interfaces:**
- Consumes: nothing
- Produces: `func newTestRepo(t *testing.T) string` — returns a fresh temp directory to use as a repo path, with `XDG_DATA_HOME` and `XDG_RUNTIME_DIR` redirected into the test's own temp space for the duration of the test.

- [ ] **Step 1: Write the failing test**

Add to `todo_store_test.go`:

```go
// newTestRepo must sandbox every hive-owned path, or the suite writes into the
// developer's real data and runtime dirs.
func TestNewTestRepoSandboxesHivePaths(t *testing.T) {
	realData := os.Getenv("XDG_DATA_HOME")
	realRun := os.Getenv("XDG_RUNTIME_DIR")

	dir := newTestRepo(t)

	data := os.Getenv("XDG_DATA_HOME")
	run := os.Getenv("XDG_RUNTIME_DIR")
	if data == "" || data == realData {
		t.Errorf("XDG_DATA_HOME not redirected: %q", data)
	}
	if run == "" || run == realRun {
		t.Errorf("XDG_RUNTIME_DIR not redirected: %q", run)
	}
	home, err := os.UserHomeDir()
	if err == nil {
		if strings.HasPrefix(data, home+"/.local") || strings.HasPrefix(run, home+"/.local") {
			t.Errorf("redirected into the real home: data=%q run=%q", data, run)
		}
	}
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("repo dir not usable: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -run TestNewTestRepoSandboxesHivePaths . -v`
Expected: FAIL — `undefined: newTestRepo`

- [ ] **Step 3: Write minimal implementation**

Add to `todo_store_test.go`, beside `chdir`:

```go
// newTestRepo returns a directory to use as a repo path, with hive's data and
// runtime roots redirected into the test's own temp space. Every test that
// reaches the store must use this: the store now lives outside the repo, so a
// bare t.TempDir() would leave tasks in the developer's real ~/.local/share.
func newTestRepo(t *testing.T) string {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	return t.TempDir()
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -run TestNewTestRepoSandboxesHivePaths . -v`
Expected: PASS

- [ ] **Step 5: Migrate every store-touching test to the helper**

In `todo_store_test.go`, `cmd_todo_edit_test.go`, `cmd_todo_state_test.go` and
`drawer_test.go`, replace the repo-fixture line:

```go
dir := t.TempDir()
```

with:

```go
dir := newTestRepo(t)
```

Only for the directory that is passed to `withTodos`, `loadTodos`,
`todoFilePath`, `chdir` or a drawer repo path. Leave `t.TempDir()` calls that
serve some other purpose alone. In `todo_store_test.go` that includes the
second fixture at line 235.

`TestTodoLockPathIsOutsideTheRepo` (`todo_store_test.go:157`) already asserts
the lock is outside the repo — it keeps asserting exactly that, now against a
redirected runtime dir.

- [ ] **Step 6: Run the whole suite**

Run: `go test ./...`
Expected: PASS — this task changes no production code, so a failure means a
fixture was migrated wrongly.

- [ ] **Step 7: Verify nothing leaks**

Run:
```bash
ls ~/.local/share/hive 2>/dev/null; echo "exit=$?"
go test ./... >/dev/null && ls ~/.local/share/hive 2>/dev/null; echo "exit=$?"
```
Expected: identical before and after — the suite creates no `~/.local/share/hive`.

- [ ] **Step 8: Commit**

```bash
git add todo_store_test.go cmd_todo_edit_test.go cmd_todo_state_test.go drawer_test.go
git commit -m "test: sandbox hive's data and runtime roots in store tests"
```

---

### Task 2: Durable repo identity

**Files:**
- Create: `repo_key.go`
- Create: `repo_key_test.go`

**Interfaces:**
- Consumes: `mainWorktree(repoPath string) string` (`todo.go:84`)
- Produces:
  - `func repoIdentity(repoPath string) (id string, tier int)` — tier 0 remote, 1 first-commit, 2 path
  - `func repoKey(repoPath string) string` — 8 lowercase hex chars
  - `func normalizeRemote(raw string) string`

- [ ] **Step 1: Write the failing test**

Create `repo_key_test.go`:

```go
package main

import (
	"os/exec"
	"testing"
)

func gitInit(t *testing.T, dir string) {
	t.Helper()
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "Test"},
		{"commit", "-q", "--allow-empty", "-m", "root"},
	} {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
}

func gitRemote(t *testing.T, dir, url string) {
	t.Helper()
	cmd := exec.Command("git", "-C", dir, "remote", "add", "origin", url)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git remote add: %v: %s", err, out)
	}
}

func TestNormalizeRemoteFormsAgree(t *testing.T) {
	want := "github.com/stevenlawton/hive"
	for _, raw := range []string{
		"https://github.com/stevenlawton/hive.git",
		"https://github.com/stevenlawton/hive",
		"git@github.com:stevenlawton/hive.git",
		"ssh://git@github.com/stevenlawton/hive.git",
		"HTTPS://GitHub.com/StevenLawton/Hive.git",
	} {
		if got := normalizeRemote(raw); got != want {
			t.Errorf("normalizeRemote(%q) = %q, want %q", raw, got, want)
		}
	}
}

// A remote beats a first commit, which beats the path. The tier is what later
// lets a re-keyed repo find the store it wrote under a weaker identity.
func TestRepoIdentityPrecedence(t *testing.T) {
	dir := t.TempDir()
	if _, tier := repoIdentity(dir); tier != 2 {
		t.Errorf("non-git dir: tier = %d, want 2 (path)", tier)
	}

	gitInit(t, dir)
	id, tier := repoIdentity(dir)
	if tier != 1 {
		t.Errorf("git dir with no remote: tier = %d, want 1 (first commit)", tier)
	}
	if len(id) != 40 {
		t.Errorf("first-commit identity = %q, want a 40-char sha", id)
	}

	gitRemote(t, dir, "git@github.com:stevenlawton/hive.git")
	id, tier = repoIdentity(dir)
	if tier != 0 {
		t.Errorf("git dir with a remote: tier = %d, want 0 (remote)", tier)
	}
	if id != "github.com/stevenlawton/hive" {
		t.Errorf("remote identity = %q", id)
	}
}

// Moving a repo must not change its key: the key addresses the backlog.
func TestRepoKeySurvivesAMove(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir)
	gitRemote(t, dir, "https://github.com/x/y.git")
	before := repoKey(dir)

	moved := t.TempDir() + "/elsewhere"
	if err := os.Rename(dir, moved); err != nil {
		t.Fatal(err)
	}
	if after := repoKey(moved); after != before {
		t.Errorf("key changed on move: %q -> %q", before, after)
	}
}

// A fork shares its history but not its remote, so it gets its own backlog.
func TestRepoKeyDistinguishesForks(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	gitInit(t, a)
	gitInit(t, b)
	gitRemote(t, a, "https://github.com/upstream/proj.git")
	gitRemote(t, b, "https://github.com/fork/proj.git")
	if repoKey(a) == repoKey(b) {
		t.Error("fork and upstream resolved to the same key")
	}
}

func TestRepoKeyIsEightHexChars(t *testing.T) {
	key := repoKey(t.TempDir())
	if len(key) != 8 {
		t.Fatalf("key = %q, want 8 chars", key)
	}
	for _, c := range key {
		if !strings.ContainsRune("0123456789abcdef", c) {
			t.Fatalf("key = %q, want lowercase hex", key)
		}
	}
}
```

Add `"os"` and `"strings"` to the import block.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -run 'TestNormalizeRemote|TestRepoIdentity|TestRepoKey' . -v`
Expected: FAIL — `undefined: normalizeRemote`, `undefined: repoIdentity`, `undefined: repoKey`

- [ ] **Step 3: Write minimal implementation**

Create `repo_key.go`:

```go
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os/exec"
	"strings"
)

// Identity tiers, most durable first. The tier is preserved alongside the id
// because a repo can gain a stronger identity later — see adoptStore.
const (
	tierRemote = iota
	tierFirstCommit
	tierPath
)

// repoIdentity returns the most durable identity available for a repo. The
// backlog is addressed by this, so it must survive the repo being moved or
// re-cloned — which a filesystem path does not.
func repoIdentity(repoPath string) (string, int) {
	main := mainWorktree(repoPath)
	if out, err := exec.Command("git", "-C", main, "remote", "get-url", "origin").Output(); err == nil {
		if id := normalizeRemote(string(out)); id != "" {
			return id, tierRemote
		}
	}
	if out, err := exec.Command("git", "-C", main, "rev-list", "--max-parents=0", "HEAD").Output(); err == nil {
		if lines := strings.Fields(string(out)); len(lines) > 0 {
			return lines[len(lines)-1], tierFirstCommit
		}
	}
	return main, tierPath
}

// normalizeRemote reduces the forms git accepts for one remote to a single
// string, so an ssh clone and an https clone of the same project agree.
func normalizeRemote(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	if s == "" {
		return ""
	}
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	if i := strings.LastIndex(s, "@"); i >= 0 {
		s = s[i+1:] // strips ssh user and any embedded credentials
	}
	s = strings.Replace(s, ":", "/", 1) // git@host:x/y → host/x/y
	s = strings.TrimSuffix(s, "/")
	return strings.TrimSuffix(s, ".git")
}

// repoKey hashes a repo's identity into the token that names its store.
func repoKey(repoPath string) string {
	id, _ := repoIdentity(repoPath)
	sum := sha256.Sum256([]byte(id))
	return hex.EncodeToString(sum[:4])
}
```

Note: `rev-list --max-parents=0 HEAD` can print several roots for a repo with
grafted history; the last line is the oldest, so it is the stable one.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -run 'TestNormalizeRemote|TestRepoIdentity|TestRepoKey' . -v`
Expected: PASS (5 tests)

- [ ] **Step 5: Commit**

```bash
git add repo_key.go repo_key_test.go
git commit -m "feat(todo): resolve a durable identity for a repo"
```

---

### Task 3: The store path

**Files:**
- Modify: `repo_key.go`
- Modify: `repo_key_test.go`

**Interfaces:**
- Consumes: `repoKey`, `mainWorktree`
- Produces:
  - `func hiveDataDir() string`
  - `func todoStorePath(repoPath string) string`
  - `func slugify(s string) string`

- [ ] **Step 1: Write the failing test**

Append to `repo_key_test.go`:

```go
func TestHiveDataDirHonoursXDG(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "/tmp/xdg-test")
	if got := hiveDataDir(); got != "/tmp/xdg-test/hive" {
		t.Errorf("hiveDataDir() = %q", got)
	}
	t.Setenv("XDG_DATA_HOME", "")
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir")
	}
	if got, want := hiveDataDir(), filepath.Join(home, ".local", "share", "hive"); got != want {
		t.Errorf("hiveDataDir() = %q, want %q", got, want)
	}
}

// The store is named for the repo so the directory can be read by a human, but
// keyed by the hash so the name carries no meaning.
func TestTodoStorePathShape(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "/tmp/xdg-test")
	dir := t.TempDir() + "/My Repo"
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	got := todoStorePath(dir)
	wantDir := "/tmp/xdg-test/hive/todos"
	if filepath.Dir(got) != wantDir {
		t.Errorf("store dir = %q, want %q", filepath.Dir(got), wantDir)
	}
	base := filepath.Base(got)
	if !strings.HasPrefix(base, "my-repo-") || !strings.HasSuffix(base, ".md") {
		t.Errorf("store file = %q, want my-repo-<key>.md", base)
	}
	if !strings.HasSuffix(base, repoKey(dir)+".md") {
		t.Errorf("store file %q does not end in the repo key", base)
	}
}

// The store must be outside the repo — that is the whole point of the change.
func TestTodoStorePathIsOutsideTheRepo(t *testing.T) {
	dir := newTestRepo(t)
	if strings.HasPrefix(todoStorePath(dir), dir) {
		t.Errorf("store path %q is inside the repo %q", todoStorePath(dir), dir)
	}
}

// Two worktrees of one repo share a backlog, as they do today via mainWorktree.
func TestTodoStorePathSharedAcrossWorktrees(t *testing.T) {
	dir := newTestRepo(t)
	gitInit(t, dir)
	wt := filepath.Join(t.TempDir(), "wt")
	cmd := exec.Command("git", "-C", dir, "worktree", "add", "-q", "-b", "side", wt)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("worktree add: %v: %s", err, out)
	}
	if todoStorePath(dir) != todoStorePath(wt) {
		t.Errorf("worktrees disagree: %q vs %q", todoStorePath(dir), todoStorePath(wt))
	}
}
```

Add `"path/filepath"` to the import block.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -run 'TestHiveDataDir|TestTodoStorePath' . -v`
Expected: FAIL — `undefined: hiveDataDir`, `undefined: todoStorePath`

- [ ] **Step 3: Write minimal implementation**

Append to `repo_key.go` (and add `"os"`, `"path/filepath"` to its imports):

```go
// hiveDataDir is hive's data root. Data, not runtime: the backlog must survive
// a reboot, which is why it does not live beside the lock.
func hiveDataDir() string {
	if dir := os.Getenv("XDG_DATA_HOME"); dir != "" {
		return filepath.Join(dir, "hive")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "hive")
	}
	return filepath.Join(home, ".local", "share", "hive")
}

// todoStorePath is where a repo's backlog lives. The slug is the repo's
// directory name, present only so the store directory can be read at a glance;
// the key is the identity.
func todoStorePath(repoPath string) string {
	slug := slugify(filepath.Base(mainWorktree(repoPath)))
	return filepath.Join(hiveDataDir(), "todos", slug+"-"+repoKey(repoPath)+".md")
}

// slugify reduces a directory name to something safe in a filename.
func slugify(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-.")
	if out == "" {
		return "repo"
	}
	return out
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -run 'TestHiveDataDir|TestTodoStorePath' . -v`
Expected: PASS (4 tests)

- [ ] **Step 5: Commit**

```bash
git add repo_key.go repo_key_test.go
git commit -m "feat(todo): derive the store path from the repo key"
```

---

### Task 4: Read the legacy in-repo backlog

The importer's read half, on its own so it can be tested without touching the
live store.

**Files:**
- Create: `todo_import.go`
- Create: `todo_import_test.go`

**Interfaces:**
- Consumes: `mainWorktree`, `extractBlock` (`todo.go:141`), `parseTodos` (`todo.go:209`)
- Produces:
  - `func legacyRepoFile(repoPath string) string` — path to the repo's `docs/TODO.md` or `TODO.md` if it holds a TASKS block, else `""`
  - `func legacyBlock(path string) string` — the block body, or `""`

- [ ] **Step 1: Write the failing test**

Create `todo_import_test.go`:

```go
package main

import (
	"os"
	"path/filepath"
	"testing"
)

func writeRepoBacklog(t *testing.T, repo, rel, block string) string {
	t.Helper()
	path := filepath.Join(repo, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "# Open work\n\n" + tasksBegin + "\n" + block + tasksEnd + "\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLegacyRepoFilePrefersDocs(t *testing.T) {
	repo := newTestRepo(t)
	writeRepoBacklog(t, repo, "TODO.md", "\n### Tasks\n\n- [ ] **root** <!-- id:aaa -->\n")
	docs := writeRepoBacklog(t, repo, "docs/TODO.md", "\n### Tasks\n\n- [ ] **docs** <!-- id:bbb -->\n")
	if got := legacyRepoFile(repo); got != docs {
		t.Errorf("legacyRepoFile = %q, want %q", got, docs)
	}
}

func TestLegacyRepoFileFallsBackToRoot(t *testing.T) {
	repo := newTestRepo(t)
	root := writeRepoBacklog(t, repo, "TODO.md", "\n### Tasks\n\n- [ ] **root** <!-- id:aaa -->\n")
	if got := legacyRepoFile(repo); got != root {
		t.Errorf("legacyRepoFile = %q, want %q", got, root)
	}
}

// A hand-written TODO.md with no TASKS block is not hive's and must be ignored.
func TestLegacyRepoFileIgnoresFileWithoutBlock(t *testing.T) {
	repo := newTestRepo(t)
	path := filepath.Join(repo, "TODO.md")
	if err := os.WriteFile(path, []byte("# my notes\n\n- buy milk\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := legacyRepoFile(repo); got != "" {
		t.Errorf("legacyRepoFile = %q, want \"\"", got)
	}
}

func TestLegacyRepoFileAbsentIsEmpty(t *testing.T) {
	if got := legacyRepoFile(newTestRepo(t)); got != "" {
		t.Errorf("legacyRepoFile = %q, want \"\"", got)
	}
}

// The block must survive verbatim: ids, claims, states and since stamps are
// what running sessions address tasks by.
func TestLegacyBlockPreservesEveryField(t *testing.T) {
	repo := newTestRepo(t)
	block := "\n### Now\n\n" +
		"- [~] **claimed one** - body <!-- @split-1 since:2026-08-01T00:00:00Z id:aaa state:ready -->\n" +
		"- [x] **done one** <!-- id:bbb -->\n" +
		"- [-] **parked one** <!-- id:ccc -->\n"
	path := writeRepoBacklog(t, repo, "docs/TODO.md", block)

	got := parseTodos(legacyBlock(path))
	if len(got) != 3 {
		t.Fatalf("got %d tasks, want 3", len(got))
	}
	if got[0].ID != "aaa" || got[0].Claim != "split-1" || got[0].State != StateReady ||
		got[0].Since != "2026-08-01T00:00:00Z" || got[0].Section != "Now" {
		t.Errorf("claimed task lost fields: %+v", got[0])
	}
	if !got[1].Done || got[1].ID != "bbb" {
		t.Errorf("done task: %+v", got[1])
	}
	if !got[2].Deferred || got[2].ID != "ccc" {
		t.Errorf("parked task: %+v", got[2])
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -run TestLegacy . -v`
Expected: FAIL — `undefined: legacyRepoFile`, `undefined: legacyBlock`

- [ ] **Step 3: Write minimal implementation**

Create `todo_import.go`:

```go
package main

import (
	"os"
	"path/filepath"
)

// legacyRepoFile is the in-repo backlog a repo had before the store moved out,
// or "" if it has none. Only a file carrying a TASKS block counts: a
// hand-written TODO.md is somebody's notes, not hive's data.
func legacyRepoFile(repoPath string) string {
	main := mainWorktree(repoPath)
	for _, rel := range [][]string{{"docs", "TODO.md"}, {"TODO.md"}} {
		path := filepath.Join(append([]string{main}, rel...)...)
		if legacyBlock(path) != "" {
			return path
		}
	}
	return ""
}

// legacyBlock returns the TASKS block body of an in-repo backlog, or "".
func legacyBlock(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return extractBlock(string(data))
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -run TestLegacy . -v`
Expected: PASS (5 tests)

- [ ] **Step 5: Commit**

```bash
git add todo_import.go todo_import_test.go
git commit -m "feat(todo): read a repo's legacy in-repo backlog"
```

---

### Task 5: Switch the store to the new path, importing on first access

The cutover. After this task the backlog no longer lives in any repo.

**Files:**
- Modify: `todo.go:116-127` (`todoFilePath` — becomes a thin alias, then callers move)
- Modify: `todo.go:133-138` (`loadTodos`)
- Modify: `todo_store.go:18-44` (`withTodos`)
- Modify: `todo_store.go:90-96` (`todoLockPath`)
- Modify: `todo_store_test.go` — the layout tests
- Test: `todo_import_test.go`

**Interfaces:**
- Consumes: `todoStorePath` (Task 3), `legacyRepoFile`/`legacyBlock` (Task 4), `repoKey` (Task 2)
- Produces:
  - `func readStore(repoPath string) (content string, existed bool)` — the store's bytes, importing the legacy repo file on first access. Caller must already hold the lock.
  - `loadTodos` and `withTodos` keep their existing signatures.

- [ ] **Step 1: Write the failing test**

Append to `todo_import_test.go`:

```go
// The move must be invisible to a session: its tasks are all still there, with
// the ids peers address them by.
func TestFirstAccessImportsLegacyBacklog(t *testing.T) {
	repo := newTestRepo(t)
	writeRepoBacklog(t, repo, "docs/TODO.md",
		"\n### Now\n\n- [~] **claimed** - body <!-- @split-1 id:aaa state:ready -->\n- [ ] **open** <!-- id:bbb -->\n")

	got := loadTodos(repo)
	if len(got) != 2 {
		t.Fatalf("got %d tasks, want 2", len(got))
	}
	if got[0].ID != "aaa" || got[0].Claim != "split-1" || got[0].State != StateReady {
		t.Errorf("claim or state lost on import: %+v", got[0])
	}
	if got[1].ID != "bbb" {
		t.Errorf("id lost on import: %+v", got[1])
	}
	if _, err := os.Stat(todoStorePath(repo)); err != nil {
		t.Errorf("import did not create the store: %v", err)
	}
}

// Import is one-shot. After it, the repo file is inert: hive must not read it
// again, or a stale checkout would resurrect deleted tasks.
func TestImportHappensOnlyOnce(t *testing.T) {
	repo := newTestRepo(t)
	path := writeRepoBacklog(t, repo, "docs/TODO.md", "\n### Now\n\n- [ ] **first** <!-- id:aaa -->\n")

	if got := loadTodos(repo); len(got) != 1 {
		t.Fatalf("got %d tasks on import, want 1", len(got))
	}
	if _, err := withTodos(repo, func(ts []Todo) []Todo {
		return deleteTodo(ts, 0)
	}); err != nil {
		t.Fatal(err)
	}
	// The repo file still says otherwise. Hive must not care.
	if legacyBlock(path) == "" {
		t.Fatal("fixture is wrong: the repo file should still hold its block")
	}
	if got := loadTodos(repo); len(got) != 0 {
		t.Errorf("got %d tasks after delete, want 0 — the repo file was re-read", len(got))
	}
}

// Mutating must never write into the repo. That is the change, stated as a test.
func TestMutationDoesNotTouchTheRepo(t *testing.T) {
	repo := newTestRepo(t)
	path := writeRepoBacklog(t, repo, "docs/TODO.md", "\n### Now\n\n- [ ] **first** <!-- id:aaa -->\n")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := withTodos(repo, func(ts []Todo) []Todo {
		return addTodo(ts, "Now", "second", "")
	}); err != nil {
		t.Fatal(err)
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Error("the repo's docs/TODO.md was modified")
	}
	entries, err := os.ReadDir(filepath.Join(repo, "docs"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("docs/ holds %d entries, want just TODO.md — a temp file leaked", len(entries))
	}
}

// A repo with nothing to import starts empty rather than erroring.
func TestFirstAccessWithNoLegacyBacklogStartsEmpty(t *testing.T) {
	repo := newTestRepo(t)
	if got := loadTodos(repo); len(got) != 0 {
		t.Errorf("got %d tasks, want 0", len(got))
	}
}

// Concurrent first-touch must produce one store, not a race between importers.
func TestConcurrentFirstAccessImportsOnce(t *testing.T) {
	repo := newTestRepo(t)
	writeRepoBacklog(t, repo, "docs/TODO.md", "\n### Now\n\n- [ ] **first** <!-- id:aaa -->\n")

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if _, err := withTodos(repo, func(ts []Todo) []Todo {
				return addTodo(ts, "Now", fmt.Sprintf("added-%d", i), "")
			}); err != nil {
				t.Errorf("writer %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()

	got := loadTodos(repo)
	if len(got) != 9 {
		t.Fatalf("got %d tasks, want 9 (1 imported + 8 added)", len(got))
	}
	if got[0].ID != "aaa" {
		t.Errorf("the imported task is not first or lost its id: %+v", got[0])
	}
}
```

Add `"fmt"` and `"sync"` to the import block.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -run 'TestFirstAccess|TestImportHappens|TestMutationDoesNotTouch|TestConcurrentFirstAccess' . -v`
Expected: FAIL — tasks are read from and written to the repo's `docs/TODO.md`, so
`TestMutationDoesNotTouchTheRepo` fails on a modified file and
`TestImportHappensOnlyOnce` fails with 1 task instead of 0.

- [ ] **Step 3: Point the store at the new path**

In `todo.go`, replace `todoFilePath` (lines 114-127) with:

```go
// todoFilePath is the backlog store for a repo. It lives under hive's data
// directory, never in the repo: a hive-owned file in a git tree dirties every
// deploy pre-flight and invites sessions to edit it by hand.
func todoFilePath(repoPath string) string {
	return todoStorePath(repoPath)
}
```

`fileExists` (`todo.go:122`) is now unused by this function but is used
elsewhere; leave it.

In `todo_store.go`, replace `todoLockPath` (lines 90-96) with:

```go
// todoLockPath is the lock for a repo's backlog, keyed by the same identity as
// the store so the two cannot disagree about which repo they serve. It lives in
// the runtime dir: a lock should not survive a reboot, and a backlog should.
func todoLockPath(repoPath string) string {
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "hive", "todo-"+repoKey(repoPath)+".lock")
}
```

Remove the now-unused `crypto/sha256` and `encoding/hex` imports from
`todo_store.go`.

- [ ] **Step 4: Add the importing read**

Append to `todo_import.go`:

```go
// readStore returns a repo's store content, importing its legacy in-repo
// backlog the first time the store is touched. The caller must already hold the
// backlog lock: two worktrees reaching a repo for the first time at once must
// not both import.
//
// Import is one-shot by construction — once the store exists it is the only
// thing read, so a stale checkout of the old file cannot resurrect tasks.
func readStore(repoPath string) (string, bool) {
	path := todoStorePath(repoPath)
	if data, err := os.ReadFile(path); err == nil {
		return string(data), true
	}
	src := legacyRepoFile(repoPath)
	if src == "" {
		return "", false
	}
	block := legacyBlock(src)
	n := len(parseTodos(block))
	// stderr, not stdout: `hive todo statusline` renders stdout into the prompt.
	fmt.Fprintf(os.Stderr, "hive: imported %d task(s) from %s into %s\n", n, src, path)
	return replaceBlock("", block), true
}
```

Add `"fmt"` to `todo_import.go`'s imports.

- [ ] **Step 5: Route both readers through it**

In `todo_store.go`, in `withTodos`, replace the read block (lines 25-31):

```go
	path := todoFilePath(repoPath)
	existing := ""
	existed := false
	if data, err := os.ReadFile(path); err == nil {
		existing = string(data)
		existed = true
	}
```

with:

```go
	path := todoFilePath(repoPath)
	existing, existed := readStore(repoPath)
```

In `todo.go`, replace `loadTodos` (lines 128-138) with:

```go
// loadTodos reads a repo's backlog. It goes through withTodos rather than
// reading the file directly so that a first access imports the repo's legacy
// backlog — the first thing to touch a repo after the move is usually the
// statusline, which only reads.
func loadTodos(repoPath string) []Todo {
	todos, err := withTodos(repoPath, func(ts []Todo) []Todo { return ts })
	if err != nil {
		return nil
	}
	return todos
}
```

- [ ] **Step 6: Run the new tests**

Run: `go test -run 'TestFirstAccess|TestImportHappens|TestMutationDoesNotTouch|TestConcurrentFirstAccess' . -v`
Expected: PASS (5 tests)

- [ ] **Step 7: Rework the layout tests**

Three tests in `todo_store_test.go` assert the old in-repo layout and must now
assert the new one. Their *intent* is unchanged; only the path moves.

In `TestWithTodosBackfillsBeforeAndAfterMutate` (`:52`) and
`TestWithTodosPreservesSurroundingProse` (`:80`), replace the fixture path:

```go
	path := filepath.Join(dir, "docs", "TODO.md")
```

with:

```go
	path := todoStorePath(dir)
```

and drop the `os.MkdirAll(filepath.Dir(path), ...)` guard only if
`writeTodoFile` already creates it — it does (`todo_store.go:102`), but these
tests write with `os.WriteFile` directly, so keep the `MkdirAll`.

In `TestWriteTodoFileLeavesNoTempFiles` (`:109`), replace:

```go
	path := filepath.Join(dir, "TODO.md")
```

with:

```go
	path := todoStorePath(dir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
```

and read the directory it scans for leftovers from `filepath.Dir(path)` rather
than `dir`.

Rename `TestTodoLockPathIsOutsideTheRepo` (`:157`) to
`TestTodoLockPathIsOutsideTheRepo` unchanged — it still asserts exactly the
right thing.

- [ ] **Step 8: Run the whole suite**

Run: `go test ./...`
Expected: PASS

- [ ] **Step 9: Verify against the real binary**

```bash
SB=$(mktemp -d)
export XDG_DATA_HOME="$SB/data"
go build -o "$SB/hive" .
mkdir -p "$SB/repo/docs" && git -C "$SB/repo" init -q
printf '# Open work\n\n<!-- TASKS:BEGIN (managed by hive) -->\n\n### Tasks\n\n- [ ] **kept** - body <!-- id:aaa -->\n<!-- TASKS:END -->\n' > "$SB/repo/docs/TODO.md"
cp "$SB/repo/docs/TODO.md" "$SB/before.md"
cd "$SB/repo" && "$SB/hive" todo list && "$SB/hive" todo add "new one - added after the move"
echo "--- repo file unchanged? ---"; diff "$SB/before.md" "$SB/repo/docs/TODO.md" && echo "UNCHANGED"
echo "--- store ---"; find "$SB/data" -name '*.md' -exec cat {} +
echo "--- git status clean? ---"; git -C "$SB/repo" status --porcelain
```
Expected: `UNCHANGED`; the store holds both tasks with `id:aaa` intact; `git status` shows only the untracked `docs/` fixture, never a modification.

- [ ] **Step 10: Commit**

```bash
git add todo.go todo_store.go todo_import.go todo_import_test.go todo_store_test.go
git commit -m "feat(todo): move the backlog store out of the repo

Importing each repo's TASKS block on first access, ids intact."
```

---

### Task 6: Adopt a store written under a weaker identity

Without this, `git remote add origin` silently orphans a backlog.

**Files:**
- Modify: `todo_import.go`
- Modify: `todo_import_test.go`

**Interfaces:**
- Consumes: `repoIdentity`, `todoStorePath`, `hiveDataDir`, `slugify`
- Produces: `func adoptStore(repoPath string) string` — path of a store found under a lower-precedence identity and renamed to the current one, or `""`

- [ ] **Step 1: Write the failing test**

Append to `todo_import_test.go`:

```go
// A repo that gains a remote re-keys. Its backlog must follow, not be orphaned.
func TestStoreIsAdoptedWhenTheRepoGainsARemote(t *testing.T) {
	repo := newTestRepo(t)
	gitInit(t, repo)

	if _, err := withTodos(repo, func(ts []Todo) []Todo {
		return addTodo(ts, "Now", "before the remote", "")
	}); err != nil {
		t.Fatal(err)
	}
	oldPath := todoStorePath(repo)
	id := loadTodos(repo)[0].ID

	gitRemote(t, repo, "https://github.com/x/y.git")
	newPath := todoStorePath(repo)
	if oldPath == newPath {
		t.Fatal("fixture is wrong: adding a remote should re-key the repo")
	}

	got := loadTodos(repo)
	if len(got) != 1 {
		t.Fatalf("got %d tasks after re-key, want 1 — the backlog was orphaned", len(got))
	}
	if got[0].ID != id {
		t.Errorf("id changed on adoption: %q -> %q", id, got[0].ID)
	}
	if _, err := os.Stat(newPath); err != nil {
		t.Errorf("store was not renamed to the new key: %v", err)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Error("the old store still exists — adoption copied instead of renaming")
	}
}

// Adoption is a rename, so it happens once. A second call finds the store where
// it now belongs and does no further work.
func TestAdoptionIsIdempotent(t *testing.T) {
	repo := newTestRepo(t)
	gitInit(t, repo)
	if _, err := withTodos(repo, func(ts []Todo) []Todo {
		return addTodo(ts, "Now", "task", "")
	}); err != nil {
		t.Fatal(err)
	}
	gitRemote(t, repo, "https://github.com/x/y.git")

	_ = loadTodos(repo)
	if got := adoptStore(repo); got != "" {
		t.Errorf("adoptStore = %q on a settled repo, want \"\"", got)
	}
	if got := loadTodos(repo); len(got) != 1 {
		t.Errorf("got %d tasks, want 1", len(got))
	}
}

// A legacy repo file must not win over a store that already exists under a
// weaker key — adoption is checked first.
func TestAdoptionBeatsLegacyImport(t *testing.T) {
	repo := newTestRepo(t)
	gitInit(t, repo)
	if _, err := withTodos(repo, func(ts []Todo) []Todo {
		return addTodo(ts, "Now", "from the store", "")
	}); err != nil {
		t.Fatal(err)
	}
	writeRepoBacklog(t, repo, "docs/TODO.md", "\n### Now\n\n- [ ] **from the repo file** <!-- id:zzz -->\n")
	gitRemote(t, repo, "https://github.com/x/y.git")

	got := loadTodos(repo)
	if len(got) != 1 || got[0].Subject != "from the store" {
		t.Errorf("got %+v, want the adopted store to win", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -run 'TestStoreIsAdopted|TestAdoption' . -v`
Expected: FAIL — `undefined: adoptStore`, and `TestStoreIsAdoptedWhenTheRepoGainsARemote` reports 0 tasks after re-key.

- [ ] **Step 3: Write minimal implementation**

Append to `todo_import.go`:

```go
// adoptStore finds a store written under a weaker identity than the repo now
// has and renames it to the current key, returning the new path. A repo that
// gains its first remote, or that moved before it had one, re-keys — and its
// backlog has to follow it, or the tasks are simply lost.
//
// Renaming means the walk costs one stat per tier once, not on every call.
func adoptStore(repoPath string) string {
	_, tier := repoIdentity(repoPath)
	want := todoStorePath(repoPath)
	main := mainWorktree(repoPath)
	slug := slugify(filepath.Base(main))

	for _, id := range weakerIdentities(repoPath, tier) {
		sum := sha256.Sum256([]byte(id))
		old := filepath.Join(hiveDataDir(), "todos", slug+"-"+hex.EncodeToString(sum[:4])+".md")
		if old == want {
			continue
		}
		if _, err := os.Stat(old); err != nil {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(want), 0o755); err != nil {
			return ""
		}
		if err := os.Rename(old, want); err != nil {
			return ""
		}
		fmt.Fprintf(os.Stderr, "hive: adopted backlog %s → %s\n", old, want)
		return want
	}
	return ""
}

// weakerIdentities lists the identities this repo would have resolved to under
// each tier below the one it holds now, strongest first.
func weakerIdentities(repoPath string, tier int) []string {
	main := mainWorktree(repoPath)
	var out []string
	if tier < tierFirstCommit {
		if o, err := exec.Command("git", "-C", main, "rev-list", "--max-parents=0", "HEAD").Output(); err == nil {
			if lines := strings.Fields(string(o)); len(lines) > 0 {
				out = append(out, lines[len(lines)-1])
			}
		}
	}
	if tier < tierPath {
		out = append(out, main)
	}
	return out
}
```

Add `"crypto/sha256"`, `"encoding/hex"`, `"os/exec"` and `"strings"` to
`todo_import.go`'s imports.

- [ ] **Step 4: Check adoption before importing**

In `readStore`, insert the adoption check between the store read and the legacy
lookup:

```go
func readStore(repoPath string) (string, bool) {
	path := todoStorePath(repoPath)
	if data, err := os.ReadFile(path); err == nil {
		return string(data), true
	}
	if adopted := adoptStore(repoPath); adopted != "" {
		if data, err := os.ReadFile(adopted); err == nil {
			return string(data), true
		}
	}
	src := legacyRepoFile(repoPath)
	...
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test -run 'TestStoreIsAdopted|TestAdoption' . -v`
Expected: PASS (3 tests)

- [ ] **Step 6: Run the whole suite**

Run: `go test ./...`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add todo_import.go todo_import_test.go
git commit -m "fix(todo): adopt a backlog when the repo's identity strengthens"
```

---

### Task 7: Memoise the key so the statusline stays cheap

`hive todo statusline` runs on every Claude turn and already spends two git
subprocesses. Key resolution would add a third on every turn.

**Files:**
- Modify: `repo_key.go`
- Modify: `repo_key_test.go`

**Interfaces:**
- Consumes: `repoIdentity`, `mainWorktree`
- Produces: unchanged `repoKey` signature; adds `func repoKeyMemoPath(main string) string`

- [ ] **Step 1: Write the failing test**

Append to `repo_key_test.go`:

```go
// The memo must be a cache, never a source of truth: deleting it changes
// nothing, and a corrupt one is ignored rather than believed.
func TestRepoKeyMemoIsOnlyACache(t *testing.T) {
	dir := newTestRepo(t)
	gitInit(t, dir)
	gitRemote(t, dir, "https://github.com/x/y.git")

	want := repoKey(dir)
	memo := repoKeyMemoPath(mainWorktree(dir))
	if _, err := os.Stat(memo); err != nil {
		t.Fatalf("first resolution wrote no memo: %v", err)
	}
	if got := repoKey(dir); got != want {
		t.Errorf("memoised key = %q, want %q", got, want)
	}

	if err := os.WriteFile(memo, []byte("not-a-key!!\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := repoKey(dir); got != want {
		t.Errorf("corrupt memo believed: got %q, want %q", got, want)
	}

	if err := os.Remove(memo); err != nil {
		t.Fatal(err)
	}
	if got := repoKey(dir); got != want {
		t.Errorf("key changed after deleting the memo: got %q, want %q", got, want)
	}
}

// A memo written for one repo must never be served to another.
func TestRepoKeyMemoIsPerRepo(t *testing.T) {
	a, b := newTestRepo(t), t.TempDir()
	gitInit(t, a)
	gitInit(t, b)
	gitRemote(t, a, "https://github.com/x/a.git")
	gitRemote(t, b, "https://github.com/x/b.git")
	if repoKey(a) == repoKey(b) {
		t.Error("two repos shared a memoised key")
	}
	if repoKeyMemoPath(mainWorktree(a)) == repoKeyMemoPath(mainWorktree(b)) {
		t.Error("two repos shared a memo path")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -run TestRepoKeyMemo . -v`
Expected: FAIL — `undefined: repoKeyMemoPath`

- [ ] **Step 3: Write minimal implementation**

In `repo_key.go`, replace `repoKey` with:

```go
// repoKey hashes a repo's identity into the token that names its store. The
// result is memoised in the runtime dir: statusline runs on every Claude turn
// and already spends two git subprocesses, so resolving the identity afresh
// each time is a cost paid on a hot path for an answer that does not change.
func repoKey(repoPath string) string {
	main := mainWorktree(repoPath)
	memo := repoKeyMemoPath(main)
	if data, err := os.ReadFile(memo); err == nil {
		if key := strings.TrimSpace(string(data)); isRepoKey(key) {
			return key
		}
	}
	id, _ := repoIdentity(repoPath)
	sum := sha256.Sum256([]byte(id))
	key := hex.EncodeToString(sum[:4])
	if err := os.MkdirAll(filepath.Dir(memo), 0o700); err == nil {
		_ = os.WriteFile(memo, []byte(key+"\n"), 0o600)
	}
	return key
}

// repoKeyMemoPath is where a resolved key is cached, addressed by the main
// worktree path — the one thing already known before resolution starts.
func repoKeyMemoPath(main string) string {
	sum := sha256.Sum256([]byte(main))
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "hive", "repokey-"+hex.EncodeToString(sum[:4]))
}

// isRepoKey guards against believing a truncated or corrupt memo.
func isRepoKey(s string) bool {
	if len(s) != 8 {
		return false
	}
	for _, c := range s {
		if !strings.ContainsRune("0123456789abcdef", c) {
			return false
		}
	}
	return true
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -run TestRepoKeyMemo . -v`
Expected: PASS (2 tests)

- [ ] **Step 5: Confirm adoption still works with the memo in play**

Adoption re-keys a repo, so a memo written before the re-key would pin the old
key. `adoptStore` must invalidate it. Add to `adoptStore`, immediately before
`want := todoStorePath(repoPath)`:

```go
	_ = os.Remove(repoKeyMemoPath(mainWorktree(repoPath)))
```

Run: `go test ./...`
Expected: PASS — in particular `TestStoreIsAdoptedWhenTheRepoGainsARemote`.

- [ ] **Step 6: Commit**

```bash
git add repo_key.go repo_key_test.go todo_import.go
git commit -m "perf(todo): memoise the repo key off the statusline's hot path"
```

---

### Task 8: Stop claiming the backlog is uncommitted

**Files:**
- Modify: `cmd_todo.go:166-174`
- Modify: `.gitignore:4`
- Test: `cmd_todo_edit_test.go`

**Interfaces:**
- Consumes: `todoFilePath`
- Produces: nothing new

- [ ] **Step 1: Write the failing test**

Append to `cmd_todo_edit_test.go`:

```go
// The store is hive's, not git's. Telling the user their add is "uncommitted"
// was true of a tracked file and is now simply wrong.
func TestAddDoesNotReportUncommitted(t *testing.T) {
	dir := newTestRepo(t)
	chdir(t, dir)

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdout
	os.Stdout = w
	rc := runTodoAdd([]string{"a task - a body"})
	os.Stdout = orig
	w.Close()

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil {
		t.Fatal(err)
	}
	out := buf.String()

	if rc != 0 {
		t.Fatalf("add returned %d", rc)
	}
	if strings.Contains(out, "uncommitted") {
		t.Errorf("add still reports the store as uncommitted:\n%s", out)
	}
	if !strings.Contains(out, todoFilePath(dir)) {
		t.Errorf("add did not name the store path:\n%s", out)
	}
}
```

Add `"bytes"`, `"os"` and `"strings"` to the import block.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -run TestAddDoesNotReportUncommitted . -v`
Expected: FAIL — output contains `(uncommitted)`

- [ ] **Step 3: Write minimal implementation**

In `cmd_todo.go`, replace the comment and print at lines 166-174:

```go
	// Name the file. The list lives on the main worktree so every worktree
	// shares one, which means adding from a branch leaves an uncommitted
	// change in a checkout the caller may not be looking at — enough to abort
	// a deploy pre-flight that insists on a clean tree.
	fmt.Printf("added %s: %s\n  %s (uncommitted)\n",
		todos[len(todos)-1].ID, subj, todoFilePath(cwd))
```

with:

```go
	// Name the store. Every worktree of a repo shares one, and it lives under
	// hive's data directory rather than in the repo, so an add leaves no mark
	// on the working tree at all.
	fmt.Printf("added %s: %s\n  %s\n",
		todos[len(todos)-1].ID, subj, todoFilePath(cwd))
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -run TestAddDoesNotReportUncommitted . -v`
Expected: PASS

- [ ] **Step 5: Prove the statusline keeps stdout clean**

The import notice runs on the statusline's path (Task 5, Step 5). Claude renders
this command's stdout directly into the prompt, so a notice landing there would
be visible corruption.

Append to `cmd_todo_edit_test.go`:

```go
// The first access to a repo imports its legacy backlog, and statusline is
// usually what gets there first. Its stdout is rendered into Claude's prompt,
// so the migration notice must go to stderr and nothing else may follow it.
func TestStatuslineImportNoticeGoesToStderr(t *testing.T) {
	dir := newTestRepo(t)
	writeRepoBacklog(t, dir, "docs/TODO.md", "\n### Now\n\n- [ ] **a task** <!-- id:aaa -->\n")

	stdinR, stdinW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		fmt.Fprintf(stdinW, `{"cwd":%q}`, dir)
		stdinW.Close()
	}()
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}

	origIn, origOut, origErr := os.Stdin, os.Stdout, os.Stderr
	os.Stdin, os.Stdout, os.Stderr = stdinR, outW, errW
	rc := runTodoStatusline()
	os.Stdin, os.Stdout, os.Stderr = origIn, origOut, origErr
	outW.Close()
	errW.Close()

	var out, errOut bytes.Buffer
	if _, err := out.ReadFrom(outR); err != nil {
		t.Fatal(err)
	}
	if _, err := errOut.ReadFrom(errR); err != nil {
		t.Fatal(err)
	}

	if rc != 0 {
		t.Fatalf("statusline returned %d", rc)
	}
	if strings.Contains(out.String(), "imported") {
		t.Errorf("the import notice reached stdout:\n%s", out.String())
	}
	if !strings.Contains(errOut.String(), "imported") {
		t.Errorf("the import notice did not reach stderr:\n%s", errOut.String())
	}
	if !strings.Contains(out.String(), "a task") {
		t.Errorf("stdout did not render the status line:\n%s", out.String())
	}
}
```

Add `"fmt"` to the import block. `writeRepoBacklog` comes from
`todo_import_test.go` — same package, no import needed.

Run: `go test -run TestStatuslineImportNoticeGoesToStderr . -v`
Expected: PASS. If it fails on stdout containing "imported", the notice in
`readStore` is using `fmt.Printf` rather than `fmt.Fprintf(os.Stderr, ...)`.

- [ ] **Step 6: Drop the obsolete ignore rule**

Hive's atomic-write temp files are now written beside the store, never in a
repo. Remove line 4 of `.gitignore`:

```
.TODO.md.*.tmp
```

- [ ] **Step 7: Run the whole suite**

Run: `go test ./...`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add cmd_todo.go cmd_todo_edit_test.go .gitignore
git commit -m "fix(todo): stop describing the store as an uncommitted repo file"
```

---

### Task 9: Documentation

**Files:**
- Modify: `docs/claude-commands/todo.md` (lines 3, 9-10, 42-44)
- Modify: `~/.claude/commands/todo.md` (live mirror — copy, not a symlink)
- Modify: `docs/claude-commands/next.md` (reconcile with `~/.claude/commands/next.md`, which has drifted)
- Modify: `docs/superpowers/specs/2026-08-07-todo-concurrency-design.md:57-59`

**Interfaces:**
- Consumes: the behaviour built in Tasks 1-8
- Produces: nothing code-facing

- [ ] **Step 1: Rewrite the storage description in `docs/claude-commands/todo.md`**

Line 3 (the frontmatter `description:`) currently reads:

```
description: Add / curate this repo's hive task list (the docs/TODO.md TASKS block on the main worktree) — the list shown in the hive drawer and the Claude statusline.
```

Replace with:

```
description: Add / curate this repo's hive task list (stored by hive outside the repo) — the list shown in the hive drawer and the Claude statusline.
```

Lines 42-44 currently read:

```
sections and rendered `- [box] **subject** — description` in the `docs/TODO.md`
(or `TODO.md`) `TASKS:BEGIN/END` block on the main worktree; content outside
that block is left untouched.
```

Replace with:

```
sections and stored by hive under `~/.local/share/hive/todos/`, keyed by the
repo's git remote. It is not in the repo and not in git: every worktree of a
repo shares one backlog, and adding a task leaves the working tree untouched.
```

Lines 8-11 currently read:

```
Manage the local per-repo task list backed by the `hive todo` CLI. The list is
the `TASKS:BEGIN/END` block in `docs/TODO.md` (or `TODO.md`) on the repo's
**main worktree** (shared across all worktrees/branches); `hive todo` resolves
it from the current directory, so just run the commands — no path handling needed.
```

Replace with:

```
Manage the local per-repo task list backed by the `hive todo` CLI. The list is
stored by hive outside the repo, keyed by the repo's git remote and shared
across all its worktrees and branches; `hive todo` resolves it from the current
directory, so just run the commands — no path handling needed.
```

Then add this rule to the Rules list:

```
- **Never edit the store by hand.** It is hive's file, not the repo's, and
  nothing outside `hive todo` is expected to write it. If a verb misbehaves,
  say so on the bus (`hive_bus_ask` / `hive_bus_intent`) and let it be fixed —
  a hand-edit hides the bug from everyone else who is hitting it too.
```

- [ ] **Step 2: Mirror it**

```bash
cp docs/claude-commands/todo.md ~/.claude/commands/todo.md
diff docs/claude-commands/todo.md ~/.claude/commands/todo.md && echo IDENTICAL
```
Expected: `IDENTICAL`

- [ ] **Step 3: Reconcile the drifted `next.md`**

```bash
diff docs/claude-commands/next.md ~/.claude/commands/next.md
```

The live copy is authoritative — it is what runs. Copy it back into the repo,
then re-read it for any storage-location prose and fix that too:

```bash
cp ~/.claude/commands/next.md docs/claude-commands/next.md
grep -n 'TODO\.md\|main worktree' docs/claude-commands/next.md
```
Expected after the fix: no hits, or only hits that describe the CLI.

- [ ] **Step 4: Close the deferred question in the 2026-08-07 spec**

At `docs/superpowers/specs/2026-08-07-todo-concurrency-design.md:57-59`, the
non-goal reads:

```
- Whether `TODO.md` should be git-tracked, and the churn/deploy consequences of
  it living in the main worktree's dirty tree. Real, separate, deliberately out
  of scope.
```

Append to it:

```
  **Resolved 2026-08-25** by `2026-08-25-todo-store-out-of-repo-design.md`: the
  store moved out of the repos entirely.
```

- [ ] **Step 5: Verify the whole thing once more**

Run: `go test ./... && gofmt -l .`
Expected: all packages PASS, `gofmt -l` prints nothing.

- [ ] **Step 6: Commit**

```bash
git add docs/claude-commands/todo.md docs/claude-commands/next.md \
        docs/superpowers/specs/2026-08-07-todo-concurrency-design.md
git commit -m "docs: describe the backlog store's new home"
```

---

## After the plan

P1 is complete and hive owns the interface. Two follow-ups, both out of scope
here and recorded in the spec:

1. **The P2 broadcast.** Tell every session to delete `docs/TODO.md` and the
   machinery built around it — `settle_tree()` in
   `stevenlawton.com/scripts/deploy-watch.sh:99-111`, the exemption in
   `scripts/gate.sh:38`, the cases in `scripts/test-deploy-watch.sh`, the
   stash/rebase choreography in both repos' `main-deploy-loop` skills, and the
   `Bash(git ... docs/TODO.md)` permission in
   `he-events/.claude/settings.local.json`. None of it breaks before it is
   removed; it simply goes quiet.
2. **Durability.** 251 tasks now live in exactly one unbacked-up place. This
   should not outlive P2.
