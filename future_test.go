package main

import (
	"testing"
	"time"
)

func TestFleetResetAtTakesTheFreshestReading(t *testing.T) {
	base := time.Date(2026, 8, 28, 14, 0, 0, 0, time.UTC)
	snaps := map[string]SessionSnapshot{
		"older": {
			HasFiveHour:      true,
			CapturedAt:       base.Add(-3 * time.Hour),
			FiveHourResetsAt: base.Add(30 * time.Minute).Unix(),
		},
		"fresh": {
			HasFiveHour:      true,
			CapturedAt:       base.Add(-1 * time.Minute),
			FiveHourResetsAt: base.Add(90 * time.Minute).Unix(),
		},
	}

	got, ok := fleetResetAt(snaps, base)

	if !ok {
		t.Fatal("no reset time found, want the one from the freshest snapshot")
	}
	if want := base.Add(90 * time.Minute).Unix(); got != want {
		t.Errorf("got reset %d, want %d (the freshest snapshot's)", got, want)
	}
}

func TestFleetResetAtWithNothingReporting(t *testing.T) {
	snaps := map[string]SessionSnapshot{
		"quiet": {HasFiveHour: false, FiveHourResetsAt: 999},
	}

	if _, ok := fleetResetAt(snaps, time.Now()); ok {
		t.Error("a snapshot with no five-hour window supplied a reset time")
	}
}

func TestFutureStoreRoundTripsQueuesByTmuxSession(t *testing.T) {
	dir := t.TempDir()

	want := map[string]FutureQueue{
		"hive-workspace-split-6": {
			Prompts:  []string{"check the bus", "run the suite"},
			AutoSend: true,
			ArmedFor: 1756400000,
		},
		"hive-workspace-split-4": {
			AutoSend:   true,
			AutoResume: true,
			ArmedFor:   1756400000,
		},
	}
	if err := newFutureStore(dir).Save(want); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got := newFutureStore(dir).Queues()

	if len(got) != 2 {
		t.Fatalf("got %d queues, want 2", len(got))
	}
	six := got["hive-workspace-split-6"]
	if len(six.Prompts) != 2 || six.Prompts[0] != "check the bus" {
		t.Errorf("prompts did not survive the round trip: %#v", six.Prompts)
	}
	if six.ArmedFor != 1756400000 {
		t.Errorf("got ArmedFor %d, want 1756400000", six.ArmedFor)
	}
	if !got["hive-workspace-split-4"].AutoResume {
		t.Error("AutoResume did not survive the round trip")
	}
}

func TestFutureStoreResumeTextDefaults(t *testing.T) {
	got := newFutureStore(t.TempDir()).ResumeText()

	if got != "resume" {
		t.Errorf("got resume text %q, want %q", got, "resume")
	}
}

func TestFutureDueWaitsOutTheGraceAfterReset(t *testing.T) {
	reset := time.Date(2026, 8, 28, 18, 40, 0, 0, time.UTC)
	queues := map[string]FutureQueue{
		"blocked": {Prompts: []string{"carry on"}, AutoSend: true, ArmedFor: reset.Unix()},
	}

	if got := futureDue(queues, reset); len(got) != 0 {
		t.Errorf("fired at the reset instant, want it held back: %v", got)
	}
	if got := futureDue(queues, reset.Add(4*time.Minute)); len(got) != 0 {
		t.Errorf("fired inside the grace window, want it held back: %v", got)
	}

	got := futureDue(queues, reset.Add(futureFireGrace))

	if len(got) != 1 || got[0] != "blocked" {
		t.Errorf("got %v due, want [blocked] once the grace has passed", got)
	}
}

func TestFutureDueIgnoresUnarmedQueues(t *testing.T) {
	reset := time.Date(2026, 8, 28, 18, 40, 0, 0, time.UTC)
	queues := map[string]FutureQueue{
		"notes-only":  {Prompts: []string{"a thought"}, AutoSend: false, ArmedFor: reset.Unix()},
		"never-armed": {Prompts: []string{"another"}, AutoSend: true},
	}

	if got := futureDue(queues, reset.Add(time.Hour)); len(got) != 0 {
		t.Errorf("got %v due, want none — neither queue is armed", got)
	}
}

