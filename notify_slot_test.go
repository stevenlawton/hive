package main

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// fakeSender records notify-send invocations and hands back canned ids.
type fakeSender struct {
	calls   [][]string
	ids     []string
	actions [][]string // one action key set per send, nil for no actions
	err     error
}

func (f *fakeSender) send(args []string) (string, <-chan string, error) {
	f.calls = append(f.calls, args)
	if f.err != nil {
		return "", nil, f.err
	}
	if len(f.ids) == 0 {
		return "", nil, nil
	}
	id := f.ids[0]
	f.ids = f.ids[1:]
	if len(f.actions) == 0 {
		return id, nil, nil
	}
	keys := f.actions[0]
	f.actions = f.actions[1:]
	ch := make(chan string, len(keys))
	for _, k := range keys {
		ch <- k
	}
	close(ch)
	return id, ch, nil
}

func hasArg(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}

func TestNotifierFirstCallHasNoReplaceID(t *testing.T) {
	f := &fakeSender{ids: []string{"7"}}
	n := newDesktopNotifier(f.send)

	n.Notify("he-events", "Hive: he-events", "waiting")

	if len(f.calls) != 1 {
		t.Fatalf("got %d calls, want 1", len(f.calls))
	}
	if hasArg(f.calls[0], "--replace-id=") || hasArg(f.calls[0], "--replace-id=7") {
		t.Errorf("first call must not replace: %v", f.calls[0])
	}
}

func TestNotifierReusesIDForSameRepo(t *testing.T) {
	f := &fakeSender{ids: []string{"7", "7"}}
	n := newDesktopNotifier(f.send)

	n.Notify("he-events", "Hive: he-events", "waiting")
	n.Notify("he-events", "Hive: he-events", "still waiting")

	if len(f.calls) != 2 {
		t.Fatalf("got %d calls, want 2", len(f.calls))
	}
	if !hasArg(f.calls[1], "--replace-id=7") {
		t.Errorf("second call should replace id 7, got %v", f.calls[1])
	}
}

func TestNotifierKeepsASlotPerRepo(t *testing.T) {
	f := &fakeSender{ids: []string{"7", "9", "7", "9"}}
	n := newDesktopNotifier(f.send)

	n.Notify("he-events", "t", "m")
	n.Notify("stevenlawton.com", "t", "m")
	n.Notify("he-events", "t", "m")
	n.Notify("stevenlawton.com", "t", "m")

	if !hasArg(f.calls[2], "--replace-id=7") {
		t.Errorf("he-events should reuse 7, got %v", f.calls[2])
	}
	if !hasArg(f.calls[3], "--replace-id=9") {
		t.Errorf("stevenlawton.com should reuse 9, got %v", f.calls[3])
	}
}

func TestNotifierForgetsIDOnSendFailure(t *testing.T) {
	f := &fakeSender{err: errors.New("no notify-send")}
	n := newDesktopNotifier(f.send)

	n.Notify("he-events", "t", "m")
	n.Notify("he-events", "t", "m")

	// A failed send yields no id, so the follow-up must not claim to
	// replace a notification that was never created.
	if hasArg(f.calls[1], "--replace-id=") {
		t.Errorf("must not replace after a failed send: %v", f.calls[1])
	}
}

func TestNotifierCarriesAClickAction(t *testing.T) {
	f := &fakeSender{ids: []string{"7"}}
	n := newDesktopNotifier(f.send)

	n.Notify("he-events", "t", "m")

	if !hasArg(f.calls[0], "--action=default=Open") {
		t.Errorf("notification must offer a default action: %v", f.calls[0])
	}
	// The desktop-entry hint made GNOME try to launch a new terminal rather
	// than raise the running one, so it is deliberately absent.
	for _, a := range f.calls[0] {
		if strings.HasPrefix(a, "--hint=string:desktop-entry:") {
			t.Errorf("desktop-entry hint should not be sent: %v", f.calls[0])
		}
	}
}

func TestNotifierReportsClickWithRepoKey(t *testing.T) {
	f := &fakeSender{ids: []string{"7"}, actions: [][]string{{"default"}}}
	n := newDesktopNotifier(f.send)

	clicked := make(chan string, 1)
	n.onClick = func(repoKey string) { clicked <- repoKey }

	n.Notify("he-events", "t", "m")

	select {
	case got := <-clicked:
		if got != "he-events" {
			t.Errorf("clicked repo: got %q, want \"he-events\"", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("click was never reported")
	}
}

func TestNotifierSurvivesClickWithNoHandler(t *testing.T) {
	f := &fakeSender{ids: []string{"7"}, actions: [][]string{{"default"}}}
	n := newDesktopNotifier(f.send)
	// onClick deliberately unset — must not panic.
	n.Notify("he-events", "t", "m")
}

func TestRepoGroupKey(t *testing.T) {
	parent := Repo{DirName: "he-events"}
	wt := Repo{DirName: "he-events-wt-split-1", IsWorktree: true, Parent: "he-events"}
	orphan := Repo{DirName: "loose-wt", IsWorktree: true}

	if got := repoGroupKey(parent); got != "he-events" {
		t.Errorf("parent: got %q", got)
	}
	if got := repoGroupKey(wt); got != "he-events" {
		t.Errorf("worktree should fold into parent: got %q", got)
	}
	if got := repoGroupKey(orphan); got != "loose-wt" {
		t.Errorf("parentless worktree keeps own key: got %q", got)
	}
}
