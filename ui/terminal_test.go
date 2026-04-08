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

	tp.ScrollUp(3)
	if tp.ScrollOffset != 3 {
		t.Errorf("expected ScrollOffset 3, got %d", tp.ScrollOffset)
	}
	if !tp.IsScrolledUp() {
		t.Error("expected IsScrolledUp true")
	}

	tp.ScrollUp(100) // should clamp to max (10 - 5 = 5)
	if tp.ScrollOffset != 5 {
		t.Errorf("expected ScrollOffset 5, got %d", tp.ScrollOffset)
	}

	tp.ScrollDown(3)
	if tp.ScrollOffset != 2 {
		t.Errorf("expected ScrollOffset 2, got %d", tp.ScrollOffset)
	}

	tp.ScrollDown(100) // should clamp to 0 (live)
	if tp.ScrollOffset != 0 {
		t.Errorf("expected ScrollOffset 0, got %d", tp.ScrollOffset)
	}
	if tp.IsScrolledUp() {
		t.Error("expected IsScrolledUp false at bottom")
	}
}
