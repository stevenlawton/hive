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
	nameStyle     = lipgloss.NewStyle().Bold(true).Width(24)
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
	if m.mode == viewHelp {
		v = tea.NewView(m.viewHelp())
	} else {
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

	active, favourites, rest := m.groupItems()

	if len(active) > 0 {
		b.WriteString(sectionStyle.Render("── active ──"))
		b.WriteString("\n")
		for _, vi := range active {
			b.WriteString(m.renderItem(vi))
			b.WriteString("\n")
		}
	}

	if len(favourites) > 0 {
		b.WriteString(sectionStyle.Render("── favourites ──"))
		b.WriteString("\n")
		for _, vi := range favourites {
			b.WriteString(m.renderItem(vi))
			b.WriteString("\n")
		}
	}

	if len(rest) > 0 {
		b.WriteString(sectionStyle.Render("── repos ──"))
		b.WriteString("\n")
		for _, vi := range rest {
			b.WriteString(m.renderItem(vi))
			b.WriteString("\n")
		}
	}

	if len(m.filtered) == 0 {
		b.WriteString(subtitleStyle.Render("  no matches"))
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

func (m model) groupItems() (active, favourites, rest []viewItem) {
	cursorIdx := 0
	if m.cursor >= 0 && m.cursor < len(m.filtered) {
		cursorIdx = m.cursor
	}

	for listPos, itemIdx := range m.filtered {
		item := m.items[itemIdx]
		vi := viewItem{index: listPos, item: item}

		switch {
		case item.status != statusNone:
			active = append(active, vi)
		case item.repo.Favourite:
			favourites = append(favourites, vi)
		default:
			rest = append(rest, vi)
		}
	}

	// Suppress unused variable
	_ = cursorIdx

	return active, favourites, rest
}

func (m model) renderItem(vi viewItem) string {
	cursor := "  "
	if vi.index == m.cursor {
		cursor = cursorStyle.Render("> ")
	}

	name := vi.item.repo.Name
	if vi.item.repo.IsScratch {
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
