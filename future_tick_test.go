package main

import (
	"testing"
	"time"
)

func TestFutureWorkFiresADueQueue(t *testing.T) {
	reset := time.Date(2026, 8, 28, 18, 40, 0, 0, time.UTC)
	now := reset.Add(futureFireGrace)
	queues := map[string]FutureQueue{
		"blocked": {Prompts: []string{"carry on", "then this"}, AutoSend: true, ArmedFor: reset.Unix()},
	}

	got, sends, changed := futureWork(queues, map[string]bool{}, "resume", now)

	if !changed {
		t.Fatal("futureWork reported no change on a due queue")
	}
	if len(sends) != 1 || sends[0].session != "blocked" || sends[0].text != "carry on" {
		t.Fatalf("got sends %#v, want the first parked prompt for blocked", sends)
	}
	if got["blocked"].ArmedFor != 0 {
		t.Error("the queue is still armed and would fire again next tick")
	}
	if !got["blocked"].AwaitingPickup {
		t.Error("the queue is not awaiting pickup, so prompt #2 could go before #1 lands")
	}
}

func TestFutureWorkDoesNotFireBeforeTheGrace(t *testing.T) {
	reset := time.Date(2026, 8, 28, 18, 40, 0, 0, time.UTC)
	queues := map[string]FutureQueue{
		"blocked": {Prompts: []string{"carry on"}, AutoSend: true, ArmedFor: reset.Unix()},
	}

	_, sends, changed := futureWork(queues, map[string]bool{}, "resume", reset.Add(time.Minute))

	if len(sends) != 0 {
		t.Errorf("fired inside the grace window: %#v", sends)
	}
	if changed {
		t.Error("reported a change with nothing to do — this rewrites future.yaml every tick")
	}
}

func TestFutureWorkDisarmsASessionThatResumedByHand(t *testing.T) {
	reset := time.Date(2026, 8, 28, 18, 40, 0, 0, time.UTC)
	queues := map[string]FutureQueue{
		"steve-typed-here": {Prompts: []string{"carry on"}, AutoSend: true, ArmedFor: reset.Unix()},
	}

	got, sends, changed := futureWork(queues,
		map[string]bool{"steve-typed-here": true}, "resume", reset.Add(time.Hour))

	if len(sends) != 0 {
		t.Fatalf("fired into a session that had already resumed: %#v", sends)
	}
	if !changed {
		t.Error("the disarm was not persisted")
	}
	if got["steve-typed-here"].AutoSend {
		t.Error("the queue is still armed after the session resumed")
	}
	if len(got["steve-typed-here"].Prompts) != 1 {
		t.Error("disarming threw away the parked note")
	}
}

func TestFutureWorkAutoResumeSendsTheResumeText(t *testing.T) {
	reset := time.Date(2026, 8, 28, 18, 40, 0, 0, time.UTC)
	queues := map[string]FutureQueue{
		"pane": {AutoSend: true, AutoResume: true, ArmedFor: reset.Unix()},
	}

	got, sends, _ := futureWork(queues, map[string]bool{}, "carry on", reset.Add(futureFireGrace))

	if len(sends) != 1 || sends[0].text != "carry on" {
		t.Fatalf("got %#v, want the configured resume text once", sends)
	}
	if got["pane"].AutoResume {
		t.Error("auto resume stayed armed and would fire again")
	}
}

func TestFutureWorkWaitsForPickupBeforeDraining(t *testing.T) {
	now := time.Now()
	queues := map[string]FutureQueue{
		"just-fired": {
			Prompts:        []string{"the second one"},
			AutoSend:       true,
			Draining:       true,
			AwaitingPickup: true,
			SentAt:         now.Unix(),
		},
	}

	_, sends, _ := futureWork(queues, map[string]bool{}, "resume", now)

	if len(sends) != 0 {
		t.Errorf("sent prompt #2 before #1 was picked up: %#v", sends)
	}
}

