package main

import (
	"time"
)

// AttentionState tracks whether a session needs user attention and escalation.
type AttentionState struct {
	WaitingSince time.Time // when we first detected waiting state (zero = not waiting)
	Notified     int       // escalation level reached (0=none, 1=flash, 2=desktop, 3=external)
}

// AttentionThresholds defines when each escalation fires.
var AttentionThresholds = []time.Duration{
	10 * time.Second,  // level 1: flash tab
	60 * time.Second,  // level 2: desktop notification
	5 * time.Minute,   // level 3: telegram/external
}

// DetectClaudeWaiting checks if a tmux session has a bell flag set.
// Claude Code rings the terminal bell when it finishes and needs input.
func DetectClaudeWaiting(sessionName string) bool {
	return TmuxWindowHasBell(sessionName)
}

// CheckAttention updates attention state for a session and returns the escalation action needed.
// Returns: -1=clear, 0=nothing, 1=flash tab, 2=desktop notify, 3=external notify
func CheckAttention(state *AttentionState, sessionName string) int {
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

	// Check each threshold
	for level := len(AttentionThresholds); level >= 1; level-- {
		if elapsed >= AttentionThresholds[level-1] && state.Notified < level {
			state.Notified = level
			return level
		}
	}

	return 0
}

