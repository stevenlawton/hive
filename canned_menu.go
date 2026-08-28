package main

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"

	"github.com/charmbracelet/x/ansi"
)

// The canned-response menu is a popup over the workspace view: pick a
// pre-written prompt and it lands in a session's input box, submitted. State
// lives on the model, like the todo drawer, because the session panes stay
// visible underneath.

// Pauses in the send sequence. An interrupt needs a beat before Claude's input
// box is ready for text, and the text needs a beat to land before Enter
// submits it — otherwise Enter races the keystrokes tmux is still delivering.
const (
	cannedInterruptPause = 250 * time.Millisecond
	cannedSubmitPause    = 120 * time.Millisecond
)

type cannedMenu struct {
	open    bool
	items   []CannedPrompt
	cursor  int
	session string // target tmux session
	geom    cannedGeom

	editing bool // the add/edit form owns input
	adding  bool // editing a new entry rather than the cursor's
	field   int  // 0 = label, 1 = text
	label   textinput.Model
	text    textinput.Model
}

func (c *cannedMenu) move(delta int) {
	if len(c.items) == 0 {
		return
	}
	c.cursor += delta
	if c.cursor < 0 {
		c.cursor = 0
	}
	if c.cursor >= len(c.items) {
		c.cursor = len(c.items) - 1
	}
}

// cannedDigitIndex maps a number key to a list position: 1-9 for the first
// nine, 0 for the tenth. Anything else returns -1.
func cannedDigitIndex(key string) int {
	if len(key) != 1 {
		return -1
	}
	switch c := key[0]; {
	case c == '0':
		return 9
	case c >= '1' && c <= '9':
		return int(c - '1')
	}
	return -1
}

// cannedGeom is the popup's screen rectangle, resolved once when the menu
// opens so rendering and mouse hit-testing agree on where the rows are.
type cannedGeom struct {
	x, y          int
	width, height int
	rows          int
}

const cannedHint = "↑↓ pick · 1-9,0 send · a add · e edit · d del · ^u later · esc"

// cannedFormMinWidth keeps the edit form usable even when the menu it replaces
// is only as wide as its longest label.
const cannedFormMinWidth = 60

func cannedGeometry(items []CannedPrompt, clickX, clickY, screenW, screenH int) cannedGeom {
	widest := runeLen(cannedHint)
	for i, p := range items {
		if w := runeLen(cannedRowText(i, p)); w > widest {
			widest = w
		}
	}
	// Borders (2) plus a space of padding either side (2).
	g := cannedGeom{
		width:  widest + 4,
		height: len(items) + 3, // top border, rows, hint, bottom border
		rows:   len(items),
	}
	if g.width > screenW {
		g.width = screenW
	}
	if g.height > screenH {
		g.height = screenH
	}
	g.x = clampInt(clickX, 0, screenW-g.width)
	g.y = clampInt(clickY, 0, screenH-g.height)
	return g
}

// widenTo grows the box to at least min cells, re-clamping it onto a screen
// of the given width.
func (g cannedGeom) widenTo(min, screenW int) cannedGeom {
	if g.width >= min {
		return g
	}
	g.width = min
	if g.width > screenW {
		g.width = screenW
	}
	g.x = clampInt(g.x, 0, screenW-g.width)
	return g
}

// firstRowY is the screen row of the first prompt, just inside the top border.
func (g cannedGeom) firstRowY() int { return g.y + 1 }

// rowAt returns the prompt index under a screen coordinate, or -1.
func (g cannedGeom) rowAt(x, y int) int {
	if x < g.x || x >= g.x+g.width {
		return -1
	}
	row := y - g.firstRowY()
	if row < 0 || row >= g.rows {
		return -1
	}
	return row
}

