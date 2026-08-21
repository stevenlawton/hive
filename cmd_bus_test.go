package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stevenlawton/hive/bus"
)

// A repoItem whose worktree directory has been deleted must not be offered as
// a bus Peer. Otherwise the responder fleet keeps spawning `claude -p` into a
// missing directory on every announcement (chdir: no such file or directory).
func TestPeerFromRepoSkipsMissingPath(t *testing.T) {
	item := repoItem{
		repo:    Repo{Path: filepath.Join(t.TempDir(), "deleted-worktree")},
		tmuxSes: "hive-gone",
	}
	if _, ok := peerFromRepo(item); ok {
		t.Fatal("peerFromRepo returned a peer for a nonexistent worktree path")
	}
}

// A live worktree with an active session is still a valid peer.
func TestPeerFromRepoAcceptsLivePath(t *testing.T) {
	dir := t.TempDir()
	item := repoItem{
		repo:    Repo{Path: dir},
		tmuxSes: "hive-live",
	}
	p, ok := peerFromRepo(item)
	if !ok {
		t.Fatal("peerFromRepo rejected a live worktree path")
	}
	if p.Path != dir {
		t.Errorf("peer path = %q, want %q", p.Path, dir)
	}
}

func TestBuildInboxDigestContainsHeadlines(t *testing.T) {
	unseen := []bus.Announcement{
		{ID: "m1", From: "wt:a", Headline: "started auth refactor", At: time.Now()},
		{ID: "m2", From: "wt:b", Headline: "released test slot", At: time.Now()},
	}
	d := buildInboxDigest(unseen, false)
	if !strings.Contains(d, "2 new bus announcement") {
		t.Errorf("digest missing count header: %q", d)
	}
	if !strings.Contains(d, "started auth refactor") || !strings.Contains(d, "released test slot") {
		t.Errorf("digest missing headlines: %q", d)
	}
}

// PostToolUse hooks only inject context via JSON hookSpecificOutput.additionalContext
// (plain stdout goes to debug logs), so the envelope must be valid JSON of that shape.
func TestPostToolUseEnvelopeShape(t *testing.T) {
	out, err := postToolUseEnvelope("hello context")
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		HookSpecificOutput struct {
			HookEventName     string `json:"hookEventName"`
			AdditionalContext string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("output is not valid JSON: %v (%q)", err, out)
	}
	if parsed.HookSpecificOutput.HookEventName != "PostToolUse" {
		t.Errorf("hookEventName = %q, want PostToolUse", parsed.HookSpecificOutput.HookEventName)
	}
	if parsed.HookSpecificOutput.AdditionalContext != "hello context" {
		t.Errorf("additionalContext = %q", parsed.HookSpecificOutput.AdditionalContext)
	}
}

// A responder may still post a question, which carries no ReplyTo. Left
// unguarded that wakes every other peer's responder, each free to ask again —
// so nothing a responder emits may trigger another round.
func TestResponderOutputDoesNotTriggerResponders(t *testing.T) {
	cases := []struct {
		name string
		msg  bus.Announcement
		want bool
	}{
		{"fresh intent from a session", bus.Announcement{Kind: bus.KindIntent}, true},
		{"reply", bus.Announcement{ReplyTo: "msg_abc"}, false},
		{"auto reply", bus.Announcement{ReplyTo: "msg_abc", Auto: true}, false},
		{"auto question", bus.Announcement{Kind: bus.KindQuestion, Auto: true}, false},
	}
	for _, tc := range cases {
		if got := shouldTriggerResponders(tc.msg); got != tc.want {
			t.Errorf("%s: shouldTriggerResponders = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func manyAnnouncements(n int) []bus.Announcement {
	msgs := make([]bus.Announcement, n)
	for i := range msgs {
		msgs[i] = bus.Announcement{
			ID:       fmt.Sprintf("m%d", i),
			From:     "wt:a",
			Headline: fmt.Sprintf("headline %d", i),
			At:       time.Now(),
		}
	}
	return msgs
}

func digestLineCount(d string) int {
	n := 0
	for _, line := range strings.Split(d, "\n") {
		if strings.HasPrefix(line, "  [m") {
			n++
		}
	}
	return n
}

// The whole point of the ticket: a worktree checking in for the first time
// must get a short tail, not the entire log.
func TestBuildInboxDigestGivesFirstContactAShortTail(t *testing.T) {
	d := buildInboxDigest(manyAnnouncements(400), true)

	if got := digestLineCount(d); got != firstContactTail {
		t.Errorf("got %d message lines, want %d", got, firstContactTail)
	}
	if !strings.Contains(d, "headline 399") {
		t.Error("first-contact tail should keep the most recent messages")
	}
	if strings.Contains(d, "headline 0\n") {
		t.Error("first-contact tail should not include the oldest messages")
	}
	if !strings.Contains(d, "390 older") {
		t.Errorf("digest should say how many messages it withheld: %q", d)
	}
}

// An established session that has been away a long time is capped too — the
// cursor bounds it, but "since last check" can still be hundreds of messages.
func TestBuildInboxDigestCapsALongBacklog(t *testing.T) {
	d := buildInboxDigest(manyAnnouncements(200), false)

	if got := digestLineCount(d); got != maxDigestMessages {
		t.Errorf("got %d message lines, want %d", got, maxDigestMessages)
	}
	if !strings.Contains(d, "200 new bus announcement") {
		t.Errorf("digest should still report the true total: %q", d)
	}
}

func TestBuildInboxDigestUncappedWhenSmall(t *testing.T) {
	d := buildInboxDigest(manyAnnouncements(3), false)

	if got := digestLineCount(d); got != 3 {
		t.Errorf("got %d message lines, want 3", got)
	}
	if strings.Contains(d, "older") {
		t.Errorf("nothing was withheld, digest should not say otherwise: %q", d)
	}
}

// Headlines are meant to be one line. Senders paste whole reports into them —
// one such message cost more context than the rest of a digest together.
func TestDigestLineTruncatesAMultilineHeadline(t *testing.T) {
	line := digestLine(bus.Announcement{
		ID: "m1", From: "wt:a", At: time.Now(),
		Headline: "STOP THE REBASE\n\nHere is why, at length:\nreason one\nreason two",
	})

	if strings.Contains(line, "\n") {
		t.Errorf("digest line must stay on one line: %q", line)
	}
	if !strings.Contains(line, "STOP THE REBASE…") {
		t.Errorf("expected a truncation marker after the first line: %q", line)
	}
}

func TestDigestLineTruncatesAnOverlongHeadline(t *testing.T) {
	line := digestLine(bus.Announcement{
		ID: "m1", From: "wt:a", At: time.Now(),
		Headline: strings.Repeat("x", 2656),
	})

	if len([]rune(line)) > bus.MaxHeadline+80 {
		t.Errorf("digest line not truncated: %d runes", len([]rune(line)))
	}
	if !strings.HasSuffix(line, "…") {
		t.Errorf("expected a truncation marker: %q", line)
	}
}

func TestDigestLineLeavesAShortHeadlineAlone(t *testing.T) {
	line := digestLine(bus.Announcement{
		ID: "m1", From: "wt:a", At: time.Now(), Headline: "released test slot",
	})

	if !strings.HasSuffix(line, "released test slot") {
		t.Errorf("short headline should pass through untouched: %q", line)
	}
}
