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