func clampInt(v, lo, hi int) int {
	if hi < lo {
		return lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func runeLen(s string) int { return len([]rune(s)) }

// cannedOp is one step of the send sequence: a raw key or a literal string,
// preceded by an optional pause.
type cannedOp struct {
	key     string
	literal string
	delay   time.Duration
}

// cannedSendPlan builds the keystroke sequence for sending a prompt. A busy
// session is interrupted first; an idle one is not, because an Escape there
// would clear whatever was half-typed in the input box.
func cannedSendPlan(busy bool, text string) []cannedOp {
	text = flattenLines(text)
	if text == "" {
		return nil
	}
	var plan []cannedOp
	if busy {
		plan = append(plan, cannedOp{key: "escape"})
		plan = append(plan, cannedOp{literal: text, delay: cannedInterruptPause})
	} else {
		plan = append(plan, cannedOp{literal: text})
	}
	return append(plan, cannedOp{key: "enter", delay: cannedSubmitPause})
}

// sessionIsBusy reports whether claude is currently generating in a session,
// per the session-event-driven richStatus. An unknown session, or one with no
// status file, counts as idle: queueing a prompt behind whatever it is doing
// is harmless, whereas a needless Escape is not.
func sessionIsBusy(items []repoItem, sesName string) bool {
	if sesName == "" {
		return false
	}
	for i := range items {
		if items[i].tmuxSes == sesName {
			return items[i].richStatus != nil && items[i].richStatus.Status == "running"
		}
	}
	return false
}

// cannedRowText renders one menu row: its number key and label. Entries past
// the tenth are still selectable with the cursor, just not by digit.
func cannedRowText(index int, p CannedPrompt) string {
	switch {
	case index < 9:
		return fmt.Sprintf("%d. %s", index+1, p.Label)
	case index == 9:
		return "0. " + p.Label
	}
	return "   " + p.Label
}

var (
	cannedBorderStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("#ff8c00")).
				Background(lipgloss.Color("#101010"))
	cannedRowStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#cccccc")).
			Background(lipgloss.Color("#101010"))
	cannedSelectedStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#000000")).
				Background(lipgloss.Color("#ff8c00")).
				Bold(true)
	cannedHintStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#666666")).
			Background(lipgloss.Color("#101010"))
)

// renderCannedMenu draws the popup at exactly the size its geometry claims, so
// mouse hit-testing and what is on screen can't drift apart.
func renderCannedMenu(c cannedMenu) string {
	inner := c.geom.width - 2
	rows := make([]string, 0, c.geom.rows+1)
	for i, p := range c.items {
		if i >= c.geom.rows {
			break
		}
		style := cannedRowStyle
		if i == c.cursor {
			style = cannedSelectedStyle
		}
		rows = append(rows, style.Width(inner).Render(" "+truncateCells(cannedRowText(i, p), inner-2)))
	}
	rows = append(rows, cannedHintStyle.Width(inner).Render(" "+truncateCells(cannedHint, inner-2)))
	return cannedBorderStyle.Render(strings.Join(rows, "\n"))
}

// renderCannedPopup draws whatever the popup is currently showing: the prompt
// list, or the add/edit form in its place.
func (m model) renderCannedPopup() string {
	if m.canned.editing {
		return renderCannedForm(m.canned)
	}
	return renderCannedMenu(m.canned)
}

func renderCannedForm(c cannedMenu) string {
	inner := c.geom.width - 2
	if inner < 20 {
		inner = 20
	}
	title := "new prompt"
	if !c.adding {
		title = "edit prompt"
	}
	lines := []string{
		cannedHintStyle.Width(inner).Render(" " + title),
		cannedRowStyle.Width(inner).Render(" " + truncateCells(c.label.View(), inner-2)),
		cannedRowStyle.Width(inner).Render(" " + truncateCells(c.text.View(), inner-2)),
		cannedHintStyle.Width(inner).Render(" " + truncateCells("enter next/save · tab switch · esc cancel", inner-2)),
	}
	return cannedBorderStyle.Render(strings.Join(lines, "\n"))
}

// truncateCells cuts a possibly-styled string to a cell width. It has to be
// ANSI-aware: a rune count would treat every escape sequence as visible text
// and chop the line to a fraction of the space it actually occupies.
func truncateCells(s string, width int) string {
	if width < 1 {
		return ""
	}
	return ansi.Truncate(s, width, "…")
}

// newCannedInput builds a form field. Unlike the drawer's, it keeps bubbles'
// virtual cursor: the popup floats over a session pane whose own sentinel
// already owns the frame's hardware cursor.
func newCannedInput(prompt, placeholder, value string, width int) textinput.Model {
	ti := textinput.New()
	ti.Prompt = prompt
	ti.Placeholder = placeholder
	ti.SetWidth(width)
	if value != "" {
		ti.SetValue(value)
	}
	return ti
}

// openCannedMenu shows the popup over a session pane, anchored at (x, y).
// Prompts are re-read from disk here, so a hand-edited canned.yaml takes
// effect on the next open rather than the next restart.
func (m *model) openCannedMenu(session string, x, y int) {
	if session == "" {
		return
	}
	items := m.cannedStore.Prompts()
	if len(items) == 0 {
		return
	}
	m.canned = cannedMenu{
		open:    true,
		items:   items,
		session: session,
		geom:    cannedGeometry(items, x, y, m.width, m.height),
	}
}

func (m *model) closeCannedMenu() {
	m.canned = cannedMenu{}
}

// sendCannedOps delivers a plan to a session. A var so tests can watch the
// sequence without a live tmux.
var sendCannedOps = func(session string, plan []cannedOp) {
	for _, op := range plan {
		if op.delay > 0 {
			time.Sleep(op.delay)
		}
		switch {
		case op.literal != "":
			TmuxSendLiteral(session, op.literal)
		case op.key != "":
			TmuxSendRawKeys(session, op.key)
		}
	}
}

// sendCannedPrompt closes the menu and fires the prompt at its target session.
func (m model) sendCannedPrompt(index int) (tea.Model, tea.Cmd) {
	if index < 0 || index >= len(m.canned.items) {
		return m, nil
	}
	session := m.canned.session
	plan := cannedSendPlan(sessionIsBusy(m.items, session), m.canned.items[index].Text)
	m.closeCannedMenu()
	if len(plan) == 0 {
		return m, nil
	}
	// The plan sleeps between keystrokes, so it runs off the Update loop.
	return m, func() tea.Msg {
		sendCannedOps(session, plan)
		return nil
	}
}

func (m model) handleCannedKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.canned.editing {
		return m.handleCannedFormKey(msg)
	}
	switch key := msg.String(); key {
	case "esc", "escape":
		m.closeCannedMenu()
		return m, nil
	case "ctrl+u":
		// Same pane, but parked for later rather than sent now. This is the
		// mouse route into the future-prompt popup: right-click opens this
		// menu, and ctrl+u hands the session over.
		session, x, y := m.canned.session, m.canned.geom.x, m.canned.geom.y
		m.closeCannedMenu()
		m.openFuturePopup(session, x, y)
		return m, nil
	case "enter":
		return m.sendCannedPrompt(m.canned.cursor)
	case "up", "k":
		m.canned.move(-1)
		return m, nil
	case "down", "j":
		m.canned.move(1)
		return m, nil
	case "J":
		m.reorderCanned(1)
		return m, nil
	case "K":
		m.reorderCanned(-1)
		return m, nil
	case "a":
		m.startCannedEdit(true)
		return m, nil
	case "e":
		m.startCannedEdit(false)
		return m, nil
	case "d":
		m.deleteCannedPrompt()
		return m, nil
	default:
		if i := cannedDigitIndex(key); i >= 0 && i < len(m.canned.items) {
			return m.sendCannedPrompt(i)
		}
	}
	return m, nil
}

