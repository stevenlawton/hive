package main

import (
	"strings"
	"time"
)

// AttentionState tracks whether a session needs user attention and escalation.
type AttentionState struct {
	WaitingSince time.Time // when we first detected waiting state (zero = not waiting)
	LastContent  string    // hash of last capture to detect changes
	Notified     int       // escalation level reached (0=none, 1=flash, 2=desktop, 3=external)
}

// AttentionThresholds defines when each escalation fires.
var AttentionThresholds = []time.Duration{
	10 * time.Second,  // level 1: flash tab
	60 * time.Second,  // level 2: desktop notification
	5 * time.Minute,   // level 3: telegram/external
}

// DetectClaudeWaiting checks if the tmux capture output shows Claude waiting for input.
// Looks for the ❯ prompt on an empty line with no active spinner.
func DetectClaudeWaiting(content string) bool {
	lines := strings.Split(content, "\n")

	// Scan from bottom for the prompt
	for i := len(lines) - 1; i >= 0 && i >= len(lines)-6; i-- {
		trimmed := strings.TrimSpace(lines[i])
		// Empty prompt line = waiting
		if trimmed == "❯" || trimmed == ">" || trimmed == "›" {
			// Check lines above for active spinner (working indicator)
			for j := i - 1; j >= 0 && j >= i-3; j-- {
				above := strings.TrimSpace(lines[j])
				// Active spinner patterns: elapsed time, token count
				if strings.Contains(above, "tokens)") || strings.Contains(above, "remaining") {
					return false // still working
				}
			}
			return true
		}
	}
	return false
}

// CheckAttention updates attention state for a session and returns the escalation action needed.
// Returns: 0=nothing, 1=flash tab, 2=desktop notify, 3=external notify
func CheckAttention(state *AttentionState, content string, cfg *NotificationConfig) int {
	contentKey := contentHash(content)

	if contentKey != state.LastContent {
		// Content changed — user is active or Claude is working
		state.LastContent = contentKey
		wasNotified := state.Notified > 0
		state.WaitingSince = time.Time{}
		state.Notified = 0
		if wasNotified {
			return -1 // signal to clear flash
		}
		return 0
	}

	// Content unchanged — check if Claude is waiting
	if !DetectClaudeWaiting(content) {
		state.WaitingSince = time.Time{}
		state.Notified = 0
		return 0
	}

	// Claude is waiting and content is static
	if state.WaitingSince.IsZero() {
		state.WaitingSince = time.Now()
		return 0
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

// contentHash returns a simple hash of content for change detection.
func contentHash(content string) string {
	// Use last 10 lines as the hash — captures prompt state without noise
	lines := strings.Split(content, "\n")
	if len(lines) > 10 {
		lines = lines[len(lines)-10:]
	}
	return strings.Join(lines, "\n")
}
