package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNextSplitBranchOnEmptyRepo(t *testing.T) {
	if got := nextSplitBranch(t.TempDir()); got != "split-1" {
		t.Errorf("got %q, want \"split-1\"", got)
	}
}

func TestNextSplitBranchSkipsExisting(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"split-1", "split-2"} {
		if err := os.MkdirAll(filepath.Join(dir, ".worktrees", n), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if got := nextSplitBranch(dir); got != "split-3" {
		t.Errorf("got %q, want \"split-3\"", got)
	}
}

func TestNextSplitBranchFillsGaps(t *testing.T) {
	// split-2 removed: the next worker reuses the free slot rather than
	// climbing forever.
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".worktrees", "split-1"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".worktrees", "split-3"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := nextSplitBranch(dir); got != "split-2" {
		t.Errorf("got %q, want \"split-2\"", got)
	}
}

func TestNextWorkerPromptIsTheSlashCommand(t *testing.T) {
	if nextWorkerPrompt != "/next" {
		t.Errorf("got %q, want \"/next\"", nextWorkerPrompt)
	}
}