func TestFutureWorkDrainsOnceTheSessionHasTakenItsTurn(t *testing.T) {
	now := time.Date(2026, 8, 28, 18, 50, 0, 0, time.UTC)
	queues := map[string]FutureQueue{
		"working": {
			Prompts:        []string{"the second one"},
			AutoSend:       true,
			Draining:       true,
			AwaitingPickup: true,
			SentAt:         now.Unix(),
		},
	}

	// The session picks the prompt up and starts generating.
	queues, sends, _ := futureWork(queues, map[string]bool{"working": true}, "resume", now)
	if len(sends) != 0 {
		t.Fatalf("sent while the session was still generating: %#v", sends)
	}
	if queues["working"].AwaitingPickup {
		t.Fatal("seeing the session run did not clear the awaiting-pickup flag")
	}

	// It finishes the turn and goes idle.
	_, sends, _ = futureWork(queues, map[string]bool{}, "resume", now.Add(time.Minute))

	if len(sends) != 1 || sends[0].text != "the second one" {
		t.Fatalf("got %#v, want the next parked prompt once the session went idle", sends)
	}
}

func TestFutureWorkWithNothingParkedIsQuiet(t *testing.T) {
	_, sends, changed := futureWork(map[string]FutureQueue{}, map[string]bool{}, "resume", time.Now())

	if len(sends) != 0 || changed {
		t.Errorf("got sends %#v changed %v, want a quiet no-op", sends, changed)
	}
}

// A session blocked on the five-hour limit stops rendering its statusline, so
// its snapshot goes stale. It may still report as generating — which must not
// be mistaken for the user having resumed it, or the queue would disarm itself
// the moment it was armed and silently never fire.
func TestFutureWorkDoesNotMistakeAStalledSessionForAResumedOne(t *testing.T) {
	reset := time.Date(2026, 8, 28, 18, 40, 0, 0, time.UTC)
	queues := map[string]FutureQueue{
		"stalled": {Prompts: []string{"carry on"}, AutoSend: true, ArmedFor: reset.Unix()},
	}
	generating := map[string]bool{"stalled": true}
	resumed := map[string]bool{} // nothing is reporting fresh telemetry

	got, _, _ := futureWorkFor(queues, generating, resumed, "resume", reset.Add(time.Minute))

	if !got["stalled"].AutoSend || got["stalled"].ArmedFor == 0 {
		t.Errorf("a stalled session was treated as resumed and disarmed: %#v", got["stalled"])
	}
}

func TestFutureWorkDisarmsOnFreshEvidenceOfAResume(t *testing.T) {
	reset := time.Date(2026, 8, 28, 18, 40, 0, 0, time.UTC)
	queues := map[string]FutureQueue{
		"steve-typed-here": {Prompts: []string{"carry on"}, AutoSend: true, ArmedFor: reset.Unix()},
	}
	live := map[string]bool{"steve-typed-here": true}

	got, sends, _ := futureWorkFor(queues, live, live, "resume", reset.Add(time.Hour))

	if len(sends) != 0 {
		t.Fatalf("fired into a session that had genuinely resumed: %#v", sends)
	}
	if got["steve-typed-here"].AutoSend {
		t.Error("a session reporting fresh telemetry while generating was not disarmed")
	}
}

// A drain waits for the session to be seen generating before sending the next
// prompt. If that is never observed — the pane died, or the turn began and
// ended inside one tick — the rest of the queue must not sit there forever.
func TestFutureWorkGivesUpWaitingForAPickupThatNeverComes(t *testing.T) {
	now := time.Date(2026, 8, 28, 18, 40, 0, 0, time.UTC)
	queues := map[string]FutureQueue{
		"quiet": {
			Prompts:        []string{"the second one"},
			AutoSend:       true,
			Draining:       true,
			AwaitingPickup: true,
			SentAt:         now.Add(-futurePickupWait - time.Minute).Unix(),
		},
	}

	_, sends, _ := futureWorkFor(queues, map[string]bool{}, map[string]bool{}, "resume", now)

	if len(sends) != 1 || sends[0].text != "the second one" {
		t.Fatalf("got %#v, want the drain to continue once the wait expired", sends)
	}
}

func TestFutureWorkStillWaitsInsideThePickupWindow(t *testing.T) {
	now := time.Date(2026, 8, 28, 18, 40, 0, 0, time.UTC)
	queues := map[string]FutureQueue{
		"just-sent": {
			Prompts:        []string{"the second one"},
			AutoSend:       true,
			Draining:       true,
			AwaitingPickup: true,
			SentAt:         now.Add(-10 * time.Second).Unix(),
		},
	}

	_, sends, _ := futureWorkFor(queues, map[string]bool{}, map[string]bool{}, "resume", now)

	if len(sends) != 0 {
		t.Errorf("sent prompt #2 ten seconds after #1: %#v", sends)
	}
}
