package main

import (
	"os"
	"path/filepath"
)

// worktreeInitScriptRel is the repo-relative path hive looks for. It follows
// the scripts/gate.sh convention used by he-events and stevenlawton.com: a
// repo-local script found by name, whose absence is not an error.
const worktreeInitScriptRel = "scripts/wt-init.sh"

// hasWorktreeInitScript reports whether repoPath contains a wt-init script.
// It requires a regular file; a directory, a dangling symlink, or an absent
// path all report false.
//
// The executable bit is deliberately NOT checked: hive invokes the script as
// `bash <path>`, so the bit is irrelevant, and requiring it would make the
// feature fail silently on a checkout where it was lost.
//
// Callers MUST pass the PARENT checkout path, never the new worktree. Trust is
// granted per repo, so the script that runs has to be the one in the checkout
// the human reviewed, not one that arrived on a fetched branch.
func hasWorktreeInitScript(repoPath string) bool {
	fi, err := os.Stat(filepath.Join(repoPath, worktreeInitScriptRel))
	return err == nil && fi.Mode().IsRegular()
}

// worktreeLaunch is the input to worktreeLaunchLine. A struct rather than
// positional parameters because the call site in createWorktree is not covered
// by any test, so a swapped pair of adjacent strings would compile, pass the
// suite, and ship.
type worktreeLaunch struct {
	ScriptPresent bool   // parent has scripts/wt-init.sh
	Enabled       bool   // workspace has worktree_init set
	ParentPath    string // absolute path of the parent checkout
	Branch        string // new branch name
	DirName       string // workspace dir name, for the notice text only
	ClaudeCmd     string // fully-built claude invocation
}

// worktreeLaunchLine builds the single line hive types into the new worktree's
// tmux pane. ClaudeCmd always ends the line.
//
//	present && enabled  -> "bash '<parent>/scripts/wt-init.sh' '<parent>' '<branch>' ; <ClaudeCmd>"
//	present && !enabled -> "echo '<notice>' ; <ClaudeCmd>"
//	!present            -> ClaudeCmd, returned unchanged
//
// Two invariants this shape depends on, both previously true by luck:
//
//  1. One TmuxSendKeys call, one line. Because bash receives the whole "a ; b"
//     list in a single read, it parses the claude invocation into its command
//     list before running the bootstrap. A four-minute composer install
//     therefore does not leave claude sitting in the tty input buffer.
//     Splitting this into two TmuxSendKeys calls would reintroduce exactly
//     that race.
//  2. tmux argv and the trailing ';'. tmuxSendKeysArgs passes the command as
//     one argv element, and tmux treats an argument ending in an unescaped ';'
//     as a command terminator. This is the first ';' ever to appear in a
//     send-keys argument in this codebase; it is safe only because the line
//     always ends with ClaudeCmd, which never ends in ';'.
func worktreeLaunchLine(w worktreeLaunch) string {
	if !w.ScriptPresent {
		return w.ClaudeCmd
	}

	if !w.Enabled {
		notice := "hive: scripts/wt-init.sh found but worktree init is off for " + w.DirName + "; enable it with E in the manager"
		return "echo " + shellQuote(notice) + " ; " + w.ClaudeCmd
	}

	script := filepath.Join(w.ParentPath, worktreeInitScriptRel)
	return "bash " + shellQuote(script) + " " + shellQuote(w.ParentPath) + " " + shellQuote(w.Branch) + " ; " + w.ClaudeCmd
}
