package main

import (
	"testing"
	"time"
)

func TestReapReleasesDeadAndStaleClaims(t *testing.T) {
	// The caller derives the cutoff as now-older, so a fresh claim is stamped
	// after it and a stale one before.
	now := time.Date(2026, 8, 21, 10, 0, 0, 0, time.UTC)
	cutoff := now.Add(-4 * time.Hour)
	stamp := func(ago time.Duration) string { return now.Add(-ago).Format(time.RFC3339) }
	todos := []Todo{
		{Subject: "live worker", ID: "aaa", Claim: "split-1",
			Since: stamp(time.Minute), State: StateReady},
		{Subject: "dead worktree", ID: "bbb", Claim: "split-9",
			Since: stamp(time.Minute), State: StatePlanReview},
		{Subject: "stale but live branch", ID: "ccc", Claim: "split-1",
			Since: stamp(5 * time.Hour), State: StateReady},
		{Subject: "unclaimed", ID: "ddd"},
	}
	live := map[string]bool{"split-1": true}

	got, released := reapClaims(todos, live, cutoff)

	if got[0].Claim != "split-1" {
		t.Errorf("live claim was reaped: %#v", got[0])
	}
	if got[1].Claim != "" || got[2].Claim != "" {
		t.Errorf("dead/stale claims survived: %q %q", got[1].Claim, got[2].Claim)
	}
	if got[1].State != StatePlanReview {
		t.Errorf("reaping must not touch state: %q", got[1].State)
	}
	if got[1].Since != "" || got[2].Since != "" {
		t.Error("since should be cleared with the claim")
	}
	if len(released) != 2 {
		t.Errorf("released %d, want 2: %v", len(released), released)
	}
}

// An unparseable or absent timestamp must not be read as "infinitely old" —
// that would reap a claim taken by a hive too old to stamp one.
func TestReapKeepsClaimsWithNoTimestampWhenBranchIsLive(t *testing.T) {
	cutoff := time.Date(2026, 8, 21, 10, 0, 0, 0, time.UTC)
	todos := []Todo{{Subject: "x", ID: "aaa", Claim: "split-1"}}
	got, released := reapClaims(todos, map[string]bool{"split-1": true}, cutoff)
	if got[0].Claim == "" {
		t.Error("claim with no timestamp on a live branch was reaped")
	}
	if len(released) != 0 {
		t.Errorf("released %v", released)
	}
}
