package main

import (
	"testing"
)

func TestMapTmuxSessionsToItems(t *testing.T) {
	items := []repoItem{
		{repo: Repo{DirName: "SliceWise", Short: "SW"}},
		{repo: Repo{DirName: "lom2", Short: "LOM"}},
	}
	sessions := []TmuxSession{
		{Name: "kl-SliceWise", RepoKey: "SliceWise"},
		{Name: "kl-rc-SliceWise", RepoKey: "SliceWise", IsRemote: true},
	}

	MapSessionsToItems(items, sessions)

	if items[0].status != statusRemote {
		t.Errorf("SliceWise should be statusRemote, got %d", items[0].status)
	}
	if items[0].tmuxSes != "kl-SliceWise" {
		t.Errorf("expected kl-SliceWise, got %s", items[0].tmuxSes)
	}
	if items[1].status != statusNone {
		t.Errorf("lom2 should have no session, got %d", items[1].status)
	}
}
