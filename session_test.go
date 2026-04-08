package main

import (
	"testing"
)

func TestMapTmuxSessionsToItems(t *testing.T) {
	items := []repoItem{
		{repo: Repo{DirName: "project-a", Short: "PA"}},
		{repo: Repo{DirName: "project-b", Short: "PB"}},
	}
	sessions := []TmuxSession{
		{Name: "kl-project-a", RepoKey: "project-a"},
		{Name: "kl-rc-project-a", RepoKey: "project-a", IsRemote: true},
	}

	MapSessionsToItems(items, sessions)

	if items[0].status != statusRemote {
		t.Errorf("project-a should be statusRemote, got %d", items[0].status)
	}
	if items[0].tmuxSes != "kl-project-a" {
		t.Errorf("expected kl-project-a, got %s", items[0].tmuxSes)
	}
	if items[1].status != statusNone {
		t.Errorf("project-b should have no session, got %d", items[1].status)
	}
}
