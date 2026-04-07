package main

import (
	"fmt"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
)

var (
	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#ff8c00")).Padding(1, 0, 0, 1)
	subtitleStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#888888")).Padding(0, 0, 0, 1)
	cursorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#00ff88")).Bold(true)
	nameStyle     = lipgloss.NewStyle().Bold(true).Width(36)
	claudeStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#00ff88")).Width(50)
	shellStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#00bbff")).Width(50)
	idleStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#555555")).Width(50)
	remoteStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#ff8c00"))
	scratchStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#555555")).Italic(true)
	deadStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#ff0000")).Bold(true)
	waitStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#ffff00"))
	telegramStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#0088cc")).Width(50)
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
	var layout listLayout
	switch m.mode {
	case viewHelp:
		v = tea.NewView(m.viewHelp())
	case viewEdit:
		v = tea.NewView(m.viewEdit())
	case viewWorkspace:
		m.workspace.SetSize(m.width, m.height)
		statusBar := m.renderWorkspaceStatusBar()
		v = tea.NewView(m.workspace.View(statusBar))
	case viewAttach:
		v = tea.NewView("") // TUI hidden during attach
	default:
		// Manager view — two-pane layout
		listContent, l := m.viewList()
		layout = l
		m.manager.SetSize(m.width, m.height)
		statusBar := m.renderStatusBar() + "\n" + m.renderKeyBarString()
		v = tea.NewView(m.manager.View(listContent, statusBar))
	}
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion

	v.OnMouse = func(msg tea.MouseMsg) tea.Cmd {
		mouse := msg.Mouse()
		switch msg.(type) {
		case tea.MouseClickMsg:
			// Click on list item → select it
			for idx, y := range layout.itemY {
				if mouse.Y == y {
					clickIdx := idx
					return func() tea.Msg { return itemClickMsg{index: clickIdx} }
				}
			}
			// Click on key bar → trigger action
			if mouse.Y >= layout.keyBarY {
				row := mouse.Y - layout.keyBarY
				for _, btn := range layout.keyButtons {
					if btn.row == row && mouse.X >= btn.xStart && mouse.X < btn.xEnd {
						action := btn.action
						return func() tea.Msg { return keyBarClickMsg{action: action} }
					}
				}
			}
		case tea.MouseWheelMsg:
			dir := 1
			if mouse.Button == tea.MouseWheelUp {
				dir = -1
			}
			return func() tea.Msg { return scrollMsg{dir: dir} }
		}
		return nil
	}

	return v
}

type listLayout struct {
	itemY      map[int]int    // displayOrder index → screen Y
	keyBarY    int
	keyButtons []keyBarButton
}

