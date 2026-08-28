package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The bug this fixes: /clear starts a new Claude session id, so the pane ends
// up with two snapshot files and the worst-verdict-wins collision rule keeps
// painting the tab from the session that no longer exists. Observed live:
// hive-he-events-wt-split-2 held an 11:21 wrap_up (47%, $85) next to a 12:20
// keep_going (6%, $1.14), and stayed amber.
func TestSessionEndClearsTheTintLeftByAClearedSession(t *testing.T) {
	rt := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", rt)
	now := time.Date(2026, 8, 28, 12, 30, 0, 0, time.UTC)

	write := func(id, verdict string, ctx float64, at time.Time) {
		s := SessionSnapshot{SessionID: id, TmuxSession: "hive-he-events-wt-split-2",
			Verdict: verdict, CtxPct: ctx, CapturedAt: at}
		if err := writeSnapshot(sessionSnapshotPath(id), s); err != nil {
			t.Fatal(err)
		}
	}
	write("before-clear", VerdictWrapUp, 47, now.Add(-69*time.Minute))
	write("after-clear", VerdictKeepGoing, 6, now.Add(-10*time.Minute))

	cfg := testTelemetryConfig()
	if got := readSessionSnapshotsFrom(sessionSnapshotDir(), cfg, now); got["hive-he-events-wt-split-2"].Verdict != VerdictWrapUp {
		t.Fatalf("precondition: the cleared session should still be tinting, got %q", got["hive-he-events-wt-split-2"].Verdict)
	}

	if code := runSessionEndCmd(strings.NewReader(`{"session_id":"before-clear","reason":"clear"}`)); code != 0 {
		t.Fatalf("runSessionEndCmd = %d, want 0", code)
	}

	got := readSessionSnapshotsFrom(sessionSnapshotDir(), cfg, now)
	if got["hive-he-events-wt-split-2"].Verdict != VerdictKeepGoing {
		t.Errorf("after the cleared session ended the pane reads %q (%.0f%%), want the live session's keep_going",
			got["hive-he-events-wt-split-2"].Verdict, got["hive-he-events-wt-split-2"].CtxPct)
	}
}

// Ending one session must not disturb another. Two sessions really can share a
// directory, and that collision is what the worst-verdict-wins rule exists for
// — the fix has to remove the dead one only.
func TestSessionEndLeavesEveryOtherSnapshotAlone(t *testing.T) {
	rt := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", rt)
	now := time.Date(2026, 8, 28, 12, 30, 0, 0, time.UTC)

	for _, id := range []string{"ending", "sibling"} {
		s := SessionSnapshot{SessionID: id, TmuxSession: "hive-workspace",
			Verdict: VerdictWrapUp, CapturedAt: now}
		if err := writeSnapshot(sessionSnapshotPath(id), s); err != nil {
			t.Fatal(err)
		}
	}

	if code := runSessionEndCmd(strings.NewReader(`{"session_id":"ending","reason":"prompt_input_exit"}`)); code != 0 {
		t.Fatalf("runSessionEndCmd = %d, want 0", code)
	}

	if _, err := os.Stat(sessionSnapshotPath("ending")); !os.IsNotExist(err) {
		t.Errorf("the ended session's snapshot survived: %v", err)
	}
	if _, err := os.Stat(sessionSnapshotPath("sibling")); err != nil {
		t.Errorf("the sibling session's snapshot was removed: %v", err)
	}
}

// A hook that fails is a hook that breaks the session it fires in. Nothing here
// is worth a non-zero exit: the worst case of doing nothing is the tint the fix
// was written to remove, one more time.
func TestSessionEndSurvivesRubbishInput(t *testing.T) {
	rt := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", rt)

	for _, in := range []string{"", "not json", "{}", `{"session_id":""}`, `{"session_id":"never-seen"}`} {
		if code := runSessionEndCmd(strings.NewReader(in)); code != 0 {
			t.Errorf("runSessionEndCmd(%q) = %d, want 0", in, code)
		}
	}
}

// A session id becomes a filename via slugify, so a hostile or odd id must not
// reach outside the snapshot directory.
func TestSessionEndCannotEscapeTheSnapshotDir(t *testing.T) {
	rt := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", rt)

	victim := filepath.Join(rt, "hive", "victim.json")
	if err := os.MkdirAll(filepath.Dir(victim), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(victim, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	if code := runSessionEndCmd(strings.NewReader(`{"session_id":"../victim"}`)); code != 0 {
		t.Fatalf("runSessionEndCmd = %d, want 0", code)
	}
	if _, err := os.Stat(victim); err != nil {
		t.Errorf("a session id escaped the snapshot dir: %v", err)
	}
}
