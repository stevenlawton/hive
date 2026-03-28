package main

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
)

var (
	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#ff8c00")).Padding(1, 0, 0, 1)
	subtitleStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#888888")).Padding(0, 0, 0, 1)
	cursorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#00ff88")).Bold(true)
	nameStyle     = lipgloss.NewStyle().Bold(true).Width(36)
	statusStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#888888")).Width(20)
	remoteStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#ff8c00"))
	scratchStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#555555")).Italic(true)
	deadStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#ff0000")).Bold(true)
	waitStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#ffff00"))
	helpStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#555555")).Padding(1, 0, 0, 1)
	barKeyStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#ff8c00"))
	barValStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))
	sectionStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#555555")).Bold(true).Padding(0, 0, 0, 1)
	dividerStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#333333"))
)

type viewItem struct {
	index int
	item  repoItem
}

func (m model) View() tea.View {
	var v tea.View
	switch m.mode {
	case viewHelp:
		v = tea.NewView(m.viewHelp())
	case viewEdit:
		v = tea.NewView(m.viewEdit())
	default:
		v = tea.NewView(m.viewList())
	}
	v.AltScreen = true
	return v
}

func (m model) viewList() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render("⚡ KittyLauncher"))
	b.WriteString("\n")

	if m.filtering {
		b.WriteString(subtitleStyle.Render(m.filter.View()))
		b.WriteString("\n")
	}

	if m.mode == viewPromote {
		b.WriteString(subtitleStyle.Render("Promote to ~/repos/"))
		b.WriteString(m.promote.View())
		b.WriteString("\n")
	}

	// Build display lines from displayOrder (already sorted: active > favourites > rest)
	type displayLine struct {
		text       string
		cursorPos  int // position in displayOrder, -1 for section headers
	}
	var lines []displayLine

	lastSection := ""
	for cursorPos, itemIdx := range m.displayOrder {
		item := m.items[itemIdx]

		// Determine section (must match rebuildDisplayOrder logic)
		// Children inherit parent's section
		var section string
		if item.repo.Parent != "" {
			// Find parent's section
			for _, pi := range m.displayOrder {
				p := m.items[pi]
				if p.repo.DirName == item.repo.Parent {
					hasParentTab := p.status == statusClaude || p.status == statusShell ||
						(p.status == statusRemote && TmuxHasSession(TmuxSessionName(p.repo.DirName, false)))
					switch {
					case hasParentTab:
						section = "active"
					case p.repo.Favourite:
						section = "favourites"
					default:
						section = "repos"
					}
					break
				}
			}
		}
		if section == "" {
			hasInteractiveTab := item.status == statusClaude || item.status == statusShell ||
				(item.status == statusRemote && TmuxHasSession(TmuxSessionName(item.repo.DirName, false)))
			switch {
			case hasInteractiveTab:
				section = "active"
			case item.repo.Favourite:
				section = "favourites"
			default:
				section = "repos"
			}
		}

		// Insert section header on change
		if section != lastSection {
			lines = append(lines, displayLine{
				text:      sectionStyle.Render("── " + section + " ──"),
				cursorPos: -1,
			})
			lastSection = section
		}

		vi := viewItem{index: cursorPos, item: item}
		lines = append(lines, displayLine{
			text:      m.renderItem(vi),
			cursorPos: cursorPos,
		})
	}

	if len(m.displayOrder) == 0 {
		lines = append(lines, displayLine{text: subtitleStyle.Render("  no matches"), cursorPos: -1})
	}

	// Calculate visible window: reserve lines for header (3) and footer (3)
	maxVisible := m.height - 6
	if maxVisible < 5 {
		maxVisible = 5
	}

	// Find which line the cursor is on
	cursorLine := 0
	for i, l := range lines {
		if l.cursorPos == m.cursor {
			cursorLine = i
			break
		}
	}

	// Scroll window around cursor
	start := 0
	if len(lines) > maxVisible {
		start = cursorLine - maxVisible/2
		if start < 0 {
			start = 0
		}
		if start+maxVisible > len(lines) {
			start = len(lines) - maxVisible
		}
	}
	end := start + maxVisible
	if end > len(lines) {
		end = len(lines)
	}

	// Render visible lines
	for _, l := range lines[start:end] {
		b.WriteString(l.text)
		b.WriteString("\n")
	}

	// Scroll indicator
	if len(lines) > maxVisible {
		pos := fmt.Sprintf(" %d/%d", m.cursor+1, len(m.displayOrder))
		b.WriteString(subtitleStyle.Render(pos))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(m.renderKeyBar())

	if m.err != nil {
		b.WriteString("\n")
		b.WriteString(deadStyle.Render(fmt.Sprintf("  error: %v", m.err)))
	}

	return b.String()
}

