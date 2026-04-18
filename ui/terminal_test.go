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
	tp.SetSize(80, 5) // InnerHeight = 5 (no border)
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