func (m model) viewList() (string, listLayout) {
	layout := listLayout{itemY: make(map[int]int)}

	w := m.width
	if w < 40 {
		w = 40
	}

	// Build box inner content
	var inner strings.Builder

	// Title inside box
	inner.WriteString(titleStyle.UnsetPadding().Render("⚡ Hive"))
	inner.WriteString("\n")

	if m.filtering {
		inner.WriteString(subtitleStyle.UnsetPadding().Render(m.filter.View()))
		inner.WriteString("\n")
	}

	if m.mode == viewPromote {
		inner.WriteString(subtitleStyle.UnsetPadding().Render("Promote to ~/repos/"))
		inner.WriteString(m.promote.View())
		inner.WriteString("\n")
	}

	if m.mode == viewConfirm {
		inner.WriteString(waitStyle.Render("  " + m.confirmMsg))
		inner.WriteString("\n")
	}

	if m.mode == viewWorktree {
		inner.WriteString(subtitleStyle.UnsetPadding().Render("  New worktree"))
		inner.WriteString("\n")
		for i, f := range m.wtFields {
			prefix := "  "
			if i == m.wtFocus {
				prefix = "> "
			}
			inner.WriteString(prefix + f.View() + "\n")
		}
		// Yolo toggle
		check := "[ ]"
		if m.wtYolo {
			check = "[x]"
		}
		prefix := "  "
		if m.wtFocus == wtFieldCount {
			prefix = "> "
		}
		inner.WriteString(prefix + check + " Yolo\n")
		inner.WriteString(subtitleStyle.UnsetPadding().Render("  ctrl+s to create, esc to cancel"))
		inner.WriteString("\n")
	}

	// Build display lines from displayOrder (already sorted: active > favourites > rest)
	type displayLine struct {
		text      string
		cursorPos int // position in displayOrder, -1 for section headers
	}
	var lines []displayLine

	lastSection := ""
	for cursorPos, itemIdx := range m.displayOrder {
		section := m.itemSection[itemIdx]

		if section != lastSection {
			lines = append(lines, displayLine{
				text:      sectionStyle.UnsetPadding().Render("── " + section + " ──"),
				cursorPos: -1,
			})
			lastSection = section
		}

		vi := viewItem{index: cursorPos, item: m.items[itemIdx]}
		lines = append(lines, displayLine{
			text:      m.renderItem(vi),
			cursorPos: cursorPos,
		})
	}

	if len(m.displayOrder) == 0 {
		lines = append(lines, displayLine{text: subtitleStyle.UnsetPadding().Render("  no matches"), cursorPos: -1})
	}

	// Calculate visible window
	// Reserved: border top/bottom (2) + title line (1) + status bar (1) + key bar (2)
	reserved := 6
	if m.filtering {
		reserved++
	}
	if m.mode == viewPromote {
		reserved++
	}
	if len(lines) > (m.height - reserved) {
		reserved++ // scroll indicator
	}
	maxVisible := m.height - reserved
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

	// Track Y positions for mouse support
	// Y=0 is the border top, Y=1 is the title, then filter/promote, then items
	yOffset := 1 // border top line
	yOffset++     // title line
	if m.filtering {
		yOffset++
	}
	if m.mode == viewPromote {
		yOffset++
	}

	// Render visible lines
	for i, l := range lines[start:end] {
		inner.WriteString(l.text)
		inner.WriteString("\n")
		if l.cursorPos >= 0 {
			layout.itemY[l.cursorPos] = yOffset + i
		}
	}

	// Scroll indicator
	if len(lines) > maxVisible {
		pos := fmt.Sprintf(" %d/%d", m.cursor+1, len(m.displayOrder))
		inner.WriteString(subtitleStyle.UnsetPadding().Render(pos))
		inner.WriteString("\n")
	}

	// Wrap in bordered box
	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#555555")).
		Width(w - 2).
		Padding(0, 1)

	var b strings.Builder
	boxRendered := boxStyle.Render(inner.String())
	b.WriteString(boxRendered)
	b.WriteString("\n")

	// Status info bar
	b.WriteString(m.renderStatusBar())
	b.WriteString("\n")

	// Key bar
	boxHeight := strings.Count(boxRendered, "\n") + 1
	layout.keyBarY = boxHeight + 1 // +1 for status bar line

	keyBarText, buttons := m.renderKeyBar()
	layout.keyButtons = buttons
	b.WriteString(keyBarText)

	if m.err != nil {
		b.WriteString("\n")
		b.WriteString(deadStyle.Render(fmt.Sprintf("  error: %v", m.err)))
	}

	return b.String(), layout
}

func (m model) viewHelp() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render("⚡ Hive — Help"))
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
	toggleLabels := []string{"Yolo", "Remote", "Favourite", "Collection"}
	for i, label := range toggleLabels {
		focusIdx := editToggleYolo + i
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
	if vi.item.repo.Parent != "" && m.itemSection[m.displayOrder[vi.index]] != "active" {
		indent = "  "
	}

	cursor := indent + "  "
	if vi.index == m.cursor {
		cursor = indent + cursorStyle.Render("> ")
	}

	name := vi.item.repo.Name
	if vi.item.repo.IsArchived {
		name = scratchStyle.Width(36).Render(name) // dim, like scratch
	} else if vi.item.repo.IsCollection {
		name = sectionStyle.UnsetPadding().Width(36).Render("▸ " + name)
	} else if vi.item.repo.IsScratch {
		name = scratchStyle.Width(36).Render(name)
	} else {
		name = nameStyle.Render(name)
	}

	// Build flags as inline icons
	var flags []string
	if vi.item.repo.Favourite {
		flags = append(flags, barKeyStyle.Render("★"))
	}
	if vi.item.repo.Remote {
		flags = append(flags, remoteStyle.Render("📡"))
	}
	if vi.item.repo.Yolo {
		flags = append(flags, waitStyle.Render("⚡"))
	}
	if alert, ok := m.alerts[vi.item.repo.DirName]; ok {
		flags = append(flags, waitStyle.Render(alert))
	}

	flagStr := ""
	if len(flags) > 0 {
		flagStr = strings.Join(flags, " ") + " "
	}

	status := m.renderStatus(vi.item)

	desc := ""
	if vi.item.repo.Description != "" {
		desc = "  " + idleStyle.UnsetWidth().Render(vi.item.repo.Description)
	}

	return cursor + name + "  " + flagStr + status + desc
}

