package main

import (
	"errors"
	"testing"
)

// fakeSender records notify-send invocations and hands back canned ids.
type fakeSender struct {
	calls [][]string
	ids   []string
	err   error
}

func (f *fakeSender) send(args []string) (string, error) {
	f.calls = append(f.calls, args)
	if f.err != nil {
		return "", f.err
	}
	if len(f.ids) == 0 {
		return "", nil
	}
	id := f.ids[0]
	f.ids = f.ids[1:]
	return id, nil
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
