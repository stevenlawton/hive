package main

import (
	"sync"
	"testing"

	"github.com/fsnotify/fsnotify"
)

func TestStatusToEvent(t *testing.T) {
	cases := []struct {
		status string
		wantOK bool
		wantEv string
	}{
		{"waiting", true, "completed"}, // claude waiting for user input → flash
		{"idle", true, "completed"},    // also "needs user input"; claude uses both
		{"busy", true, "started"},      // claude generating → clear flash
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

func TestTrackPropagatesInitialFlag(t *testing.T) {
	// fsnotify watcher just needs to exist; we don't trigger any events.
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatalf("fsnotify: %v", err)
	}
	defer fsw.Close()

	var got []SessionEvent
	w := &SessionWatcher{
		fsw:   fsw,
		emit:  func(ev SessionEvent) { got = append(got, ev) },
		files: make(map[string]*watchedSession),
		mu:    sync.Mutex{},
	}

	w.track(claudeSessionMeta{PID: 1, SessionID: "a", CWD: "/x", Kind: "interactive", Status: "waiting"}, true)
	w.track(claudeSessionMeta{PID: 2, SessionID: "b", CWD: "/y", Kind: "interactive", Status: "busy"}, false)

	if len(got) != 2 {
		t.Fatalf("expected 2 events, got %d: %+v", len(got), got)
	}
	if got[0].Event != "completed" || !got[0].Initial {
		t.Errorf("bootstrap waiting event: want completed+Initial, got %+v", got[0])
	}
	if got[1].Event != "started" || got[1].Initial {
		t.Errorf("fresh busy event: want started without Initial, got %+v", got[1])
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
