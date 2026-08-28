package main

import (
	"strings"
	"testing"
)

func TestFutureSendPlanTypesASingleLineNote(t *testing.T) {
	plan := futureSendPlan("carry on with the parked work")

	if len(plan) != 2 {
		t.Fatalf("got %d ops, want a literal and an enter: %#v", len(plan), plan)
	}
	if plan[0].literal != "carry on with the parked work" {
		t.Errorf("got literal %q, want the note typed as keystrokes", plan[0].literal)
	}
	if plan[1].key != "enter" {
		t.Errorf("got %q, want the note submitted", plan[1].key)
	}
}

// A newline delivered as a keystroke submits the prompt, so a note written
// across several lines would arrive as several half-finished prompts. It goes
// as a bracketed paste instead, which lands as one block.
func TestFutureSendPlanPastesAMultiLineNote(t *testing.T) {
	note := "rework the importer:\n- keep the csv path\n- drop the xml one"

	plan := futureSendPlan(note)

	if len(plan) != 2 {
		t.Fatalf("got %d ops, want a paste and an enter: %#v", len(plan), plan)
	}
	if plan[0].paste != note {
		t.Errorf("got paste %q, want the whole note", plan[0].paste)
	}
	if plan[0].literal != "" {
		t.Error("a multi-line note was also queued as keystrokes")
	}
	if plan[1].key != "enter" {
		t.Errorf("got %q, want the note submitted after the paste", plan[1].key)
	}
}

func TestFutureSendPlanIgnoresAnEmptyNote(t *testing.T) {
	if plan := futureSendPlan("   \n  "); len(plan) != 0 {
		t.Errorf("got %#v, want nothing sent", plan)
	}
}

func TestFutureSendPlanTrimsTrailingBlankLines(t *testing.T) {
	plan := futureSendPlan("a note\n\n\n")

	if len(plan) != 2 || plan[0].literal != "a note" {
		t.Errorf("trailing blank lines were not trimmed: %#v", plan)
	}
	if strings.Contains(plan[0].literal, "\n") {
		t.Error("a single-line note was sent as a paste")
	}
}
