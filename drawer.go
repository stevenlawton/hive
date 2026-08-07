package main

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"

	"github.com/stevenlawton/hive/ui"
)

// The todo drawer is a bottom panel over the manager view. State lives on the
// model (drawer* fields); it is not a viewMode because the manager list stays
// visible above it.

func newDrawerInput(prompt, placeholder, value string) textinput.Model {
	ti := textinput.New()
	ti.Prompt = prompt
	ti.Placeholder = placeholder
	ti.SetWidth(60)
	if value != "" {
		ti.SetValue(value) // bubbles places the cursor at the end
	}
	visibleCursorStyle(&ti)
	return ti
}

// toggleDrawer opens the drawer for the current context — the focused workspace
// tab's repo when in a tab, otherwise the selected manager row — loading that
// repo's list. Closes the drawer if already open. It never changes mode, so the
// drawer overlays wherever you already are.
func (m *model) toggleDrawer() tea.Cmd {
	if m.drawerOpen {
		m.closeDrawer()
		return nil
	}
	path, name, ok := m.drawerTargetRepo()
	if !ok {
		return nil
	}
	m.drawerRepo = path
	m.drawerRepoName = name
	m.drawerClaim = worktreeClaim(path)
	m.loadDrawerTodos(path)
	m.drawerCursor = 0
	m.drawerInputOn = false
	m.drawerEditID, m.drawerAdding = "", false
	m.drawerOpen = true
	return nil
}

// drawerTargetRepo picks whose list the drawer shows: the active tab's repo in
// workspace mode, else the selected manager row.
func (m model) drawerTargetRepo() (path, name string, ok bool) {
	if m.mode == viewWorkspace {
		if tab := m.workspace.ActiveTab(); tab != nil {
			// Prefer the focused split's session — each split is its own
			// worktree, so the drawer claims for the pane you're looking at.
			if split := tab.SplitPane.FocusedSplit(); split != nil && split.SessionName != "" {
				for i := range m.items {
					if m.items[i].tmuxSes == split.SessionName {
						return m.items[i].repo.Path, m.items[i].repo.Name, true
					}
				}
			}
			for i := range m.items {
				if m.items[i].repo.DirName == tab.ID {
					return m.items[i].repo.Path, m.items[i].repo.Name, true
				}
			}
		}
	}
	if item := m.selectedItem(); item != nil && !item.isTGSession {
		return item.repo.Path, item.repo.Name, true
	}
	return "", "", false
}

func (m *model) closeDrawer() {
	m.drawerOpen = false
	m.stopDrawerInput()
}

// stopDrawerInput ends an add/edit and clears the text field.
func (m *model) stopDrawerInput() {
	if m.drawerInputOn {
		m.drawerInput.Blur()
		m.drawerInput.SetValue("")
	}
	m.drawerInputOn = false
	m.drawerEditID, m.drawerAdding = "", false
}

// reloadDrawerForContext re-points an open drawer at the currently-focused
// tab's repo when it changes, so the drawer follows tab switches. Edits are
// already persisted to disk, so reloading loses nothing.
func (m *model) reloadDrawerForContext() {
	if !m.drawerOpen {
		return
	}
	path, name, ok := m.drawerTargetRepo()
	if !ok || path == m.drawerRepo {
		return
	}
	m.drawerRepo = path
	m.drawerRepoName = name
	m.drawerClaim = worktreeClaim(path)
	m.loadDrawerTodos(path)
	m.drawerCursor = 0
	m.stopDrawerInput()
}

// refreshDrawerFromDisk re-reads the open drawer's list so external edits
// (/todo, hive todo CLI, a peer) show up. Skipped mid add/edit so it can't
// clobber what the user is typing; the cursor is clamped to the new length.
func (m *model) refreshDrawerFromDisk() {
	if !m.drawerOpen || m.drawerInputOn {
		return
	}
	id := m.cursorTodoID()
	m.drawerTodos = loadTodos(m.drawerRepo)
	m.restoreCursor(id)
}

