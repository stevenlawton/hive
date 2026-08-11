package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func sampleState() SavedState {
	return SavedState{
		SavedAt: time.Date(2026, 8, 11, 16, 20, 0, 0, time.UTC),
		Sessions: []SavedSession{
			{Name: "hive-he-events", Cwd: "/r/he-events", Repo: "he-events", Label: "he-events"},
			{Name: "hive-he-events-wt-split-1", Cwd: "/r/he-events/.worktrees/split-1", Repo: "he-events", Label: "wt:split-1"},
			{Name: "hive-workspace", Cwd: "/r/workspace", Repo: "workspace", Label: "workspace"},
		},
	}
}

func TestStateStoreRoundTrip(t *testing.T) {
	s := newStateStore(t.TempDir())
	if err := s.Save(sampleState()); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, ok := s.Load()
	if !ok {
		t.Fatal("Load reported nothing to restore")
	}
	if len(got.Sessions) != 3 {
		t.Fatalf("got %d sessions, want 3", len(got.Sessions))
	}
	if got.Sessions[1].Cwd != "/r/he-events/.worktrees/split-1" {
		t.Errorf("cwd not preserved: %q", got.Sessions[1].Cwd)
	}
}

func TestStateStoreMissingFile(t *testing.T) {
	if _, ok := newStateStore(t.TempDir()).Load(); ok {
		t.Error("empty dir should report nothing to restore")
	}
}

func TestStateStoreCorruptFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "state.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := newStateStore(dir).Load(); ok {
		t.Error("corrupt state must not be offered for restore")
	}
}

func TestStateStoreEmptySessionsIsNothingToOffer(t *testing.T) {
	dir := t.TempDir()
	s := newStateStore(dir)
	if err := s.Save(SavedState{SavedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Load(); ok {
		t.Error("a checkpoint with no sessions should not prompt")
	}
}

func TestStateStoreClear(t *testing.T) {
	s := newStateStore(t.TempDir())
	if err := s.Save(sampleState()); err != nil {
		t.Fatal(err)
	}
	if err := s.Clear(); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if _, ok := s.Load(); ok {
		t.Error("state should be gone after Clear")
	}
	// Clearing an absent file is not an error — restore runs it unconditionally.
	if err := s.Clear(); err != nil {
		t.Errorf("second Clear: %v", err)
	}
}

func TestSavedStateSummaryGroupsByRepo(t *testing.T) {
	got := sampleState().Summary()

	if !strings.Contains(got, "he-events  (2 sessions)") {
		t.Errorf("repo with splits should show its count:\n%s", got)
	}
	if strings.Contains(got, "workspace  (1 sessions)") {
		t.Errorf("single-session repo should not be pluralised:\n%s", got)
	}
	if !strings.Contains(got, "Restore all 3? (y/n)") {
		t.Errorf("summary should total the sessions, not the repos:\n%s", got)
	}
	if !strings.Contains(got, "11 Aug 16:20") {
		t.Errorf("summary should say when it was saved:\n%s", got)
	}
}

func TestStateStoreNilReceiver(t *testing.T) {
	var s *StateStore
	if _, ok := s.Load(); ok {
		t.Error("nil store should report nothing")
	}
	if err := s.Save(sampleState()); err != nil {
		t.Errorf("nil Save: %v", err)
	}
	if err := s.Clear(); err != nil {
		t.Errorf("nil Clear: %v", err)
	}
}