func TestDisarmResumedKeepsThePromptsButStopsTheFiring(t *testing.T) {
	reset := time.Date(2026, 8, 28, 18, 40, 0, 0, time.UTC)
	queues := map[string]FutureQueue{
		"steve-resumed-this": {
			Prompts:  []string{"carry on with the parked work"},
			AutoSend: true,
			ArmedFor: reset.Unix(),
		},
		"still-blocked": {
			Prompts:  []string{"and this one"},
			AutoSend: true,
			ArmedFor: reset.Unix(),
		},
	}

	got, changed := disarmResumed(queues, map[string]bool{"steve-resumed-this": true})

	if !changed {
		t.Fatal("disarmResumed reported no change, want the resumed session disarmed")
	}
	resumed := got["steve-resumed-this"]
	if resumed.AutoSend || resumed.ArmedFor != 0 {
		t.Errorf("resumed session is still armed: %#v", resumed)
	}
	if len(resumed.Prompts) != 1 {
		t.Errorf("disarming threw away the parked notes: %#v", resumed.Prompts)
	}
	if !got["still-blocked"].AutoSend {
		t.Error("a session that never resumed was disarmed too")
	}
}

func TestDisarmResumedClearsAutoResumeOutright(t *testing.T) {
	queues := map[string]FutureQueue{
		"pane": {AutoSend: true, AutoResume: true, ArmedFor: 1756400000},
	}

	got, _ := disarmResumed(queues, map[string]bool{"pane": true})

	if q, ok := got["pane"]; ok && q.AutoResume {
		t.Errorf("auto resume survived the session resuming by hand: %#v", q)
	}
}

func TestDisarmResumedIsANoOpWhenNothingResumed(t *testing.T) {
	queues := map[string]FutureQueue{
		"pane": {Prompts: []string{"x"}, AutoSend: true, ArmedFor: 1756400000},
	}

	if _, changed := disarmResumed(queues, map[string]bool{}); changed {
		t.Error("reported a change with nothing running — this would rewrite future.yaml every tick")
	}
}

func TestFutureNextSendsTheFirstPromptAndLeavesTheRestDraining(t *testing.T) {
	q := FutureQueue{
		Prompts:  []string{"check the bus", "run the suite", "merge if green"},
		AutoSend: true,
		ArmedFor: 1756400000,
	}

	text, rest, ok := futureNext(q, "resume")

	if !ok {
		t.Fatal("nothing to send, want the first parked prompt")
	}
	if text != "check the bus" {
		t.Errorf("got %q, want the first parked prompt", text)
	}
	if len(rest.Prompts) != 2 || rest.Prompts[0] != "run the suite" {
		t.Errorf("the sent prompt was not consumed: %#v", rest.Prompts)
	}
	if rest.ArmedFor != 0 {
		t.Error("the queue is still armed against the reset — it would fire again next tick")
	}
	if !rest.Draining {
		t.Error("the remainder is not draining, so the rest would never be sent")
	}
}

func TestFutureNextOnTheLastPromptStopsDraining(t *testing.T) {
	q := FutureQueue{Prompts: []string{"the only one"}, AutoSend: true, Draining: true}

	_, rest, ok := futureNext(q, "resume")

	if !ok {
		t.Fatal("nothing to send, want the last parked prompt")
	}
	if rest.Draining || rest.AutoSend {
		t.Errorf("an empty queue is still live: %#v", rest)
	}
}

func TestFutureNextAutoResumeSendsTheResumeTextAndIsSpent(t *testing.T) {
	q := FutureQueue{AutoSend: true, AutoResume: true, ArmedFor: 1756400000}

	text, rest, ok := futureNext(q, "carry on")

	if !ok {
		t.Fatal("auto resume sent nothing")
	}
	if text != "carry on" {
		t.Errorf("got %q, want the configured resume text", text)
	}
	if rest.AutoResume || rest.AutoSend || rest.ArmedFor != 0 || rest.Draining {
		t.Errorf("auto resume fired more than once: %#v", rest)
	}
}

