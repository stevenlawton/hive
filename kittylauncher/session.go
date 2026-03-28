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
		dirName := items[i].repo.DirName
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
	if item == nil {
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
		TmuxKillSession(rcName)
		KittyCloseTab("title:^" + repo.Short + " ⟳")
		if TmuxHasSession(TmuxSessionName(repo.DirName, false)) {
			item.status = statusClaude
		} else {
			item.status = statusNone
		}
	} else {
		if err := TmuxNewSession(rcName, repo.Path); err != nil {
			m.err = err
			return nil
		}
		TmuxSendKeys(rcName, "claude remote-control")
		tabTitle := repo.Short + " ⟳"
		KittyLaunchTab(tabTitle, "tmux", "attach", "-t", rcName)
		KittySetTabColor(tabTitle, repo.Color)
		item.status = statusRemote
	}

	return nil
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
		KittyCloseTab("title:^" + repo.Short + " ⟳")
	}

	item.status = statusNone
	item.tmuxSes = ""
	item.title = ""
	delete(m.alerts, repo.DirName)

	return nil
}

func (m *model) detachSelected() tea.Cmd {
	item := m.selectedItem()
	if item == nil || item.status == statusNone {
		return nil
	}
	KittyCloseTab("title:^" + item.repo.Short)
	return nil
}

func (m *model) reconnectSessions() {
	sessions, err := TmuxListSessions()
	if err != nil || len(sessions) == 0 {
		return
	}

	MapSessionsToItems(m.items, sessions)

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
		if item.status == statusNone {
			continue
		}

		if tabTitles[item.repo.Short] || tabTitles[item.repo.Short+" ⟳"] {
			continue
		}

		if item.tmuxSes != "" {
			tabTitle := item.repo.Short
			if item.status == statusRemote && !TmuxHasSession(TmuxSessionName(item.repo.DirName, false)) {
				tabTitle = item.repo.Short + " ⟳"
			}
			KittyLaunchTab(tabTitle, "tmux", "attach", "-t", item.tmuxSes)
			KittySetTabColor(tabTitle, item.repo.Color)
		}
	}
}
