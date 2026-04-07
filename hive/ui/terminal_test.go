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

func TestTerminalPaneScrollMode(t *testing.T) {
	tp := NewTerminalPane("test-session")
	tp.SetSize(80, 5)
	tp.SetFullContent("a\nb\nc\nd\ne\nf\ng\nh\ni\nj")

	tp.EnterScrollMode()
	if !tp.ScrollMode {
		t.Error("expected scroll mode on")
	}
	if tp.scrollPos != 5 { // 10 lines - 5 height
		t.Errorf("expected scrollPos 5, got %d", tp.scrollPos)
	}

	tp.ScrollUp(3)
	if tp.scrollPos != 2 {
		t.Errorf("expected scrollPos 2, got %d", tp.scrollPos)
	}

	tp.ScrollUp(10) // should clamp to 0
	if tp.scrollPos != 0 {
		t.Errorf("expected scrollPos 0, got %d", tp.scrollPos)
	}

	tp.ScrollDown(100) // should clamp to max
	if tp.scrollPos != 5 {
		t.Errorf("expected scrollPos 5, got %d", tp.scrollPos)
	}

	tp.ExitScrollMode()
	if tp.ScrollMode {
		t.Error("expected scroll mode off")
	}
}
