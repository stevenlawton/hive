package main

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/bubbles/v2/textinput"
)

const (
	editFieldName   = 0
	editFieldShort  = 1
	editFieldColor  = 2
	editFieldDesc   = 3
	editToggleYolo       = 4
	editToggleRemote     = 5
	editToggleFavourite  = 6
	editToggleCollection = 7
	editFieldCount = 8
)

func (m *model) openEditPanel() tea.Cmd {
	item := m.selectedItem()
	if item == nil {
		return nil
	}

	repo := item.repo

	// Create text input fields
	fields := make([]textinput.Model, 4)

	nameInput := textinput.New()
	nameInput.Prompt = "Name:       "
	nameInput.SetValue(repo.Name)
	fields[editFieldName] = nameInput

	shortInput := textinput.New()
	shortInput.Prompt = "Short:      "
	shortInput.SetValue(repo.Short)
	fields[editFieldShort] = shortInput

	colorInput := textinput.New()
	colorInput.Prompt = "Color:      "
	colorInput.SetValue(repo.Color)
	colorInput.Placeholder = "#hex"
	fields[editFieldColor] = colorInput

	descInput := textinput.New()
	descInput.Prompt = "Desc:       "
	descInput.SetValue(repo.Description)
	descInput.Placeholder = "short description"
	fields[editFieldDesc] = descInput

	m.editFields = fields
	m.editToggles = []bool{repo.Yolo, repo.Remote, repo.Favourite, repo.IsCollection}
	m.editFocus = editFieldName
	m.editDirName = repo.DirName
	m.mode = viewEdit

	return m.editFields[0].Focus()
}

func (m *model) saveEditPanel() {
	if m.editDirName == "" {
		return
	}

	// Find the item
	for i := range m.items {
		if m.items[i].repo.DirName != m.editDirName {
			continue
		}

		item := &m.items[i]

		name := m.editFields[editFieldName].Value()
		short := m.editFields[editFieldShort].Value()
		color := m.editFields[editFieldColor].Value()
		desc := m.editFields[editFieldDesc].Value()

		if name != "" {
			item.repo.Name = name
		}
		if short != "" {
			item.repo.Short = short
		}
		item.repo.Color = color
		item.repo.Description = desc
		item.repo.Yolo = m.editToggles[0]
		item.repo.Remote = m.editToggles[1]
		item.repo.Favourite = m.editToggles[2]
		item.repo.IsCollection = m.editToggles[3]

		// Save to config
		ws := m.cfg.Workspaces[m.editDirName]
		ws.Name = item.repo.Name
		ws.Short = item.repo.Short
		ws.Color = item.repo.Color
		ws.Description = item.repo.Description
		ws.Yolo = item.repo.Yolo
		ws.Remote = item.repo.Remote
		ws.Favourite = item.repo.Favourite
		ws.Collection = item.repo.IsCollection
		m.cfg.Workspaces[m.editDirName] = ws
		SaveConfig(m.cfgPath, m.cfg)

		// If remote was just enabled, start it
		if item.repo.Remote {
			rcName := TmuxSessionName(item.repo.DirName, true)
			if !TmuxHasSession(rcName) {
				TmuxNewSessionWithCmd(rcName, item.repo.Path, "claude remote-control")
				if item.status == statusNone {
					item.status = statusRemote
				}
			}
		}

		break
	}

	// Rescan repos to pick up structural changes (collection toggle, etc.)
	m.reloadItems()
}

func (m model) handleEditKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	switch key {
	case "escape":
		m.mode = viewManager
		return m, nil

	case "enter":
		// If on a toggle, toggle it
		if m.editFocus >= editToggleYolo {
			toggleIdx := m.editFocus - editToggleYolo
			m.editToggles[toggleIdx] = !m.editToggles[toggleIdx]
			return m, nil
		}
		// On a text field, move to next field
		m.editFocus++
		if m.editFocus >= editFieldCount {
			// Save and close
			m.saveEditPanel()
			m.mode = viewManager
			return m, nil
		}
		return m, m.focusEditField()

	case "tab", "down":
		m.editFocus++
		if m.editFocus >= editFieldCount {
			m.editFocus = 0
		}
		return m, m.focusEditField()

	case "shift+tab", "up":
		m.editFocus--
		if m.editFocus < 0 {
			m.editFocus = editFieldCount - 1
		}
		return m, m.focusEditField()

	case " ":
		// Space toggles on toggle fields
		if m.editFocus >= editToggleYolo {
			toggleIdx := m.editFocus - editToggleYolo
			m.editToggles[toggleIdx] = !m.editToggles[toggleIdx]
			return m, nil
		}

	case "ctrl+s":
		m.saveEditPanel()
		m.mode = viewManager
		return m, nil
	}

	// Pass to text input if focused on one
	if m.editFocus < len(m.editFields) {
		var cmd tea.Cmd
		m.editFields[m.editFocus], cmd = m.editFields[m.editFocus].Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m *model) focusEditField() tea.Cmd {
	// Blur all text fields
	for i := range m.editFields {
		m.editFields[i].Blur()
	}
	// Focus the current one if it's a text field
	if m.editFocus < len(m.editFields) {
		return m.editFields[m.editFocus].Focus()
	}
	return nil
}
