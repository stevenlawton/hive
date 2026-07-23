package main

import "strings"
import "testing"

func TestParseTodoLine(t *testing.T) {
	cases := []struct {
		line    string
		ok      bool
		status  TodoStatus
		subject string
		desc    string
	}{
		{"- [ ] **#142 fix cache** — add a bounded cache", true, TodoPending, "#142 fix cache", "add a bounded cache"},
		{"- [~] **refactor pipeline**", true, TodoCurrent, "refactor pipeline", ""},
		{"- [x] **done thing** — it shipped", true, TodoDone, "done thing", "it shipped"},
		{"- [ ] plain subject — no bold", true, TodoPending, "plain subject", "no bold"},
		{"- [ ] just a subject", true, TodoPending, "just a subject", ""},
		{"- [ ]", false, 0, "", ""},
		{"### section", false, 0, "", ""},
		{"random", false, 0, "", ""},
	}
	for _, c := range cases {
		got, ok := parseTodoLine(c.line)
		if ok != c.ok {
			t.Errorf("parseTodoLine(%q) ok=%v want %v", c.line, ok, c.ok)
			continue
		}
		if ok && (got.Status != c.status || got.Subject != c.subject || got.Description != c.desc) {
			t.Errorf("parseTodoLine(%q) = %+v want status=%v subj=%q desc=%q", c.line, got, c.status, c.subject, c.desc)
		}
	}
}

func TestParseTodosWithSections(t *testing.T) {
	block := "Last sync: **2026-07-22**\n\n### Alpha\n\n- [ ] **one** — first\n- [x] **two**\n\n### Beta\n\n- [~] **three** — third\n"
	got := parseTodos(block)
	if len(got) != 3 {
		t.Fatalf("parsed %d, want 3: %+v", len(got), got)
	}
	if got[0].Section != "Alpha" || got[1].Section != "Alpha" || got[2].Section != "Beta" {
		t.Errorf("sections wrong: %q %q %q", got[0].Section, got[1].Section, got[2].Section)
	}
	if got[2].Subject != "three" || got[2].Description != "third" || got[2].Status != TodoCurrent {
		t.Errorf("beta task wrong: %+v", got[2])
	}
}

func TestFormatRoundTrip(t *testing.T) {
	todos := []Todo{
		{TodoPending, "Alpha", "one", "first"},
		{TodoDone, "Alpha", "two", ""},
		{TodoCurrent, "Beta", "three", "third desc"},
	}
	got := parseTodos(formatTodos(todos))
	if len(got) != len(todos) {
		t.Fatalf("round trip len %d want %d", len(got), len(todos))
	}
	for i := range todos {
		if got[i] != todos[i] {
			t.Errorf("round trip[%d] = %+v want %+v", i, got[i], todos[i])
		}
	}
}

func TestReplaceBlockPreservesSurroundings(t *testing.T) {
	content := "# Open work\n\nsome intro\n\n" +
		"<!-- TASKS:BEGIN (auto-generated ...) -->\nLast sync: **old**\n- [ ] **old task**\n" + tasksEnd +
		"\n\n## Recently completed\n\n- shipped a thing\n\n## Notes\nkeep me\n"

	out := replaceBlock(content, "Last sync: **new**\n\n### S\n\n- [ ] **new task**\n")

	for _, must := range []string{"# Open work", "some intro", "## Recently completed", "shipped a thing", "## Notes", "keep me", "new task"} {
		if !strings.Contains(out, must) {
			t.Errorf("replaceBlock dropped %q\n---\n%s", must, out)
		}
	}
	if strings.Contains(out, "old task") {
		t.Errorf("replaceBlock kept stale block body:\n%s", out)
	}
	// The original BEGIN marker line (with its comment) is preserved.
	if !strings.Contains(out, "TASKS:BEGIN (auto-generated ...)") {
		t.Errorf("original BEGIN marker not preserved:\n%s", out)
	}
	// Exactly one BEGIN and one END marker remain.
	if strings.Count(out, "TASKS:BEGIN") != 1 || strings.Count(out, "TASKS:END") != 1 {
		t.Errorf("marker count off:\n%s", out)
	}
}

func TestReplaceBlockScaffoldsWhenAbsent(t *testing.T) {
	out := replaceBlock("", "### S\n\n- [ ] **task**\n")
	if !strings.Contains(out, tasksBegin) || !strings.Contains(out, tasksEnd) || !strings.Contains(out, "task") {
		t.Errorf("scaffold missing markers/body:\n%s", out)
	}
	// Round-trips through extract+parse.
	if got := parseTodos(extractBlock(out)); len(got) != 1 || got[0].Subject != "task" {
		t.Errorf("scaffold didn't round-trip: %+v", got)
	}
}

func TestAddTodo(t *testing.T) {
	todos := addTodo(nil, "Sec", "subj", "desc")
	if len(todos) != 1 || todos[0].Section != "Sec" || todos[0].Subject != "subj" || todos[0].Description != "desc" || todos[0].Status != TodoPending {
		t.Errorf("addTodo wrong: %+v", todos)
	}
	if got := addTodo(todos, "", "  ", ""); len(got) != 1 {
		t.Errorf("blank subject should be ignored: %+v", got)
	}
	if addTodo(nil, "", "x", "")[0].Section != defaultSection {
		t.Errorf("blank section should default to %q", defaultSection)
	}
}

func TestSetTodoCurrentSingle(t *testing.T) {
	todos := []Todo{{TodoCurrent, "S", "a", ""}, {TodoPending, "S", "b", ""}}
	todos = setTodoCurrent(todos, 1)
	if todos[0].Status != TodoPending || todos[1].Status != TodoCurrent {
		t.Errorf("single-current not enforced: %+v", todos)
	}
	todos = setTodoCurrent(todos, 1)
	if todos[1].Status != TodoPending {
		t.Errorf("toggling current should clear it: %+v", todos)
	}
}

func TestCurrentTodo(t *testing.T) {
	if currentTodo(nil) != nil {
		t.Error("empty → nil")
	}
	todos := []Todo{{TodoDone, "S", "a", ""}, {TodoPending, "S", "b", ""}, {TodoCurrent, "S", "c", ""}}
	if c := currentTodo(todos); c == nil || c.Subject != "c" {
		t.Errorf("want current 'c', got %+v", c)
	}
	todos = []Todo{{TodoDone, "S", "a", ""}, {TodoPending, "S", "b", ""}}
	if c := currentTodo(todos); c == nil || c.Subject != "b" {
		t.Errorf("want first pending 'b', got %+v", c)
	}
}

func TestSplitSubjectDesc(t *testing.T) {
	if s, d := splitSubjectDesc("subj — desc"); s != "subj" || d != "desc" {
		t.Errorf("split = %q/%q want subj/desc", s, d)
	}
	if s, d := splitSubjectDesc("just subj"); s != "just subj" || d != "" {
		t.Errorf("split no-dash = %q/%q", s, d)
	}
}

func TestTruncStr(t *testing.T) {
	if got := truncStr("hello world", 5); got != "hell…" {
		t.Errorf("trunc = %q want %q", got, "hell…")
	}
}
