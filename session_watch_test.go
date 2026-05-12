package main

import (
	"testing"
)

func TestStatusToEvent(t *testing.T) {
	cases := []struct {
		status string
		wantOK bool
		wantEv string
	}{
		{"idle", true, "completed"},
		{"busy", true, "started"},
		{"", false, ""},
		{"starting", false, ""},
		{"unknown", false, ""},
	}
	for _, c := range cases {
		ev, ok := statusToEvent(c.status, "myrepo", "hive-myrepo")
		if ok != c.wantOK {
			t.Errorf("statusToEvent(%q) ok=%v, want %v", c.status, ok, c.wantOK)
			continue
		}
		if !ok {
			continue
		}
		if ev.Event != c.wantEv {
			t.Errorf("statusToEvent(%q) Event=%q, want %q", c.status, ev.Event, c.wantEv)
		}
		if ev.Repo != "myrepo" || ev.Session != "hive-myrepo" {
			t.Errorf("statusToEvent(%q) didn't propagate repo/session: %+v", c.status, ev)
		}
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
