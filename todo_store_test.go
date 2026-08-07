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

// A mutate that changes nothing must not rewrite the file: no mtime bump, no
// spurious trigger for mtime-based watchers or rsync-style deploy syncs. We
// compare inode identity (os.SameFile) rather than mtime, since a rename can
// land within the same mtime tick on coarser filesystems and produce a false
// pass on the unfixed code; inode identity flags the rewrite unconditionally.
func TestWithTodosNoopMutateDoesNotRewrite(t *testing.T) {
	dir := t.TempDir()
	if _, err := withTodos(dir, func(ts []Todo) []Todo {
		return addTodo(ts, "Tasks", "existing", "")
	}); err != nil {
		t.Fatal(err)
	}
	path := todoFilePath(dir)
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := withTodos(dir, func(ts []Todo) []Todo { return ts }); err != nil {
		t.Fatal(err)
	}

	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) {
		t.Error("no-op mutate rewrote the file (new inode) on the second call")
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
