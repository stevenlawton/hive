package main

import "time"

// ChordAction represents an action triggered by a chord.
type ChordAction int

const (
	ChordReturnManager ChordAction = iota
	ChordNextTab
	ChordPrevTab
	ChordJumpTab
	ChordVSplit
	ChordHSplit
	ChordFocusLeft
	ChordFocusRight
	ChordFocusUp
	ChordFocusDown
	ChordCloseSplit
	ChordFullScreen
	ChordWorktree
	ChordDetachSplit
)

// ChordHandler manages the Ctrl+Space chord prefix state.
type ChordHandler struct {
	pending   bool
	startTime time.Time
	timeout   time.Duration
	TabIndex  int
}

// NewChordHandler creates a chord handler with the given timeout.
func NewChordHandler(timeout time.Duration) *ChordHandler {
	return &ChordHandler{timeout: timeout}
}

// Start begins a chord sequence.
func (c *ChordHandler) Start() {
	c.pending = true
	c.startTime = time.Now()
}

// Pending returns whether a chord is in progress.
func (c *ChordHandler) Pending() bool {
	return c.pending
}

// TimedOut returns whether the chord has exceeded its timeout.
func (c *ChordHandler) TimedOut() bool {
	return c.pending && time.Since(c.startTime) > c.timeout
}

// Cancel aborts the chord.
func (c *ChordHandler) Cancel() {
	c.pending = false
}

// Complete resolves the chord with a second key press.
func (c *ChordHandler) Complete(key string) (ChordAction, bool) {
	c.pending = false

	switch key {
	case "q":
		return ChordReturnManager, true
	case "n":
		return ChordNextTab, true
	case "p":
		return ChordPrevTab, true
	case "v":
		return ChordVSplit, true
	case "h":
		return ChordHSplit, true
	case "left":
		return ChordFocusLeft, true
	case "right":
		return ChordFocusRight, true
	case "up":
		return ChordFocusUp, true
	case "down":
		return ChordFocusDown, true
	case "x":
		return ChordCloseSplit, true
	case "f":
		return ChordFullScreen, true
	case "w":
		return ChordWorktree, true
	case "d":
		return ChordDetachSplit, true
	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		c.TabIndex = int(key[0] - '0')
		return ChordJumpTab, true
	default:
		return 0, false
	}
}
