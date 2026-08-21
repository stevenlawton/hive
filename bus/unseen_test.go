package bus

import (
	"path/filepath"
	"strings"
	"testing"
)

func busWithMessages(t *testing.T, n int) (*Bus, []Announcement) {
	t.Helper()
	store, err := OpenStore(filepath.Join(t.TempDir(), "bus.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	b := New(store, "wt:test")
	var msgs []Announcement
	for i := 0; i < n; i++ {
		msg, err := b.Announce(Announcement{Headline: "msg"})
		if err != nil {
			t.Fatal(err)
		}
		msgs = append(msgs, msg)
	}
	return b, msgs
}

// A listener we have never seen before cannot be bounded by a cursor. Unseen
// must say so rather than silently handing back the whole log as if the
// listener had simply fallen behind.
func TestUnseenReportsAnEmptyCursorAsUnresolved(t *testing.T) {
	b, msgs := busWithMessages(t, 3)

	got, resolved := b.Unseen("")
	if resolved {
		t.Error("empty cursor should not resolve")
	}
	if len(got) != len(msgs) {
		t.Errorf("got %d messages, want %d", len(got), len(msgs))
	}
}

// A cursor that has been rotated out of the log is just as unbounded as no
// cursor at all — an established session must not be flooded either.
func TestUnseenReportsAMissingCursorAsUnresolved(t *testing.T) {
	b, _ := busWithMessages(t, 3)

	if _, resolved := b.Unseen("msg_rotated_away"); resolved {
		t.Error("cursor absent from the log should not resolve")
	}
}

func TestUnseenAfterAKnownCursorResolves(t *testing.T) {
	b, msgs := busWithMessages(t, 3)

	got, resolved := b.Unseen(msgs[0].ID)
	if !resolved {
		t.Fatal("a cursor present in the log should resolve")
	}
	if len(got) != 2 {
		t.Fatalf("got %d messages, want 2", len(got))
	}
	if got[0].ID != msgs[1].ID {
		t.Errorf("got first id %q, want %q", got[0].ID, msgs[1].ID)
	}
}

func TestShortHeadlineKeepsOnlyTheFirstLine(t *testing.T) {
	a := Announcement{Headline: "STOP THE REBASE\n\nHere is why, at length:\nreason one"}

	got := a.ShortHeadline()
	if got != "STOP THE REBASE…" {
		t.Errorf("got %q, want %q", got, "STOP THE REBASE…")
	}
}

func TestShortHeadlineCapsLength(t *testing.T) {
	a := Announcement{Headline: strings.Repeat("x", 2656)}

	if got := len([]rune(a.ShortHeadline())); got != MaxHeadline+1 {
		t.Errorf("got %d runes, want %d (cap plus the ellipsis)", got, MaxHeadline+1)
	}
}

func TestShortHeadlineLeavesAOneLinerAlone(t *testing.T) {
	a := Announcement{Headline: "released test slot"}

	if got := a.ShortHeadline(); got != "released test slot" {
		t.Errorf("got %q, want it untouched", got)
	}
}