// splitSubjectDesc parses drawer input "subject - description" (accepting an
// em-dash too) into its parts.
func splitSubjectDesc(s string) (subject, desc string) {
	if i := indexSeparator(s); i >= 0 {
		return strings.TrimSpace(s[:i]), trimSeparator(s[i:])
	}
	return strings.TrimSpace(s), ""
}

// todoEditText reconstructs the editable "subject — description" line.
func todoEditText(t Todo) string {
	if t.Description != "" {
		return t.Subject + descSep + t.Description
	}
	return t.Subject
}

// drawerCursorSection is the section a newly-added task inherits.
func (m model) drawerCursorSection() string {
	if m.drawerCursor >= 0 && m.drawerCursor < len(m.drawerTodos) {
		return m.drawerTodos[m.drawerCursor].sectionOrDefault()
	}
	return defaultSection
}

// loadDrawerTodos loads a repo's list for display, stamping ids on any task that
// lacks one so every visible row is addressable by the delta handlers. Opening a
// list whose tasks all carry ids touches nothing: withTodos skips the write when
// only the generated Last-sync date would differ.
func (m *model) loadDrawerTodos(path string) {
	todos, err := withTodos(path, func(ts []Todo) []Todo { return ts })
	if err != nil {
		m.err = err
		todos = loadTodos(path)
	}
	m.drawerTodos = todos
}

// applyDrawer runs a delta against the on-disk list and adopts the result, so a
// peer's concurrent edits survive. The cursor stays on the task it was on, even
// if the peer inserted rows above it.
func (m *model) applyDrawer(mutate func([]Todo) []Todo) {
	id := m.cursorTodoID()
	todos, err := withTodos(m.drawerRepo, mutate)
	if err != nil {
		m.err = err
		return
	}
	m.drawerTodos = todos
	m.restoreCursor(id)
}

func (m model) cursorTodoID() string {
	if m.drawerCursor >= 0 && m.drawerCursor < len(m.drawerTodos) {
		return m.drawerTodos[m.drawerCursor].ID
	}
	return ""
}

// restoreCursor puts the cursor back on id, clamping when it is gone — deleted
// here or by a peer.
func (m *model) restoreCursor(id string) {
	if i, ok := indexByID(m.drawerTodos, id); ok {
		m.drawerCursor = i
		return
	}
	if m.drawerCursor >= len(m.drawerTodos) {
		m.drawerCursor = max(0, len(m.drawerTodos)-1)
	}
}