func TestFutureNextAutoResumeIgnoresParkedPrompts(t *testing.T) {
	q := FutureQueue{
		Prompts:    []string{"these are disabled in the popup"},
		AutoSend:   true,
		AutoResume: true,
	}

	text, _, _ := futureNext(q, "resume")

	if text != "resume" {
		t.Errorf("got %q, want the resume text — auto resume greys the editor out", text)
	}
}

func TestFutureNextOnAnEmptyQueueSendsNothing(t *testing.T) {
	if _, _, ok := futureNext(FutureQueue{AutoSend: true}, "resume"); ok {
		t.Error("an empty queue produced something to send")
	}
}

func TestFutureDrainReadyOnlyWhenTheSessionIsIdle(t *testing.T) {
	queues := map[string]FutureQueue{
		"working": {Prompts: []string{"next"}, AutoSend: true, Draining: true},
		"waiting": {Prompts: []string{"next"}, AutoSend: true, Draining: true},
		"parked":  {Prompts: []string{"not draining yet"}, AutoSend: true, ArmedFor: 1756400000},
	}

	got := futureDrainReady(queues, map[string]bool{"working": true})

	if len(got) != 1 || got[0] != "waiting" {
		t.Errorf("got %v ready to drain, want [waiting] — the busy one keeps its turn", got)
	}
}

func TestArmFutureCapturesTheResetTime(t *testing.T) {
	q := armFuture(FutureQueue{Prompts: []string{"a note"}}, 1756400000)

	if !q.AutoSend {
		t.Error("arming did not set auto send")
	}
	if q.ArmedFor != 1756400000 {
		t.Errorf("got ArmedFor %d, want the reset captured at arm time", q.ArmedFor)
	}
}

func TestArmFutureWithNoKnownResetLeavesItUnarmed(t *testing.T) {
	q := armFuture(FutureQueue{Prompts: []string{"a note"}}, 0)

	if q.AutoSend || q.ArmedFor != 0 {
		t.Errorf("armed against an unknown reset time, so it would never fire: %#v", q)
	}
}

func TestFleetResetAtIgnoresAWindowThatHasAlreadyPassed(t *testing.T) {
	now := time.Date(2026, 8, 28, 14, 0, 0, 0, time.UTC)
	snaps := map[string]SessionSnapshot{
		"yesterday": {
			HasFiveHour:      true,
			CapturedAt:       now.Add(-30 * time.Second),
			FiveHourResetsAt: now.Add(-3 * time.Hour).Unix(),
		},
	}

	if got, ok := fleetResetAt(snaps, now); ok {
		t.Errorf("armed against a reset %d that has already passed", got)
	}
}

func TestFleetResetAtIgnoresStaleSnapshots(t *testing.T) {
	now := time.Date(2026, 8, 28, 14, 0, 0, 0, time.UTC)
	snaps := map[string]SessionSnapshot{
		"gone-quiet": {
			HasFiveHour:      true,
			Stale:            true,
			CapturedAt:       now.Add(-90 * time.Minute),
			FiveHourResetsAt: now.Add(time.Hour).Unix(),
		},
	}

	if _, ok := fleetResetAt(snaps, now); ok {
		t.Error("took a reset time from a snapshot that had gone stale")
	}
}

func TestFutureDueIgnoresAnArmingFromAWindowLongGone(t *testing.T) {
	now := time.Date(2026, 8, 28, 14, 0, 0, 0, time.UTC)
	queues := map[string]FutureQueue{
		"yesterday": {
			Prompts:  []string{"this was for a window that closed long ago"},
			AutoSend: true,
			ArmedFor: now.Add(-26 * time.Hour).Unix(),
		},
	}

	if got := futureDue(queues, now); len(got) != 0 {
		t.Errorf("got %v due — a day-old arming fired into whatever the session is doing now", got)
	}
}
