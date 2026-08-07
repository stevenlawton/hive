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

func TestBlockBoundsIgnoresProseMentions(t *testing.T) {
	// Regression: prose that names the markers above the block must not latch
	// the boundary onto itself (which ate the real marker + intro lines).
	content := "# Title\n\n" +
		"Edit with `hive todo`. Anything outside the `TASKS:BEGIN`/`TASKS:END` markers is left alone.\n\n" +
		"---\n\n" +
		tasksBegin + "\nLast sync: **x**\n- [ ] **old**\n" + tasksEnd + "\n\n## Notes\nkeep\n"

	out := replaceBlock(content, "### S\n\n- [ ] **new**\n")

	if !strings.Contains(out, "Anything outside the") {
		t.Errorf("prose marker-mention line was eaten:\n%s", out)
	}
	if strings.Count(out, tasksBegin) != 1 {
		t.Errorf("real BEGIN marker lost/duplicated (count=%d):\n%s", strings.Count(out, tasksBegin), out)
	}
	if !strings.Contains(out, "## Notes") || !strings.Contains(out, "new") || strings.Contains(out, "old") {
		t.Errorf("block not replaced correctly:\n%s", out)
	}
}

func TestBlockBoundsProseEndMention(t *testing.T) {
	// A prose TASKS:END mention above the block must not set end with begin=-1.
	content := "note: the `TASKS:END` marker closes the block\n\n" +
		tasksBegin + "\n- [ ] **a**\n" + tasksEnd + "\n"
	if got := parseTodos(extractBlock(content)); len(got) != 1 || got[0].Subject != "a" {
		t.Errorf("extractBlock fooled by a prose END mention: %+v", got)
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

func TestReleaseClaimOnlyOwnersClaims(t *testing.T) {
	// Regression: releasing a claim must not touch, let alone claim, any other
	// task (reported: `claim clear` grabbing an unrelated ticket).
	todos := []Todo{
		{Subject: "a", Claim: "wt1"},
		{Subject: "b"}, // unclaimed — must stay untouched
		{Subject: "c", Claim: "wt2"},
	}
	todos = releaseClaim(todos, "wt1")
	if todos[0].Claim != "" {
		t.Errorf("own claim not released: %+v", todos[0])
	}
	if todos[1].Claim != "" {
		t.Errorf("unclaimed task got a claim: %+v", todos[1])
	}
	if todos[2].Claim != "wt2" {
		t.Errorf("another worktree's claim was disturbed: %+v", todos[2])
	}
}

func TestDeferTodo(t *testing.T) {
	todos := []Todo{{Subject: "a", Claim: "wt1"}}
	todos = deferTodo(todos, 0)
	if !todos[0].Deferred || todos[0].Claim != "" {
		t.Errorf("defer should park + release claim: %+v", todos[0])
	}
	if got := todos[0].boxChar(); got != "-" {
		t.Errorf("deferred boxChar = %q want -", got)
	}
	// un-defer toggles back
	todos = deferTodo(todos, 0)
	if todos[0].Deferred {
		t.Errorf("defer toggle failed: %+v", todos[0])
	}
	// claiming a deferred task un-parks it
	todos = deferTodo(todos, 0)
	todos, ok := claimTodo(todos, 0, "wt1")
	if !ok || todos[0].Deferred || todos[0].Claim != "wt1" {
		t.Errorf("claiming a deferred task should un-park + claim: %+v ok=%v", todos[0], ok)
	}
}

func TestCurrentForClaimSkipsDeferred(t *testing.T) {
	todos := []Todo{
		{Subject: "parked", Deferred: true},
		{Subject: "free"},
	}
	if c := currentForClaim(todos, "wt3"); c == nil || c.Subject != "free" {
		t.Errorf("next should skip deferred, got %+v", c)
	}
	// only a deferred task left → no "next"
	if c := currentForClaim([]Todo{{Subject: "p", Deferred: true}}, "wt3"); c != nil {
		t.Errorf("deferred-only should yield no next, got %+v", c)
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

func TestParseTodoLineMarker(t *testing.T) {
	cases := []struct {
		line    string
		claim   string
		id      string
		subject string
		desc    string
	}{
		{"- [ ] **subj** - desc <!-- id:kdx -->", "", "kdx", "subj", "desc"},
		{"- [~] **subj** - desc <!-- @split-1 id:kdx -->", "split-1", "kdx", "subj", "desc"},
		{"- [~] **subj** - desc <!-- id:kdx @split-1 -->", "split-1", "kdx", "subj", "desc"},
		{"- [~] **subj** - desc <!-- @split-1 -->", "split-1", "", "subj", "desc"},
		{"- [x] **subj** - desc <!-- id:kdx -->", "", "kdx", "subj", "desc"},
		{"- [ ] **subj** - desc <!-- @split-1 id:kdx future:9 -->", "split-1", "kdx", "subj", "desc"},
		{"- [ ] **subj** - desc <!-- just a note -->", "", "", "subj", "desc <!-- just a note -->"},
	}
	for _, c := range cases {
		got, ok := parseTodoLine(c.line)
		if !ok {
			t.Errorf("parseTodoLine(%q) failed to parse", c.line)
			continue
		}
		if got.Claim != c.claim || got.ID != c.id || got.Subject != c.subject || got.Description != c.desc {
			t.Errorf("parseTodoLine(%q) = claim=%q id=%q subj=%q desc=%q; want claim=%q id=%q subj=%q desc=%q",
				c.line, got.Claim, got.ID, got.Subject, got.Description, c.claim, c.id, c.subject, c.desc)
		}
	}
}

// A done or deferred task must keep its id — otherwise it cannot be addressed
// to reopen it — while its claim is dropped.
func TestFormatKeepsIDInEveryState(t *testing.T) {
	cases := []struct {
		todo Todo
		want string
	}{
		{Todo{Subject: "s", ID: "kdx"}, "- [ ] **s** <!-- id:kdx -->"},
		{Todo{Subject: "s", ID: "kdx", Claim: "wt"}, "- [~] **s** <!-- @wt id:kdx -->"},
		{Todo{Subject: "s", ID: "kdx", Done: true, Claim: "wt"}, "- [x] **s** <!-- id:kdx -->"},
		{Todo{Subject: "s", ID: "kdx", Deferred: true, Claim: "wt"}, "- [-] **s** <!-- id:kdx -->"},
		{Todo{Subject: "s"}, "- [ ] **s**"},
	}
	for _, c := range cases {
		out := formatTodos([]Todo{c.todo})
		if !strings.Contains(out, c.want) {
			t.Errorf("formatTodos(%+v) = %q; want it to contain %q", c.todo, out, c.want)
		}
	}
}

func TestMarkerRoundTrip(t *testing.T) {
	in := []Todo{
		{Section: "Alpha", Subject: "one", Description: "first", ID: "kdx"},
		{Section: "Alpha", Subject: "two", ID: "mfp", Claim: "split-1"},
		{Section: "Alpha", Subject: "three", ID: "qrz", Done: true},
	}
	got := parseTodos(formatTodos(in))
	if len(got) != len(in) {
		t.Fatalf("round trip produced %d todos, want %d", len(got), len(in))
	}
	for i := range in {
		if got[i].ID != in[i].ID || got[i].Claim != in[i].Claim || got[i].Done != in[i].Done {
			t.Errorf("todo %d round-tripped as %+v, want id=%q claim=%q done=%v",
				i, got[i], in[i].ID, in[i].Claim, in[i].Done)
		}
	}
}

func TestBackfillIDsStampsOnlyMissing(t *testing.T) {
	todos := []Todo{
		{Subject: "keeps its id", ID: "kdx"},
		{Subject: "needs one"},
		{Subject: "also needs one"},
	}
	got := backfillIDs(todos)

	if got[0].ID != "kdx" {
		t.Errorf("existing id was overwritten: %q", got[0].ID)
	}
	seen := map[string]bool{}
	for i, td := range got {
		if td.ID == "" {
			t.Fatalf("todo %d still has no id", i)
		}
		if seen[td.ID] {
			t.Errorf("duplicate id %q", td.ID)
		}
		seen[td.ID] = true
		if strings.ContainsAny(td.ID, "0123456789") {
			t.Errorf("id %q contains a digit — ids must never be confusable with a position", td.ID)
		}
	}
}

func TestBackfillIDsIsIdempotent(t *testing.T) {
	first := backfillIDs([]Todo{{Subject: "a"}, {Subject: "b"}})
	second := backfillIDs(append([]Todo{}, first...))
	for i := range first {
		if first[i].ID != second[i].ID {
			t.Errorf("todo %d renumbered on second pass: %q -> %q", i, first[i].ID, second[i].ID)
		}
	}
}

func TestNewTodoIDAvoidsCollisions(t *testing.T) {
	taken := map[string]bool{}
	for i := 0; i < 500; i++ {
		id := newTodoID(taken)
		if taken[id] {
			t.Fatalf("newTodoID returned a taken id %q", id)
		}
		taken[id] = true
	}
}
