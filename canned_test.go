package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCannedStoreSeedsDefaultsOnFirstRead(t *testing.T) {
	dir := t.TempDir()
	s := newCannedStore(dir)

	got := s.Prompts()

	if len(got) == 0 {
		t.Fatal("empty dir: got no prompts, want the built-in defaults")
	}
	if _, err := os.Stat(filepath.Join(dir, "canned.yaml")); err != nil {
		t.Errorf("defaults were not written to disk: %v", err)
	}
}

func TestCannedStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s := newCannedStore(dir)

	want := []CannedPrompt{
		{Label: "continue", Text: "continue"},
		{Label: "tests", Text: "run the tests and fix whatever fails"},
	}
	if err := s.Save(want); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got := newCannedStore(dir).Prompts()

	if len(got) != len(want) {
		t.Fatalf("got %d prompts, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("prompt %d: got %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestCannedStoreRereadsEditsFromDisk(t *testing.T) {
	dir := t.TempDir()
	s := newCannedStore(dir)
	s.Prompts() // seed

	edited := "prompts:\n  - label: hand-edited\n    text: do the thing\n"
	if err := os.WriteFile(filepath.Join(dir, "canned.yaml"), []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}

	got := s.Prompts()

	if len(got) != 1 || got[0].Label != "hand-edited" {
		t.Errorf("got %+v, want the hand-edited prompt", got)
	}
}

func TestCannedPromptTextIsFlattenedToOneLine(t *testing.T) {
	dir := t.TempDir()
	multiline := "prompts:\n  - label: two lines\n    text: |\n      first line\n      second line\n"
	if err := os.WriteFile(filepath.Join(dir, "canned.yaml"), []byte(multiline), 0o644); err != nil {
		t.Fatal(err)
	}

	got := newCannedStore(dir).Prompts()

	if len(got) != 1 {
		t.Fatalf("got %d prompts, want 1", len(got))
	}
	if strings.ContainsAny(got[0].Text, "\n\r") {
		t.Errorf("text still contains a newline: %q", got[0].Text)
	}
	if got[0].Text != "first line second line" {
		t.Errorf("got %q, want %q", got[0].Text, "first line second line")
	}
}

func TestCannedStoreCorruptFileFallsBackToDefaults(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "canned.yaml"), []byte("{not yaml"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := newCannedStore(dir).Prompts()

	if len(got) != len(cannedDefaults()) {
		t.Errorf("corrupt file: got %d prompts, want the %d defaults", len(got), len(cannedDefaults()))
	}
}

func TestCannedStoreDropsEmptyEntries(t *testing.T) {
	dir := t.TempDir()
	body := "prompts:\n  - label: real\n    text: do it\n  - label: blank\n    text: \"\"\n"
	if err := os.WriteFile(filepath.Join(dir, "canned.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	got := newCannedStore(dir).Prompts()

	if len(got) != 1 || got[0].Label != "real" {
		t.Errorf("got %+v, want only the entry with text", got)
	}
}

func TestCannedPromptLabelDefaultsToItsText(t *testing.T) {
	dir := t.TempDir()
	body := "prompts:\n  - text: unlabelled prompt\n"
	if err := os.WriteFile(filepath.Join(dir, "canned.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	got := newCannedStore(dir).Prompts()

	if len(got) != 1 || got[0].Label != "unlabelled prompt" {
		t.Errorf("got %+v, want the label filled in from the text", got)
	}
}
