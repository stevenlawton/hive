package main

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// futureTestModel builds a model with the future popup open over one session,
// backed by a store in a temp dir.
func futureTestModel(t *testing.T, q FutureQueue) model {
	t.Helper()
	store := newFutureStore(t.TempDir())
	reset := time.Now().Add(2 * time.Hour).Unix()
	return model{
		width: 100, height: 40,
		items:       []repoItem{{tmuxSes: "hive-x", richStatus: &SessionStatus{Status: "completed"}}},
		futureStore: store,
		future:      newFutureMenu("hive-x", q, reset, store.ResumeText()),
	}
}

// keyParkNote is ctrl+a: Enter now breaks the line inside a note.
var keyParkNote = tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl}

func pressFuture(t *testing.T, m model, msg tea.KeyPressMsg) model {
	t.Helper()
	next, _ := m.handleFutureKey(msg)
	return next.(model)
}

func TestFuturePopupParksATypedNoteOnEnter(t *testing.T) {
	m := futureTestModel(t, FutureQueue{})
	m.future.input.SetValue("check the bus")

	m = pressFuture(t, m, keyParkNote)

	if len(m.future.q.Prompts) != 1 || m.future.q.Prompts[0] != "check the bus" {
		t.Fatalf("note was not parked: %#v", m.future.q.Prompts)
	}
	if !m.future.open {
		t.Error("parking a note closed the popup")
	}
}

func TestFuturePopupEscapePersistsAndCloses(t *testing.T) {
	m := futureTestModel(t, FutureQueue{})
	m.future.input.SetValue("carry on tomorrow")
	m = pressFuture(t, m, keyParkNote)

	m = pressFuture(t, m, keyEscape)

	if m.future.open {
		t.Error("escape did not close the popup")
	}
	saved := m.futureStore.Queues()["hive-x"]
	if len(saved.Prompts) != 1 || saved.Prompts[0] != "carry on tomorrow" {
		t.Fatalf("the parked note was not written to disk: %#v", saved)
	}
	if !saved.AutoSend || saved.ArmedFor == 0 {
		t.Error("the queue was saved unarmed, so it would never fire")
	}
}

func TestFuturePopupTogglesAutoResumeWithCtrlR(t *testing.T) {
	m := futureTestModel(t, FutureQueue{})

	m = pressFuture(t, m, tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})

	if !m.future.q.AutoResume {
		t.Error("r did not tick auto resume")
	}
	if m.future.editorEnabled() {
		t.Error("the editor is still live under auto resume")
	}
}

func TestFuturePopupTypingIsIgnoredUnderAutoResume(t *testing.T) {
	m := futureTestModel(t, FutureQueue{})
	m = pressFuture(t, m, tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})

	m = pressFuture(t, m, keyRune('x'))

	if m.future.input.Value() != "" {
		t.Errorf("the disabled editor accepted %q", m.future.input.Value())
	}
}

func TestFuturePopupDeletesWithCtrlD(t *testing.T) {
	m := futureTestModel(t, FutureQueue{Prompts: []string{"first", "second"}})
	m.future.cursor = 0

	next, _ := m.handleFutureKey(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})
	m = next.(model)

	if len(m.future.q.Prompts) != 1 || m.future.q.Prompts[0] != "second" {
		t.Errorf("ctrl+d did not delete the selected prompt: %#v", m.future.q.Prompts)
	}
}

func TestFutureTickFiresIntoTheSessionAtTheReset(t *testing.T) {
	sent := captureCannedSends(t)
	store := newFutureStore(t.TempDir())
	reset := time.Now().Add(-futureFireGrace - time.Minute)
	if err := store.Save(map[string]FutureQueue{
		"hive-x": {Prompts: []string{"carry on"}, AutoSend: true, ArmedFor: reset.Unix()},
	}); err != nil {
		t.Fatal(err)
	}
	m := model{
		items:       []repoItem{{tmuxSes: "hive-x", richStatus: &SessionStatus{Status: "completed"}}},
		futureStore: store,
	}

	if cmd := m.runFutureQueues(time.Now()); cmd != nil {
		cmd()
	}

	if len(*sent) != 1 {
		t.Fatalf("got %d sends, want 1", len(*sent))
	}
	if (*sent)[0].session != "hive-x" {
		t.Errorf("fired at %q, want hive-x", (*sent)[0].session)
	}
	if store.Queues()["hive-x"].ArmedFor != 0 {
		t.Error("the fired queue is still armed on disk and would fire again")
	}
}

