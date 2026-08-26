package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
	if _, tier := repoIdentity(dir); tier != tierPath {
		t.Errorf("non-git dir: tier = %d, want %d (path)", tier, tierPath)
	}

	gitInit(t, dir)
	id, tier := repoIdentity(dir)
	if tier != tierFirstCommit {
		t.Errorf("git dir with no remote: tier = %d, want %d (first commit)", tier, tierFirstCommit)
	}
	if len(id) != 40 {
		t.Errorf("first-commit identity = %q, want a 40-char sha", id)
	}

	gitRemote(t, dir, "git@github.com:stevenlawton/hive.git")
	id, tier = repoIdentity(dir)
	if tier != tierRemote {
		t.Errorf("git dir with a remote: tier = %d, want %d (remote)", tier, tierRemote)
	}
	if id != "github.com/stevenlawton/hive" {
		t.Errorf("remote identity = %q", id)
	}
}

// Moving a repo must not change its key: the key addresses the backlog.
func TestRepoKeySurvivesAMove(t *testing.T) {
	dir := newTestRepo(t)
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
	a, b := newTestRepo(t), t.TempDir()
	gitInit(t, a)
	gitInit(t, b)
	gitRemote(t, a, "https://github.com/upstream/proj.git")
	gitRemote(t, b, "https://github.com/fork/proj.git")
	if repoKey(a) == repoKey(b) {
		t.Error("fork and upstream resolved to the same key")
	}
}

func TestRepoKeyIsEightHexChars(t *testing.T) {
	key := repoKey(newTestRepo(t))
	if len(key) != 8 {
		t.Fatalf("key = %q, want 8 chars", key)
	}
	for _, c := range key {
		if !strings.ContainsRune("0123456789abcdef", c) {
			t.Fatalf("key = %q, want lowercase hex", key)
		}
	}
}

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