func (m model) viewHelp() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render("⚡ KittyLauncher — Help"))
	b.WriteString("\n\n")

	bindings := m.keys.FullHelp()
	for _, group := range bindings {
		for _, binding := range group {
			help := binding.Help()
			key := barKeyStyle.Render(fmt.Sprintf("  %-14s", help.Key))
			desc := barValStyle.Render(help.Desc)
			b.WriteString(key + desc + "\n")
		}
		b.WriteString("\n")
	}

	b.WriteString(helpStyle.Render("Press ? or esc to return"))

	return b.String()
}


var (
	editLabelStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#888888")).Width(14)
	editActiveStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#00ff88"))
	editBoxStyle    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#555555")).Padding(1, 2).Width(50)
)

func (m model) viewEdit() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render("⚡ Edit — " + m.editDirName))
	b.WriteString("\n\n")

	// Text fields
	for i, field := range m.editFields {
		marker := "  "
		if i == m.editFocus {
			marker = editActiveStyle.Render("> ")
		}
		b.WriteString(marker + field.View() + "\n")
	}

	b.WriteString("\n")

	// Toggle fields
	toggleLabels := []string{"Remote", "Favourite", "Collection"}
	for i, label := range toggleLabels {
		focusIdx := editToggleRemote + i
		marker := "  "
		if focusIdx == m.editFocus {
			marker = editActiveStyle.Render("> ")
		}

		checkbox := "[ ]"
		if m.editToggles[i] {
			checkbox = editActiveStyle.Render("[✓]")
		}

		b.WriteString(fmt.Sprintf("%s%s %s\n", marker, editLabelStyle.Render(label+":"), checkbox))
	}

	b.WriteString("\n")
	b.WriteString(dividerStyle.Render(strings.Repeat("─", 40)))
	b.WriteString("\n")

	hints := []struct{ key, val string }{
		{"Tab/↑↓", "navigate"},
		{"Space/Enter", "toggle"},
		{"Ctrl+S", "save"},
		{"Esc", "cancel"},
	}
	var parts []string
	for _, h := range hints {
		parts = append(parts, barKeyStyle.Render(h.key)+" "+barValStyle.Render(h.val))
	}
	b.WriteString(helpStyle.Render(strings.Join(parts, "  ")))

	return b.String()
}

func (m model) renderItem(vi viewItem) string {
	indent := ""
	if vi.item.repo.Parent != "" {
		indent = "  "
	}

	cursor := indent + "  "
	if vi.index == m.cursor {
		cursor = indent + cursorStyle.Render("> ")
	}

	name := vi.item.repo.Name
	if vi.item.repo.IsCollection {
		name = sectionStyle.Render("▸ " + name)
	} else if vi.item.repo.IsScratch {
		name = scratchStyle.Render(name)
	} else {
		name = nameStyle.Render(name)
	}

	status := m.renderStatus(vi.item)

	var badges []string
	if vi.item.repo.Remote {
		badges = append(badges, remoteStyle.Render("[remote]"))
	}
	if vi.item.repo.Favourite {
		badges = append(badges, barKeyStyle.Render("★"))
	}
	if alert, ok := m.alerts[vi.item.repo.DirName]; ok {
		badges = append(badges, waitStyle.Render(alert))
	}

	line := cursor + name + "  " + status
	if len(badges) > 0 {
		line += "  " + strings.Join(badges, " ")
	}

	return line
}

func (m model) renderStatus(item repoItem) string {
	switch item.status {
	case statusClaude:
		title := item.title
		if title == "" {
			title = "claude"
		}
		return statusStyle.Render("● " + title)
	case statusShell:
		return statusStyle.Render("● shell")
	case statusRemote:
		return remoteStyle.Render("● remote")
	case statusDead:
		return deadStyle.Render("✖ dead")
	case statusWaiting:
		return waitStyle.Render("◌ waiting…")
	default:
		return statusStyle.Render("○ idle")
	}
}

func (m model) renderKeyBar() string {
	pairs := []struct{ key, val string }{
		{"enter", "open"},
		{"s", "scratch"},
		{"/", "filter"},
		{"?", "help"},
		{"q", "quit"},
	}

	var parts []string
	for _, p := range pairs {
		parts = append(parts, barKeyStyle.Render(p.key)+" "+barValStyle.Render(p.val))
	}

	return dividerStyle.Render(strings.Repeat("─", 40)) + "\n" +
		helpStyle.Render(strings.Join(parts, "  "))
}
