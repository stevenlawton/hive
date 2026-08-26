package main

import "testing"

// Rewriting a task must keep its identity. rm+add mints a new id and drops the
// claim, and peers address tasks by id — so the only safe rewrite is in place.
func TestEditPreservesIdentityAndState(t *testing.T) {
	dir := newTestRepo(t)
	chdir(t, dir)

	if rc := runTodoAdd([]string{"original subject - original body"}); rc != 0 {
		t.Fatalf("add returned %d", rc)
	}
	before := loadTodos(dir)[0]
	// Set the claim directly: a temp dir has no branch for `claim` to use,
	// and what matters here is that an existing claim survives an edit.
	if _, err := withTodos(dir, func(ts []Todo) []Todo {
		ts[0].Claim = "split-1"
		return ts
	}); err != nil {
		t.Fatal(err)
	}
	claimed := loadTodos(dir)[0]
	if claimed.Claim == "" {
		t.Fatal("fixture is wrong: the task should be claimed")
	}

	if rc := runTodoEdit([]string{before.ID, "new subject", "-d", "new body"}); rc != 0 {
		t.Fatalf("edit returned %d", rc)
	}

	after := loadTodos(dir)[0]
	if after.ID != before.ID {
		t.Errorf("id changed: %q -> %q", before.ID, after.ID)
	}
	if after.Claim != claimed.Claim {
		t.Errorf("claim lost: %q -> %q", claimed.Claim, after.Claim)
	}
	if after.Subject != "new subject" {
		t.Errorf("subject: got %q", after.Subject)
	}
	if after.Description != "new body" {
		t.Errorf("description: got %q", after.Description)
	}
	if after.sectionOrDefault() != before.sectionOrDefault() {
		t.Errorf("section changed: %q -> %q", before.sectionOrDefault(), after.sectionOrDefault())
	}
}

func TestEditAcceptsTheSeparatorForm(t *testing.T) {
	dir := newTestRepo(t)
	chdir(t, dir)
	runTodoAdd([]string{"before"})
	id := loadTodos(dir)[0].ID

	if rc := runTodoEdit([]string{id, "after - with a body"}); rc != 0 {
		t.Fatalf("edit returned %d", rc)
	}
	got := loadTodos(dir)[0]
	if got.Subject != "after" || got.Description != "with a body" {
		t.Errorf("got subject=%q desc=%q", got.Subject, got.Description)
	}
}

func TestEditKeepsDoneState(t *testing.T) {
	dir := newTestRepo(t)
	chdir(t, dir)
	runTodoAdd([]string{"a task"})
	id := loadTodos(dir)[0].ID
	runTodoSetDone([]string{id}, true)

	if rc := runTodoEdit([]string{id, "reworded"}); rc != 0 {
		t.Fatalf("edit returned %d", rc)
	}
	if got := loadTodos(dir)[0]; !got.Done {
		t.Error("editing the text should not reopen a completed task")
	}
}

func TestEditRejectsUnknownRef(t *testing.T) {
	dir := newTestRepo(t)
	chdir(t, dir)
	runTodoAdd([]string{"only"})

	if rc := runTodoEdit([]string{"zzz", "new text"}); rc == 0 {
		t.Error("editing a task that does not exist should fail")
	}
}

func TestEditNeedsText(t *testing.T) {
	dir := newTestRepo(t)
	chdir(t, dir)
	runTodoAdd([]string{"only"})
	id := loadTodos(dir)[0].ID

	if rc := runTodoEdit([]string{id}); rc == 0 {
		t.Error("edit with no replacement text should fail rather than blank the task")
	}
	if got := loadTodos(dir)[0]; got.Subject != "only" {
		t.Errorf("the task should be untouched, got %q", got.Subject)
	}
}
