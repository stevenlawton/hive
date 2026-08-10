package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stevenlawton/hive/ui"
)

func TestLayoutStoreRoundTrip(t *testing.T) {
	s := newLayoutStore(t.TempDir())

	if got := s.Get("he-events"); got != "" {
		t.Errorf("empty store: got %q, want \"\"", got)
	}

	if err := s.Set("he-events", "h"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if got := s.Get("he-events"); got != "h" {
		t.Errorf("after Set: got %q, want \"h\"", got)
	}
}

func TestLayoutStorePersistsAcrossInstances(t *testing.T) {
	dir := t.TempDir()
	if err := newLayoutStore(dir).Set("stevenlawton.com", "h"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	if got := newLayoutStore(dir).Get("stevenlawton.com"); got != "h" {
		t.Errorf("reopened store: got %q, want \"h\"", got)
	}
}

func TestLayoutStoreKeepsOtherRepos(t *testing.T) {
	s := newLayoutStore(t.TempDir())
	s.Set("he-events", "h")
	s.Set("stevenlawton.com", "v")
	s.Set("he-events", "v")

	if got := s.Get("stevenlawton.com"); got != "v" {
		t.Errorf("stevenlawton.com: got %q, want \"v\"", got)
	}
	if got := s.Get("he-events"); got != "v" {
		t.Errorf("he-events: got %q, want \"v\"", got)
	}
}

func TestLayoutStoreCorruptFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "layout.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := newLayoutStore(dir)

	if got := s.Get("he-events"); got != "" {
		t.Errorf("corrupt file: got %q, want \"\"", got)
	}
	// A corrupt file must not block writes — it gets replaced.
	if err := s.Set("he-events", "h"); err != nil {
		t.Fatalf("Set over corrupt file: %v", err)
	}
	if got := newLayoutStore(dir).Get("he-events"); got != "h" {
		t.Errorf("after rewrite: got %q, want \"h\"", got)
	}
}

func TestLayoutStoreNilReceiver(t *testing.T) {
	// newModel tolerates a store that failed to open; callers must not panic.
	var s *LayoutStore
	if got := s.Get("he-events"); got != "" {
		t.Errorf("nil store Get: got %q, want \"\"", got)
	}
	if err := s.Set("he-events", "h"); err != nil {
		t.Errorf("nil store Set: %v", err)
	}
}

func TestOrientCode(t *testing.T) {
	if got := orientCode(ui.SplitHorizontal); got != "h" {
		t.Errorf("SplitHorizontal: got %q, want \"h\"", got)
	}
	if got := orientCode(ui.SplitVertical); got != "v" {
		t.Errorf("SplitVertical: got %q, want \"v\"", got)
	}
}

func TestParseOrient(t *testing.T) {
	tests := []struct {
		code string
		want ui.SplitOrientation
		ok   bool
	}{
		{"h", ui.SplitHorizontal, true},
		{"v", ui.SplitVertical, true},
		{"", ui.SplitVertical, false},
		{"garbage", ui.SplitVertical, false},
	}
	for _, tt := range tests {
		got, ok := parseOrient(tt.code)
		if ok != tt.ok {
			t.Errorf("parseOrient(%q): ok=%v, want %v", tt.code, ok, tt.ok)
		}
		if got != tt.want {
			t.Errorf("parseOrient(%q): got %v, want %v", tt.code, got, tt.want)
		}
	}
}
