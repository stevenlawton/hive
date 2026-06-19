package main

import (
	"testing"
)

func TestStripTGSessionItems(t *testing.T) {
	items := []repoItem{
		{repo: Repo{DirName: "foo"}},
		{repo: Repo{DirName: "foo"}, isTGSession: true},
		{repo: Repo{DirName: "bar"}},
		{repo: Repo{DirName: "bar"}, isTGSession: true},
	}
	out := stripTGSessionItems(items)
	if len(out) != 2 {
		t.Fatalf("expected 2 non-synth items, got %d", len(out))
	}
	for _, it := range out {
		if it.isTGSession {
			t.Errorf("synth row leaked through: %+v", it)
		}
	}
}

func TestResolveBridgeRepoDirectHit(t *testing.T) {
	repos := map[string]*Repo{
		"SliceWise": {DirName: "SliceWise", Path: "/repos/SliceWise"},
	}
	entry := BridgeEntry{RepoPath: "/repos/SliceWise", SessionID: "abc"}
	r := resolveBridgeRepo(repos, "SliceWise", entry)
	if r == nil || r.DirName != "SliceWise" {
		t.Fatalf("expected SliceWise, got %+v", r)
	}
}

func TestResolveBridgeRepoBasenameFallback(t *testing.T) {
	// Collection child: DirName is "manuscripts/manuscript", bot wrote bare key.
	repos := map[string]*Repo{
		"manuscripts/manuscript": {DirName: "manuscripts/manuscript", Path: "/repos/manuscripts/manuscript"},
	}
	entry := BridgeEntry{RepoPath: "/repos/manuscripts/manuscript", SessionID: "abc"}
	r := resolveBridgeRepo(repos, "manuscript", entry)
	if r == nil || r.DirName != "manuscripts/manuscript" {
		t.Fatalf("expected collection-child match, got %+v", r)
	}
}

func TestResolveBridgeRepoBasenameFallbackRejectsPathMismatch(t *testing.T) {
	// Two repos with the same basename — fallback must verify path to
	// avoid cross-binding.
	repos := map[string]*Repo{
		"manuscripts/manuscript": {DirName: "manuscripts/manuscript", Path: "/repos/manuscripts/manuscript"},
		"other/manuscript":       {DirName: "other/manuscript", Path: "/repos/other/manuscript"},
	}
	entry := BridgeEntry{RepoPath: "/repos/manuscripts/manuscript", SessionID: "abc"}
	r := resolveBridgeRepo(repos, "manuscript", entry)
	if r == nil {
		t.Fatal("expected a match")
	}
	if r.Path != "/repos/manuscripts/manuscript" {
		t.Errorf("fallback matched wrong repo: %+v", r)
	}
}

func TestResolveBridgeRepoBasenameFallbackNoPathInEntry(t *testing.T) {
	// Older registry entries without repo_path field — fallback still works
	// when the basename uniquely identifies a repo.
	repos := map[string]*Repo{
		"manuscripts/manuscript": {DirName: "manuscripts/manuscript", Path: "/repos/manuscripts/manuscript"},
	}
	entry := BridgeEntry{SessionID: "abc"} // no RepoPath
	r := resolveBridgeRepo(repos, "manuscript", entry)
	if r == nil || r.DirName != "manuscripts/manuscript" {
		t.Fatalf("expected basename match without RepoPath, got %+v", r)
	}
}

func TestResolveBridgeRepoMiss(t *testing.T) {
	repos := map[string]*Repo{
		"foo": {DirName: "foo", Path: "/repos/foo"},
	}
	entry := BridgeEntry{SessionID: "abc"}
	r := resolveBridgeRepo(repos, "nonexistent", entry)
	if r != nil {
		t.Errorf("expected nil for unknown key, got %+v", r)
	}
}

func TestInterleaveSynthRows(t *testing.T) {
	items := []repoItem{
		{repo: Repo{DirName: "alpha"}},
		{repo: Repo{DirName: "bravo"}},
		{repo: Repo{DirName: "charlie"}},
	}
	synth := []repoItem{
		{repo: Repo{DirName: "bravo"}, isTGSession: true},
		{repo: Repo{DirName: "alpha"}, isTGSession: true},
	}
	out := interleaveSynthRows(items, synth)
	if len(out) != 5 {
		t.Fatalf("expected 5 rows, got %d", len(out))
	}
	// Expect: alpha, alpha(synth), bravo, bravo(synth), charlie
	expect := []struct {
		dir   string
		synth bool
	}{
		{"alpha", false}, {"alpha", true},
		{"bravo", false}, {"bravo", true},
		{"charlie", false},
	}
	for i, want := range expect {
		got := out[i]
		if got.repo.DirName != want.dir || got.isTGSession != want.synth {
			t.Errorf("row %d: want %s synth=%v, got %s synth=%v",
				i, want.dir, want.synth, got.repo.DirName, got.isTGSession)
		}
	}
}

func TestInterleaveSynthRowsEmpty(t *testing.T) {
	items := []repoItem{{repo: Repo{DirName: "foo"}}}
	out := interleaveSynthRows(items, nil)
	if len(out) != 1 {
		t.Errorf("expected pass-through with no synth, got len=%d", len(out))
	}
}
