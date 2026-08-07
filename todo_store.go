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
