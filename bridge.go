package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	tea "charm.land/bubbletea/v2"
)

// BridgeEntry represents a session in the shared bridge registry.
type BridgeEntry struct {
	SessionID   string `json:"session_id"`
	RepoPath    string `json:"repo_path"`
	Driver      string `json:"driver"`       // "telegram", "desktop", "none"
	DriverSince string `json:"driver_since"` // ISO 8601
}

func bridgeFilePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "bridge-sessions.json")
}

// ReadBridgeRegistry reads ~/.claude/bridge-sessions.json and returns repo_key -> BridgeEntry.
func ReadBridgeRegistry() map[string]BridgeEntry {
	data, err := os.ReadFile(bridgeFilePath())
	if err != nil {
		return nil
	}
	var registry map[string]BridgeEntry
	if err := json.Unmarshal(data, &registry); err != nil {
		return nil
	}
	return registry
}

// takeoverTelegram claims a telegram-driven session for the desktop TUI.
func (m *model) takeoverTelegram() tea.Cmd {
	item := m.selectedItem()
	if item == nil || item.status != statusTelegram {
		return nil
	}
	UpdateBridgeEntry(sanitizeSessionName(item.repo.DirName), "desktop")
	return m.openSelected(true)
}

// UpdateBridgeEntry updates the driver field for a repo in the bridge registry.
func UpdateBridgeEntry(repoKey string, driver string) error {
	registry := ReadBridgeRegistry()
	if registry == nil {
		registry = make(map[string]BridgeEntry)
	}

	entry := registry[repoKey]
	entry.Driver = driver
	entry.DriverSince = time.Now().UTC().Format(time.RFC3339)
	registry[repoKey] = entry

	data, err := json.MarshalIndent(registry, "", "  ")
	if err != nil {
		return err
	}

	path := bridgeFilePath()
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
