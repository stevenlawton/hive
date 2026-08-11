package main

import (
	"testing"
	"time"
)

func TestChordHandler(t *testing.T) {
	ch := NewChordHandler(500 * time.Millisecond)

	if ch.Pending() {
		t.Error("should not be pending initially")
	}

	ch.Start()
	if !ch.Pending() {
		t.Error("should be pending after Start")
	}

	action, ok := ch.Complete("q")
	if !ok {
		t.Error("expected valid action")
	}
	if action != ChordReturnManager {
		t.Errorf("expected ChordReturnManager, got %d", action)
	}
	if ch.Pending() {
		t.Error("should not be pending after Complete")
	}
}

func TestChordReorient(t *testing.T) {
	ch := NewChordHandler(500 * time.Millisecond)
	ch.Start()

	action, ok := ch.Complete("o")
	if !ok {
		t.Fatal("expected valid action for o")
	}
	if action != ChordReorient {
		t.Errorf("expected ChordReorient, got %d", action)
	}
}

func TestChordNextWorker(t *testing.T) {
	ch := NewChordHandler(500 * time.Millisecond)
	ch.Start()

	action, ok := ch.Complete("g")
	if !ok {
		t.Fatal("expected valid action for g")
	}
	if action != ChordNextWorker {
		t.Errorf("expected ChordNextWorker, got %d", action)
	}
}

func TestChordNextWorkerDoesNotShadowSplits(t *testing.T) {
	// g must not disturb the existing worktree-split keys.
	for key, want := range map[string]ChordAction{
		"v": ChordVSplit,
		"h": ChordHSplit,
	} {
		ch := NewChordHandler(500 * time.Millisecond)
		ch.Start()
		got, ok := ch.Complete(key)
		if !ok || got != want {
			t.Errorf("key %q: got %d ok=%v, want %d", key, got, ok, want)
		}
	}
}

func TestChordSaveState(t *testing.T) {
	ch := NewChordHandler(500 * time.Millisecond)
	ch.Start()

	action, ok := ch.Complete("z")
	if !ok {
		t.Fatal("expected valid action for z")
	}
	if action != ChordSaveState {
		t.Errorf("expected ChordSaveState, got %d", action)
	}
}

func TestChordActionsAreDistinct(t *testing.T) {
	// Every bound key must map to its own action — a duplicated constant
	// would silently make one binding shadow another.
	ch := NewChordHandler(500 * time.Millisecond)
	seen := map[ChordAction]string{}
	for _, key := range []string{"q", "n", "p", "v", "h", "x", "f", "w", "d", "s", "r", "t", "o", "g", "z"} {
		ch.Start()
		action, ok := ch.Complete(key)
		if !ok {
			t.Errorf("key %q is unbound", key)
			continue
		}
		if prev, dup := seen[action]; dup {
			t.Errorf("keys %q and %q both map to action %d", prev, key, action)
		}
		seen[action] = key
	}
}

func TestChordHandlerUnknownKey(t *testing.T) {
	ch := NewChordHandler(500 * time.Millisecond)
	ch.Start()

	// Punctuation rather than a letter: letters keep getting bound, and this
	// test previously used "z" until the save-state chord claimed it.
	_, ok := ch.Complete("!")
	if ok {
		t.Error("expected no action for unknown key")
	}
	if ch.Pending() {
		t.Error("should not be pending after unknown key")
	}
}

func TestChordHandlerTimeout(t *testing.T) {
	ch := NewChordHandler(50 * time.Millisecond)
	ch.Start()
	time.Sleep(60 * time.Millisecond)

	if !ch.TimedOut() {
		t.Error("should be timed out")
	}
	ch.Cancel()
	if ch.Pending() {
		t.Error("should not be pending after cancel")
	}
}

func TestChordHandlerNumberKeys(t *testing.T) {
	ch := NewChordHandler(500 * time.Millisecond)
	ch.Start()
	action, ok := ch.Complete("3")
	if !ok {
		t.Error("expected valid action for number key")
	}
	if action != ChordJumpTab {
		t.Errorf("expected ChordJumpTab, got %d", action)
	}
	if ch.TabIndex != 3 {
		t.Errorf("expected TabIndex 3, got %d", ch.TabIndex)
	}
}

func TestChordHandlerAllActions(t *testing.T) {
	tests := []struct {
		key    string
		action ChordAction
	}{
		{"q", ChordReturnManager},
		{"n", ChordNextTab},
		{"p", ChordPrevTab},
		{"v", ChordVSplit},
		{"h", ChordHSplit},
		{"left", ChordFocusLeft},
		{"right", ChordFocusRight},
		{"x", ChordCloseSplit},
		{"f", ChordFullScreen},
		{"w", ChordWorktree},
		{"d", ChordDetachSplit},
		{"r", ChordRefresh},
	}

	for _, tt := range tests {
		ch := NewChordHandler(500 * time.Millisecond)
		ch.Start()
		action, ok := ch.Complete(tt.key)
		if !ok {
			t.Errorf("key %q: expected valid action", tt.key)
		}
		if action != tt.action {
			t.Errorf("key %q: expected %d, got %d", tt.key, tt.action, action)
		}
	}
}
