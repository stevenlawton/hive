package main

import (
	"strings"
	"testing"
)

func TestStateVerbMovesForward(t *testing.T) {
	dir := newTestRepo(t)
	chdir(t, dir)

	if rc := runTodoAdd([]string{"fix the parser - it eats flags"}); rc != 0 {
		t.Fatalf("add returned %d", rc)
	}
	id := loadTodos(dir)[0].ID

	if rc := runTodoState([]string{id, "plan-review"}); rc != 0 {
		t.Fatalf("state returned %d", rc)
	}
	if got := loadTodos(dir)[0].State; got != StatePlanReview {
		t.Errorf("state: got %q, want %q", got, StatePlanReview)
	}
}

// Going backwards without saying why produces a ticket that gets replanned
// identically, so the note is required rather than encouraged.
func TestBackwardsMoveRequiresANote(t *testing.T) {
	dir := newTestRepo(t)
	chdir(t, dir)
	runTodoAdd([]string{"fix the parser"})
	id := loadTodos(dir)[0].ID
	runTodoState([]string{id, "ready"})

	if rc := runTodoState([]string{id, "plan-review"}); rc == 0 {
		t.Fatal("backwards move without a note should fail")
	}
	if got := loadTodos(dir)[0].State; got != StateReady {
		t.Errorf("state changed despite the error: %q", got)
	}

	if rc := runTodoState([]string{id, "plan-review", "--note", "contract missed the retry path"}); rc != 0 {
		t.Fatalf("state with note returned %d", rc)
	}
	after := loadTodos(dir)[0]
	if after.State != StatePlanReview {
		t.Errorf("state: got %q", after.State)
	}
	if !strings.Contains(after.Description, "contract missed the retry path") {
		t.Errorf("note not recorded: %q", after.Description)
	}
}

func TestStateVerbRejectsUnknownState(t *testing.T) {
	dir := newTestRepo(t)
	chdir(t, dir)
	runTodoAdd([]string{"fix the parser"})
	id := loadTodos(dir)[0].ID

	if rc := runTodoState([]string{id, "banana"}); rc == 0 {
		t.Fatal("unknown state should be refused")
	}
}

func TestStateVerbClears(t *testing.T) {
	dir := newTestRepo(t)
	chdir(t, dir)
	runTodoAdd([]string{"fix the parser"})
	id := loadTodos(dir)[0].ID
	runTodoState([]string{id, "triage"})

	if rc := runTodoState([]string{id, "clear", "--note", "starting over"}); rc != 0 {
		t.Fatalf("clear returned %d", rc)
	}
	if got := loadTodos(dir)[0].State; got != StateUnrefined {
		t.Errorf("state: got %q, want empty", got)
	}
}