func (m *model) reorderCanned(delta int) {
	from := m.canned.cursor
	to := from + delta
	if to < 0 || to >= len(m.canned.items) {
		return
	}
	m.canned.items[from], m.canned.items[to] = m.canned.items[to], m.canned.items[from]
	m.canned.cursor = to
	m.persistCanned()
}

func (m *model) deleteCannedPrompt() {
	i := m.canned.cursor
	if i < 0 || i >= len(m.canned.items) {
		return
	}
	m.canned.items = append(m.canned.items[:i:i], m.canned.items[i+1:]...)
	if m.canned.cursor >= len(m.canned.items) {
		m.canned.cursor = len(m.canned.items) - 1
	}
	if m.canned.cursor < 0 {
		m.canned.cursor = 0
	}
	m.persistCanned()
}

// startCannedEdit opens the two-field form, either on a new entry or the one
// under the cursor.
func (m *model) startCannedEdit(adding bool) {
	label, text := "", ""
	if !adding {
		if i := m.canned.cursor; i >= 0 && i < len(m.canned.items) {
			label, text = m.canned.items[i].Label, m.canned.items[i].Text
		} else {
			return
		}
	}
	m.canned.editing = true
	m.canned.adding = adding
	m.canned.field = 0
	m.canned.geom = m.canned.geom.widenTo(cannedFormMinWidth, m.width)

	// Box width less both borders, the row's padding either side, the field's
	// own prompt, and the cell the cursor sits in past the last character.
	fieldWidth := m.canned.geom.width - 4 - runeLen(cannedFieldPrompts[0]) - 1
	m.canned.label = newCannedInput(cannedFieldPrompts[0], "short name", label, fieldWidth)
	m.canned.text = newCannedInput(cannedFieldPrompts[1], "prompt sent to the session", text, fieldWidth)
	m.canned.label.Focus()
}