// handleDrawerKey handles input while the drawer is open. Editing keys act on
// the drawer; chords still pass through so you can switch tabs (the drawer then
// follows the newly-focused tab via syncModeFromActiveTab).
func (m model) handleDrawerKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	keystroke := msg.Keystroke()
	key := msg.String()

	if m.drawerInputOn {
		switch key {
		case "enter":
			val := strings.TrimSpace(m.drawerInput.Value())
			if val != "" {
				subj, desc := splitSubjectDesc(val)
				if m.drawerAdding {
					section := m.drawerCursorSection()
					todos, err := withTodos(m.drawerRepo, func(ts []Todo) []Todo {
						return addTodo(ts, section, subj, desc)
					})
					if err != nil {
						m.err = err
					} else {
						m.drawerTodos = todos
						m.restoreCursor(todos[len(todos)-1].ID)
					}
				} else {
					editID := m.drawerEditID
					m.applyDrawer(func(ts []Todo) []Todo {
						if i, ok := indexByID(ts, editID); ok {
							ts[i].Subject, ts[i].Description = subj, desc
						}
						return ts
					})
					m.restoreCursor(editID)
				}
			}
			m.stopDrawerInput()
			return m, nil
		case "esc", "escape":
			m.stopDrawerInput()
			return m, nil
		}
		var cmd tea.Cmd
		m.drawerInput, cmd = m.drawerInput.Update(msg)
		return m, cmd
	}

	// Chords pass through so tab navigation still works with the drawer open;
	// the drawer then follows the newly-focused tab (via syncModeFromActiveTab).
	if m.chord.Pending() {
		if action, ok := m.chord.Complete(keystroke); ok {
			updated, cmd := m.handleChordAction(action)
			if mm, ok := updated.(model); ok {
				mm.reloadDrawerForContext()
				return mm, cmd
			}
			return updated, cmd
		}
		m.chord.Cancel()
		return m, nil
	}
	if keystroke == "ctrl+@" || keystroke == "ctrl+space" {
		m.chord.Start()
		return m, nil
	}

	switch key {
	case "esc", "escape", "t":
		m.closeDrawer()
	case "a":
		m.drawerInput = newDrawerInput("add: ", "subject — optional description", "")
		m.drawerInputOn = true
		m.drawerEditID, m.drawerAdding = "", true
		return m, m.drawerInput.Focus()
	case "e":
		if m.drawerCursor >= 0 && m.drawerCursor < len(m.drawerTodos) {
			m.drawerInput = newDrawerInput("edit: ", "", todoEditText(m.drawerTodos[m.drawerCursor]))
			m.drawerInputOn = true
			m.drawerEditID, m.drawerAdding = m.drawerTodos[m.drawerCursor].ID, false
			return m, m.drawerInput.Focus()
		}
	case "up", "k":
		if m.drawerCursor > 0 {
			m.drawerCursor--
		}
	case "down", "j":
		if m.drawerCursor < len(m.drawerTodos)-1 {
			m.drawerCursor++
		}
	case "space", " ", "x":
		id := m.cursorTodoID()
		m.applyDrawer(func(ts []Todo) []Todo {
			if i, ok := indexByID(ts, id); ok {
				return toggleTodoDone(ts, i)
			}
			return ts
		})
	case "~", "enter", "c":
		id := m.cursorTodoID()
		var held string
		m.applyDrawer(func(ts []Todo) []Todo {
			i, ok := indexByID(ts, id)
			if !ok {
				return ts
			}
			out, changed := claimTodo(ts, i, m.drawerClaim)
			if !changed {
				held = ts[i].Claim
				return ts
			}
			return out
		})
		if held != "" {
			m.err = fmt.Errorf("task claimed by %s", held)
		}
	case ">":
		id := m.cursorTodoID()
		m.applyDrawer(func(ts []Todo) []Todo {
			if i, ok := indexByID(ts, id); ok {
				return deferTodo(ts, i)
			}
			return ts
		})
	case "d":
		id := m.cursorTodoID()
		m.applyDrawer(func(ts []Todo) []Todo {
			if i, ok := indexByID(ts, id); ok {
				return deleteTodo(ts, i)
			}
			return ts
		})
	}
	return m, nil
}

// drawerPanelHeight is how many rows the drawer claims from the bottom.
func (m model) drawerPanelHeight() int {
	h := len(m.drawerTodos) + 3 // top border + title + hint
	if h < 6 {
		h = 6
	}
	maxH := m.height / 2
	if maxH < 6 {
		maxH = 6
	}
	if h > maxH {
		h = maxH
	}
	return h
}

var (
	drawerTitleStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#ff8c00"))
	drawerHintStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))
	drawerDoneStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#555555")).Strikethrough(true)
	drawerCurStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("#ff8c00")).Bold(true)
	drawerEmptyStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#555555")).Italic(true)
	drawerSectionStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#888888")).Bold(true)
	drawerDescStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#666666"))
	drawerClaimedStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#777777"))
	drawerClaimTagStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#0088cc"))
	drawerDeferStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#555555")).Italic(true)
)

// drawerLine is one rendered row in the drawer: a section header (todoIdx -1)
// or a task row.
type drawerLine struct {
	text    string
	todoIdx int
}

