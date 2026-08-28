package main

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// cannedTestModel opens the menu on a session with the given busy state,
// backed by a store in a temp dir.
func cannedTestModel(t *testing.T, busy bool, prompts ...CannedPrompt) model {
	t.Helper()
	store := newCannedStore(t.TempDir())
	if err := store.Save(prompts); err != nil {
		t.Fatal(err)
	}
	status := "completed"
	if busy {
		status = "running"
	}
	return model{
		width: 100, height: 40,
		items:       []repoItem{{tmuxSes: "hive-x", richStatus: &SessionStatus{Status: status}}},
		cannedStore: store,
		canned: cannedMenu{
			open:    true,
			items:   store.Prompts(),
			session: "hive-x",
			geom:    cannedGeometry(store.Prompts(), 2, 2, 100, 40),
		},
	}
}

// captureCannedSends swaps the send seam for the duration of a test and
// returns a pointer to what was sent.
func captureCannedSends(t *testing.T) *[]cannedSend {
	t.Helper()
	var got []cannedSend
	prev := sendCannedOps
	sendCannedOps = func(session string, plan []cannedOp) {
		got = append(got, cannedSend{session: session, plan: plan})
	}
	t.Cleanup(func() { sendCannedOps = prev })
	return &got
}

type cannedSend struct {
	session string
	plan    []cannedOp
}

func pressCanned(t *testing.T, m model, msg tea.KeyPressMsg) (model, tea.Cmd) {
	t.Helper()
	out, cmd := m.handleCannedKey(msg)
	mm, ok := out.(model)
	if !ok {
		t.Fatalf("handleCannedKey returned %T, want model", out)
	}
	return mm, cmd
}

func keyRune(r rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: r, Text: string(r)} }

var (
	keyEnter  = tea.KeyPressMsg{Code: tea.KeyEnter}
	keyEscape = tea.KeyPressMsg{Code: tea.KeyEscape}
	keyDown   = tea.KeyPressMsg{Code: tea.KeyDown}
)

func TestCannedDigitKeySendsThatPromptAndClosesTheMenu(t *testing.T) {
	sends := captureCannedSends(t)
	m := cannedTestModel(t, false,
		CannedPrompt{Label: "one", Text: "first prompt"},
		CannedPrompt{Label: "two", Text: "second prompt"})

	m, cmd := pressCanned(t, m, keyRune('2'))
	if cmd != nil {
		cmd()
	}

	if m.canned.open {
		t.Error("menu stayed open after sending")
	}
	if len(*sends) != 1 {
		t.Fatalf("got %d sends, want 1", len(*sends))
	}
	if (*sends)[0].session != "hive-x" {
		t.Errorf("session: got %q, want hive-x", (*sends)[0].session)
	}
	if (*sends)[0].plan[0].literal != "second prompt" {
		t.Errorf("plan: got %+v, want the second prompt", (*sends)[0].plan)
	}
}

func TestCannedEnterSendsTheHighlightedPrompt(t *testing.T) {
	sends := captureCannedSends(t)
	m := cannedTestModel(t, false,
		CannedPrompt{Label: "one", Text: "first prompt"},
		CannedPrompt{Label: "two", Text: "second prompt"})

	m, _ = pressCanned(t, m, keyDown)
	_, cmd := pressCanned(t, m, keyEnter)
	if cmd != nil {
		cmd()
	}

	if len(*sends) != 1 || (*sends)[0].plan[0].literal != "second prompt" {
		t.Errorf("got %+v, want the cursor's prompt", *sends)
	}
}

func TestCannedEscapeClosesWithoutSending(t *testing.T) {
	sends := captureCannedSends(t)
	m := cannedTestModel(t, true, CannedPrompt{Label: "one", Text: "first prompt"})

	m, cmd := pressCanned(t, m, keyEscape)
	if cmd != nil {
		cmd()
	}

	if m.canned.open {
		t.Error("escape left the menu open")
	}
	if len(*sends) != 0 {
		t.Errorf("escape sent %+v, want nothing", *sends)
	}
}

func TestCannedSendToBusySessionInterruptsFirst(t *testing.T) {
	sends := captureCannedSends(t)
	m := cannedTestModel(t, true, CannedPrompt{Label: "one", Text: "first prompt"})

	_, cmd := pressCanned(t, m, keyRune('1'))
	if cmd != nil {
		cmd()
	}

	if len(*sends) != 1 {
		t.Fatalf("got %d sends, want 1", len(*sends))
	}
	if (*sends)[0].plan[0].key != "escape" {
		t.Errorf("busy session: got %+v, want an escape first", (*sends)[0].plan)
	}
}

