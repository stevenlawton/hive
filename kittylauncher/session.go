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
		KittyFocusTab("title:^" + repo.Short)
		return nil
	}

	if err := TmuxNewSession(sessionName, repo.Path); err != nil {
		m.err = err
		return nil
	}

	if withClaude {
		TmuxSendKeys(sessionName, "claude")
		item.status = statusClaude
	} else {
		item.status = statusShell
	}
	item.tmuxSes = sessionName

	KittyLaunchTab(repo.Short, "tmux", "attach", "-t", sessionName)
	KittySetTabColor(repo.Short, repo.Color)
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
	rcTabTitle := "⟳ " + repo.Short

	if TmuxHasSession(rcName) {
		// Remote is running — focus or open a tab to view it
		tabs, _ := KittyListTabs()
		for _, tab := range tabs {
			if tab.Title == rcTabTitle {
				// Tab exists, just focus it
				KittyFocusTab("title:" + rcTabTitle)
				return nil
			}
		}
		// No tab open — attach to it
		KittyLaunchTab(rcTabTitle, "tmux", "attach", "-t", rcName)
		KittySetTabColor(rcTabTitle, repo.Color)
	} else {
		// Start remote session + open tab
		if err := TmuxNewSessionWithCmd(rcName, repo.Path, "claude remote-control"); err != nil {
			m.err = err
			return nil
		}
		KittyLaunchTab(rcTabTitle, "tmux", "attach", "-t", rcName)
		KittySetTabColor(rcTabTitle, repo.Color)
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

func (m *model) focusSelectedTab() tea.Cmd {
	item := m.selectedItem()
	if item == nil || item.status == statusNone {
		return nil
	}
	KittyFocusTab("title:^" + item.repo.Short)
	return nil
}

func (m *model) killSelected() tea.Cmd {
	item := m.selectedItem()
	if item == nil || item.status == statusNone {
		return nil
	}

	repo := item.repo

	interactiveName := TmuxSessionName(repo.DirName, false)
	if TmuxHasSession(interactiveName) {
		TmuxKillSession(interactiveName)
		KittyCloseTab("title:^" + repo.Short)
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
	item := m.selectedItem()
	if item == nil || item.status == statusNone {
		return nil
	}
	// Close both interactive and remote tabs (tmux sessions stay alive)
	KittyCloseTab("title:^" + item.repo.Short)
	KittyCloseTab("title:^⟳ " + item.repo.Short)
	return nil
}

func (m *model) reconnectSessions() {
	sessions, err := TmuxListSessions()
	if err != nil || len(sessions) == 0 {
		return
	}

	MapSessionsToItems(m.items, sessions)

	// Re-attach interactive sessions (not remote — those are background-only)
	tabs, err := KittyListTabs()
	if err != nil {
		return
	}
	tabTitles := make(map[string]bool)
	for _, tab := range tabs {
		tabTitles[tab.Title] = true
	}

	for i := range m.items {
		item := &m.items[i]
		interactiveName := TmuxSessionName(item.repo.DirName, false)
		if !TmuxHasSession(interactiveName) {
			continue
		}
		if tabTitles[item.repo.Short] {
			continue // tab already exists
		}
		KittyLaunchTab(item.repo.Short, "tmux", "attach", "-t", interactiveName)
		KittySetTabColor(item.repo.Short, item.repo.Color)
	}

	// Auto-start configured remote sessions
	m.startConfiguredRemotes()
	m.rebuildDisplayOrder()
}
