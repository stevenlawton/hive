package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig_Defaults(t *testing.T) {
	cfg, err := LoadConfig("/nonexistent/path/config.yaml")
	if err != nil {
		t.Fatalf("expected no error for missing config, got: %v", err)
	}
	home, _ := os.UserHomeDir()
	if cfg.ReposDir != filepath.Join(home, "repos") {
		t.Errorf("expected default repos_dir %s/repos, got %s", home, cfg.ReposDir)
	}
	if cfg.ScratchDir != "/tmp/kl-scratch" {
		t.Errorf("expected default scratch_dir /tmp/kl-scratch, got %s", cfg.ScratchDir)
	}
	if cfg.DefaultAction != "claude" {
		t.Errorf("expected default_action claude, got %s", cfg.DefaultAction)
	}
}

func TestLoadConfig_ParsesYAML(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	content := []byte(`
repos_dir: /tmp/test-repos
scratch_dir: /tmp/test-scratch
default_action: shell
workspaces:
  myrepo:
    name: "My Repo"
    short: "MR"
    color: "#ff0000"
    remote: true
    favourite: true
`)
	os.WriteFile(cfgPath, content, 0644)

	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ReposDir != "/tmp/test-repos" {
		t.Errorf("expected /tmp/test-repos, got %s", cfg.ReposDir)
	}
	if cfg.DefaultAction != "shell" {
		t.Errorf("expected shell, got %s", cfg.DefaultAction)
	}
	ws, ok := cfg.Workspaces["myrepo"]
	if !ok {
		t.Fatal("expected myrepo workspace")
	}
	if ws.Name != "My Repo" || ws.Short != "MR" || ws.Color != "#ff0000" || !ws.Remote || !ws.Favourite {
		t.Errorf("workspace fields not parsed correctly: %+v", ws)
	}
}

func TestDiscoverRepos(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "repo-a"), 0755)
	os.MkdirAll(filepath.Join(dir, "repo-b"), 0755)
	os.WriteFile(filepath.Join(dir, "not-a-dir.txt"), []byte("hi"), 0644)

	cfg := &Config{
		ReposDir: dir,
		Workspaces: map[string]WorkspaceConfig{
			"repo-a": {Name: "Alpha", Short: "AL", Color: "#ff0000", Favourite: true},
		},
	}

	repos := DiscoverRepos(cfg)
	if len(repos) != 2 {
		t.Fatalf("expected 2 repos, got %d", len(repos))
	}

	var alpha, bravo *Repo
	for i := range repos {
		if repos[i].DirName == "repo-a" {
			alpha = &repos[i]
		}
		if repos[i].DirName == "repo-b" {
			bravo = &repos[i]
		}
	}
	if alpha == nil || bravo == nil {
		t.Fatal("missing expected repos")
	}
	if alpha.Name != "Alpha" || alpha.Short != "AL" {
		t.Errorf("alpha overrides not applied: %+v", alpha)
	}
	if bravo.Name != "repo-b" || bravo.Short != "REP" {
		t.Errorf("bravo defaults not applied: name=%s short=%s", bravo.Name, bravo.Short)
	}
}