// drawerLines builds the section-grouped display rows and the display index of
// the row holding the cursor task.
func (m model) drawerLines(width int) (lines []drawerLine, cursorLine int) {
	lastSection := ""
	for i, t := range m.drawerTodos {
		if sec := t.sectionOrDefault(); sec != lastSection {
			lastSection = sec
			lines = append(lines, drawerLine{text: drawerSectionStyle.Render("  " + sec), todoIdx: -1})
		}
		if i == m.drawerCursor {
			cursorLine = len(lines)
		}
		lines = append(lines, drawerLine{text: drawerRow(t, i == m.drawerCursor, m.drawerClaim, width), todoIdx: i})
	}
	return lines, cursorLine
}

// renderTodoDrawer draws the bottom panel: a divider, a title, the task rows
// (scrolled to keep the cursor visible), and an add-input or key hint.
func (m model) renderTodoDrawer(width, height int) string {
	if height < 3 {
		height = 3
	}
	done, active, deferred := todoProgress(m.drawerTodos)

	title := " TODO — " + m.drawerRepoName
	if m.drawerClaim != "" {
		title += " @" + m.drawerClaim
	}
	title += fmt.Sprintf("  (%d/%d done", done, active)
	if deferred > 0 {
		title += fmt.Sprintf(" · %d parked", deferred)
	}
	title += ")"
	lines := []string{
		dividerStyle.Render(strings.Repeat("─", width)),
		drawerTitleStyle.Render(title),
	}

	bodyRows := height - 3 // divider + title + hint
	if bodyRows < 1 {
		bodyRows = 1
	}
	if len(m.drawerTodos) == 0 {
		lines = append(lines, drawerEmptyStyle.Render("  no tasks — press a to add one"))
	} else {
		dl, cursorLine := m.drawerLines(width)
		start := 0
		if cursorLine >= bodyRows {
			start = cursorLine - bodyRows + 1
		}
		end := start + bodyRows
		if end > len(dl) {
			end = len(dl)
			if start = end - bodyRows; start < 0 {
				start = 0
			}
		}
		for i := start; i < end; i++ {
			lines = append(lines, dl[i].text)
		}
	}

	for len(lines) < height-1 {
		lines = append(lines, "")
	}

	if m.drawerInputOn {
		lines = append(lines, "  "+ui.CursorSentinel+m.drawerInput.View())
	} else {
		lines = append(lines, drawerHintStyle.Render("  a add · e edit · space done · ~ claim · > park · d delete · j/k move · esc close"))
	}

	if len(lines) > height {
		lines = lines[:height]
	}
	return strings.Join(lines, "\n")
}

func drawerRow(t Todo, selected bool, myClaim string, width int) string {
	cursor := "  "
	if selected {
		cursor = cursorStyle.Render("▸ ")
	}

	// Tag tasks another worktree holds so they read as taken.
	tag := ""
	if !t.Done && t.Claim != "" && t.Claim != myClaim {
		tag = " 🔒@" + t.Claim
	}

	avail := max(1, width-6-len([]rune(tag))) // cursor(2) + "[x] "(4) + tag
	subject := t.Subject
	if len([]rune(subject)) > avail {
		subject = truncStr(subject, avail)
	}
	rem := avail - len([]rune(subject))
	desc := ""
	if t.Description != "" && rem > 3 {
		desc = truncStr(descSep+t.Description, rem)
	}

	var box, subjStyled string
	switch {
	case t.Done:
		box, subjStyled = "[x]", drawerDoneStyle.Render(subject)
	case t.Deferred:
		box, subjStyled = "[-]", drawerDeferStyle.Render(subject) // parked
	case t.Claim != "" && t.Claim == myClaim:
		box, subjStyled = drawerCurStyle.Render("[~]"), drawerCurStyle.Render(subject) // mine
	case t.Claim != "":
		box, subjStyled = "[~]", drawerClaimedStyle.Render(subject) // held by another worktree
	default:
		box, subjStyled = "[ ]", subject
	}
	line := fmt.Sprintf("%s%s %s", cursor, box, subjStyled)
	if desc != "" {
		line += drawerDescStyle.Render(desc)
	}
	if tag != "" {
		line += drawerClaimTagStyle.Render(tag)
	}
	return line
}
