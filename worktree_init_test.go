package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The script is found by name in the PARENT checkout. Absence is not an error,
// and only a regular file counts.
func TestHasWorktreeInitScript(t *testing.T) {
	empty := t.TempDir()
	if hasWorktreeInitScript(empty) {
		t.Errorf("empty dir: got true, want false")
	}

	noScript := t.TempDir()
	if err := os.MkdirAll(filepath.Join(noScript, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if hasWorktreeInitScript(noScript) {
		t.Errorf("scripts/ without wt-init.sh: got true, want false")
	}

	// Mode 0o644 on purpose: the executable bit is NOT required. hive invokes
	// the script as `bash <path>`, so the bit is irrelevant, and requiring it
	// would make the feature fail silently on a checkout where it was lost.
	plainFile := t.TempDir()
	if err := os.MkdirAll(filepath.Join(plainFile, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(plainFile, "scripts", "wt-init.sh"), []byte("#!/usr/bin/env bash\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !hasWorktreeInitScript(plainFile) {
		t.Errorf("non-executable regular scripts/wt-init.sh: got false, want true (the executable bit must not be required)")
	}

	asDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(asDir, "scripts", "wt-init.sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	if hasWorktreeInitScript(asDir) {
		t.Errorf("scripts/wt-init.sh as a directory: got true, want false")
	}
}

// The no-op-if-absent guarantee: with no script in the parent, hive types
// exactly what it types today, byte for byte. Exact equality, never Contains.
func TestWorktreeLaunchLineNoScript(t *testing.T) {
	claude := "env -u X claude --permission-mode bypassPermissions"

	enabled := worktreeLaunch{
		ScriptPresent: false,
		Enabled:       true,
		ParentPath:    "/home/steve/repos/he-events",
		Branch:        "split-1",
		DirName:       "he-events",
		ClaudeCmd:     claude,
	}
	if got := worktreeLaunchLine(enabled); got != claude {
		t.Errorf("no script + enabled:\n got  %q\n want %q", got, claude)
	}

	disabled := enabled
	disabled.Enabled = false
	if got := worktreeLaunchLine(disabled); got != claude {
		t.Errorf("no script + disabled:\n got  %q\n want %q", got, claude)
	}
}

// Exact equality, because the quoting and the " ; " spacing are the contract.
func TestWorktreeLaunchLineRunsScript(t *testing.T) {
	w := worktreeLaunch{
		ScriptPresent: true,
		Enabled:       true,
		ParentPath:    "/home/steve/repos/he-events",
		Branch:        "split-1",
		DirName:       "he-events",
		ClaudeCmd:     "env -u X claude",
	}
	want := `bash '/home/steve/repos/he-events/scripts/wt-init.sh' '/home/steve/repos/he-events' 'split-1' ; env -u X claude`
	if got := worktreeLaunchLine(w); got != want {
		t.Errorf("launch line mismatch:\n got  %q\n want %q", got, want)
	}
}

// The security assertion: a repo-controlled script must NOT execute without the
// flag. The pane gets an actionable notice naming the workspace, and nothing
// else changes.
func TestWorktreeLaunchLineDisabledNotice(t *testing.T) {
	w := worktreeLaunch{
		ScriptPresent: true,
		Enabled:       false,
		ParentPath:    "/home/steve/repos/he-events",
		Branch:        "split-1",
		DirName:       "he-events",
		ClaudeCmd:     "env -u X claude",
	}
	got := worktreeLaunchLine(w)

	if strings.Contains(got, "bash ") {
		t.Errorf("disabled line must not invoke bash, got %q", got)
	}
	if strings.Contains(got, "wt-init.sh'") {
		t.Errorf("disabled line must not pass wt-init.sh as a command, got %q", got)
	}
	if !strings.Contains(got, w.DirName) {
		t.Errorf("disabled line should name the workspace %q, got %q", w.DirName, got)
	}
	if !strings.HasSuffix(got, w.ClaudeCmd) {
		t.Errorf("disabled line should end with ClaudeCmd %q, got %q", w.ClaudeCmd, got)
	}
}

// A branch name is unsanitised user input. POSIX single-quoting, with an
// embedded quote escaped the shellQuote way, must keep it inside its own
// argument.
func TestWorktreeLaunchLineQuotesHostileInputs(t *testing.T) {
	w := worktreeLaunch{
		ScriptPresent: true,
		Enabled:       true,
		ParentPath:    "/tmp/it's here",
		Branch:        "a'b",
		DirName:       "hostile",
		ClaudeCmd:     "claude",
	}
	got := worktreeLaunchLine(w)

	wantScript := `'/tmp/it'\''s here/scripts/wt-init.sh'`
	if !strings.Contains(got, wantScript) {
		t.Errorf("script path not shell-quoted:\n got  %q\n want it to contain %q", got, wantScript)
	}
	wantBranch := `'a'\''b'`
	if !strings.Contains(got, wantBranch) {
		t.Errorf("branch not shell-quoted:\n got  %q\n want it to contain %q", got, wantBranch)
	}
}

// tmux invariant: tmuxSendKeysArgs passes the command as one argv element, and
// tmux treats an argv element ending in an unescaped ';' as a command
// terminator. The line is safe only because it always ends with ClaudeCmd,
// which never ends in ';'. This pins that for all three shapes.
func TestWorktreeLaunchLineEndsWithClaude(t *testing.T) {
	base := worktreeLaunch{
		ParentPath: "/home/steve/repos/he-events",
		Branch:     "split-1",
		DirName:    "he-events",
		ClaudeCmd:  "env -u X claude",
	}

	absent := base
	absent.ScriptPresent = false

	enabled := base
	enabled.ScriptPresent = true
	enabled.Enabled = true

	disabled := base
	disabled.ScriptPresent = true
	disabled.Enabled = false

	cases := []struct {
		name string
		w    worktreeLaunch
	}{
		{"script absent", absent},
		{"script present, enabled", enabled},
		{"script present, disabled", disabled},
	}
	for _, c := range cases {
		got := worktreeLaunchLine(c.w)
		if !strings.HasSuffix(got, c.w.ClaudeCmd) {
			t.Errorf("%s: line must end with ClaudeCmd %q, got %q", c.name, c.w.ClaudeCmd, got)
		}
	}
}
