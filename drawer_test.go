package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// newDrawerModel builds a model with the drawer open on a temp repo seeded with
// the given subjects.
func newDrawerModel(t *testing.T, subjects ...string) (model, string) {
	t.Helper()
	dir := t.TempDir()
	for _, s := range subjects {
		if _, err := withTodos(dir, func(ts []Todo) []Todo {
			return addTodo(ts, "Tasks", s, "")
		}); err != nil {
			t.Fatal(err)
		}
	}
	m := model{drawerOpen: true, drawerRepo: dir, drawerClaim: "wt-test", chord: NewChordHandler(500 * time.Millisecond)}
	m.loadDrawerTodos(dir)
	return m, dir
}

func press(t *testing.T, m model, r rune) model {
	t.Helper()
	out, _ := m.handleDrawerKey(tea.KeyPressMsg{Code: r, Text: string(r)})
	mm, ok := out.(model)
	if !ok {
		t.Fatalf("handleDrawerKey returned %T, want model", out)
	}
	return mm
}

// The drawer's old whole-array write reverted anything a peer did while its copy
// was in memory. A delta must leave the peer's task alone.
func TestDrawerToggleDoesNotClobberAPeersConcurrentAdd(t *testing.T) {
	m, dir := newDrawerModel(t, "alpha", "beta")
	m.drawerCursor = 0
	targetID := m.drawerTodos[0].ID

	// A peer adds a task while the drawer holds its stale copy.
	if _, err := withTodos(dir, func(ts []Todo) []Todo {
		return addTodo(ts, "Tasks", "peer-task", "")
	}); err != nil {
		t.Fatal(err)
	}

	m = press(t, m, 'x') // toggle done on the cursor

	got := loadTodos(dir)
	if len(got) != 3 {
		t.Fatalf("got %d tasks, want 3 — the peer's add was clobbered", len(got))
	}
	var found bool
	for _, td := range got {
		if td.ID == targetID {
			found = true
			if !td.Done {
				t.Error("the cursor's task was not marked done")
			}
		}
		if td.Subject == "peer-task" && td.Done {
			t.Error("the peer's task was wrongly marked done")
		}
	}
	if !found {
		t.Error("the cursor's task vanished")
	}
}

// A peer inserting above the cursor must not slide the cursor onto a different
// task.
func TestDrawerCursorFollowsItsTaskByID(t *testing.T) {
	m, dir := newDrawerModel(t, "alpha", "beta", "gamma")
	m.drawerCursor = 2
	wantID := m.drawerTodos[2].ID

	if _, err := withTodos(dir, func(ts []Todo) []Todo {
		return append([]Todo{{Section: "Tasks", Subject: "inserted"}}, ts...)
	}); err != nil {
		t.Fatal(err)
	}

	m = press(t, m, 'x')

	if m.drawerTodos[m.drawerCursor].ID != wantID {
		t.Errorf("cursor landed on %q, want the task it was on (%q)",
			m.drawerTodos[m.drawerCursor].ID, wantID)
	}
}

// The 100ms tick path re-reads the list by index, not by id. A peer insert
// landing between ticks must not slide the cursor onto a different task —
// otherwise the next keypress (e.g. delete) acts on the wrong one.
func TestRefreshDrawerFromDiskKeepsCursorOnItsTaskByID(t *testing.T) {
	m, dir := newDrawerModel(t, "alpha", "beta", "gamma")
	m.drawerCursor = 2
	wantID := m.drawerTodos[2].ID

	// A peer inserts a task above the cursor.
	if _, err := withTodos(dir, func(ts []Todo) []Todo {
		return append([]Todo{{Section: "Tasks", Subject: "inserted"}}, ts...)
	}); err != nil {
		t.Fatal(err)
	}

	m.refreshDrawerFromDisk()

	if m.drawerCursor < 0 || m.drawerCursor >= len(m.drawerTodos) || m.drawerTodos[m.drawerCursor].ID != wantID {
		t.Errorf("cursor landed on index %d, want the task it was on (id %q)", m.drawerCursor, wantID)
	}
}

// Opening the drawer on a repo that never had a TODO.md must not create one —
// it should be a pure read, like it was before withTodos existed.
func TestLoadDrawerTodosDoesNotCreateFileWhenNoneExists(t *testing.T) {
	dir := t.TempDir()
	var m model
	m.loadDrawerTodos(dir)

	if _, err := os.Stat(filepath.Join(dir, "docs", "TODO.md")); !os.IsNotExist(err) {
		t.Error("loadDrawerTodos created docs/TODO.md in a repo that never had one")
	}
	if _, err := os.Stat(filepath.Join(dir, "docs")); !os.IsNotExist(err) {
		t.Error("loadDrawerTodos created a docs/ directory in a repo that never had one")
	}
}

func TestDrawerDeleteClampsCursorWhenTaskIsGone(t *testing.T) {
	m, _ := newDrawerModel(t, "alpha", "beta")
	m.drawerCursor = 1
	m = press(t, m, 'd')

	if len(m.drawerTodos) != 1 {
		t.Fatalf("got %d tasks after delete, want 1", len(m.drawerTodos))
	}
	if m.drawerCursor != 0 {
		t.Errorf("cursor = %d, want 0 after deleting the last row", m.drawerCursor)
	}
}
