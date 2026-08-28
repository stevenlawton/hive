package main

import (
	"testing"
	"time"
)

func TestLimitBannerFindsTheResetClock(t *testing.T) {
	now := time.Date(2026, 8, 28, 14, 0, 0, 0, time.Local)

	cases := []struct {
		name string
		pane string
		want time.Time
	}{
		{
			name: "sentence form with a bare hour",
			pane: "Claude usage limit reached. Your limit will reset at 6pm.",
			want: time.Date(2026, 8, 28, 18, 0, 0, 0, time.Local),
		},
		{
			name: "status form with minutes",
			pane: "5-hour limit reached ∙ resets 3:30pm",
			want: time.Date(2026, 8, 28, 15, 30, 0, 0, time.Local),
		},
		{
			name: "twenty-four hour clock",
			pane: "You've hit your usage limit — resets at 18:40",
			want: time.Date(2026, 8, 28, 18, 40, 0, 0, time.Local),
		},
		{
			name: "a reset already past today is tomorrow's",
			pane: "usage limit reached · resets at 9:00am",
			want: time.Date(2026, 8, 29, 9, 0, 0, 0, time.Local),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := limitBannerReset(tc.pane, now)
			if !ok {
				t.Fatalf("no reset found in %q", tc.pane)
			}
			if got != tc.want.Unix() {
				t.Errorf("got %s, want %s",
					time.Unix(got, 0).Local(), tc.want)
			}
		})
	}
}

func TestLimitBannerIgnoresUnrelatedPaneText(t *testing.T) {
	now := time.Date(2026, 8, 28, 14, 0, 0, 0, time.Local)

	for _, pane := range []string{
		"",
		"$ go test ./... — ok 9.5s",
		"the meeting resets at 3pm",              // no mention of a limit
		"rate limit middleware added to auth.go", // no reset
	} {
		if got, ok := limitBannerReset(pane, now); ok {
			t.Errorf("found reset %d in unrelated text %q", got, pane)
		}
	}
}

func TestLimitBannerReadsTheLastMatchingLine(t *testing.T) {
	now := time.Date(2026, 8, 28, 14, 0, 0, 0, time.Local)
	pane := "usage limit reached · resets at 15:00\n" +
		"...\n" +
		"usage limit reached · resets at 19:00"

	got, ok := limitBannerReset(pane, now)

	if !ok {
		t.Fatal("no reset found")
	}
	want := time.Date(2026, 8, 28, 19, 0, 0, 0, time.Local).Unix()
	if got != want {
		t.Errorf("got %s, want the most recent banner at 19:00",
			time.Unix(got, 0).Local())
	}
}

func TestPaneShowsLimitBanner(t *testing.T) {
	if !paneShowsLimitBanner("Claude usage limit reached. Your limit will reset at 6pm.") {
		t.Error("did not recognise a limit banner")
	}
	if paneShowsLimitBanner("$ ls -la") {
		t.Error("mistook ordinary pane text for a limit banner")
	}
}

func TestPopupFallsBackToTheBannerWhenTelemetryIsSilent(t *testing.T) {
	prev := capturePane
	capturePane = func(string) (string, error) {
		return "Claude usage limit reached · resets at 23:30", nil
	}
	t.Cleanup(func() { capturePane = prev })

	m := model{
		width: 100, height: 40,
		futureStore: newFutureStore(t.TempDir()),
		snapshotsFor: func(time.Time) map[string]SessionSnapshot {
			return map[string]SessionSnapshot{} // nothing reporting
		},
	}

	m.openFuturePopup("hive-x", 2, 2)

	if m.future.resetAt == 0 {
		t.Fatal("popup found no reset time, though the pane is showing one")
	}
	if !m.future.q.AutoSend {
		t.Error("the queue was not armed against the banner's reset")
	}
	if m.future.resetSource != futureResetFromBanner {
		t.Errorf("got source %v, want the banner", m.future.resetSource)
	}
}

func TestPopupPrefersTelemetryOverTheBanner(t *testing.T) {
	prev := capturePane
	capturePane = func(string) (string, error) {
		return "usage limit reached · resets at 23:30", nil
	}
	t.Cleanup(func() { capturePane = prev })

	fleet := time.Now().Add(90 * time.Minute).Truncate(time.Second)
	m := model{
		width: 100, height: 40,
		futureStore: newFutureStore(t.TempDir()),
		snapshotsFor: func(now time.Time) map[string]SessionSnapshot {
			return map[string]SessionSnapshot{
				"other": {HasFiveHour: true, CapturedAt: now, FiveHourResetsAt: fleet.Unix()},
			}
		},
	}

	m.openFuturePopup("hive-x", 2, 2)

	if m.future.resetAt != fleet.Unix() {
		t.Errorf("got reset %d, want the fleet's %d", m.future.resetAt, fleet.Unix())
	}
	if m.future.resetSource != futureResetFromFleet {
		t.Errorf("got source %v, want the fleet", m.future.resetSource)
	}
}
