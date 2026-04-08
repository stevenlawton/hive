package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverScratches(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "scratch-20260328-001"), 0755)
	os.MkdirAll(filepath.Join(dir, "scratch-20260328-002"), 0755)
	os.WriteFile(filepath.Join(dir, "not-a-scratch.txt"), []byte("hi"), 0644)

	cfg := &Config{ScratchDir: dir}
	scratches := DiscoverScratches(cfg)

	if len(scratches) != 2 {
		t.Fatalf("expected 2 scratches, got %d", len(scratches))
	}
	if !scratches[0].IsScratch {
		t.Error("expected IsScratch=true")
	}
}

func TestNextScratchName(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "scratch-20260328-001"), 0755)

	name := nextScratchDir(dir)
	if name == "" {
		t.Error("expected non-empty scratch name")
	}
}

func TestPromoteScratch(t *testing.T) {
	scratchDir := t.TempDir()
	reposDir := t.TempDir()

	scratchPath := filepath.Join(scratchDir, "scratch-20260328-001")
	os.MkdirAll(scratchPath, 0755)
	os.WriteFile(filepath.Join(scratchPath, "test.txt"), []byte("hello"), 0644)

	newPath, err := PromoteScratch(scratchPath, reposDir, "my-project")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := filepath.Join(reposDir, "my-project")
	if newPath != expected {
		t.Errorf("expected %s, got %s", expected, newPath)
	}

	data, err := os.ReadFile(filepath.Join(newPath, "test.txt"))
	if err != nil {
		t.Fatalf("file not found after promote: %v", err)
	}
	if string(data) != "hello" {
		t.Error("file content mismatch")
	}

	if _, err := os.Stat(scratchPath); !os.IsNotExist(err) {
		t.Error("scratch dir should be removed after promotion")
	}
}
