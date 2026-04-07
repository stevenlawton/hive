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

func TestChordHandlerUnknownKey(t *testing.T) {
	ch := NewChordHandler(500 * time.Millisecond)
	ch.Start()

	_, ok := ch.Complete("z")
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
