package bus

import (
	"path/filepath"
	"strings"
	"testing"
)

func testBus(t *testing.T) *Bus {
	t.Helper()
	store, err := OpenStore(filepath.Join(t.TempDir(), "bus.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	return New(store, "wt:test")
}

func announce(t *testing.T, b *Bus, msg Announcement) Announcement {
	t.Helper()
	got, err := b.Announce(msg)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// The habit this exists to stop: senders paste a whole report into the
// headline field. Nothing is lost — the overflow lands in the body, where it
// was always meant to go.
func TestAnnounceFoldsAMultilineHeadlineIntoTheBody(t *testing.T) {
	b := testBus(t)

	got := announce(t, b, Announcement{
		Headline: "STOP THE REBASE\n\nI have already done this merge.\nConflicts checked one by one.",
	})

	if got.Headline != "STOP THE REBASE…" {
		t.Errorf("headline = %q, want %q", got.Headline, "STOP THE REBASE…")
	}
	if !strings.Contains(got.Body, "I have already done this merge.") {
		t.Errorf("overflow missing from body: %q", got.Body)
	}
	if !strings.Contains(got.Body, "Conflicts checked one by one.") {
		t.Errorf("body should keep every line of the overflow: %q", got.Body)
	}
}

func TestAnnounceFoldsAnOverlongHeadlineAtAWordBoundary(t *testing.T) {
	b := testBus(t)
	long := strings.TrimSpace(strings.Repeat("word ", 200))

	got := announce(t, b, Announcement{Headline: long})

	if n := len([]rune(got.Headline)); n > MaxHeadline {
		t.Errorf("headline is %d runes, want at most %d", n, MaxHeadline)
	}
	if !strings.HasSuffix(got.Headline, "…") {
		t.Errorf("expected a continuation marker: %q", got.Headline)
	}
	if strings.Contains(got.Headline, "wor…") {
		t.Errorf("should break on a word boundary, not mid-word: %q", got.Headline)
	}
	if rejoined := strings.TrimSuffix(got.Headline, "…") + got.Body; strings.ReplaceAll(rejoined, " ", "") != strings.ReplaceAll(long, " ", "") {
		t.Error("folding must not lose or duplicate any of the text")
	}
}

// An overflowing headline must not bury a body the sender wrote deliberately.
func TestAnnounceKeepsTheSendersBodyBelowTheOverflow(t *testing.T) {
	b := testBus(t)

	got := announce(t, b, Announcement{
		Headline: "first line\nsecond line",
		Body:     "the real body",
	})

	overflow := strings.Index(got.Body, "second line")
	body := strings.Index(got.Body, "the real body")
	if overflow < 0 || body < 0 {
		t.Fatalf("body lost content: %q", got.Body)
	}
	if overflow > body {
		t.Errorf("overflow should precede the sender's own body: %q", got.Body)
	}
}

func TestAnnounceLeavesAnOrdinaryHeadlineAlone(t *testing.T) {
	b := testBus(t)

	got := announce(t, b, Announcement{Headline: "released test slot", Body: "details"})

	if got.Headline != "released test slot" {
		t.Errorf("headline = %q, want it untouched", got.Headline)
	}
	if got.Body != "details" {
		t.Errorf("body = %q, want it untouched", got.Body)
	}
}

// The guarantee the digest relies on: nothing on the log can exceed the cap,
// whichever write path put it there.
func TestAnnouncedHeadlinesAreAlwaysWithinTheCap(t *testing.T) {
	b := testBus(t)
	for _, headline := range []string{
		"short",
		strings.Repeat("x", 2656),
		"one\ntwo\nthree",
		strings.Repeat("word ", 400) + "\n" + strings.Repeat("more ", 400),
		"   leading and trailing whitespace   ",
	} {
		got := announce(t, b, Announcement{Headline: headline})
		if n := len([]rune(got.Headline)); n > MaxHeadline {
			t.Errorf("headline %d runes (want <= %d) for input %.20q", n, MaxHeadline, headline)
		}
		if strings.ContainsAny(got.Headline, "\r\n") {
			t.Errorf("headline still multi-line for input %.20q", headline)
		}
	}
}
