package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
	if adopted := adoptStore(repoPath); adopted != "" {
		if data, err := os.ReadFile(adopted); err == nil {
			return string(data), true
		}
	}
	src := legacyRepoFile(repoPath)
	if src == "" {
		return "", false
	}
	block := legacyBlock(src)
	// stderr, not stdout: `hive todo statusline` renders stdout into the prompt.
	fmt.Fprintf(os.Stderr, "hive: imported %d task(s) from %s into %s\n",
		len(parseTodos(block)), src, path)
	return replaceBlock("", block), true
}

// adoptStore finds a store written under a weaker identity than the repo now
// has and renames it to the current key, returning the new path. A repo that
// gains its first remote, or that moved before it had one, re-keys — and its
// backlog has to follow, or the tasks are simply lost.
//
// Renaming means the walk costs one stat per tier once, not on every call.
func adoptStore(repoPath string) string {
	_, tier := repoIdentity(repoPath)
	_ = os.Remove(repoKeyMemoPath(mainWorktree(repoPath))) // the memo pins the old key
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
