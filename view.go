package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/stevenlawton/hive/bus"
	"github.com/stevenlawton/hive/ui"

	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
)

var (
	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#ff8c00")).Padding(1, 0, 0, 1)
	subtitleStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#888888")).Padding(0, 0, 0, 1)
	cursorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#ff8c00")).Bold(true)
	nameStyle     = lipgloss.NewStyle().Bold(true).Width(36)
	claudeStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#ff8c00")).Width(50)
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
	case viewConfirm:
		v = tea.NewView(m.viewConfirmModal())
	case viewWorkspace:
		m.workspace.SetSize(m.width, m.height)
		statusBar := m.renderWorkspaceStatusBar()
		v = tea.NewView(m.workspace.View(statusBar))
	case viewBus:
		m.workspace.TabBar.Width = m.width
		tabBar := m.workspace.TabBar.View()
		v = tea.NewView(tabBar + "\n" + m.viewBus())
	case viewWorktree:
		if m.wtSplitMode {
			v = tea.NewView(m.viewWorktreeSplit())
		} else {
			listContent, l := m.viewList()
			layout = l
			m.manager.SetSize(m.width, m.height)
			statusBar := m.renderStatusBar() + "\n" + m.renderKeyBarString()
			v = tea.NewView(m.manager.View(listContent, statusBar))
		}
	case viewAttach:
		v = tea.NewView("") // TUI hidden during attach
	default:
		// Manager view — two-pane layout with tab bar always visible
		m.workspace.TabBar.Width = m.width
		tabBar := m.workspace.TabBar.View()
		managerHeight := m.height - 1 // reserve one line for the tab bar
		listContent, l := m.viewList()
		layout = l
		m.manager.SetSize(m.width, managerHeight)
		statusBar := m.renderStatusBar() + "\n" + m.renderKeyBarString()
		managerContent := m.manager.View(listContent, statusBar)
		v = tea.NewView(tabBar + "\n" + managerContent)
		// Shift layout Y positions down for tab bar
		for k, y := range layout.itemY {
			layout.itemY[k] = y + 1
		}
		layout.keyBarY++
	}
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion

	v.OnMouse = func(msg tea.MouseMsg) tea.Cmd {
		mouse := msg.Mouse()
		switch msg.(type) {
		case tea.MouseClickMsg:
			if m.mode == viewWorkspace {
				// Click on tab bar (row 0) → switch tab
				if mouse.Y == 0 {
					widths := m.workspace.TabBar.TabWidths()
					x := 0
					for i, w := range widths {
						if mouse.X >= x && mouse.X < x+w {
							idx := i
							return func() tea.Msg { return tabClickMsg{index: idx} }
						}
						x += w
					}
				}
				// Click on split pane → focus it
				if tab := m.workspace.ActiveTab(); tab != nil {
					x := mouse.X
					for i, split := range tab.SplitPane.Splits {
						if x < split.Terminal.Width {
							idx := i
							return func() tea.Msg { return splitClickMsg{index: idx} }
						}
						x -= split.Terminal.Width
					}
				}
			} else {
				// Click on tab bar (row 0) → switch tab
				if mouse.Y == 0 {
					widths := m.workspace.TabBar.TabWidths()
					x := 0
					for i, w := range widths {
						if mouse.X >= x && mouse.X < x+w {
							idx := i
							return func() tea.Msg { return tabClickMsg{index: idx} }
						}
						x += w
					}
				}
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

	w := m.manager.ListWidth
	if w < 28 {
		w = 28
	}

	var content strings.Builder

	if m.filtering {
		content.WriteString(subtitleStyle.UnsetPadding().Render(" " + m.filter.View()))
		content.WriteString("\n")
	}

	if m.mode == viewPromote {
		content.WriteString(subtitleStyle.UnsetPadding().Render(" Promote to ~/repos/"))
		content.WriteString(m.promote.View())
		content.WriteString("\n")
	}

	if m.mode == viewWorktree {
		content.WriteString(subtitleStyle.UnsetPadding().Render("  New worktree"))
		content.WriteString("\n")
		for i, f := range m.wtFields {
			prefix := "  "
			if i == m.wtFocus {
				prefix = "> "
			}
			content.WriteString(prefix + f.View() + "\n")
		}
		check := "[ ]"
		if m.wtYolo {
			check = "[x]"
		}
		prefix := "  "
		if m.wtFocus == wtFieldCount {
			prefix = "> "
		}
		content.WriteString(prefix + check + " Yolo\n")
		content.WriteString(subtitleStyle.UnsetPadding().Render("  ctrl+s to create, esc to cancel"))
		content.WriteString("\n")
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
				text:      sectionStyle.UnsetPadding().Render(" ── " + section + " ──"),
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
	// Reserved: status bar (1) + key bar (2)
	reserved := 3
	if m.filtering {
		reserved++
	}
	if m.mode == viewPromote {
		reserved++
	}
	if len(lines) > (m.height - reserved - 1) {
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
	yOffset := 0
	if m.filtering {
		yOffset++
	}
	if m.mode == viewPromote {
		yOffset++
	}

	// Render visible lines, clamping to column width
	for i, l := range lines[start:end] {
		content.WriteString(ui.ClampToWidth(l.text, w))
		content.WriteString("\n")
		if l.cursorPos >= 0 {
			layout.itemY[l.cursorPos] = yOffset + i
		}
	}

	// Scroll indicator
	if len(lines) > maxVisible {
		pos := fmt.Sprintf(" %d/%d", m.cursor+1, len(m.displayOrder))
		content.WriteString(subtitleStyle.UnsetPadding().Render(pos))
	}

	// Track key bar Y for mouse support
	totalLines := strings.Count(content.String(), "\n") + 1
	layout.keyBarY = totalLines + 1 // +1 for status bar line

	_, buttons := m.renderKeyBar()
	layout.keyButtons = buttons

	if m.err != nil {
		content.WriteString("\n")
		content.WriteString(deadStyle.Render(fmt.Sprintf("  error: %v", m.err)))
	}

	return content.String(), layout
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
	editActiveStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#ff8c00"))
	editBoxStyle    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#555555")).Padding(1, 2).Width(50)
)

func (m model) viewConfirmModal() string {
	var form strings.Builder

	form.WriteString(titleStyle.UnsetPadding().Render("⚡ Confirm"))
	form.WriteString("\n\n")
	form.WriteString(waitStyle.Render("  " + m.confirmMsg))
	form.WriteString("\n\n")
	form.WriteString(helpStyle.UnsetPadding().Render("y confirm  n cancel"))

	box := editBoxStyle.Render(form.String())

	// Center vertically and horizontally
	boxLines := strings.Split(box, "\n")
	padTop := (m.height - len(boxLines)) / 2
	padLeft := (m.width - lipgloss.Width(boxLines[0])) / 2
	if padTop < 0 {
		padTop = 0
	}
	if padLeft < 0 {
		padLeft = 0
	}

	var out strings.Builder
	for range padTop {
		out.WriteString("\n")
	}
	leftPad := strings.Repeat(" ", padLeft)
	for _, line := range boxLines {
		out.WriteString(leftPad + line + "\n")
	}

	return out.String()
}

func (m model) viewWorktreeSplit() string {
	var form strings.Builder

	form.WriteString(titleStyle.UnsetPadding().Render("⚡ New Worktree Split"))
	form.WriteString("\n\n")

	for i, f := range m.wtFields {
		marker := "  "
		if i == m.wtFocus {
			marker = editActiveStyle.Render("> ")
		}
		form.WriteString(marker + f.View() + "\n")
	}

	check := "[ ]"
	if m.wtYolo {
		check = editActiveStyle.Render("[✓]")
	}
	prefix := "  "
	if m.wtFocus == wtFieldCount {
		prefix = editActiveStyle.Render("> ")
	}
	form.WriteString("\n" + prefix + check + " Yolo\n")
	form.WriteString("\n" + helpStyle.UnsetPadding().Render("enter create  tab next  esc cancel"))

	box := editBoxStyle.Render(form.String())

	// Center vertically and horizontally
	boxLines := strings.Split(box, "\n")
	padTop := (m.height - len(boxLines)) / 2
	padLeft := (m.width - lipgloss.Width(boxLines[0])) / 2
	if padTop < 0 {
		padTop = 0
	}
	if padLeft < 0 {
		padLeft = 0
	}

	var out strings.Builder
	for range padTop {
		out.WriteString("\n")
	}
	leftPad := strings.Repeat(" ", padLeft)
	for _, line := range boxLines {
		out.WriteString(leftPad + line + "\n")
	}

	return out.String()
}

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

	diffStats := ""
	if vi.item.diffStats != "" {
		diffStats = " " + idleStyle.UnsetWidth().Render(vi.item.diffStats)
	}

	desc := ""
	if vi.item.repo.Description != "" {
		desc = "  " + idleStyle.UnsetWidth().Render(vi.item.repo.Description)
	}

	return cursor + name + "  " + flagStr + status + diffStats + desc
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

var (
	busFromStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("#ff8c00")).Bold(true)
	busTimeStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("#555555"))
	busQuestionStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#ffff00"))
	busWaitingStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("#ffaa00"))
	busDoneStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("#00ff88"))
	busIntentStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("#00bbff"))
	busReplyStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("#888888")).Italic(true)
	busBodyStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("#bbbbbb"))
	busIDStyle           = lipgloss.NewStyle().Foreground(lipgloss.Color("#444444"))
	busTailStatusStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#00ff88")).Bold(true).Padding(0, 0, 0, 2)
	busFrozenStatusStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#ffaa00")).Bold(true).Padding(0, 0, 0, 2)
)

