package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// storeGitInit turns the store directory into a git repo, the way `git init`
// would once a human has opted in.
func storeGitInit(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(hiveDataDir(), "todos")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "Test"},
	} {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	return dir
}

func storeCommits(t *testing.T, dir string) int {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "rev-list", "--all", "--count").Output()
	if err != nil {
		return 0 // no commits yet
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		t.Fatalf("unreadable commit count %q: %v", out, err)
	}
	return n
}

// A write into a git-backed store must land as a commit, so the backlog has the
// history and the backup that leaving the repos took away.
func TestBackupCommitsAStoreWrite(t *testing.T) {
	repo := newTestRepo(t)
	dir := storeGitInit(t)

	if _, err := withTodos(repo, func(ts []Todo) []Todo {
		return addTodo(ts, "Tasks", "first", "")
	}); err != nil {
		t.Fatal(err)
	}

	if n := storeCommits(t, dir); n != 1 {
		t.Fatalf("got %d commits after one write, want 1", n)
	}
	out, err := exec.Command("git", "-C", dir, "show", "--stat", "--oneline", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), ".md") {
		t.Errorf("the commit does not contain a store file:\n%s", out)
	}
}

// Opt-in: with no git repo in the store directory hive must behave exactly as
// it did before, and must not create one behind the user's back.
func TestBackupIsANoOpWithoutAGitRepo(t *testing.T) {
	repo := newTestRepo(t)

	if _, err := withTodos(repo, func(ts []Todo) []Todo {
		return addTodo(ts, "Tasks", "first", "")
	}); err != nil {
		t.Fatalf("write failed with no git repo present: %v", err)
	}

	dir := filepath.Join(hiveDataDir(), "todos")
	if err := exec.Command("git", "-C", dir, "rev-parse", "--git-dir").Run(); err == nil {
		t.Error("a git repo was created in the store directory without being asked for")
	}
}

// A read changes no bytes, so it must not manufacture an empty commit.
func TestBackupSkipsWhenNothingChanged(t *testing.T) {
	repo := newTestRepo(t)
	dir := storeGitInit(t)

	if _, err := withTodos(repo, func(ts []Todo) []Todo {
		return addTodo(ts, "Tasks", "first", "")
	}); err != nil {
		t.Fatal(err)
	}
	before := storeCommits(t, dir)

	_ = loadTodos(repo)
	if _, err := withTodos(repo, func(ts []Todo) []Todo { return ts }); err != nil {
		t.Fatal(err)
	}

	if after := storeCommits(t, dir); after != before {
		t.Errorf("commits went %d -> %d on a no-op; a read should commit nothing", before, after)
	}
}

// The push is fire-and-forget by design: an unreachable remote must not slow,
// warn, or fail a todo command, and the commit must still land locally.
func TestBackupSurvivesAnUnreachableRemote(t *testing.T) {
	repo := newTestRepo(t)
	dir := storeGitInit(t)
	cmd := exec.Command("git", "-C", dir, "remote", "add", "origin",
		"https://127.0.0.1:1/nope.git")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("remote add: %v: %s", err, out)
	}

	if _, err := withTodos(repo, func(ts []Todo) []Todo {
		return addTodo(ts, "Tasks", "first", "")
	}); err != nil {
		t.Fatalf("an unreachable remote failed the command: %v", err)
	}
	if n := storeCommits(t, dir); n != 1 {
		t.Errorf("got %d commits, want 1 — the commit must land even when the push cannot", n)
	}
}
