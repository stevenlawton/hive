package main

import (
	"encoding/json"
	"io"
	"os"
)

// A telemetry snapshot is written per Claude session id but read per tmux
// session name, so a pane whose session ended still has that session's file
// sitting in the runtime dir. dropDeadSessions covers the case where tmux went
// with it; this covers the case where it did not — /clear starts a new session
// id in the same pane, and the old snapshot went on colouring the tab from a
// verdict about a conversation that no longer exists.
//
// Claude Code's SessionEnd hook is the only moment that fact is known. Every
// reason it fires with (clear, resume, logout, prompt_input_exit, other) means
// the same thing here: nothing will ever report against this id again.
type sessionEndPayload struct {
	SessionID string `json:"session_id"`
}

// runSessionEndCmd always succeeds. A hook that exits non-zero is noise in the
// session it fires in, and the worst case of doing nothing is one more stale
// tint — which is what the rest of the fleet view already tolerates.
func runSessionEndCmd(r io.Reader) int {
	b, err := io.ReadAll(r)
	if err != nil {
		return 0
	}
	var p sessionEndPayload
	if err := json.Unmarshal(b, &p); err != nil || p.SessionID == "" {
		return 0
	}
	_ = os.Remove(sessionSnapshotPath(p.SessionID))
	return 0
}
