package main

import (
	"testing"
)

func TestMapTmuxSessionsToItems(t *testing.T) {
	// A repo with both interactive and remote sessions should be marked
	// statusClaude (interactive wins) so a workspace tab gets opened —
	// the remote helper doesn't downgrade the repo.
	items := []repoItem{
		{repo: Repo{DirName: "project-a", Short: "PA"}},
		{repo: Repo{DirName: "project-b", Short: "PB"}},
		{repo: Repo{DirName: "project-c", Short: "PC"}},
	}
	sessions := []TmuxSession{
		{Name: "kl-project-a", RepoKey: "project-a"},
		{Name: "kl-rc-project-a", RepoKey: "project-a", IsRemote: true},
		{Name: "hive-rc-project-c", RepoKey: "project-c", IsRemote: true},
	}

	MapSessionsToItems(items, sessions)

	// project-a has both → interactive wins
	if items[0].status != statusClaude {
		t.Errorf("project-a should be statusClaude (interactive wins), got %d", items[0].status)
	}
	if items[0].tmuxSes != "kl-project-a" {
		t.Errorf("expected kl-project-a, got %s", items[0].tmuxSes)
	}
	// project-b has no sessions
	if items[1].status != statusNone {
		t.Errorf("project-b should have no session, got %d", items[1].status)
	}
	// project-c has only a remote helper → statusRemote
	if items[2].status != statusRemote {
		t.Errorf("project-c should be statusRemote, got %d", items[2].status)
	}
	if items[2].tmuxSes != "hive-rc-project-c" {
		t.Errorf("expected hive-rc-project-c, got %s", items[2].tmuxSes)
	}
}

func TestMapTmuxSessionsToItemsPrefersHivePrefix(t *testing.T) {
	// When both legacy (kl-) and current (hive-) interactive sessions
	// exist for the same repo, prefer the hive- prefix so the session
	// tmuxSes matches what TmuxSessionName() would construct.
	items := []repoItem{
		{repo: Repo{DirName: "polybot", Short: "PB"}},
	}
	sessions := []TmuxSession{
		{Name: "hive-polybot", RepoKey: "polybot"},
		{Name: "kl-polybot", RepoKey: "polybot"},
	}

	MapSessionsToItems(items, sessions)

	if items[0].tmuxSes != "hive-polybot" {
		t.Errorf("expected hive-polybot (non-legacy preferred), got %s", items[0].tmuxSes)
	}
	if items[0].status != statusClaude {
		t.Errorf("expected statusClaude, got %d", items[0].status)
	}
}
