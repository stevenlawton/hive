package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// repoForPickupSession returns the DirName of the repo a hive-tg-<sanitized>
// tmux session belongs to, by reverse-matching the sanitized suffix against
// the known repos' DirNames. Returns false if the session isn't a pickup or
// no repo matches.
func repoForPickupSession(sessionName string, repos map[string]*Repo) (string, bool) {
	const prefix = tmuxPickupPrefix
	if len(sessionName) <= len(prefix) || sessionName[:len(prefix)] != prefix {
		return "", false
	}
	suffix := sessionName[len(prefix):]
	for dirName := range repos {
		if sanitizeSessionName(dirName) == suffix {
			return dirName, true
		}
	}
	return "", false
}

// stripTGSessionItems returns items with all synthetic TG-session rows removed.
// Called at the start of each bridge-registry refresh so synthetic rows can be
// regenerated from scratch without per-entry diffing.
func stripTGSessionItems(items []repoItem) []repoItem {
	out := items[:0]
	for _, it := range items {
		if it.isTGSession {
			continue
		}
		out = append(out, it)
	}
	return out
}

// indexReposByKey builds a lookup of real (non-synthetic) repo items keyed by
// DirName. Bridge-registry resolution checks this first directly, then falls
// back to filepath.Base(DirName) so collection-namespaced repos (DirName like
// "manuscripts/manuscript") can match bare bot-written keys ("manuscript").
func indexReposByKey(items []repoItem) map[string]*Repo {
	out := make(map[string]*Repo, len(items))
	for i := range items {
		if items[i].isTGSession {
			continue
		}
		r := items[i].repo
		out[r.DirName] = &r
	}
	return out
}

// interleaveSynthRows returns a new items slice where each synth row appears
// immediately after the parent repo item it references. Synth rows with no
// matching parent (shouldn't occur — resolveBridgeRepo filters those — but
// defensive) are appended at the end.
func interleaveSynthRows(items []repoItem, synth []repoItem) []repoItem {
	if len(synth) == 0 {
		return items
	}
	byParent := make(map[string][]repoItem, len(synth))
	for _, s := range synth {
		byParent[s.repo.DirName] = append(byParent[s.repo.DirName], s)
	}
	out := make([]repoItem, 0, len(items)+len(synth))
	for _, it := range items {
		out = append(out, it)
		if children, ok := byParent[it.repo.DirName]; ok {
			out = append(out, children...)
			delete(byParent, it.repo.DirName)
		}
	}
	for _, leftover := range byParent {
		out = append(out, leftover...)
	}
	return out
}

// resolveBridgeRepo finds the repo a bridge-registry key refers to. Tries the
// direct key first, then falls back to basename matching (for collection
// children whose DirName is "<parent>/<child>" while the bot writes "<child>").
// The fallback is only accepted if the entry's repo_path matches the discovered
// repo's path on disk — otherwise two repos sharing a basename could cross-bind.
func resolveBridgeRepo(repos map[string]*Repo, key string, entry BridgeEntry) *Repo {
	if r, ok := repos[key]; ok {
		return r
	}
	// Direct miss — basename fallback for collection children.
	for dirName, r := range repos {
		if filepath.Base(dirName) != key {
			continue
		}
		if entry.RepoPath != "" && entry.RepoPath != r.Path {
			continue
		}
		return r
	}
	return nil
}

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

// tmuxPickupPrefix is the tmux session-name prefix for picked-up TG
// sessions on the desktop side. Distinct from the interactive prefix
// ("hive-") so a pickup never overwrites the user's existing interactive
// session — both can coexist as independent rows in the manager.
const tmuxPickupPrefix = "hive-tg-"

// TmuxPickupSessionName returns the tmux session name for a picked-up
// TG session on a given repo's DirName.
func TmuxPickupSessionName(dirName string) string {
	return tmuxPickupPrefix + sanitizeSessionName(dirName)
}

// takeoverTelegram performs the handoff from the Telegram-driven claude
// to the desktop TUI. It creates a NEW tmux session named `hive-tg-<dir>`
// running `claude --resume <session_id>` so the picked-up conversation
// runs alongside (never replaces) any existing interactive session for
// the same repo. Also flips the registry to driver="desktop" so the bot
// knows to release.
func (m *model) takeoverTelegram() {
	item := m.selectedItem()
	if item == nil || item.status != statusTelegram {
		return
	}
	if item.bridgeEntry == nil || item.bridgeEntry.SessionID == "" {
		return
	}
	repo := item.repo
	sessionID := item.bridgeEntry.SessionID
	regKey := item.bridgeKey
	if regKey == "" {
		regKey = repo.DirName
	}

	claudeCmd := "claude --resume " + sessionID
	if repo.Yolo {
		claudeCmd += " --permission-mode bypassPermissions"
	}

	pickupName := TmuxPickupSessionName(repo.DirName)
	if TmuxHasSession(pickupName) {
		// A previous pickup for this repo is still around — focus it
		// instead of spawning a duplicate.
		m.openAsTab(repo, pickupName)
		return
	}
	if err := TmuxNewSessionWithCmd(pickupName, repo.Path, claudeCmd); err != nil {
		m.err = err
		return
	}

	UpdateBridgeEntry(regKey, "desktop")
	// Synth row's in-memory state updates — it now represents the live
	// pickup tmux session, not the (now-defunct) bridge registry entry.
	item.bridgeEntry = nil
	item.tmuxSes = pickupName
	m.rebuildDisplayOrder()
	m.openAsTab(repo, pickupName)
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