func (m model) renderStatusBar() string {
	var active, idle, remote, telegram int
	for _, idx := range m.displayOrder {
		item := m.items[idx]
		switch item.status {
		case statusClaude, statusShell:
			active++
		case statusRemote:
			remote++
		case statusTelegram:
			telegram++
		default:
			idle++
		}
	}

	left := claudeStyle.UnsetWidth().Render(fmt.Sprintf("%d active", active)) + "  " +
		idleStyle.UnsetWidth().Render(fmt.Sprintf("%d idle", idle))
	if remote > 0 {
		left += "  " + remoteStyle.Render(fmt.Sprintf("%d remote", remote))
	}
	if telegram > 0 {
		left += "  " + telegramStyle.UnsetWidth().Render(fmt.Sprintf("%d telegram", telegram))
	}

	right := ""
	if item := m.selectedItem(); item != nil {
		home, _ := os.UserHomeDir()
		path := item.repo.Path
		if home != "" {
			path = strings.Replace(path, home, "~", 1)
		}
		right = subtitleStyle.UnsetPadding().Render(path)
	}

	w := m.width
	if w < 40 {
		w = 40
	}
	gap := w - lipgloss.Width(left) - lipgloss.Width(right) - 1
	if gap < 2 {
		gap = 2
	}
	return " " + left + strings.Repeat(" ", gap) + right
}

var completedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#888888")).Width(50)

func (m model) renderStatus(item repoItem) string {
	// Check for rich status from plugin
	if rs := item.richStatus; rs != nil {
		switch rs.Status {
		case "completed":
			label := "✔ done"
			if rs.ToolCount > 0 {
				label = fmt.Sprintf("✔ done (%d tools)", rs.ToolCount)
			}
			return completedStyle.Render(label)
		case "ended":
			return idleStyle.Render("○ ended")
		}
	}

	switch item.status {
	case statusClaude:
		title := item.title
		if title == "" {
			title = "claude"
		}
		// Append tool count from rich status if available
		if rs := item.richStatus; rs != nil && rs.ToolCount > 0 {
			title = fmt.Sprintf("%s [%d]", title, rs.ToolCount)
		}
		return claudeStyle.Render("● " + title)
	case statusShell:
		return shellStyle.Render("● shell")
	case statusRemote:
		return remoteStyle.Width(50).Render("● remote")
	case statusDead:
		return deadStyle.Width(50).Render("✖ dead")
	case statusWaiting:
		return waitStyle.Width(50).Render("◌ waiting…")
	case statusNone:
		return "" // blank for idle — less noise
	case statusTelegram:
		label := "telegram"
		if item.bridgeEntry != nil && item.bridgeEntry.SessionID != "" {
			sid := item.bridgeEntry.SessionID
			if len(sid) > 8 {
				sid = sid[:8]
			}
			label = "tg:" + sid
		}
		return telegramStyle.Render("📱 " + label)
	default:
		return ""
	}
}

type keyBarButton struct {
	xStart, xEnd int
	row          int
	action       string // key name to simulate
}

func (m model) renderKeyBarString() string {
	s, _ := m.renderKeyBar()
	return s
}

func (m model) renderKeyBar() (string, []keyBarButton) {
	var buttons []keyBarButton
	row1 := []struct{ key, val, action string }{
		{"enter", "open", "enter"}, {"⇧↵", "shell", "shift+enter"}, {"r", "remote", "r"},
		{"s", "scratch", "s"}, {"w", "worktree", "w"}, {"tab", "focus", "tab"}, {"/", "filter", "/"},
	}
	row2 := []struct{ key, val, action string }{
		{"x", "kill", "x"}, {"d", "detach", "d"}, {"E", "edit", "E"},
		{"F", "fav", "F"}, {"A", "archive", "A"}, {"?", "help", "?"}, {"q", "quit", "q"},
	}

	renderRow := func(pairs []struct{ key, val, action string }, rowIdx int) string {
		var parts []string
		x := 1 // account for left padding
		for _, p := range pairs {
			btn := barKeyStyle.Render(p.key) + " " + barValStyle.Render(p.val)
			btnWidth := lipgloss.Width(btn)
			buttons = append(buttons, keyBarButton{
				xStart: x, xEnd: x + btnWidth, row: rowIdx, action: p.action,
			})
			parts = append(parts, btn)
			x += btnWidth + 2 // 2 for gap between buttons
		}
		return strings.Join(parts, "  ")
	}

	line1 := renderRow(row1, 0)
	line2 := renderRow(row2, 1)

	return " " + line1 + "\n " + line2, buttons
}
