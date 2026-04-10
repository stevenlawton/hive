package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// PruneZombieSessions kills hive-managed tmux sessions whose working
// directory no longer exists on disk. The most common case: a worktree
// session left behind after the worktree itself was deleted, visible as
// a dangling "(deleted)" suffix in /proc/<pid>/cwd.
//
// Only touches sessions with the current `hive-` prefix (including
// scratch, excluding remote). Legacy `kl-*`, numbered sessions, and
// user-created sessions are untouched. Returns the names of sessions
// that were killed.
func PruneZombieSessions(sessions []TmuxSession) []string {
	var killed []string
	for _, s := range sessions {
		// Scope: only hive-* interactive sessions. Remote sessions
		// (hive-rc-*) don't necessarily have a cwd on this machine, so
		// don't prune them based on cwd.
		if s.IsRemote {
			continue
		}
		if !strings.HasPrefix(s.Name, tmuxPrefix) {
			continue
		}
		cwd, err := TmuxSessionCwd(s.Name)
		if err != nil || !isZombieCwd(cwd) {
			continue
		}
		if err := TmuxKillSession(s.Name); err != nil {
			continue
		}
		killed = append(killed, s.Name)
	}
	return killed
}

// isZombieCwd returns true if the given path indicates a deleted working
// directory. Linux marks a deleted cwd with " (deleted)" in /proc/<pid>/cwd;
// we also treat a genuinely-not-found path as zombie.
func isZombieCwd(cwd string) bool {
	if strings.HasSuffix(cwd, " (deleted)") {
		return true
	}
	if _, err := os.Stat(cwd); errors.Is(err, os.ErrNotExist) {
		return true
	}
	return false
}

func MapSessionsToItems(items []repoItem, sessions []TmuxSession) {
	sessionMap := make(map[string][]TmuxSession)
	for _, s := range sessions {
		sessionMap[s.RepoKey] = append(sessionMap[s.RepoKey], s)
	}

	for i := range items {
		dirName := sanitizeSessionName(items[i].repo.DirName)
		sess, ok := sessionMap[dirName]
		if !ok {
			items[i].status = statusNone
			items[i].tmuxSes = ""
			continue
		}

		// Pick the best interactive and best remote session names.
		// Preference order for interactive: first non-legacy (hive-) match,
		// then fall back to legacy (kl-) match. Same for remote.
		var interactiveName, remoteName string
		var hasLegacyInteractive, hasLegacyRemote bool
		for _, s := range sess {
			legacy := strings.HasPrefix(s.Name, legacyPrefix) ||
				strings.HasPrefix(s.Name, legacyRemotePrefix) ||
				strings.HasPrefix(s.Name, legacyScratchPfx)
			if s.IsRemote {
				if remoteName == "" || (hasLegacyRemote && !legacy) {
					remoteName = s.Name
					hasLegacyRemote = legacy
				}
			} else {
				if interactiveName == "" || (hasLegacyInteractive && !legacy) {
					interactiveName = s.Name
					hasLegacyInteractive = legacy
				}
			}
		}

		// Status: interactive wins. A remote helper running alongside an
		// interactive session doesn't downgrade the repo to statusRemote —
		// we still want a workspace tab for the interactive pane.
		switch {
		case interactiveName != "":
			items[i].tmuxSes = interactiveName
			items[i].status = statusClaude
		case remoteName != "":
			items[i].tmuxSes = remoteName
			items[i].status = statusRemote
		default:
			items[i].status = statusNone
			items[i].tmuxSes = ""
		}
	}
}

func (m *model) openSelected(withClaude bool) tea.Cmd {
	item := m.selectedItem()
	if item == nil || item.repo.IsCollection {
		return nil
	}

	repo := item.repo
	sessionName := TmuxSessionName(repo.DirName, false)

	if TmuxHasSession(sessionName) {
		// Transition from a telegram-bridged session to interactive: kill
		// the bot-driven claude and resume the same conversation for the
		// human. Guarded on status, not just bridgeEntry — a stale bridge
		// file would otherwise fire this every time the user re-enters a
		// normal claude session after a Hive restart.
		if withClaude && item.status == statusTelegram && item.bridgeEntry != nil && item.bridgeEntry.SessionID != "" {
			TmuxSendKeys(sessionName, "C-c")
			TmuxSendKeys(sessionName, "claude --resume "+item.bridgeEntry.SessionID)
			item.status = statusClaude
			item.bridgeEntry = nil
			m.rebuildDisplayOrder()
			m.openAsTab(repo, sessionName)
			return nil
		}
		m.clearFlash(item)
		m.openAsTab(repo, sessionName)
		return nil
	}

	if err := TmuxNewSession(sessionName, repo.Path); err != nil {
		m.err = err
		return nil
	}

	if withClaude {
		claudeCmd := "claude"
		if repo.Yolo {
			claudeCmd = "claude --permission-mode bypassPermissions"
		}
		TmuxSendKeys(sessionName, claudeCmd)
		item.status = statusClaude
	} else {
		item.status = statusShell
	}
	item.tmuxSes = sessionName
	m.rebuildDisplayOrder()

	m.openAsTab(repo, sessionName)
	return nil
}

