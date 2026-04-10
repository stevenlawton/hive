package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/stevenlawton/hive/bus"
)

// todoHookInput models the JSON payload Claude Code delivers on stdin to
// a PostToolUse hook when the tool is TodoWrite. We only care about a few
// fields; json.Unmarshal silently ignores the rest.
type todoHookInput struct {
	SessionID string `json:"session_id"`
	ToolInput struct {
		Todos []todoItem `json:"todos"`
	} `json:"tool_input"`
}

type todoItem struct {
	Content    string `json:"content"`
	Status     string `json:"status"`      // "pending" | "in_progress" | "completed"
	ActiveForm string `json:"activeForm"`  // "Doing the thing"
}

// busTodoHookCmd reads a TodoWrite hook payload from stdin and emits bus
// announcements for interesting state transitions against the previous
// snapshot of the same session's todo list.
//
// This is invoked from Claude Code as a PostToolUse hook — the hook spec
// includes matcher="TodoWrite" so only relevant tool calls reach us.
func busTodoHookCmd(_ []string) int {
	raw, err := io.ReadAll(os.Stdin)
	if err != nil {
		return 0 // never break Claude's flow over a hook error
	}

	var input todoHookInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return 0
	}
	if input.SessionID == "" || len(input.ToolInput.Todos) == 0 {
		return 0
	}

	// Load the previous snapshot for this session.
	statePath, err := todoStatePath(input.SessionID)
	if err != nil {
		return 0
	}
	prev := loadTodoState(statePath)

	// Open the bus using the session's auto-detected sender id.
	b, err := openBus()
	if err != nil {
		return 0
	}

	// Diff by content (Claude reuses identical content strings across
	// updates; this is the stable identity key).
	prevByContent := make(map[string]todoItem, len(prev))
	for _, t := range prev {
		prevByContent[t.Content] = t
	}

	// Also build the human-readable todo list for bodies.
	todoBody := renderTodoList(input.ToolInput.Todos)

	for _, cur := range input.ToolInput.Todos {
		old, existed := prevByContent[cur.Content]

		switch {
		case !existed && cur.Status == "in_progress":
			// Brand-new todo that Claude already put in_progress
			announceTodoIntent(b, cur, todoBody)

		case !existed && cur.Status == "completed":
			// Brand-new todo that's already done — rare, but announce as done
			announceTodoDone(b, cur)

		case existed && old.Status != cur.Status:
			// Transition
			switch cur.Status {
			case "in_progress":
				announceTodoIntent(b, cur, todoBody)
			case "completed":
				announceTodoDone(b, cur)
			}
		}
		// pending→pending or no-change: ignore
		// newly-added pending: ignore (queued, not yet intent)
	}

	// Persist the new state for next diff.
	saveTodoState(statePath, input.ToolInput.Todos)
	return 0
}

func announceTodoIntent(b *bus.Bus, t todoItem, body string) {
	headline := t.ActiveForm
	if headline == "" {
		headline = t.Content
	}
	_, _ = b.Announce(bus.Announcement{
		Kind:     bus.KindIntent,
		Headline: headline,
		Body:     body,
	})
}

func announceTodoDone(b *bus.Bus, t todoItem) {
	_, _ = b.Announce(bus.Announcement{
		Kind:     bus.KindDone,
		Headline: t.Content,
	})
}

func renderTodoList(todos []todoItem) string {
	var b strings.Builder
	b.WriteString("Current plan:\n")
	for _, t := range todos {
		marker := "[ ]"
		switch t.Status {
		case "in_progress":
			marker = "[…]"
		case "completed":
			marker = "[✓]"
		}
		fmt.Fprintf(&b, "  %s %s\n", marker, t.Content)
	}
	return strings.TrimRight(b.String(), "\n")
}

func todoStatePath(sessionID string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".config", "hive", "todos")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	// Sanitize the session id (it's usually a UUID, but be safe).
	safe := strings.NewReplacer("/", "_", "..", "_", " ", "_").Replace(sessionID)
	return filepath.Join(dir, safe+".json"), nil
}

func loadTodoState(path string) []todoItem {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var todos []todoItem
	if err := json.Unmarshal(data, &todos); err != nil {
		return nil
	}
	return todos
}

func saveTodoState(path string, todos []todoItem) {
	data, err := json.Marshal(todos)
	if err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o644)
}
