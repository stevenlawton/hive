package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

// The modal should pre-fill the branch with the next free wt-N so the user
// can just hit enter, and never collide with an existing worktree.
func TestDefaultWorktreeBranch(t *testing.T) {
	dir := t.TempDir()
	if got := defaultWorktreeBranch(dir); got != "wt-1" {
		t.Fatalf("empty repo: got %q, want wt-1", got)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".worktrees", "wt-1"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := defaultWorktreeBranch(dir); got != "wt-2" {
		t.Fatalf("with wt-1 present: got %q, want wt-2", got)
	}
}

var wtAnsiRe = regexp.MustCompile("\x1b\\[[0-9;<]*[A-Za-z]")

func wtStrip(s string) string { return wtAnsiRe.ReplaceAllString(s, "") }

// Typing into the worktree form must actually land in the focused field.
// The bug: handleWorktreeKey forwarded an empty tea.KeyPressMsg{} to the
// input, so nothing could be typed, the branch stayed empty, and ctrl+s
// failed with "branch name required" — i.e. no worktree could be created.
func TestWorktreeFormAcceptsTypedInput(t *testing.T) {
	m := &model{mode: viewWorktree, width: 120}
	m.wtFields = []textinput.Model{
		newWorktreeField("Branch: ", "feature-name", ""),
		newWorktreeField("Prompt: ", "optional task for Claude", ""),
	}
	m.wtFocus = 0
	m.wtFields[0].Focus()
	for _, r := range "my-feature" {
		m.handleWorktreeKey(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	if got := m.wtFields[0].Value(); got != "my-feature" {
		t.Fatalf("typing into the branch field should work, got %q", got)
	}
}

// A worktree input with only a placeholder must render the WHOLE placeholder,
// not a single character. bubbles' textinput collapses the placeholder to one
// char when Width is 0 (the default) — that's the "borked modal" the user saw,
// where empty fields showed "Branch: f" / "Prompt: o".
func TestWorktreeFieldRendersFullPlaceholder(t *testing.T) {
	ti := newWorktreeField("Branch: ", "feature-name", "")
	got := wtStrip(ti.View())
	if !strings.Contains(got, "feature-name") {
		t.Fatalf("placeholder should render in full, got %q", got)
	}
	if worktreeFieldWidth < len("feature-name") {
		t.Errorf("field width %d too small for placeholder", worktreeFieldWidth)
	}
}