func TestFutureTickIsQuietWithNothingDue(t *testing.T) {
	sent := captureCannedSends(t)
	store := newFutureStore(t.TempDir())
	if err := store.Save(map[string]FutureQueue{
		"hive-x": {Prompts: []string{"later"}, AutoSend: true,
			ArmedFor: time.Now().Add(time.Hour).Unix()},
	}); err != nil {
		t.Fatal(err)
	}
	m := model{
		items:       []repoItem{{tmuxSes: "hive-x", richStatus: &SessionStatus{Status: "completed"}}},
		futureStore: store,
	}

	if cmd := m.runFutureQueues(time.Now()); cmd != nil {
		cmd()
	}

	if len(*sent) != 0 {
		t.Errorf("fired with nothing due: %#v", *sent)
	}
}

func TestCannedPopupHandsOffToFuturePrompts(t *testing.T) {
	m := cannedTestModel(t, false, CannedPrompt{Label: "continue", Text: "continue"})
	m.futureStore = newFutureStore(t.TempDir())

	next, _ := m.handleCannedKey(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	m = next.(model)

	if m.canned.open {
		t.Error("the canned popup stayed open behind the future one")
	}
	if !m.future.open || m.future.session != "hive-x" {
		t.Errorf("future popup did not open on the same session: open=%v session=%q",
			m.future.open, m.future.session)
	}
}

// A tick can fire the queue while the popup sits open on it. Closing the popup
// must not write back the snapshot taken when it opened, or the prompt that
// just went would be re-armed and typed a second time.
func TestFuturePopupDoesNotResurrectAPromptThatFiredWhileItWasOpen(t *testing.T) {
	store := newFutureStore(t.TempDir())
	reset := time.Now().Add(-futureFireGrace - time.Minute)
	if err := store.Save(map[string]FutureQueue{
		"hive-x": {Prompts: []string{"p1", "p2"}, AutoSend: true, ArmedFor: reset.Unix()},
	}); err != nil {
		t.Fatal(err)
	}
	m := model{
		width: 100, height: 40,
		items:       []repoItem{{tmuxSes: "hive-x", richStatus: &SessionStatus{Status: "completed"}}},
		futureStore: store,
	}
	m.openFuturePopup("hive-x", 2, 2)

	// The tick fires p1 underneath the open popup.
	if cmd := m.runFutureQueues(time.Now()); cmd != nil {
		cmd()
	}

	// The user closes the popup.
	next, _ := m.handleFutureKey(keyEscape)
	m = next.(model)

	saved := m.futureStore.Queues()["hive-x"]
	if saved.ArmedFor != 0 {
		t.Errorf("closing the popup re-armed a queue that had already fired: %#v", saved)
	}
	for _, p := range saved.Prompts {
		if p == "p1" {
			t.Fatalf("the prompt that already fired was resurrected: %#v", saved.Prompts)
		}
	}
}

func TestFuturePopupKeepsNotesTypedWhileTheQueueFiredUnderneath(t *testing.T) {
	store := newFutureStore(t.TempDir())
	reset := time.Now().Add(-futureFireGrace - time.Minute)
	if err := store.Save(map[string]FutureQueue{
		"hive-x": {Prompts: []string{"p1"}, AutoSend: true, ArmedFor: reset.Unix()},
	}); err != nil {
		t.Fatal(err)
	}
	m := model{
		width: 100, height: 40,
		items:       []repoItem{{tmuxSes: "hive-x", richStatus: &SessionStatus{Status: "completed"}}},
		futureStore: store,
	}
	m.openFuturePopup("hive-x", 2, 2)
	m.future.input.SetValue("a thought I had while it fired")
	next, _ := m.handleFutureKey(keyParkNote)
	m = next.(model)

	if cmd := m.runFutureQueues(time.Now()); cmd != nil {
		cmd()
	}
	next, _ = m.handleFutureKey(keyEscape)
	m = next.(model)

	saved := m.futureStore.Queues()["hive-x"]
	found := false
	for _, p := range saved.Prompts {
		if p == "a thought I had while it fired" {
			found = true
		}
	}
	if !found {
		t.Errorf("the note typed while the queue fired was lost: %#v", saved.Prompts)
	}
}
