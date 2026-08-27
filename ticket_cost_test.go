package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTicketCwdRoundTrip(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)

	recordTicketCwd("dmy", "/repo/.claude/worktrees/wt-init", now)
	recordTicketCwd("wdd", "/repo/.worktrees/split-9", now)
	// A second sighting of the same pair must not duplicate it.
	recordTicketCwd("dmy", "/repo/.claude/worktrees/wt-init", now.Add(time.Minute))

	m := loadTicketCwds()
	if len(m) != 2 {
		t.Fatalf("got %d mappings, want 2: %+v", len(m), m)
	}
	if m["/repo/.claude/worktrees/wt-init"] != "dmy" {
		t.Errorf("wrong ticket for the worktree: %+v", m)
	}
}

// Sub-agents inherit their parent's cwd, and a builder's work happens in
// subdirectories of the worktree. Attribution has to follow the tree down.
func TestTicketForCwdMatchesTheDeepestPrefix(t *testing.T) {
	m := map[string]string{
		"/repo":                       "aaa",
		"/repo/.claude/worktrees/wt1": "dmy",
	}
	cases := []struct{ cwd, want string }{
		{"/repo/.claude/worktrees/wt1", "dmy"},
		{"/repo/.claude/worktrees/wt1/internal/pkg", "dmy"}, // deeper wins
		{"/repo/cmd", "aaa"},
		{"/elsewhere", ""},
		{"", ""},
		{"/repo/.claude/worktrees/wt10", "aaa"}, // not a path-boundary match for wt1
	}
	for _, c := range cases {
		if got := ticketForCwd(m, c.cwd); got != c.want {
			t.Errorf("ticketForCwd(%q) = %q, want %q", c.cwd, got, c.want)
		}
	}
}

func TestScanAgentSpendAttributesByCwd(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sess", "subagents")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, cwd string, msgs [][2]int) {
		var b []byte
		for _, m := range msgs {
			row := map[string]any{
				"type": "assistant", "cwd": cwd,
				"message": map[string]any{"model": "claude-opus-5", "usage": map[string]any{
					"input_tokens": 0, "output_tokens": m[0],
					"cache_creation_input_tokens": m[1], "cache_read_input_tokens": 0,
				}},
			}
			j, _ := json.Marshal(row)
			b = append(b, j...)
			b = append(b, '\n')
		}
		if err := os.WriteFile(filepath.Join(sub, name), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("agent-a.jsonl", "/repo/wt1", [][2]int{{100, 900}, {50, 50}}) // 1100 to dmy
	write("agent-b.jsonl", "/repo/wt1/sub", [][2]int{{200, 0}})         // 200 to dmy (sub-agent)
	write("agent-c.jsonl", "/elsewhere", [][2]int{{999, 0}})            // unattributed

	m := map[string]string{"/repo/wt1": "dmy"}
	got := scanAgentSpend(dir, m)
	if got["dmy"].NewTokens != 1300 {
		t.Errorf("dmy tokens = %d, want 1300 (both agents under the worktree)", got["dmy"].NewTokens)
	}
	if got["dmy"].Runs != 2 {
		t.Errorf("dmy runs = %d, want 2", got["dmy"].Runs)
	}
	if _, leaked := got[""]; leaked {
		t.Error("unattributable spend must not be filed under the empty ticket")
	}
}

func TestScanAgentSpendMissingDirIsEmpty(t *testing.T) {
	if got := scanAgentSpend(filepath.Join(t.TempDir(), "nope"), nil); len(got) != 0 {
		t.Errorf("missing dir should scan to nothing, got %v", got)
	}
}

// Showing a ticket is not working on it. Recording on `show` meant a session
// that read several tickets filed all its agent spend under whichever it read
// last — observed live, 123k tokens attributed to a ticket nobody had touched.
// Only a commitment counts.
func TestOnlyCommitmentVerbsRecord(t *testing.T) {
	cases := map[string]bool{
		"claim": true, "current": true, "state": true,
		"show": false, "list": false, "edit": false, "rm": false, "done": false,
	}
	for verb, want := range cases {
		if got := verbCommitsToTicket(verb); got != want {
			t.Errorf("verbCommitsToTicket(%q) = %v, want %v", verb, got, want)
		}
	}
}

// One unparseable entry used to discard the whole ledger silently: SeenAt was a
// time.Time, so any timestamp Go could not decode failed the entire array and
// every attribution vanished with no error. It is informational, so it is a
// string and cannot fail the decode.
func TestTicketCwdsSurviveAnOddTimestamp(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)
	path := filepath.Join(dir, "hive", "ticket-cwds.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	raw := `[{"ticket":"dmy","cwd":"/repo/wt1","seen_at":"2026-08-27T11:55:02.886292"},
	         {"ticket":"wdd","cwd":"/repo/wt2","seen_at":""}]`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	m := loadTicketCwds()
	if len(m) != 2 {
		t.Fatalf("got %d mappings, want both to survive: %+v", len(m), m)
	}
	if m["/repo/wt1"] != "dmy" || m["/repo/wt2"] != "wdd" {
		t.Errorf("wrong contents: %+v", m)
	}
}
