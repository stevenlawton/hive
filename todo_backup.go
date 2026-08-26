package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

// backupStore commits the store directory and fires a push at whatever remote
// it has. Taking the backlog out of the repos also took away `git push` as its
// backup; this gives it one of its own.
//
// It is opt-in. With no git repo in the store directory it does nothing, so
// hive behaves exactly as before for anyone who never runs `git init` there.
//
// The commit is synchronous: it is local, it is fast, and it cannot meaningfully
// fail. The push is fired and forgotten — a dead network must never slow or
// break a todo command, least of all on the statusline's path, which runs on
// every Claude turn. A push that fails is simply retried by the next write.
func backupStore() {
	dir := filepath.Join(hiveDataDir(), "todos")
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		return
	}

	// A lock of its own. The backlog locks are per repo, but every repo's store
	// shares this one directory, so two repos writing at once would otherwise
	// race in the same git index.
	unlock, err := lockStoreGit()
	if err != nil {
		return
	}
	defer unlock()

	if gitQuiet(dir, "add", "-A") != nil {
		return
	}
	// Nothing staged: the write changed no bytes, or a peer committed it first.
	if gitQuiet(dir, "diff", "--cached", "--quiet") == nil {
		return
	}
	if gitQuiet(dir, "commit", "-q", "-m", "backlog: sync") != nil {
		return
	}

	push := exec.Command("git", "-C", dir, "push", "--quiet")
	if push.Start() != nil {
		return
	}
	// Reap it rather than leave a zombie behind the long-running TUI. The CLI
	// usually exits first, which orphans the push and lets init reap it — either
	// way its result is deliberately nobody's problem.
	go func() { _ = push.Wait() }()
}

// gitQuiet runs a git command in dir, discarding its output.
func gitQuiet(dir string, args ...string) error {
	return exec.Command("git", append([]string{"-C", dir}, args...)...).Run()
}

// lockStoreGit serialises access to the store directory's git index across
// every repo hive manages. Runtime dir, like the backlog locks: it should not
// survive a reboot.
func lockStoreGit() (func(), error) {
	noop := func() {}
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir = os.TempDir()
	}
	path := filepath.Join(dir, "hive", "todos-git.lock")
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
	return func() { f.Close() }, nil
}