// wordWrap breaks text into lines of at most `width` visible characters,
// splitting at word boundaries when possible. Returns a slice of lines.
// `prefix` is prepended to every line (and counted against the width).
func wordWrap(text, prefix string, width int) []string {
	usable := width - lipgloss.Width(prefix)
	if usable < 10 {
		usable = 10
	}

	var result []string
	for _, paragraph := range strings.Split(text, "\n") {
		words := strings.Fields(paragraph)
		if len(words) == 0 {
			result = append(result, prefix)
			continue
		}
		line := words[0]
		for _, word := range words[1:] {
			if len(line)+1+len(word) > usable {
				result = append(result, prefix+line)
				line = word
			} else {
				line += " " + word
			}
		}
		result = append(result, prefix+line)
	}
	return result
}

// renderBusLines builds the full flat slice of display lines for every
// message on the bus. Each message contributes a header line (possibly
// wrapped) plus optional body/touches/reply-marker lines. Long headlines
// and bodies are word-wrapped to the terminal width.
func (m model) renderBusLines() []string {
	var lines []string
	msgs := m.bus.Tail(500)
	if len(msgs) == 0 {
		lines = append(lines, "  no messages yet — be the first")
		return lines
	}
	w := m.width
	if w < 40 {
		w = 40
	}

	for _, msg := range msgs {
		icon := msg.Icon()
		switch msg.KindOrDefault() {
		case bus.KindQuestion:
			icon = busQuestionStyle.Render(icon)
		case bus.KindWaiting:
			icon = busWaitingStyle.Render(icon)
		case bus.KindDone:
			icon = busDoneStyle.Render(icon)
		case bus.KindIntent:
			icon = busIntentStyle.Render(icon)
		}
		if msg.ReplyTo != "" {
			icon = busReplyStyle.Render("💬")
		}
		ts := busTimeStyle.Render(msg.At.Format("15:04"))
		from := busFromStyle.Render(msg.From)
		idShort := msg.ID
		if len(idShort) > 12 {
			idShort = idShort[:12]
		}
		id := busIDStyle.Render(idShort)

		// Header: time + icon + sender + headline + id
		// Wrap the headline if the full header exceeds terminal width.
		headerPrefix := fmt.Sprintf("  %s  %s  %s  ", ts, icon, from)
		headerSuffix := "  " + id
		headlineWidth := w - lipgloss.Width(headerPrefix) - lipgloss.Width(headerSuffix)
		if headlineWidth < 20 {
			headlineWidth = 20
		}

		headlineLines := wordWrap(msg.Headline, "", headlineWidth)
		for i, hl := range headlineLines {
			if i == 0 {
				lines = append(lines, headerPrefix+hl+headerSuffix)
			} else {
				// Continuation lines indented to align with the headline start
				indent := strings.Repeat(" ", lipgloss.Width(headerPrefix))
				lines = append(lines, indent+hl)
			}
		}

		if msg.ReplyTo != "" {
			lines = append(lines, busReplyStyle.Render("        ↳ re: "+msg.ReplyTo))
		}
		if msg.Body != "" {
			for _, wrapped := range wordWrap(msg.Body, "        ", w) {
				lines = append(lines, busBodyStyle.Render(wrapped))
			}
		}
		if len(msg.Touches) > 0 {
			for _, wrapped := range wordWrap("touches: "+strings.Join(msg.Touches, ", "), "        ", w) {
				lines = append(lines, busBodyStyle.Render(wrapped))
			}
		}
	}
	return lines
}

