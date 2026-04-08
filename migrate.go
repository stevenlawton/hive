package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// DetectLegacySessions returns sessions using the legacy kl- prefix.
func DetectLegacySessions(sessions []TmuxSession) []TmuxSession {
	var legacy []TmuxSession
	for _, s := range sessions {
		if strings.HasPrefix(s.Name, "kl-") {
			legacy = append(legacy, s)
		}
	}
	return legacy
}

// NeedsConfigMigration checks if config should be copied from kl location.
func NeedsConfigMigration(hivePath, klPath string) (bool, string) {
	if _, err := os.Stat(hivePath); err == nil {
		return false, ""
	}
	if _, err := os.Stat(klPath); err == nil {
		return true, klPath
	}
	return false, ""
}

// MigrateConfig copies config from kl path to hive path.
func MigrateConfig(srcPath, dstPath string) error {
	if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	src, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("open source config: %w", err)
	}
	defer src.Close()

	dst, err := os.Create(dstPath + ".tmp")
	if err != nil {
		return fmt.Errorf("create dest config: %w", err)
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		return fmt.Errorf("copy config: %w", err)
	}
	return os.Rename(dstPath+".tmp", dstPath)
}

// RunMigration checks for and performs config migration on startup.
func RunMigration() {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	hiveCfg := filepath.Join(home, ".config", "hive", "config.yaml")
	klCfg := filepath.Join(home, ".config", "kittylauncher", "config.yaml")

	if needs, src := NeedsConfigMigration(hiveCfg, klCfg); needs {
		if err := MigrateConfig(src, hiveCfg); err != nil {
			fmt.Fprintf(os.Stderr, "config migration failed: %v\n", err)
		}
	}
}
