package main

import (
	"strings"
	"testing"
)

func TestOverlayBoxDrawsAtTheGivenPosition(t *testing.T) {
	content := "aaaaaaaa\nbbbbbbbb\ncccccccc"

	got := overlayBox(content, "[]", 2, 1)

	want := "aaaaaaaa\nbb[]bbbb\ncccccccc"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestOverlayBoxPadsShortLines(t *testing.T) {
	got := overlayBox("a\nb", "XX", 3, 0)

	want := "a  XX\nb"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestOverlayBoxClipsRowsPastTheContent(t *testing.T) {
	got := overlayBox("aaaa", "11\n22\n33", 0, 0)

	if got != "11aa" {
		t.Errorf("got %q, want %q — box rows past the content should be dropped", got, "11aa")
	}
}

func TestOverlayBoxKeepsStylingOutsideTheBox(t *testing.T) {
	const red = "\x1b[31m"
	const reset = "\x1b[0m"
	content := red + "aaaaaaaa" + reset

	got := overlayBox(content, "XX", 4, 0)

	if !strings.Contains(got, red) {
		t.Errorf("styling was stripped: %q", got)
	}
	if !strings.Contains(got, "XX") {
		t.Errorf("box missing: %q", got)
	}
}