// busViewport computes the visible slice of bus lines based on the
// current scroll state (m.busViewTop) and available height.
func (m *model) busViewport(lines []string, height int) (visible []string, status string) {
	if height < 1 {
		height = 1
	}
	total := len(lines)

	// Remember the total so keyboard handlers can clamp correctly.
	m.busLineCount = total

	tail := m.busViewTop < 0 || m.busViewTop >= total-height

	var start int
	if tail {
		start = max(0, total-height)
		m.busViewTop = -1
	} else {
		start = m.busViewTop
		if start < 0 {
			start = 0
		}
		if start > total-height {
			start = max(0, total-height)
		}
	}
	end := start + height
	if end > total {
		end = total
	}
	visible = lines[start:end]

	if tail {
		status = busTailStatusStyle.Render("● LIVE (tail)")
	} else {
		hidden := total - end
		status = busFrozenStatusStyle.Render(fmt.Sprintf("⏸ scrolled · %d rows hidden below · end/G to resume", hidden))
	}
	return visible, status
}

func (m model) viewBus() string {
	if m.bus == nil {
		return "\n  bus unavailable (check ~/.config/hive/ is writable)"
	}

	composeHeight := 4 // status + divider + compose + hint
	viewHeight := m.height - 1 /*tab bar*/ - composeHeight
	if viewHeight < 1 {
		viewHeight = 1
	}

	lines := m.renderBusLines()
	mp := &m
	visible, status := mp.busViewport(lines, viewHeight)

	var b strings.Builder
	for _, line := range visible {
		b.WriteString(line)
		b.WriteString("\n")
	}
	// Pad if we have fewer visible lines than the viewport (short bus)
	for i := len(visible); i < viewHeight; i++ {
		b.WriteString("\n")
	}

	// Status line above the compose
	b.WriteString(status + "\n")
	b.WriteString(dividerStyle.Render(strings.Repeat("─", m.width)) + "\n")
	b.WriteString("  " + m.busCompose.View() + "\n")
	b.WriteString(helpStyle.UnsetPadding().Render("  enter send · ↑↓ pgup/pgdn home/end scroll · esc back"))

	return b.String()
}
