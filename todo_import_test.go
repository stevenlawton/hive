package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
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
