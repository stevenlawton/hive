package main

import "strings"
import "testing"

func TestParseTodoLine(t *testing.T) {
	cases := []struct {
		line    string
		ok      bool
		done    bool
		claim   string
		subject string
		desc    string
	}{
		{"- [ ] **#142 fix cache** — add a bounded cache", true, false, "", "#142 fix cache", "add a bounded cache"},
		{"- [ ] **subj** - hyphen sep (normalized)", true, false, "", "subj", "hyphen sep (normalized)"},
		{"- [ ] **subj** — - leaked double sep", true, false, "", "subj", "leaked double sep"},
		{"- [ ] plain - hyphen desc", true, false, "", "plain", "hyphen desc"},
		{"- [~] **claimed one** — desc <!-- @feature-x -->", true, false, "feature-x", "claimed one", "desc"},
		{"- [x] **done thing** — it shipped", true, true, "", "done thing", "it shipped"},
		{"- [ ] plain subject — no bold", true, false, "", "plain subject", "no bold"},
		{"- [~] **bare claimed, no owner**", true, false, "", "bare claimed, no owner", ""}, // normalizes to open
		{"- [ ]", false, false, "", "", ""},
		{"### section", false, false, "", "", ""},
	}
	for _, c := range cases {
		got, ok := parseTodoLine(c.line)
		if ok != c.ok {
			t.Errorf("parseTodoLine(%q) ok=%v want %v", c.line, ok, c.ok)
			continue
		}
		if ok && (got.Done != c.done || got.Claim != c.claim || got.Subject != c.subject || got.Description != c.desc) {
			t.Errorf("parseTodoLine(%q) = %+v want done=%v claim=%q subj=%q desc=%q", c.line, got, c.done, c.claim, c.subject, c.desc)
		}
	}
}

func TestParseTodosWithSections(t *testing.T) {
	block := "Last sync: **x**\n\n### Alpha\n\n- [ ] **one** — first\n- [x] **two**\n\n### Beta\n\n- [~] **three** — third <!-- @wt -->\n"
	got := parseTodos(block)
	if len(got) != 3 {
		t.Fatalf("parsed %d, want 3: %+v", len(got), got)
	}
	if got[0].Section != "Alpha" || got[2].Section != "Beta" {
		t.Errorf("sections wrong: %q %q", got[0].Section, got[2].Section)
	}
	if got[2].Claim != "wt" || got[2].Subject != "three" {
		t.Errorf("beta task wrong: %+v", got[2])
	}
}

func TestFormatRoundTrip(t *testing.T) {
	todos := []Todo{
		{Section: "Alpha", Subject: "one", Description: "first"},
		{Section: "Alpha", Subject: "two", Done: true},
		{Section: "Beta", Subject: "three", Description: "third", Claim: "feature-x"},
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
			t.Errorf("replaceBlock dropped %q", must)
		}
	}
	if strings.Contains(out, "old task") {
		t.Errorf("replaceBlock kept stale block body")
	}
	if !strings.Contains(out, "TASKS:BEGIN (auto-generated ...)") {
		t.Errorf("original BEGIN marker not preserved")
	}
	if strings.Count(out, "TASKS:BEGIN") != 1 || strings.Count(out, "TASKS:END") != 1 {
		t.Errorf("marker count off")
	}
}

func TestReplaceBlockScaffoldsWhenAbsent(t *testing.T) {
	out := replaceBlock("", "### S\n\n- [ ] **task**\n")
	if got := parseTodos(extractBlock(out)); len(got) != 1 || got[0].Subject != "task" {
		t.Errorf("scaffold didn't round-trip: %+v", got)
	}
}

func TestClaimTodo(t *testing.T) {
	todos := []Todo{{Subject: "a"}, {Subject: "b"}}

	todos, ok := claimTodo(todos, 0, "wt1")
	if !ok || todos[0].Claim != "wt1" {
		t.Fatalf("claim failed: %+v ok=%v", todos, ok)
	}
	// same owner toggles release
	todos, ok = claimTodo(todos, 0, "wt1")
	if !ok || todos[0].Claim != "" {
		t.Errorf("release failed: %+v", todos)
	}
	// wt2 claims; wt1 cannot steal
	todos, _ = claimTodo(todos, 0, "wt2")
	if _, ok := claimTodo(todos, 0, "wt1"); ok {
		t.Errorf("wt1 should not steal wt2's claim")
	}
	// can't claim a done task
	done := []Todo{{Subject: "x", Done: true}}
	if _, ok := claimTodo(done, 0, "wt1"); ok {
		t.Errorf("should not claim a done task")
	}
}

func TestCurrentForClaim(t *testing.T) {
	todos := []Todo{
		{Subject: "a", Claim: "wt2"}, // taken by another
		{Subject: "b"},               // free
		{Subject: "c", Claim: "wt1"}, // mine
	}
	if c := currentForClaim(todos, "wt1"); c == nil || c.Subject != "c" {
		t.Errorf("want my claim 'c', got %+v", c)
	}
	// no claim of my own → first free, skipping the taken one
	if c := currentForClaim(todos, "wt3"); c == nil || c.Subject != "b" {
		t.Errorf("want first free 'b', got %+v", c)
	}
	// nothing free and unclaimed
	todos2 := []Todo{{Subject: "a", Claim: "wt2"}, {Subject: "b", Done: true}}
	if c := currentForClaim(todos2, "wt3"); c != nil {
		t.Errorf("want nil, got %+v", c)
	}
}

func TestToggleDoneClearsClaim(t *testing.T) {
	todos := []Todo{{Subject: "a", Claim: "wt1"}}
	todos = toggleTodoDone(todos, 0)
	if !todos[0].Done || todos[0].Claim != "" {
		t.Errorf("completing should clear claim: %+v", todos[0])
	}
}

func TestAddTodo(t *testing.T) {
	todos := addTodo(nil, "Sec", "subj", "desc")
	if len(todos) != 1 || todos[0].Section != "Sec" || todos[0].Subject != "subj" || todos[0].Description != "desc" || todos[0].Done {
		t.Errorf("addTodo wrong: %+v", todos)
	}
	if len(addTodo(todos, "", "  ", "")) != 1 {
		t.Errorf("blank subject should be ignored")
	}
	if addTodo(nil, "", "x", "")[0].Section != defaultSection {
		t.Errorf("blank section should default")
	}
}

func TestSplitSubjectDesc(t *testing.T) {
	if s, d := splitSubjectDesc("subj — desc"); s != "subj" || d != "desc" {
		t.Errorf("split = %q/%q", s, d)
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
