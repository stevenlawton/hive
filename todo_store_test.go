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
