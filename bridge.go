package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
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

// PruneStaleBridgeEntries reads the registry and clears the driver on entries
// where a Telegram driver is recorded but no claude session_id was ever set —
// the bot can't resume and the desktop can't pick up, so the entry is dead.
// Cleared entries are demoted to driver "none" (kept, not deleted) so the bot
// can re-claim them naturally on next use. The whole map is returned for the
// caller's use; the file is rewritten only when something actually changed.
func PruneStaleBridgeEntries() map[string]BridgeEntry {
	registry := ReadBridgeRegistry()
	if registry == nil {
		return nil
	}
	changed := false
	now := time.Now().UTC().Format(time.RFC3339)
	for key, entry := range registry {
		if entry.Driver == "telegram" && entry.SessionID == "" {
			entry.Driver = "none"
			entry.DriverSince = now
			registry[key] = entry
			changed = true
		}
	}
	if !changed {
		return registry
	}
	data, err := json.MarshalIndent(registry, "", "  ")
	if err != nil {
		return registry
	}
	path := bridgeFilePath()
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return registry
	}
	_ = os.Rename(tmp, path)
	return registry
}

// promptTelegramPickup pops the y/n confirm for taking over a TG-driven
// session. Returns true if a confirm was raised (caller should not also run
// the normal open flow). Returns false if the selected item isn't a live
// TG session, in which case the caller should fall through to its default.
func (m *model) promptTelegramPickup() bool {
	item := m.selectedItem()
	if item == nil || item.status != statusTelegram {
		return false
	}
	if item.bridgeEntry == nil || item.bridgeEntry.SessionID == "" {
		return false
	}
	repoName := item.repo.Name
	m.confirmMsg = fmt.Sprintf("Pick up TG session for %s? (y/n)", repoName)
	m.confirmAction = func() {
		m.takeoverTelegram()
	}
	m.mode = viewConfirm
	return true
}

// takeoverTelegram performs the handoff from the Telegram-driven claude
// to the desktop TUI: marks the registry "desktop", interrupts the bot's
// claude in the existing tmux session, resumes the same conversation
// interactively, and opens it as a tab.
func (m *model) takeoverTelegram() {
	item := m.selectedItem()
	if item == nil || item.status != statusTelegram {
		return
	}
	if item.bridgeEntry == nil || item.bridgeEntry.SessionID == "" {
		return
	}
	repo := item.repo
	sessionName := TmuxSessionName(repo.DirName, false)
	if !TmuxHasSession(sessionName) {
		return
	}
	TmuxSendKeys(sessionName, "C-c")
	TmuxSendKeys(sessionName, "claude --resume "+item.bridgeEntry.SessionID)
	UpdateBridgeEntry(repo.DirName, "desktop")
	item.status = statusClaude
	item.bridgeEntry = nil
	item.tmuxSes = sessionName
	m.rebuildDisplayOrder()
	m.openAsTab(repo, sessionName)
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
