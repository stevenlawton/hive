package main

import (
	"strings"
	"testing"

	lipgloss "charm.land/lipgloss/v2"
)

// styled mimics a hint that carries ANSI styling, where byte length overstates
// the space the hint actually occupies.
const styled = "\x1b[1m^Space\x1b[0m"

func widestLine(s string) int {
	w := 0
	for _, line := range strings.Split(s, "\n") {
		if lw := lipgloss.Width(line); lw > w {
			w = lw
		}
	}
	return w
}

func TestWrapKeyHintsFitsOnOneLine(t *testing.T) {
	got := wrapKeyHints([]string{"q:back", "n:next", "p:prev"}, 80)
	if want := "q:back  n:next  p:prev"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestWrapKeyHintsWrapsAtWidth(t *testing.T) {
	keys := []string{"q:back", "n:next", "p:prev", "v:vsplit", "h:hsplit"}

	got := wrapKeyHints(keys, 24)
	if widestLine(got) > 24 {
		t.Errorf("line exceeds width 24 (%d):\n%s", widestLine(got), got)
	}
	// Every hint must survive the wrap — the whole point is that nothing
	// falls off the edge unseen.
	for _, k := range keys {
		if !strings.Contains(got, k) {
			t.Errorf("hint %q lost in wrapping:\n%s", k, got)
		}
	}
	if !strings.Contains(got, "\n") {
		t.Errorf("expected a wrap at width 24:\n%s", got)
	}
}

// Hints are styled, so measuring bytes rather than display width would wrap
// lines that fit perfectly well.
func TestWrapKeyHintsMeasuresDisplayWidthNotBytes(t *testing.T) {
	if len(styled) <= lipgloss.Width(styled) {
		t.Fatal("test fixture is not actually styled")
	}
	got := wrapKeyHints([]string{styled, "q:back"}, 20)
	if strings.Contains(got, "\n") {
		t.Errorf("styled hints (6+2+6=14 cols) should fit width 20:\n%q", got)
	}
}

// Before the first WindowSizeMsg the width is zero; wrapping to it would put
// every hint on its own line.
func TestWrapKeyHintsUnknownWidthStaysOnOneLine(t *testing.T) {
	got := wrapKeyHints([]string{"q:back", "n:next"}, 0)
	if strings.Contains(got, "\n") {
		t.Errorf("unknown width should not wrap: %q", got)
	}
}

func TestWrapKeyHintsOversizedHintGetsItsOwnLine(t *testing.T) {
	got := wrapKeyHints([]string{"a", "x:merge+close", "b"}, 6)
	lines := strings.Split(got, "\n")
	if len(lines) != 3 {
		t.Fatalf("want one line per hint when they cannot share, got %d:\n%s", len(lines), got)
	}
	if lines[1] != "x:merge+close" {
		t.Errorf("oversized hint should stand alone, got %q", lines[1])
	}
}

func TestWrapKeyHintsEmpty(t *testing.T) {
	if got := wrapKeyHints(nil, 80); got != "" {
		t.Errorf("want empty string, got %q", got)
	}
}

// The full chord set at the width hive actually runs at. This is the case that
// prompted the wrap: it needs ~139 columns and had been silently clipped,
// taking o:orient, f:fullscreen, r:refresh and z:save off the edge.
func TestWrapKeyHintsFullChordSetAtRealWidth(t *testing.T) {
	keys := []string{
		"^Space …", "q:back", "n:next", "p:prev", "1-9:jump", "←→:focus",
		"v:vsplit", "h:hsplit", "g:/next", "x:merge+close", "o:orient",
		"f:fullscreen", "r:refresh", "z:save",
	}
	const width = 87

	got := wrapKeyHints(keys, width)
	t.Logf("at %d cols:\n%s", width, got)

	if widestLine(got) > width {
		t.Errorf("line exceeds %d cols (%d)", width, widestLine(got))
	}
	for _, k := range keys {
		if !strings.Contains(got, k) {
			t.Errorf("hint %q clipped", k)
		}
	}
	if n := len(strings.Split(got, "\n")); n != 2 {
		t.Errorf("want 2 lines at %d cols, got %d", width, n)
	}
}
