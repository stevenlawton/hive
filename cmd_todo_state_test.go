package main

import (
	"bytes"
	"os"
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

// The drawer is where a human works the plan-review and triage queues, so a
// ticket's state has to be visible there without opening anything.
func TestDrawerRowShowsState(t *testing.T) {
	row := drawerRow(Todo{Subject: "fix the parser", ID: "lxg", State: StatePlanReview},
		false, "split-1", 80)
	if !strings.Contains(row, "plan-review") {
		t.Errorf("state missing from row: %q", row)
	}
}

// A finished ticket's last state is history, not a queue it is still sitting in.
func TestDrawerRowOmitsStateWhenDone(t *testing.T) {
	row := drawerRow(Todo{Subject: "fix the parser", ID: "lxg", State: StateTriage, Done: true},
		false, "split-1", 80)
	if strings.Contains(row, "triage") {
		t.Errorf("done task should not show a state: %q", row)
	}
}

// `hive todo list` is the other place a state is read from — the CLI half of
// the same change, and what /next's selector greps.
func TestListShowsState(t *testing.T) {
	dir := newTestRepo(t)
	chdir(t, dir)

	if rc := runTodoAdd([]string{"fix the parser - it eats flags"}); rc != 0 {
		t.Fatalf("add returned %d", rc)
	}
	id := loadTodos(dir)[0].ID
	if rc := runTodoState([]string{id, "plan-review"}); rc != 0 {
		t.Fatalf("state returned %d", rc)
	}

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdout
	os.Stdout = w
	rc := runTodoList()
	os.Stdout = orig
	w.Close()

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil {
		t.Fatal(err)
	}
	if rc != 0 {
		t.Fatalf("list returned %d", rc)
	}
	if out := buf.String(); !strings.Contains(out, "plan-review") {
		t.Errorf("state missing from list output:\n%s", out)
	}
}
