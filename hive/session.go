package main

import (
	tea "charm.land/bubbletea/v2"
)

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

		hasInteractive := false
		hasRemote := false
		for _, s := range sess {
			if s.IsRemote {
				hasRemote = true
			} else {
				hasInteractive = true
				items[i].tmuxSes = s.Name
			}
		}

		if hasRemote {
			items[i].status = statusRemote
			if !hasInteractive {
				for _, s := range sess {
					if s.IsRemote {
						items[i].tmuxSes = s.Name
					}
				}
			}
		} else if hasInteractive {
			items[i].status = statusClaude
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
		// Telegram session takeover: send claude --resume into existing pane
		if withClaude && item.bridgeEntry != nil && item.bridgeEntry.SessionID != "" {
			TmuxSendKeys(sessionName, "C-c")
			TmuxSendKeys(sessionName, "claude --resume "+item.bridgeEntry.SessionID)
			item.status = statusClaude
			item.bridgeEntry = nil
			m.rebuildDisplayOrder()
			return nil
		}
		m.clearFlash(item)
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

	return nil
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

	MapSessionsToItems(m.items, sessions)

	// Auto-start configured remote sessions
	m.startConfiguredRemotes()
	m.rebuildDisplayOrder()
}
