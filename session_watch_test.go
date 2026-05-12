package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseJSONLLine(t *testing.T) {
	cases := []struct {
		name     string
		line     string
		wantOK   bool
		wantEv   string
		wantTool string
	}{
		{
			name:   "assistant end_turn → completed",
			line:   `{"type":"assistant","message":{"stop_reason":"end_turn","content":[{"type":"text"}]}}`,
			wantOK: true,
			wantEv: "completed",
		},
		{
			name:     "assistant tool_use → tool with name",
			line:     `{"type":"assistant","message":{"stop_reason":"tool_use","content":[{"type":"tool_use","name":"Bash"}]}}`,
			wantOK:   true,
			wantEv:   "tool",
			wantTool: "Bash",
		},
		{
			name:   "user → started",
			line:   `{"type":"user","message":{"content":[{"type":"text","text":"hi"}]}}`,
			wantOK: true,
			wantEv: "started",
		},
		{
			name:   "last-prompt metadata → ignored",
			line:   `{"type":"last-prompt","lastPrompt":"x","sessionId":"abc"}`,
			wantOK: false,
		},
		{
			name:   "attachment metadata → ignored",
			line:   `{"type":"attachment","data":"..."}`,
			wantOK: false,
		},
		{
			name:   "assistant without stop_reason → ignored (streaming partial)",
			line:   `{"type":"assistant","message":{"content":[{"type":"text"}]}}`,
			wantOK: false,
		},
		{
			name:   "malformed json → ignored",
			line:   `not json`,
			wantOK: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ev, ok := parseJSONLLine([]byte(c.line), "myrepo", "hive-myrepo")
			if ok != c.wantOK {
				t.Fatalf("ok = %v, want %v (ev=%+v)", ok, c.wantOK, ev)
			}
			if !ok {
				return
			}
			if ev.Event != c.wantEv {
				t.Errorf("Event = %q, want %q", ev.Event, c.wantEv)
			}
			if ev.ToolName != c.wantTool {
				t.Errorf("ToolName = %q, want %q", ev.ToolName, c.wantTool)
			}
			if ev.Repo != "myrepo" || ev.Session != "hive-myrepo" {
				t.Errorf("Repo/Session not propagated: %+v", ev)
			}
		})
	}
}

func TestDeriveEventsFromJSONL_IncrementalReads(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")

	// First write: one user prompt, one tool call.
	initial := `{"type":"user","message":{"content":[{"type":"text"}]}}
{"type":"assistant","message":{"stop_reason":"tool_use","content":[{"type":"tool_use","name":"Read"}]}}
`
	if err := os.WriteFile(path, []byte(initial), 0644); err != nil {
		t.Fatal(err)
	}

	events, offset, err := deriveEventsFromJSONL(path, 0, "r", "s")
	if err != nil {
		t.Fatalf("first read: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("first read: got %d events, want 2: %+v", len(events), events)
	}
	if events[0].Event != "started" || events[1].Event != "tool" {
		t.Errorf("unexpected event sequence: %+v", events)
	}

	// Append a completed turn.
	appendData := `{"type":"assistant","message":{"stop_reason":"end_turn","content":[{"type":"text"}]}}
`
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(appendData)
	f.Close()

	events2, newOffset, err := deriveEventsFromJSONL(path, offset, "r", "s")
	if err != nil {
		t.Fatalf("second read: %v", err)
	}
	if len(events2) != 1 || events2[0].Event != "completed" {
		t.Fatalf("second read: %+v", events2)
	}
	if newOffset <= offset {
		t.Errorf("offset didn't advance: %d → %d", offset, newOffset)
	}
}

func TestEncodeProjectDir(t *testing.T) {
	cases := []struct {
		cwd  string
		want string
	}{
		{"/home/steve/repos/workspace", "-home-steve-repos-workspace"},
		{"/home/steve/repos/stevenlawton.com", "-home-steve-repos-stevenlawton-com"},
		{"/home/steve/.claude/worktrees", "-home-steve--claude-worktrees"},
		{"/home/steve/repos/stevenlawton.com/.claude/worktrees/agent-x", "-home-steve-repos-stevenlawton-com--claude-worktrees-agent-x"},
	}
	for _, c := range cases {
		if got := encodeProjectDir(c.cwd); got != c.want {
			t.Errorf("encodeProjectDir(%q) = %q, want %q", c.cwd, got, c.want)
		}
	}
}

func TestReadJSONLTail(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	content := `{"type":"user","message":{"content":[{"type":"text"}]}}
{"type":"assistant","message":{"stop_reason":"tool_use","content":[{"type":"tool_use","name":"Read"}]}}
{"type":"assistant","message":{"stop_reason":"end_turn","content":[{"type":"text"}]}}
{"type":"last-prompt","lastPrompt":"x"}
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	ev, offset, ok := readJSONLTail(path, "r", "s")
	if !ok {
		t.Fatal("expected ok=true on tail with end_turn")
	}
	if ev.Event != "completed" {
		t.Errorf("expected completed, got %+v", ev)
	}
	if offset != int64(len(content)) {
		t.Errorf("offset = %d, want %d", offset, len(content))
	}
}