// cannedFieldPrompts are padded to the same width so the two inputs line up.
var cannedFieldPrompts = [2]string{"label ", "text  "}

func (m model) handleCannedFormKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "escape":
		m.cancelCannedEdit()
		return m, nil
	case "enter":
		if m.canned.field == 0 {
			m.canned.field = 1
			m.canned.label.Blur()
			m.canned.text.Focus()
			return m, nil
		}
		m.commitCannedEdit()
		return m, nil
	case "tab", "shift+tab":
		m.canned.field = 1 - m.canned.field
		if m.canned.field == 0 {
			m.canned.text.Blur()
			m.canned.label.Focus()
		} else {
			m.canned.label.Blur()
			m.canned.text.Focus()
		}
		return m, nil
	}
	var cmd tea.Cmd
	if m.canned.field == 0 {
		m.canned.label, cmd = m.canned.label.Update(msg)
	} else {
		m.canned.text, cmd = m.canned.text.Update(msg)
	}
	return m, cmd
}

func (m *model) cancelCannedEdit() {
	m.canned.editing, m.canned.adding, m.canned.field = false, false, 0
}

func (m *model) commitCannedEdit() {
	entry := CannedPrompt{
		Label: flattenLines(m.canned.label.Value()),
		Text:  flattenLines(m.canned.text.Value()),
	}
	if entry.Text != "" {
		if entry.Label == "" {
			entry.Label = entry.Text
		}
		if m.canned.adding {
			m.canned.items = append(m.canned.items, entry)
			m.canned.cursor = len(m.canned.items) - 1
		} else if i := m.canned.cursor; i >= 0 && i < len(m.canned.items) {
			m.canned.items[i] = entry
		}
		m.persistCanned()
	}
	m.cancelCannedEdit()
}

// persistCanned writes the list back and re-measures the popup, which may have
// changed shape. A failed write costs the edit, not the session.
func (m *model) persistCanned() {
	if err := m.cannedStore.Save(m.canned.items); err != nil {
		m.err = err
	}
	m.canned.geom = cannedGeometry(m.canned.items, m.canned.geom.x, m.canned.geom.y, m.width, m.height)
}

// Mouse messages. OnMouse runs against a copy of the model, so pointer work
// happens here in Update, the same way split and divider clicks do.
type cannedOpenMsg struct {
	session string
	x, y    int
}
type cannedRowClickMsg struct{ row int } // -1 = outside the popup
type cannedHoverMsg struct{ row int }
type cannedScrollMsg struct{ dir int }

// handleCannedMouse applies one of the popup's mouse messages.
func (m model) handleCannedMouse(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case cannedOpenMsg:
		m.openCannedMenu(msg.session, msg.x, msg.y)
	case cannedRowClickMsg:
		if !m.canned.open {
			break
		}
		if msg.row < 0 {
			m.closeCannedMenu()
			break
		}
		return m.sendCannedPrompt(msg.row)
	case cannedHoverMsg:
		if m.canned.open && msg.row >= 0 && msg.row < len(m.canned.items) {
			m.canned.cursor = msg.row
		}
	case cannedScrollMsg:
		if m.canned.open {
			m.canned.move(msg.dir)
		}
	}
	return m, nil
}
