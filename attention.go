package main

import (
	"time"
)

// AttentionState tracks whether a session needs user attention and escalation.
type AttentionState struct {
	WaitingSince time.Time // when we first detected waiting state (zero = not waiting)
	Notified     int       // escalation level reached (0=none, 1=flash, 2=desktop, 3=external)
}

// AttentionThresholdsVisible apply when the session's pane is currently
// visible to the user (i.e. they are actively looking at the workspace
// tab that contains this session). In that case Hive gives the user a
// grace period to see Claude finish and start typing a response without
// flashing at a tab they're already watching.
var AttentionThresholdsVisible = []time.Duration{
	10 * time.Second, // level 1: flash tab
	60 * time.Second, // level 2: desktop notification
	5 * time.Minute,  // level 3: telegram/external
}

// AttentionThresholdsHidden apply when the session is NOT visible — the
// user is on a different workspace tab, or the manager/bus tab. Level 1
// fires immediately because the whole point is to pull the user's
// attention back to a pane they can't currently see.
var AttentionThresholdsHidden = []time.Duration{
	0,                // level 1: flash immediately
	10 * time.Second, // level 2: desktop notification
	5 * time.Minute,  // level 3: telegram/external
}

// DetectClaudeWaiting checks if a tmux session has a bell flag set.
// Claude Code rings the terminal bell when it finishes and needs input.
func DetectClaudeWaiting(sessionName string) bool {
	return TmuxWindowHasBell(sessionName)
}

// CheckAttention updates attention state for a session and returns the
// escalation action needed. `visible` indicates whether the user is
// currently looking at the pane in question — hidden panes escalate
// faster because the whole point of a flash is to yank attention to
// something you can't see.
//
// Returns: -1=clear, 0=nothing, 1=flash tab, 2=desktop notify, 3=external notify
func CheckAttention(state *AttentionState, sessionName string, visible bool) int {
	waiting := DetectClaudeWaiting(sessionName)

	if !waiting {
		if state.Notified > 0 {
			// Was waiting, now active — clear notifications
			state.WaitingSince = time.Time{}
			state.Notified = 0
			return -1
		}
		state.WaitingSince = time.Time{}
		return 0
	}

	// Bell is set — Claude is waiting
	if state.WaitingSince.IsZero() {
		state.WaitingSince = time.Now()
	}
	elapsed := time.Since(state.WaitingSince)

	thresholds := AttentionThresholdsVisible
	if !visible {
		thresholds = AttentionThresholdsHidden
	}

	// Walk thresholds from high to low so we report the highest level
	// we've reached this tick (and skip re-reporting lower ones).
	for level := len(thresholds); level >= 1; level-- {
		if elapsed >= thresholds[level-1] && state.Notified < level {
			state.Notified = level
			return level
		}
	}

	return 0
}

