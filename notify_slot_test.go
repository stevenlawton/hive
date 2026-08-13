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

func TestNotifierHasNothingToReplaceIfItNeverSent(t *testing.T) {
	f := &fakeSender{err: errors.New("no bus")}
	n := newDesktopNotifier(f.send)

	n.Notify("he-events", "t", "m")
	n.Notify("he-events", "t", "m")

	// Never got an id, so there is nothing to claim to replace.
	if got := f.sends(); got[1] != "" {
		t.Errorf("must not replace after a failed send, got %q", got[1])
	}
}

// A transient failure must not cost the repo its slot. Discarding a good id
// because one send errored means the next alert opens a second entry for the
// repo instead of replacing the first — the drawer flood, reintroduced.
func TestNotifierKeepsSlotThroughATransientFailure(t *testing.T) {
	f := &fakeSender{ids: []string{"7"}}
	n := newDesktopNotifier(f.send)

	n.Notify("he-events", "t", "first")

	f.mu.Lock()
	f.err = errors.New("bus hiccup")
	f.mu.Unlock()
	n.Notify("he-events", "t", "fails")

	f.mu.Lock()
	f.err, f.ids = nil, []string{"7"}
	f.mu.Unlock()
	n.Notify("he-events", "t", "recovered")

	got := f.sends()
	if got[1] != "7" {
		t.Errorf("the failing send should still have offered id 7, got %q", got[1])
	}
	if got[2] != "7" {
		t.Errorf("slot lost to a transient failure: send 3 used %q, want 7", got[2])
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

// Visiting a repo's tab answers the notification, so the banner should go.
func TestNotifierClearRetiresTheNotification(t *testing.T) {
	f := &fakeSender{ids: []string{"7", "9"}}
	var closed []string
	n := newDesktopNotifier(f.send)
	n.close = func(id string) { closed = append(closed, id) }

	n.Notify("he-events", "t", "m")
	n.Clear("he-events")

	if len(closed) != 1 || closed[0] != "7" {
		t.Errorf("Clear should close notification 7, closed=%v", closed)
	}

	// The slot is gone, so the next alert opens a fresh entry rather than
	// replacing one the user has already dealt with.
	n.Notify("he-events", "t", "m")
	if got := f.sends(); got[1] != "" {
		t.Errorf("after Clear the next send should not replace, got %q", got[1])
	}
}

func TestNotifierClearIsSafeWithNothingToClear(t *testing.T) {
	f := &fakeSender{ids: []string{"7"}}
	var closed []string
	n := newDesktopNotifier(f.send)
	n.close = func(id string) { closed = append(closed, id) }

	n.Clear("he-events") // never notified
	n.Notify("he-events", "t", "m")
	n.Clear("stevenlawton.com") // a different repo's tab
	if len(closed) != 0 {
		t.Errorf("nothing should have been closed, closed=%v", closed)
	}
}

// A cleared notification must not still route clicks to the repo.
func TestNotifierClearForgetsTheOwner(t *testing.T) {
	f := &fakeSender{ids: []string{"7"}}
	n := newDesktopNotifier(f.send)
	n.close = func(string) {}
	n.onClick = func(string) { t.Error("a cleared notification should not report clicks") }

	n.Notify("he-events", "t", "m")
	n.Clear("he-events")
	n.handleAction("7", "default")
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