func TestCannedDeleteRemovesTheEntryAndPersists(t *testing.T) {
	m := cannedTestModel(t, false,
		CannedPrompt{Label: "one", Text: "first prompt"},
		CannedPrompt{Label: "two", Text: "second prompt"})

	m, _ = pressCanned(t, m, keyRune('d'))

	if len(m.canned.items) != 1 || m.canned.items[0].Label != "two" {
		t.Errorf("in memory: got %+v, want only the second prompt", m.canned.items)
	}
	if got := m.cannedStore.Prompts(); len(got) != 1 || got[0].Label != "two" {
		t.Errorf("on disk: got %+v, want only the second prompt", got)
	}
}

func TestCannedAddAppendsAnEntryAndPersists(t *testing.T) {
	m := cannedTestModel(t, false, CannedPrompt{Label: "one", Text: "first prompt"})

	m, _ = pressCanned(t, m, keyRune('a'))
	if !m.canned.editing {
		t.Fatal("'a' did not open the edit form")
	}
	for _, r := range "third" {
		m, _ = pressCanned(t, m, keyRune(r))
	}
	m, _ = pressCanned(t, m, keyEnter) // label → text field
	for _, r := range "third prompt" {
		m, _ = pressCanned(t, m, keyRune(r))
	}
	m, _ = pressCanned(t, m, keyEnter) // save

	if m.canned.editing {
		t.Error("form stayed open after saving")
	}
	got := m.cannedStore.Prompts()
	if len(got) != 2 || got[1].Label != "third" || got[1].Text != "third prompt" {
		t.Errorf("on disk: got %+v, want the new entry appended", got)
	}
}

func TestCannedEditReplacesTheCursorEntry(t *testing.T) {
	m := cannedTestModel(t, false, CannedPrompt{Label: "one", Text: "first prompt"})

	m, _ = pressCanned(t, m, keyRune('e'))
	if m.canned.label.Value() != "one" {
		t.Errorf("form label: got %q, want the entry's label", m.canned.label.Value())
	}
	for range "one" {
		m, _ = pressCanned(t, m, tea.KeyPressMsg{Code: tea.KeyBackspace})
	}
	for _, r := range "renamed" {
		m, _ = pressCanned(t, m, keyRune(r))
	}
	m, _ = pressCanned(t, m, keyEnter)
	m, _ = pressCanned(t, m, keyEnter)

	got := m.cannedStore.Prompts()
	if len(got) != 1 || got[0].Label != "renamed" || got[0].Text != "first prompt" {
		t.Errorf("on disk: got %+v, want the label renamed and the text kept", got)
	}
}

func TestCannedEditFormEscapeDiscardsTheEdit(t *testing.T) {
	m := cannedTestModel(t, false, CannedPrompt{Label: "one", Text: "first prompt"})

	m, _ = pressCanned(t, m, keyRune('e'))
	for _, r := range "zzz" {
		m, _ = pressCanned(t, m, keyRune(r))
	}
	m, _ = pressCanned(t, m, keyEscape)

	if m.canned.editing {
		t.Error("escape left the form open")
	}
	if !m.canned.open {
		t.Error("escape from the form closed the whole menu, want just the form")
	}
	if got := m.cannedStore.Prompts(); got[0].Label != "one" {
		t.Errorf("on disk: got %+v, want the original entry", got)
	}
}

func TestCannedReorderMovesTheEntryAndFollowsTheCursor(t *testing.T) {
	m := cannedTestModel(t, false,
		CannedPrompt{Label: "one", Text: "first prompt"},
		CannedPrompt{Label: "two", Text: "second prompt"})

	m, _ = pressCanned(t, m, keyRune('J'))

	if m.canned.items[0].Label != "two" || m.canned.items[1].Label != "one" {
		t.Errorf("in memory: got %+v, want the first entry moved down", m.canned.items)
	}
	if m.canned.cursor != 1 {
		t.Errorf("cursor: got %d, want 1 — it should follow the moved entry", m.canned.cursor)
	}
	if got := m.cannedStore.Prompts(); got[0].Label != "two" {
		t.Errorf("on disk: got %+v, want the new order", got)
	}
}

func TestCannedReorderAtTheEdgeIsANoop(t *testing.T) {
	m := cannedTestModel(t, false,
		CannedPrompt{Label: "one", Text: "first prompt"},
		CannedPrompt{Label: "two", Text: "second prompt"})

	m, _ = pressCanned(t, m, keyRune('K'))

	if m.canned.items[0].Label != "one" || m.canned.cursor != 0 {
		t.Errorf("got %+v cursor %d, want no change at the top", m.canned.items, m.canned.cursor)
	}
}
