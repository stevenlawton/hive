package main

import (
	"encoding/json"
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
	d := buildInboxDigest(unseen)
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
