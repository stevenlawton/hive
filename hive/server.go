package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sync"

	tea "charm.land/bubbletea/v2"
)

const serverPort = 9199

type SessionEvent struct {
	Session   string `json:"session"`
	Repo      string `json:"repo"`
	Event     string `json:"event"` // started, tool, completed, ended
	ToolName  string `json:"tool_name,omitempty"`
	ToolCount int    `json:"tool_count,omitempty"`
}

// SessionStatus tracks accumulated state for a Claude session.
type SessionStatus struct {
	Session  string
	Repo     string
	Status   string // running, completed, ended
	ToolCount int
	LastTool string
}

// sessionEventMsg is sent from the HTTP handler to the bubbletea model.
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

// startServer launches the HTTP event server in a goroutine.
func startServer() error {
	ch := initEventChan()

	mux := http.NewServeMux()
	mux.HandleFunc("POST /event", func(w http.ResponseWriter, r *http.Request) {
		var ev SessionEvent
		if err := json.NewDecoder(r.Body).Decode(&ev); err != nil {
			http.Error(w, "bad json", 400)
			return
		}
		select {
		case ch <- ev:
		default:
			// Channel full, drop event
		}
		w.WriteHeader(200)
		fmt.Fprint(w, "ok")
	})

	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		fmt.Fprint(w, "ok")
	})

	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", serverPort))
	if err != nil {
		return err
	}

	go http.Serve(ln, mux)
	return nil
}

// waitForEvent returns a tea.Cmd that waits for the next event from the server.
func waitForEvent() tea.Cmd {
	ch := initEventChan()
	return func() tea.Msg {
		ev := <-ch
		return sessionEventMsg(ev)
	}
}