// openAsTab opens a repo in the workspace. Worktrees become splits on parent tab.
func (m *model) openAsTab(repo Repo, sessionName string) {
	if repo.IsWorktree && repo.Parent != "" {
		// Check if parent tab already exists (e.g. from reconnect)
		if _, exists := m.workspace.Tabs[repo.Parent]; exists {
			m.workspace.TabBar.FocusByID(repo.Parent)
			m.mode = viewWorkspace
			return
		}
		// Find parent's session and create parent tab
		for _, it := range m.items {
			if it.repo.DirName == repo.Parent && it.tmuxSes != "" {
				m.workspace.OpenTab(repo.Parent, it.repo.Short, it.tmuxSes, "main")
				m.workspace.AddSplitToActive("wt:"+repo.WorktreeBranch, sessionName)
				m.mode = viewWorkspace
				return
			}
		}
	}
	// Check if a tab already exists for this repo
	if _, exists := m.workspace.Tabs[repo.DirName]; exists {
		m.workspace.TabBar.FocusByID(repo.DirName)
		m.mode = viewWorkspace
		return
	}
	m.workspace.OpenTab(repo.DirName, repo.Short, sessionName, "main")
	m.mode = viewWorkspace
}

func (m *model) toggleRemote() tea.Cmd {
	item := m.selectedItem()
	if item == nil {
		return nil
	}

	repo := item.repo
	rcName := TmuxSessionName(repo.DirName, true)

	if TmuxHasSession(rcName) {
		// Remote is already running
	} else {
		// Start remote session
		if err := TmuxNewSessionWithCmd(rcName, repo.Path, "claude remote-control"); err != nil {
			m.err = err
			return nil
		}
		if item.status == statusNone {
			item.status = statusRemote
		}
	}

	m.rebuildDisplayOrder()
	return nil
}

// startConfiguredRemotes auto-starts remote sessions for repos with remote: true
func (m *model) startConfiguredRemotes() {
	for i := range m.items {
		item := &m.items[i]
		if !item.repo.Remote {
			continue
		}
		rcName := TmuxSessionName(item.repo.DirName, true)
		if TmuxHasSession(rcName) {
			continue // already running
		}
		if err := TmuxNewSessionWithCmd(rcName, item.repo.Path, "claude remote-control"); err != nil {
			continue
		}
		if item.status == statusNone {
			item.status = statusRemote
		}
	}
}

func (m *model) toggleRemoteFlag() {
	item := m.selectedItem()
	if item == nil || item.repo.IsScratch {
		return
	}

	item.repo.Remote = !item.repo.Remote

	// Update config and save
	ws := m.cfg.Workspaces[item.repo.DirName]
	ws.Remote = item.repo.Remote
	if ws.Name == "" {
		ws.Name = item.repo.Name
	}
	m.cfg.Workspaces[item.repo.DirName] = ws
	SaveConfig(m.cfgPath, m.cfg)

	// If just enabled, start the remote session
	if item.repo.Remote {
		rcName := TmuxSessionName(item.repo.DirName, true)
		if !TmuxHasSession(rcName) {
			TmuxNewSessionWithCmd(rcName, item.repo.Path, "claude remote-control")
			if item.status == statusNone {
				item.status = statusRemote
			}
		}
	}

	m.rebuildDisplayOrder()
}

func (m *model) toggleFavouriteFlag() {
	item := m.selectedItem()
	if item == nil || item.repo.IsScratch {
		return
	}

	item.repo.Favourite = !item.repo.Favourite

	ws := m.cfg.Workspaces[item.repo.DirName]
	ws.Favourite = item.repo.Favourite
	if ws.Name == "" {
		ws.Name = item.repo.Name
	}
	m.cfg.Workspaces[item.repo.DirName] = ws
	SaveConfig(m.cfgPath, m.cfg)
	m.rebuildDisplayOrder()
}

