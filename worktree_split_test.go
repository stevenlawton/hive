package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stevenlawton/hive/ui"
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

func TestOpenWorktreeAddsSplitWhenParentTabExists(t *testing.T) {
	parent := Repo{DirName: "proj", Short: "proj"}
	wt := Repo{DirName: "proj-wt-a", Short: "proj/a", IsWorktree: true,
		WorktreeBranch: "a", Parent: "proj"}

	m := model{
		workspace: ui.NewWorkspaceView(),
		items: []repoItem{
			{repo: parent, tmuxSes: "hive-proj"},
			{repo: wt, tmuxSes: "hive-proj-wt-a"},
		},
	}

	m.openAsTab(parent, "hive-proj")
	m.openAsTab(wt, "hive-proj-wt-a")

	tab := m.workspace.Tabs["proj"]
	if tab == nil {
		t.Fatal("no parent tab")
	}
	if got := len(tab.SplitPane.Splits); got != 2 {
		t.Fatalf("got %d splits, want 2 (main + wt:a)", got)
	}
	if got := tab.SplitPane.Splits[1].SessionName; got != "hive-proj-wt-a" {
		t.Errorf("got %q, want \"hive-proj-wt-a\"", got)
	}
}

func TestRebuildWorkspaceTabsIsIdempotent(t *testing.T) {
	// A detach fires reconnectMsg, which rebuilds the tabs again. The second
	// pass must not re-add splits that are already on the parent tab.
	m := model{
		workspace: ui.NewWorkspaceView(),
		items: []repoItem{
			{repo: Repo{DirName: "proj", Short: "proj"}, tmuxSes: "hive-proj"},
			{repo: Repo{DirName: "proj-wt-a", Short: "proj/a", IsWorktree: true,
				WorktreeBranch: "a", Parent: "proj"}, tmuxSes: "hive-proj-wt-a"},
			{repo: Repo{DirName: "proj-wt-b", Short: "proj/b", IsWorktree: true,
				WorktreeBranch: "b", Parent: "proj"}, tmuxSes: "hive-proj-wt-b"},
		},
	}

	m.rebuildWorkspaceTabs()
	first := len(m.workspace.Tabs["proj"].SplitPane.Splits)
	if first != 3 {
		t.Fatalf("got %d splits on the first pass, want 3 (main + wt:a + wt:b)", first)
	}

	m.rebuildWorkspaceTabs()
	if got := len(m.workspace.Tabs["proj"].SplitPane.Splits); got != first {
		t.Errorf("got %d splits after a second rebuild, want %d", got, first)
	}
}
