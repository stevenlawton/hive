package main

import (
	"fmt"
	"os"
	"path/filepath"
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
