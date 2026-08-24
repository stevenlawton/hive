package main

import (
	"strings"
	"testing"
	"time"

	"github.com/stevenlawton/hive/ui"
)

func worktreeChordModel() model {
	m := model{workspace: ui.NewWorkspaceView(), chord: NewChordHandler(time.Second), width: 200}
	m.workspace.OpenTab("proj", "proj", "hive-proj", "main")
	m.items = []repoItem{{repo: Repo{DirName: "proj", Short: "proj"}, tmuxSes: "hive-proj"}}
	return m
}

// ^Space w makes a standalone worktree: v/h attach one to the tab as a split,
// so w is the one that leaves wtSplitMode off and lands back in the manager.
func TestChordWorktreeOpensStandaloneForm(t *testing.T) {
	m := worktreeChordModel()
	got, _ := m.handleChordAction(ChordWorktree)
	nm := got.(model)

	if nm.mode != viewWorktree {
		t.Errorf("got mode %v, want viewWorktree", nm.mode)
	}
	if nm.wtSplitMode {
		t.Error("wtSplitMode is set — w must not attach the worktree as a split")
	}
	if nm.wtParent != "proj" {
		t.Errorf("got parent %q, want \"proj\"", nm.wtParent)
	}
	if len(nm.wtFields) != wtFieldCount {
		t.Fatalf("form not built: %d fields", len(nm.wtFields))
	}
	if nm.wtFields[wtFieldBranch].Value() == "" {
		t.Error("branch field has no default")
	}
}

// A worktree tab resolves to its parent — you cannot branch a worktree.
func TestChordWorktreeFromWorktreeTabUsesParent(t *testing.T) {
	m := worktreeChordModel()
	m.workspace.OpenTab("proj-wt-a", "proj/a", "hive-proj-wt-a", "main")
	m.items = append(m.items, repoItem{
		repo: Repo{DirName: "proj-wt-a", IsWorktree: true, Parent: "proj",
			WorktreeBranch: "a"}, tmuxSes: "hive-proj-wt-a"})

	got, _ := m.handleChordAction(ChordWorktree)
	if nm := got.(model); nm.wtParent != "proj" {
		t.Errorf("got parent %q, want \"proj\" (not the worktree itself)", nm.wtParent)
	}
}

func TestChordWorktreeWithNoTabErrors(t *testing.T) {
	m := model{workspace: ui.NewWorkspaceView(), chord: NewChordHandler(time.Second)}
	got, _ := m.handleChordAction(ChordWorktree)
	nm := got.(model)
	if nm.mode == viewWorktree {
		t.Error("opened the form with no active tab")
	}
	if nm.err == nil {
		t.Error("no error reported for a missing tab")
	}
}

func TestChordHintsOfferWorktree(t *testing.T) {
	if got := newHintTestModel(1).renderWorkspaceStatusBar(); !strings.Contains(got, "w:worktree") {
		t.Errorf("chord hints omit w:worktree:\n%s", got)
	}
}
