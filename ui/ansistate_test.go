package ui

import (
	"strings"
	"testing"
)

func TestSGRCarryBackgroundAcrossRows(t *testing.T) {
	// tmux capture-pane -e emits SGR as stream transitions: a full-width
	// background set on row 1 is NOT restated on row 2.
	c := newSGRCarry()
	c.Consume("\x1b[30m\x1b[42mrow-one\x1b[39m")

	prefix := c.Prefix()
	if !strings.Contains(prefix, "42") {
		t.Errorf("expected carried bg 42 in prefix, got %q", prefix)
	}
	if strings.Contains(prefix, "30") {
		t.Errorf("fg 30 was reset by [39m, should not carry, got %q", prefix)
	}
}

func TestSGRCarryResetClears(t *testing.T) {
	c := newSGRCarry()
	c.Consume("\x1b[42m\x1b[1mfoo\x1b[0mbar")
	if got := c.Prefix(); got != "" {
		t.Errorf("expected empty prefix after [0m, got %q", got)
	}
}

func TestSGRCarryExtendedColor(t *testing.T) {
	c := newSGRCarry()
	c.Consume("\x1b[48;2;10;20;30mfoo")
	if got := c.Prefix(); !strings.Contains(got, "48;2;10;20;30") {
		t.Errorf("expected truecolor bg carried, got %q", got)
	}

	c.Consume("bar\x1b[49m")
	if got := c.Prefix(); got != "" {
		t.Errorf("expected empty prefix after [49m, got %q", got)
	}
}

func TestSGRCarryAttributes(t *testing.T) {
	c := newSGRCarry()
	c.Consume("\x1b[1m\x1b[3mfoo")
	got := c.Prefix()
	if !strings.Contains(got, "1") || !strings.Contains(got, "3") {
		t.Errorf("expected bold+italic carried, got %q", got)
	}

	c.Consume("\x1b[22mbar")
	got = c.Prefix()
	if strings.Contains(got, "1;") || strings.HasSuffix(got, "1m") {
		t.Errorf("bold should be cleared by [22m, got %q", got)
	}
	if !strings.Contains(got, "3") {
		t.Errorf("italic should survive [22m, got %q", got)
	}
}

func TestViewRestoresCarriedBackground(t *testing.T) {
	// Integration: a pane whose captured content carries a bg across rows
	// must restate that bg on the second rendered line, otherwise dark-fg
	// text on that row becomes invisible on a dark terminal.
	p := NewTerminalPane("ses-x")
	p.SetSize(20, 3)
	p.SetContent("\x1b[30m\x1b[42maaa\nbbb")

	out := p.View()
	lines := strings.Split(out, "\n")
	if len(lines) < 2 {
		t.Fatalf("expected at least 2 lines, got %d", len(lines))
	}
	second := lines[len(lines)-1]
	idx := strings.Index(second, "bbb")
	if idx < 0 {
		t.Fatalf("second line missing content: %q", second)
	}
	if !strings.Contains(second[:idx], "42") {
		t.Errorf("carried bg 42 not restated before content: %q", second)
	}
	if !strings.Contains(second[:idx], "30") {
		t.Errorf("carried fg 30 not restated before content: %q", second)
	}
}
