package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"github.com/stevenlawton/hive/ui"
)

// LayoutStore persists per-repo workspace layout across restarts.
//
// Orientation is also mirrored onto the parent tmux session as
// HIVE_ORIENTATION, which is the faster path and stays authoritative while
// tmux lives. That copy dies with the tmux server though — a crash or reboot
// silently reverted every tab to the default. This store is the durable
// fallback consulted when the env var is absent.
//
// Storage: ~/.config/hive/layout.json, a repo DirName -> "h"|"v" map.
type LayoutStore struct {
	dir string
	mu  sync.Mutex
}

type layoutState struct {
	Orientation map[string]string `json:"orientation"`
}

// OpenLayoutStore returns a store rooted at ~/.config/hive/.
func OpenLayoutStore() (*LayoutStore, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(home, ".config", "hive")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return newLayoutStore(dir), nil
}

func newLayoutStore(dir string) *LayoutStore {
	return &LayoutStore{dir: dir}
}

// Get returns the stored orientation code for a repo, or "" if none.
func (s *LayoutStore) Get(repo string) string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.load().Orientation[repo]
}

// Set records the orientation code for a repo.
func (s *LayoutStore) Set(repo, orient string) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	state := s.load()
	state.Orientation[repo] = orient

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	// Write via a temp file so a crash mid-write can't leave a truncated
	// layout.json behind — the whole point of this store is crash survival.
	tmp, err := os.CreateTemp(s.dir, "layout-*.json")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), s.path())
}

// load reads the state file. A missing or unparseable file yields an empty
// state rather than an error: a corrupt layout.json should cost the user
// their pane arrangement, not their session.
func (s *LayoutStore) load() layoutState {
	state := layoutState{Orientation: map[string]string{}}
	data, err := os.ReadFile(s.path())
	if err != nil {
		return state
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return layoutState{Orientation: map[string]string{}}
	}
	if state.Orientation == nil {
		state.Orientation = map[string]string{}
	}
	return state
}

func (s *LayoutStore) path() string {
	return filepath.Join(s.dir, "layout.json")
}

// persistOrientation records a tab's orientation in both places: the parent
// tmux session (fast, authoritative while tmux lives) and layout.json (the
// copy that survives a tmux server death).
func (m *model) persistOrientation(parentDir, parentSession string, o ui.SplitOrientation) {
	code := orientCode(o)
	if parentSession != "" {
		TmuxSetEnv(parentSession, "HIVE_ORIENTATION", code)
	}
	// A failed layout write costs the pane arrangement on next start, which
	// isn't worth interrupting the user for.
	_ = m.layout.Set(parentDir, code)
}

// orientationFor resolves a repo's stored orientation, tmux env first. The
// bool reports whether any preference was found.
func (m *model) orientationFor(parentDir, parentSession string) (ui.SplitOrientation, bool) {
	if parentSession != "" {
		if o, ok := parseOrient(TmuxGetEnv(parentSession, "HIVE_ORIENTATION")); ok {
			return o, true
		}
	}
	return parseOrient(m.layout.Get(parentDir))
}

// orientCode renders an orientation for storage.
func orientCode(o ui.SplitOrientation) string {
	if o == ui.SplitHorizontal {
		return "h"
	}
	return "v"
}

// parseOrient reads a stored orientation code. The bool reports whether the
// code was recognised, letting callers distinguish "no preference recorded"
// from an explicit "v".
func parseOrient(code string) (ui.SplitOrientation, bool) {
	switch code {
	case "h":
		return ui.SplitHorizontal, true
	case "v":
		return ui.SplitVertical, true
	}
	return ui.SplitVertical, false
}