func (m *model) toggleYoloFlag() {
	item := m.selectedItem()
	if item == nil || item.repo.IsScratch {
		return
	}

	item.repo.Yolo = !item.repo.Yolo

	ws := m.cfg.Workspaces[item.repo.DirName]
	ws.Yolo = item.repo.Yolo
	if ws.Name == "" {
		ws.Name = item.repo.Name
	}
	m.cfg.Workspaces[item.repo.DirName] = ws
	SaveConfig(m.cfgPath, m.cfg)
}

func (m *model) focusSelectedTab() tea.Cmd {
	return nil
}

// clearFlash clears any flash state for the item.
func (m *model) clearFlash(item *repoItem) {
	if m.tabFlashing[item.repo.DirName] == "" {
		return
	}
	delete(m.tabFlashing, item.repo.DirName)
}

func (m *model) killSelected() tea.Cmd {
	item := m.selectedItem()
	if item == nil || item.status == statusNone {
		return nil
	}

	// Worktrees have their own kill logic (close split, keep worktree on disk)
	if item.repo.IsWorktree {
		return m.killWorktreeSession()
	}

	repo := item.repo

	interactiveName := TmuxSessionName(repo.DirName, false)
	if TmuxHasSession(interactiveName) {
		TmuxKillSession(interactiveName)
	}

	rcName := TmuxSessionName(repo.DirName, true)
	if TmuxHasSession(rcName) {
		TmuxKillSession(rcName)
	}

	item.status = statusNone
	item.tmuxSes = ""
	item.title = ""
	delete(m.alerts, repo.DirName)
	m.rebuildDisplayOrder()

	return nil
}

func (m *model) detachSelected() tea.Cmd {
	return nil
}

func (m *model) reconnectSessions() {
	sessions, err := TmuxListSessions()
	if err != nil || len(sessions) == 0 {
		return
	}

	// Prune zombie sessions (hive-* sessions whose cwd has been deleted,
	// e.g. worktree sessions left over after the worktree was removed).
	// Re-list after pruning so downstream mapping works against the
	// current set.
	if killed := PruneZombieSessions(sessions); len(killed) > 0 {
		fmt.Fprintf(os.Stderr, "hive: pruned %d zombie session(s): %s\n",
			len(killed), strings.Join(killed, ", "))
		sessions, err = TmuxListSessions()
		if err != nil || len(sessions) == 0 {
			return
		}
	}

	MapSessionsToItems(m.items, sessions)

	// Rebuild workspace tabs: parents first, then worktrees as splits
	for _, item := range m.items {
		if item.tmuxSes == "" || item.status == statusRemote || item.repo.IsWorktree {
			continue
		}
		m.workspace.OpenTab(item.repo.DirName, item.repo.Short, item.tmuxSes, "main")
	}
	for _, item := range m.items {
		if item.tmuxSes == "" || item.status == statusRemote || !item.repo.IsWorktree {
			continue
		}
		if item.repo.Parent == "" {
			continue
		}
		// Ensure parent tab exists — create it from the first worktree if parent has no session
		if _, exists := m.workspace.Tabs[item.repo.Parent]; !exists {
			// Find parent for label
			parentLabel := item.repo.Parent
			for _, p := range m.items {
				if p.repo.DirName == item.repo.Parent {
					parentLabel = p.repo.Short
					if p.tmuxSes != "" {
						// Parent has a session — use it as the primary split
						m.workspace.OpenTab(p.repo.DirName, p.repo.Short, p.tmuxSes, "main")
					}
					break
				}
			}
			// If parent tab still doesn't exist, create it with this worktree as primary
			if _, exists := m.workspace.Tabs[item.repo.Parent]; !exists {
				m.workspace.OpenTab(item.repo.Parent, parentLabel, item.tmuxSes, "wt:"+item.repo.WorktreeBranch)
				continue // this worktree is already the primary split
			}
		}
		m.workspace.TabBar.FocusByID(item.repo.Parent)
		m.workspace.AddSplitToActive("wt:"+item.repo.WorktreeBranch, item.tmuxSes)
	}

	// Auto-start configured remote sessions
	m.startConfiguredRemotes()
	m.rebuildDisplayOrder()
}
