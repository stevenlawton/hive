package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
	existed := false
	if data, err := os.ReadFile(path); err == nil {
		existing = string(data)
		existed = true
	}

	todos := backfillIDs(mutate(backfillIDs(parseTodos(extractBlock(existing)))))
	rendered := replaceBlock(existing, formatTodos(todos))
	if stripSyncLine(rendered) == stripSyncLine(existing) {
		return todos, nil
	}
	if !existed && len(todos) == 0 {
		return todos, nil
	}
	if err := writeTodoFile(path, rendered); err != nil {
		return todos, err
	}
	return todos, nil
}

// stripSyncLine drops the generated "Last sync" line. formatTodos stamps it with
// today's date on every render, so comparing without it is what distinguishes a
// real content change from the mere passing of midnight — otherwise the first
// write of each day would rewrite a git-tracked file nobody touched. Comparing
// both sides with it removed is safe: replaceBlock preserves everything outside
// the markers verbatim, so the two strings can only differ inside the block.
func stripSyncLine(s string) string {
	lines := strings.Split(s, "\n")
	out := lines[:0]
	for _, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), "Last sync: **") {
			continue
		}
		out = append(out, l)
	}
	return strings.Join(out, "\n")
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
