package main

import "testing"

func showFixture() []Todo {
	return []Todo{
		{ID: "aaa", Subject: "claimed one", Claim: "split-1"},
		{ID: "sgx", Subject: "the one you asked for", Description: "its body"},
		{ID: "ccc", Subject: "done one", Done: true, Claim: "split-1"},
	}
}

// The reported bug: `hive todo show <id>` took no arguments at all, so it
// silently dropped the ref and printed whatever this worktree had claimed —
// a real task, with nothing to signal the wrong one was being shown.
func TestResolveTodoForShowHonoursTheRef(t *testing.T) {
	got, ok := resolveTodoForShow(showFixture(), "split-1", "sgx")
	if !ok {
		t.Fatal("an explicit ref should resolve")
	}
	if got.ID != "sgx" {
		t.Errorf("got %q (%s), want the ref'd task sgx", got.ID, got.Subject)
	}
}

func TestResolveTodoForShowFallsBackToTheClaim(t *testing.T) {
	got, ok := resolveTodoForShow(showFixture(), "split-1", "")
	if !ok {
		t.Fatal("no ref should fall back to this worktree's claim")
	}
	if got.ID != "aaa" {
		t.Errorf("got %q, want the claimed task aaa", got.ID)
	}
}

func TestResolveTodoForShowUnknownRefIsNotTheClaim(t *testing.T) {
	// Falling back to the claim here would reproduce the bug: the caller
	// asked for a specific task and must not be shown a different one.
	if _, ok := resolveTodoForShow(showFixture(), "split-1", "zzz"); ok {
		t.Error("an unknown ref must fail, not silently show the claimed task")
	}
}

func TestResolveTodoForShowNoClaim(t *testing.T) {
	if _, ok := resolveTodoForShow(showFixture(), "", ""); ok {
		t.Error("no claim and no ref should resolve to nothing")
	}
}

func TestResolveTodoForShowSkipsDoneClaims(t *testing.T) {
	only := []Todo{{ID: "ccc", Subject: "done", Done: true, Claim: "split-1"}}
	if _, ok := resolveTodoForShow(only, "split-1", ""); ok {
		t.Error("a completed task should not count as the current claim")
	}
}

func TestResolveTodoForShowByPosition(t *testing.T) {
	got, ok := resolveTodoForShow(showFixture(), "split-1", "2")
	if !ok {
		t.Fatal("a positional ref should still resolve")
	}
	if got.ID != "sgx" {
		t.Errorf("position 2 should be sgx, got %q", got.ID)
	}
}
