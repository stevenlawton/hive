package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// SavedSession is one tmux session recorded in a checkpoint: enough to
// recreate it and resume the conversation that was running in it.
type SavedSession struct {
	Name  string `json:"name"`  // tmux session name
	Cwd   string `json:"cwd"`   // working directory to resume in
	Repo  string `json:"repo"`  // owning repo DirName, for grouping
	Label string `json:"label"` // display label, e.g. "wt:split-1"
}

// SavedState is a checkpoint of the running sessions, written by the
// save-state chord and offered back on the next start.
type SavedState struct {
	SavedAt  time.Time      `json:"saved_at"`
	Sessions []SavedSession `json:"sessions"`
}

// StateStore persists a SavedState at ~/.config/hive/state.json.
//
// tmux does not survive a reboot, but Claude conversations do — they live in
// ~/.claude/projects/<slug>/. Recording each session's name and cwd is enough
// to rebuild the layout and resume each conversation with --continue.
type StateStore struct {
	dir string
}

func OpenStateStore() (*StateStore, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(home, ".config", "hive")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return newStateStore(dir), nil
}

func newStateStore(dir string) *StateStore {
	return &StateStore{dir: dir}
}

// Save writes the checkpoint, replacing any previous one.
func (s *StateStore) Save(st SavedState) error {
	if s == nil {
		return nil
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.dir, "state-*.json")
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

// Load returns the checkpoint, and false when there is nothing usable to
// offer — missing, unreadable, corrupt, or empty.
func (s *StateStore) Load() (SavedState, bool) {
	if s == nil {
		return SavedState{}, false
	}
	data, err := os.ReadFile(s.path())
	if err != nil {
		return SavedState{}, false
	}
	var st SavedState
	if err := json.Unmarshal(data, &st); err != nil {
		return SavedState{}, false
	}
	if len(st.Sessions) == 0 {
		return SavedState{}, false
	}
	return st, true
}

// Clear removes the checkpoint once it has been acted on.
func (s *StateStore) Clear() error {
	if s == nil {
		return nil
	}
	err := os.Remove(s.path())
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func (s *StateStore) path() string {
	return filepath.Join(s.dir, "state.json")
}

// captureState records every live interactive session. Remote-control and
// Telegram-driven rows are skipped: those are owned by the bridge, not by
// this hive, and are re-established by their own machinery.
func (m *model) captureState() SavedState {
	st := SavedState{SavedAt: time.Now()}
	for i := range m.items {
		item := &m.items[i]
		if item.tmuxSes == "" || item.isTGSession {
			continue
		}
		if item.status != statusClaude && item.status != statusShell {
			continue
		}
		cwd, err := TmuxSessionCwd(item.tmuxSes)
		if err != nil || cwd == "" {
			cwd = item.repo.Path
		}
		label := item.repo.Short
		if item.repo.IsWorktree {
			label = "wt:" + item.repo.WorktreeBranch
		}
		st.Sessions = append(st.Sessions, SavedSession{
			Name:  item.tmuxSes,
			Cwd:   cwd,
			Repo:  repoGroupKey(item.repo),
			Label: label,
		})
	}
	return st
}

// restoreState recreates any saved session that is not already running and
// resumes its conversation, then re-maps everything into the UI. The remap is
// required: hive only binds tmux sessions to repos in reconnectSessions, so
// sessions created while it is running are otherwise invisible.
func (m *model) restoreState(st SavedState) int {
	restored := 0
	for _, sess := range st.Sessions {
		if sess.Name == "" || TmuxHasSession(sess.Name) {
			continue
		}
		if _, err := os.Stat(sess.Cwd); err != nil {
			continue // worktree removed since the checkpoint
		}
		if err := TmuxNewSessionWithCmd(sess.Name, sess.Cwd,
			claudeCommand("--continue")); err != nil {
			continue
		}
		restored++
	}
	if restored > 0 {
		m.reconnectSessions()
	}
	return restored
}

// pendingRestore reports whether a checkpoint has sessions that are not
// currently running — the only case worth prompting about.
func pendingRestore(st SavedState) bool {
	for _, sess := range st.Sessions {
		if sess.Name != "" && !TmuxHasSession(sess.Name) {
			return true
		}
	}
	return false
}

// Summary renders the checkpoint for the restore prompt: one line per repo
// with its session count, newest repos last so the order is stable.
func (st SavedState) Summary() string {
	byRepo := map[string]int{}
	for _, sess := range st.Sessions {
		key := sess.Repo
		if key == "" {
			key = sess.Name
		}
		byRepo[key]++
	}
	repos := make([]string, 0, len(byRepo))
	for r := range byRepo {
		repos = append(repos, r)
	}
	sort.Strings(repos)

	var b strings.Builder
	fmt.Fprintf(&b, "Saved %s\n\n", st.SavedAt.Format("2 Jan 15:04"))
	for _, r := range repos {
		if n := byRepo[r]; n > 1 {
			fmt.Fprintf(&b, "  %s  (%d sessions)\n", r, n)
		} else {
			fmt.Fprintf(&b, "  %s\n", r)
		}
	}
	fmt.Fprintf(&b, "\nRestore all %d? (y/n)", len(st.Sessions))
	return b.String()
}
