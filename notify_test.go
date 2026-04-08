package main

import (
	"testing"
)

func TestDetectDeadSessions(t *testing.T) {
	items := []repoItem{
		{repo: Repo{DirName: "alive"}, status: statusClaude, tmuxSes: "kl-alive"},
		{repo: Repo{DirName: "dead"}, status: statusClaude, tmuxSes: "kl-dead"},
	}
	liveSessions := map[string]bool{"kl-alive": true}

	alerts := DetectDeadSessions(items, liveSessions)

	if len(alerts) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(alerts))
	}
	if alerts["dead"] != "session crashed" {
		t.Errorf("expected 'session crashed', got %q", alerts["dead"])
	}
}

func TestDetectDeadRemote(t *testing.T) {
	items := []repoItem{
		{repo: Repo{DirName: "myrepo", Remote: true}, status: statusRemote, tmuxSes: "kl-myrepo"},
	}
	liveSessions := map[string]bool{"kl-myrepo": true}

	alerts := DetectDeadRemotes(items, liveSessions)

	if len(alerts) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(alerts))
	}
	if alerts["myrepo"] != "remote died" {
		t.Errorf("expected 'remote died', got %q", alerts["myrepo"])
	}
}
