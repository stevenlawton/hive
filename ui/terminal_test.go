package ui

import (
	"strings"
	"testing"
)

func TestTruncateToHeight(t *testing.T) {
	content := "line1\nline2\nline3\nline4\nline5\nline6\nline7\nline8\nline9\nline10"
	result := TruncateToHeight(content, 5)
	lines := strings.Split(result, "\n")
	if len(lines) != 5 {
		t.Errorf("expected 5 lines, got %d", len(lines))
	}
	if lines[0] != "line6" {
		t.Errorf("expected first line 'line6', got '%s'", lines[0])
	}
}

func TestTruncateToHeightShortContent(t *testing.T) {
	content := "line1\nline2"
	result := TruncateToHeight(content, 5)
	lines := strings.Split(result, "\n")
	if len(lines) != 2 {
		t.Errorf("expected 2 lines, got %d", len(lines))
	}
}

func TestTruncateToHeightEmpty(t *testing.T) {
	result := TruncateToHeight("", 5)
	if result != "" {
		t.Errorf("expected empty, got '%s'", result)
	}
}

func TestClampToWidth(t *testing.T) {
	line := "this is a very long line that should be clamped"
	result := ClampToWidth(line, 10)
	if len([]rune(result)) > 10 {
		t.Errorf("expected max 10 runes, got %d", len([]rune(result)))
	}
}

func TestClampToWidthShort(t *testing.T) {
	line := "short"
	result := ClampToWidth(line, 10)
	if result != "short" {
		t.Errorf("expected 'short', got '%s'", result)
	}
}

func TestTerminalPaneScroll(t *testing.T) {
	tp := NewTerminalPane("test-session")
	tp.SetSize(80, 5)                                 // InnerHeight = 5 (no border)
	tp.SetFullContent("a\nb\nc\nd\ne\nf\ng\nh\ni\nj") // 10 lines

	// Scroll up from tail mode
	tp.ScrollBy(-3)
	if tp.ScrollTop < 0 {
		t.Errorf("expected ScrollTop >= 0 after scrolling up, got %d", tp.ScrollTop)
	}
	if !tp.IsScrolledUp() {
		t.Error("expected IsScrolledUp true")
	}

	// Scroll up a lot — should clamp to 0
	tp.ScrollBy(-100)
	if tp.ScrollTop != 0 {
		t.Errorf("expected ScrollTop 0 at top, got %d", tp.ScrollTop)
	}

	// Scroll down past bottom — should re-enter tail mode
	tp.ScrollBy(100)
	if tp.ScrollTop != -1 {
		t.Errorf("expected ScrollTop -1 (tail mode), got %d", tp.ScrollTop)
	}
	if tp.IsScrolledUp() {
		t.Error("expected IsScrolledUp false in tail mode")
	}

	// ScrollToBottom from scrolled position
	tp.ScrollBy(-5)
	tp.ScrollToBottom()
	if tp.ScrollTop != -1 {
		t.Errorf("expected ScrollTop -1 after ScrollToBottom, got %d", tp.ScrollTop)
	}

	// ScrollToTop
	tp.ScrollToTop()
	if tp.ScrollTop != 0 {
		t.Errorf("expected ScrollTop 0 after ScrollToTop, got %d", tp.ScrollTop)
	}
}

func TestTintedLineUntonedIsByteIdenticalToToday(t *testing.T) {
	// The embedded pane renderer has a live garbling bug. An untoned pane is
	// the overwhelmingly common case and must not change by a single byte.
	line := "hello \x1b[38;5;153mworld\x1b[0m"
	if got, want := tintedLine(line, 40, ""), line+"\x1b[0m\x1b[K"; got != want {
		t.Errorf("tintedLine untoned = %q, want %q", got, want)
	}
}

// Captured content carries its own resets. A background set only at the start
// of the line would be wiped by the first one, leaving the tint in stripes.
func TestTintedLineReassertsBackgroundAfterContentResets(t *testing.T) {
	bg := "\x1b[48;5;52m"
	got := tintedLine("a\x1b[0mb\x1b[0mc", 10, bg)
	if !strings.HasPrefix(got, bg) {
		t.Errorf("tint must open the line: %q", got)
	}
	if n := strings.Count(got, bg); n < 3 {
		t.Errorf("background asserted %d times, want it restored after each content reset: %q", n, got)
	}
}

func TestTintedLinePadsToWidthSoTheTintIsFlush(t *testing.T) {
	bg := "\x1b[48;5;52m"
	got := tintedLine("abc", 10, bg)
	// 3 visible chars + 7 spaces of padding, all under the background.
	if !strings.Contains(got, "abc"+strings.Repeat(" ", 7)) {
		t.Errorf("short line not padded to width: %q", got)
	}
	if !strings.HasSuffix(got, "\x1b[0m\x1b[K") {
		t.Errorf("line must reset before erasing to end: %q", got)
	}
}

func TestTintedLineNeverPadsNegative(t *testing.T) {
	bg := "\x1b[48;5;52m"
	if got := tintedLine("abcdefghijklmnop", 4, bg); got == "" {
		t.Error("an over-long line must still render")
	}
}
