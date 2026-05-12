package main

import (
	"sync"

	tea "charm.land/bubbletea/v2"
)

type SessionEvent struct {
	Session   string `json:"session"`
	Repo      string `json:"repo"`
	Event     string `json:"event"` // started, tool, completed, ended
	ToolName  string `json:"tool_name,omitempty"`
	ToolCount int    `json:"tool_count,omitempty"`
	// Initial is set on events synthesised from a session's pre-existing
	// state (hive startup bootstrap, rediscovery loop). Downstream handlers
	// flash the tab but suppress attention-escalation notifications so the
	// user isn't alerted to sessions they already knew about.
	Initial bool `json:"initial,omitempty"`
}

// SessionStatus tracks accumulated state for a Claude session.
type SessionStatus struct {
	Session   string
	Repo      string
	Status    string // running, completed, ended
	ToolCount int
	LastTool  string
}

// sessionEventMsg is the bubbletea-side message carrying a SessionEvent.
type sessionEventMsg SessionEvent

var (
	eventChan chan SessionEvent
	eventOnce sync.Once
)

func initEventChan() chan SessionEvent {
	eventOnce.Do(func() {
		eventChan = make(chan SessionEvent, 64)
	})
	return eventChan
}

// waitForEvent returns a tea.Cmd that blocks until the next SessionEvent is
// pushed onto the channel by the session watcher.
func waitForEvent() tea.Cmd {
	ch := initEventChan()
	return func() tea.Msg {
		ev := <-ch
		return sessionEventMsg(ev)
	}
}
