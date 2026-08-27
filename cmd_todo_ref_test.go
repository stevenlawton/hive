package main

import "strings"

import "testing"

// Resolving by subject means a ref can now fail for two different reasons, and
// "no such task" is a lie for the second one: the task exists, the caller just
// named several at once.
func TestTodoRefErrorNamesTheCandidates(t *testing.T) {
	msg := todoRefError(refFixture(), "e")
	if !strings.Contains(msg, "matches 3 tasks") {
		t.Errorf("got %q; want it to say how many tasks matched", msg)
	}
	for _, id := range []string{"kdx", "mfp", "qrz"} {
		if !strings.Contains(msg, id) {
			t.Errorf("got %q; want it to name candidate %s", msg, id)
		}
	}
}

func TestTodoRefErrorUnknownRef(t *testing.T) {
	msg := todoRefError(refFixture(), "zzz")
	if !strings.Contains(msg, `no such task "zzz"`) {
		t.Errorf("got %q; want the unknown-task wording", msg)
	}
	if strings.Contains(msg, "matches") {
		t.Errorf("got %q; a ref matching nothing is not ambiguous", msg)
	}
}
