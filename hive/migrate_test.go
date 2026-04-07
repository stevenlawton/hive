package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectLegacySessions(t *testing.T) {
	sessions := []TmuxSession{
		{Name: "kl-workspace", RepoKey: "workspace"},
		{Name: "hive-polybot", RepoKey: "polybot"},
		{Name: "kl-rc-SliceWize", IsRemote: true, RepoKey: "SliceWize"},
	}

	legacy := DetectLegacySessions(sessions)
	if len(legacy) != 2 {
		t.Fatalf("expected 2 legacy sessions, got %d", len(legacy))
	}
	if legacy[0].Name != "kl-workspace" {
		t.Errorf("expected kl-workspace, got %s", legacy[0].Name)
	}
	if legacy[1].Name != "kl-rc-SliceWize" {
		t.Errorf("expected kl-rc-SliceWize, got %s", legacy[1].Name)
	}
}

func TestNeedsConfigMigration(t *testing.T) {
	// Neither exists
	needs, _ := NeedsConfigMigration("/tmp/nonexistent-hive-test", "/tmp/nonexistent-kl-test")
	if needs {
		t.Error("should not need migration when neither exists")
	}

	// Only kl exists
	tmpDir := t.TempDir()
	klPath := filepath.Join(tmpDir, "kl", "config.yaml")
	hivePath := filepath.Join(tmpDir, "hive", "config.yaml")
	os.MkdirAll(filepath.Dir(klPath), 0o755)
	os.WriteFile(klPath, []byte("test: true"), 0o644)

	needs, src := NeedsConfigMigration(hivePath, klPath)
	if !needs {
		t.Error("should need migration when only kl exists")
	}
	if src != klPath {
		t.Errorf("expected source %s, got %s", klPath, src)
	}

	// Both exist (hive takes precedence)
	os.MkdirAll(filepath.Dir(hivePath), 0o755)
	os.WriteFile(hivePath, []byte("hive: true"), 0o644)
	needs, _ = NeedsConfigMigration(hivePath, klPath)
	if needs {
		t.Error("should not need migration when hive config exists")
	}
}

func TestMigrateConfig(t *testing.T) {
	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "src", "config.yaml")
	dstPath := filepath.Join(tmpDir, "dst", "config.yaml")

	os.MkdirAll(filepath.Dir(srcPath), 0o755)
	content := "repos_dir: ~/repos\ndefault_action: claude\n"
	os.WriteFile(srcPath, []byte(content), 0o644)

	err := MigrateConfig(srcPath, dstPath)
	if err != nil {
		t.Fatalf("migration failed: %v", err)
	}

	data, err := os.ReadFile(dstPath)
	if err != nil {
		t.Fatalf("failed to read migrated config: %v", err)
	}
	if string(data) != content {
		t.Errorf("expected %q, got %q", content, string(data))
	}
}
