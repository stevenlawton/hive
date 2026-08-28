package main

import (
	"strings"
	"testing"
	"time"
)

func TestOpeningTheFuturePopupTicksAutoSend(t *testing.T) {
	reset := time.Date(2026, 8, 28, 18, 40, 0, 0, time.UTC).Unix()

	c := newFutureMenu("hive-split-6", FutureQueue{}, reset, "resume")

	if !c.q.AutoSend {
		t.Error("auto send was not ticked on open")
	}
	if c.q.ArmedFor != reset {
		t.Errorf("got ArmedFor %d, want the fleet reset %d captured on open", c.q.ArmedFor, reset)
	}
}

func TestOpeningWithNoKnownResetLeavesAutoSendUnticked(t *testing.T) {
	c := newFutureMenu("hive-split-6", FutureQueue{}, 0, "resume")

	if c.q.AutoSend {
		t.Error("auto send ticked with no reset time known — it could never fire")
	}
}

func TestOpeningKeepsAQueueAlreadyDisarmedByHand(t *testing.T) {
	reset := time.Date(2026, 8, 28, 18, 40, 0, 0, time.UTC).Unix()
	parked := FutureQueue{Prompts: []string{"a note"}, AutoSend: false}

	c := newFutureMenu("hive-split-6", parked, reset, "resume")

	if c.q.AutoSend {
		t.Error("re-opening re-armed a queue the user had deliberately unticked")
	}
}

func TestTogglingAutoResumeDisablesTheEditor(t *testing.T) {
	c := newFutureMenu("hive-split-6", FutureQueue{Prompts: []string{"my own note"}}, 1756400000, "resume")

	c.toggleAutoResume()

	if !c.q.AutoResume {
		t.Fatal("auto resume did not toggle on")
	}
	if c.editorEnabled() {
		t.Error("the prompt editor is still enabled under auto resume")
	}
	if !c.q.AutoSend {
		t.Error("auto resume without auto send would never fire")
	}

	c.toggleAutoResume()

	if !c.editorEnabled() {
		t.Error("unticking auto resume did not give the editor back")
	}
	if len(c.q.Prompts) != 1 || c.q.Prompts[0] != "my own note" {
		t.Errorf("the parked note did not survive the round trip: %#v", c.q.Prompts)
	}
}

func TestFuturePopupHeaderShowsTheResetClock(t *testing.T) {
	reset := time.Date(2026, 8, 28, 18, 40, 0, 0, time.UTC)

	got := futureHeader(reset.Unix())

	want := time.Unix(reset.Unix(), 0).Local().Format("15:04")
	if !strings.Contains(got, want) {
		t.Errorf("header %q does not show the reset clock %q", got, want)
	}
}

func TestFuturePopupHeaderSaysWhenTheResetIsUnknown(t *testing.T) {
	got := futureHeader(0)

	if !strings.Contains(strings.ToLower(got), "unknown") {
		t.Errorf("header %q does not say the reset time is unknown", got)
	}
}

func TestFuturePopupAddsATypedNoteToTheQueue(t *testing.T) {
	c := newFutureMenu("hive-split-6", FutureQueue{}, 1756400000, "resume")
	c.input.SetValue("check the bus first")

	c.commitPrompt()

	if len(c.q.Prompts) != 1 || c.q.Prompts[0] != "check the bus first" {
		t.Fatalf("typed note was not parked: %#v", c.q.Prompts)
	}
	if c.input.Value() != "" {
		t.Error("the field was not cleared, so the next note would append to this one")
	}
}

func TestFuturePopupIgnoresABlankNote(t *testing.T) {
	c := newFutureMenu("hive-split-6", FutureQueue{}, 1756400000, "resume")
	c.input.SetValue("   ")

	c.commitPrompt()

	if len(c.q.Prompts) != 0 {
		t.Errorf("a blank note was parked: %#v", c.q.Prompts)
	}
}

func TestFuturePopupDeletesTheSelectedPrompt(t *testing.T) {
	c := newFutureMenu("hive-split-6", FutureQueue{
		Prompts: []string{"first", "second", "third"},
	}, 1756400000, "resume")
	c.cursor = 1

	c.deletePrompt()

	if len(c.q.Prompts) != 2 || c.q.Prompts[1] != "third" {
		t.Errorf("wrong prompt removed: %#v", c.q.Prompts)
	}
	if c.cursor != 1 {
		t.Errorf("cursor left at %d, want it holding position at 1", c.cursor)
	}
}

func TestFuturePopupRendersTheParkedPromptsAndBothTicks(t *testing.T) {
	c := newFutureMenu("hive-split-6", FutureQueue{
		Prompts: []string{"check the bus"},
	}, time.Date(2026, 8, 28, 18, 40, 0, 0, time.UTC).Unix(), "resume")

	got := renderFutureMenu(c)

	for _, want := range []string{"check the bus", "auto send", "auto resume"} {
		if !strings.Contains(got, want) {
			t.Errorf("popup does not show %q:\n%s", want, got)
		}
	}
}

func TestFuturePopupParksANoteWrittenAcrossLines(t *testing.T) {
	c := newFutureMenu("hive-x", FutureQueue{}, 1756400000, "resume")
	c.input.SetValue("rework the importer:\n- keep the csv path\n- drop the xml one")

	c.commitPrompt()

	if len(c.q.Prompts) != 1 {
		t.Fatalf("got %d prompts, want the note parked whole: %#v", len(c.q.Prompts), c.q.Prompts)
	}
	if !strings.Contains(c.q.Prompts[0], "\n- drop the xml one") {
		t.Errorf("the note was flattened on the way in: %q", c.q.Prompts[0])
	}
}

func TestFutureRowShowsAMultiLineNoteByItsFirstLine(t *testing.T) {
	got := futureRowText(0, "rework the importer:\n- keep the csv\n- drop the xml")

	if strings.Contains(got, "\n") {
		t.Fatalf("a multi-line note broke the row: %q", got)
	}
	if !strings.Contains(got, "rework the importer:") {
		t.Errorf("row %q does not lead with the note's first line", got)
	}
	if !strings.Contains(got, "+2") {
		t.Errorf("row %q does not say how many lines are hidden", got)
	}
}

func TestFutureRowLeavesASingleLineNoteAlone(t *testing.T) {
	got := futureRowText(0, "carry on")

	if strings.Contains(got, "+") {
		t.Errorf("row %q claims hidden lines that do not exist", got)
	}
}
