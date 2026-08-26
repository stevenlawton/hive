package main

import (
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
