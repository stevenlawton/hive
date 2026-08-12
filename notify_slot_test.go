package main

import (
	"errors"
	"sync"
	"testing"
	"time"
)

// fakeSender records sends and hands back canned ids.
type fakeSender struct {
	mu       sync.Mutex
	replaces []string // replace-id passed on each send, "" for none
	titles   []string
	ids      []string
	err      error
}

func (f *fakeSender) send(replaceID, title, body string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.replaces = append(f.replaces, replaceID)
	f.titles = append(f.titles, title)
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

func (f *fakeSender) sends() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.replaces...)
}

func TestNotifierFirstCallHasNoReplaceID(t *testing.T) {
	f := &fakeSender{ids: []string{"7"}}
	n := newDesktopNotifier(f.send)

	n.Notify("he-events", "Hive: he-events", "waiting")

	got := f.sends()
	if len(got) != 1 {
		t.Fatalf("got %d sends, want 1", len(got))
	}
	if got[0] != "" {
		t.Errorf("first send must not replace, got replace-id %q", got[0])
	}
}

func TestNotifierReusesIDForSameRepo(t *testing.T) {
	f := &fakeSender{ids: []string{"7", "7"}}
	n := newDesktopNotifier(f.send)

	n.Notify("he-events", "Hive: he-events", "waiting")
	n.Notify("he-events", "Hive: he-events", "still waiting")

	got := f.sends()
	if len(got) != 2 {
		t.Fatalf("got %d sends, want 2", len(got))
	}
	if got[1] != "7" {
		t.Errorf("second send should replace id 7, got %q", got[1])
	}
}

func TestNotifierKeepsASlotPerRepo(t *testing.T) {
	f := &fakeSender{ids: []string{"7", "9", "7", "9"}}
	n := newDesktopNotifier(f.send)

	n.Notify("he-events", "t", "m")
	n.Notify("stevenlawton.com", "t", "m")
	n.Notify("he-events", "t", "m")
	n.Notify("stevenlawton.com", "t", "m")

	got := f.sends()
	if got[2] != "7" {
		t.Errorf("he-events should reuse 7, got %q", got[2])
	}
	if got[3] != "9" {
		t.Errorf("stevenlawton.com should reuse 9, got %q", got[3])
	}
}

func TestNotifierForgetsIDOnSendFailure(t *testing.T) {
	f := &fakeSender{err: errors.New("no bus")}
	n := newDesktopNotifier(f.send)

	n.Notify("he-events", "t", "m")
	n.Notify("he-events", "t", "m")

	// A failed send yields no id, so the follow-up must not claim to
	// replace a notification that was never created.
	if got := f.sends(); got[1] != "" {
		t.Errorf("must not replace after a failed send, got %q", got[1])
	}
}

// The slot must outlive the notification itself. GNOME closes a notification
// when it expires or the user dismisses it, and if hive dropped the id then,
// every later alert would open a fresh entry — which is the drawer flood the
// per-repo slot exists to prevent. Replacing an unknown id is defined to
// create a new notification, so keeping a stale id costs nothing.
func TestNotifierKeepsSlotAfterNotificationCloses(t *testing.T) {
	f := &fakeSender{ids: []string{"7", "7"}}
	n := newDesktopNotifier(f.send)

	n.Notify("he-events", "t", "m")
	n.handleClosed("7")
	n.Notify("he-events", "t", "m")

	if got := f.sends(); got[1] != "7" {
		t.Errorf("slot should survive a close, got replace-id %q", got[1])
	}
}

func TestNotifierReportsClickWithRepoKey(t *testing.T) {
	f := &fakeSender{ids: []string{"7"}}
	n := newDesktopNotifier(f.send)

	clicked := make(chan string, 1)
	n.onClick = func(repoKey string) { clicked <- repoKey }

	n.Notify("he-events", "t", "m")
	n.handleAction("7", "default")

	select {
	case got := <-clicked:
		if got != "he-events" {
			t.Errorf("clicked repo: got %q, want \"he-events\"", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("click was never reported")
	}
}

// Worktree alerts fold into the parent's slot, so a click on that one
// notification must resolve to the parent repo.
func TestNotifierRoutesClickToTheOwningRepo(t *testing.T) {
	f := &fakeSender{ids: []string{"7", "9"}}
	n := newDesktopNotifier(f.send)

	var got []string
	var mu sync.Mutex
	n.onClick = func(repoKey string) { mu.Lock(); got = append(got, repoKey); mu.Unlock() }

	n.Notify("he-events", "t", "m")
	n.Notify("stevenlawton.com", "t", "m")
	n.handleAction("9", "default")

	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 || got[0] != "stevenlawton.com" {
		t.Errorf("click on id 9 should report stevenlawton.com, got %v", got)
	}
}

func TestNotifierIgnoresActionForUnknownID(t *testing.T) {
	f := &fakeSender{ids: []string{"7"}}
	n := newDesktopNotifier(f.send)
	n.onClick = func(string) { t.Error("no click should be reported for an unknown id") }

	n.Notify("he-events", "t", "m")
	n.handleAction("999", "default")
}

func TestNotifierSurvivesClickWithNoHandler(t *testing.T) {
	f := &fakeSender{ids: []string{"7"}}
	n := newDesktopNotifier(f.send)
	// onClick deliberately unset — must not panic.
	n.Notify("he-events", "t", "m")
	n.handleAction("7", "default")
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
