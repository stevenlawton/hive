package main

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
)

// The future-prompt popup: notes parked against a session while the five-hour
// quota is spent, fired when the window rolls over. It borrows the canned
// popup's geometry and delivery — the difference is when the text is sent, not
// how.

const futureMenuWidth = 56

type futureMenu struct {
	open    bool
	session string
	q       FutureQueue
	resetAt int64
	cursor  int
	input   textinput.Model

	resumeText string
	geom       cannedGeom

	// baseline is the queue as it stood when the popup opened, and typed
	// lists the notes added since. A tick can fire the queue while the popup
	// sits open on it, so closing merges against what is on disk rather than
	// writing back this copy wholesale.
	baseline FutureQueue
	typed    []string
}

// newFutureMenu opens the popup over a session's parked queue. Auto send is
// ticked on open, because reaching for this popup at all means wanting the
// notes to go — but only when there is a reset to fire against, and never for
// a queue the user has already unticked by hand.
func newFutureMenu(session string, q FutureQueue, resetAt int64, resumeText string) futureMenu {
	baseline := q
	fresh := len(q.Prompts) == 0 && !q.AutoSend && !q.AutoResume && q.ArmedFor == 0
	// A draining queue already has a firing trigger; re-arming it against the
	// next reset would give it a second one.
	if (fresh || q.AutoSend) && !q.Draining {
		q = armFuture(q, resetAt)
	}
	return futureMenu{
		open:       true,
		session:    session,
		q:          q,
		baseline:   baseline,
		resetAt:    resetAt,
		resumeText: resumeText,
		input:      newCannedInput("> ", "note for when the tokens come back", "", futureMenuWidth-6),
	}
}

// editorEnabled reports whether the prompt list is the user's to edit. Auto
// resume takes the payload over, so the editor greys out while it is ticked.
func (c futureMenu) editorEnabled() bool { return !c.q.AutoResume }

func (c *futureMenu) toggleAutoSend() {
	if c.q.AutoSend {
		c.q.AutoSend = false
		c.q.ArmedFor = 0
		return
	}
	c.q = armFuture(c.q, c.resetAt)
}

// toggleAutoResume swaps the queue for the canned "resume" payload. Ticking it
// implies auto send, since a resume that never fires means nothing. The parked
// prompts are left untouched underneath, so unticking gives them back.
func (c *futureMenu) toggleAutoResume() {
	c.q.AutoResume = !c.q.AutoResume
	if c.q.AutoResume {
		c.q = armFuture(c.q, c.resetAt)
	}
}

func (c *futureMenu) move(delta int) {
	if len(c.q.Prompts) == 0 {
		c.cursor = 0
		return
	}
	c.cursor = clampInt(c.cursor+delta, 0, len(c.q.Prompts)-1)
}

// commitPrompt parks whatever is in the field. Prompts are flattened to one
// line: the text is delivered as a literal keystroke stream, so an embedded
// newline would submit it half-typed.
func (c *futureMenu) commitPrompt() {
	text := flattenLines(c.input.Value())
	c.input.SetValue("")
	if text == "" {
		return
	}
	c.q.Prompts = append(c.q.Prompts, text)
	c.typed = append(c.typed, text)
	c.cursor = len(c.q.Prompts) - 1
}

func (c *futureMenu) deletePrompt() {
	if c.cursor < 0 || c.cursor >= len(c.q.Prompts) {
		return
	}
	c.q.Prompts = append(c.q.Prompts[:c.cursor], c.q.Prompts[c.cursor+1:]...)
	if c.cursor >= len(c.q.Prompts) {
		c.cursor = len(c.q.Prompts) - 1
	}
	if c.cursor < 0 {
		c.cursor = 0
	}
}

// futureHeader names the account-level clock everything here hangs off. The
// window is shared, so this reads the same whichever session the popup is
// opened over.
func futureHeader(resetAt int64) string {
	if resetAt <= 0 {
		return "5h window · reset time unknown"
	}
	return "5h window · resets " + time.Unix(resetAt, 0).Local().Format("15:04")
}

func futureTick(on bool) string {
	if on {
		return "[x]"
	}
	return "[ ]"
}

var (
	futureDimStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#666666")).
		Background(lipgloss.Color("#101010"))
)

// renderFutureMenu draws the popup at the size its geometry claims, so mouse
// hit-testing and what is on screen cannot drift apart.
func renderFutureMenu(c futureMenu) string {
	inner := futureMenuWidth - 2
	if c.geom.width > 2 {
		inner = c.geom.width - 2
	}

	lines := []string{
		futureDimStyle.Width(inner).Render(" " + truncateCells(futureHeader(c.resetAt), inner-2)),
	}

	switch {
	case !c.editorEnabled():
		lines = append(lines, cannedRowStyle.Width(inner).Render(
			" "+truncateCells("payload: "+c.resumeText, inner-2)))
		lines = append(lines, futureDimStyle.Width(inner).Render(
			" "+truncateCells("parked notes are held while auto resume is on", inner-2)))
	case len(c.q.Prompts) == 0:
		lines = append(lines, futureDimStyle.Width(inner).Render(
			" "+truncateCells("nothing parked yet", inner-2)))
	default:
		for i, p := range c.q.Prompts {
			style := cannedRowStyle
			if i == c.cursor {
				style = cannedSelectedStyle
			}
			lines = append(lines, style.Width(inner).Render(
				" "+truncateCells(futureRowText(i, p), inner-2)))
		}
	}

	if c.editorEnabled() {
		lines = append(lines, cannedRowStyle.Width(inner).Render(
			" "+truncateCells(c.input.View(), inner-2)))
	}

	lines = append(lines,
		cannedRowStyle.Width(inner).Render(" "+truncateCells(
			futureTick(c.q.AutoSend)+" auto send"+futureWhen(c.q, c.resetAt), inner-2)),
		cannedRowStyle.Width(inner).Render(" "+truncateCells(
			futureTick(c.q.AutoResume)+" auto resume", inner-2)),
		futureDimStyle.Width(inner).Render(" "+truncateCells(futureHint, inner-2)),
	)
	return cannedBorderStyle.Render(strings.Join(lines, "\n"))
}

const futureHint = "enter park · ^s auto send · ^r auto resume · ^d del · esc"

func futureRowText(index int, prompt string) string {
	return fmt.Sprintf("%d. %s", index+1, prompt)
}

// futureWhen spells out when an armed queue actually goes, grace included, so
// the popup does not appear to promise a send on the reset minute itself.
func futureWhen(q FutureQueue, resetAt int64) string {
	if !q.AutoSend {
		return ""
	}
	at := q.ArmedFor
	if at <= 0 {
		at = resetAt
	}
	if at <= 0 {
		return ""
	}
	return "  at " + time.Unix(at, 0).Add(futureFireGrace).Local().Format("15:04")
}

// handleFutureKey owns input while the popup is open. Text goes to the note
// field, so the shortcuts are deliberately the ones a note is unlikely to
// start with: ctrl+d to delete, esc to save and close.
func (m model) handleFutureKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "escape":
		if err := m.persistFuture(); err != nil {
			m.err = fmt.Errorf("save future prompts: %w", err)
		}
		m.future = futureMenu{}
		return m, nil

	case "enter":
		if m.future.editorEnabled() {
			m.future.commitPrompt()
		}
		return m, nil

	case "up":
		m.future.move(-1)
		return m, nil

	case "down":
		m.future.move(1)
		return m, nil

	case "ctrl+d":
		if m.future.editorEnabled() {
			m.future.deletePrompt()
		}
		return m, nil

	case "ctrl+s":
		m.future.toggleAutoSend()
		return m, nil

	case "ctrl+r":
		m.future.toggleAutoResume()
		return m, nil
	}

	// Auto resume owns the payload, so the field takes nothing while it is on.
	if !m.future.editorEnabled() {
		return m, nil
	}

	var cmd tea.Cmd
	m.future.input, cmd = m.future.input.Update(msg)
	return m, cmd
}

// persistFuture writes the popup's queue back to the store.
//
// Ticks keep running while the popup is open, so the queue on disk may have
// fired since — writing this copy back wholesale would re-arm it and type the
// same prompt a second time. When disk has moved on, disk wins and only the
// notes typed here are carried over.
func (m *model) persistFuture() error {
	if m.futureStore == nil || m.future.session == "" {
		return nil
	}
	queues, err := m.futureStore.LoadQueues()
	if err != nil {
		return err
	}

	session := m.future.session
	queues[session] = mergeFutureQueue(m.future, queues[session])
	return m.futureStore.Save(queues)
}

// mergeFutureQueue reconciles what the popup holds with what is on disk.
func mergeFutureQueue(c futureMenu, onDisk FutureQueue) FutureQueue {
	if !futureQueueFiredSince(c.baseline, onDisk) {
		return c.q
	}
	// The tick moved it. Keep its state, and re-park only the notes typed
	// here that the user has not since deleted.
	for _, note := range c.typed {
		if containsString(c.q.Prompts, note) && !containsString(onDisk.Prompts, note) {
			onDisk.Prompts = append(onDisk.Prompts, note)
		}
	}
	return onDisk
}

// futureQueueFiredSince reports whether the firing machinery has touched a
// queue since the popup opened on it.
func futureQueueFiredSince(baseline, onDisk FutureQueue) bool {
	return onDisk.ArmedFor != baseline.ArmedFor ||
		onDisk.Draining != baseline.Draining ||
		onDisk.AwaitingPickup != baseline.AwaitingPickup ||
		len(onDisk.Prompts) != len(baseline.Prompts)
}

func containsString(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

// openFuturePopup shows the parked queue for a session, reading the reset time
// from the fleet rather than from that session — the quota is account-level,
// and a session blocked on it has usually stopped reporting.
func (m *model) openFuturePopup(session string, x, y int) {
	if session == "" || m.futureStore == nil {
		return
	}
	tcfg := m.telemetryConfig()
	now := time.Now()
	resetAt, _ := fleetResetAt(readSessionSnapshots(tcfg, now), now)
	c := newFutureMenu(session, m.futureStore.Queues()[session], resetAt, m.futureStore.ResumeText())
	c.geom = cannedGeometry(make([]CannedPrompt, futureMenuRows), x, y, m.width, m.height).
		widenTo(futureMenuWidth, m.width)
	m.future = c
}

// futureMenuRows is what the popup's geometry is sized for: the header, a few
// parked prompts, the field, both tickboxes and the hint.
const futureMenuRows = 9

// runningSessions is the set of tmux sessions claude is currently generating
// in. This is what holds a drain back mid-turn.
func (m model) runningSessions() map[string]bool {
	out := map[string]bool{}
	for i := range m.items {
		it := m.items[i]
		if it.tmuxSes != "" && it.richStatus != nil && it.richStatus.Status == "running" {
			out[it.tmuxSes] = true
		}
	}
	return out
}

// runFutureQueues advances the parked queues and returns a command that types
// whatever came due. The store is only rewritten when something changed, so a
// quiet tick costs one read.
func (m *model) runFutureQueues(now time.Time) tea.Cmd {
	if m.futureStore == nil {
		return nil
	}
	queues, err := m.futureStore.LoadQueues()
	if err != nil {
		m.err = fmt.Errorf("future prompts: %w", err)
		return nil
	}
	if len(queues) == 0 {
		return nil
	}

	generating := m.runningSessions()
	next, sends, changed := futureWorkFor(
		queues, generating, m.resumedSessions(generating, now), m.futureStore.ResumeText(), now)
	if !changed {
		return nil
	}
	// Nothing is typed until the state that records it is safely on disk. A
	// failed write with the sends already away would re-fire the same prompt
	// on every tick from here on.
	if err := m.futureStore.Save(next); err != nil {
		m.err = fmt.Errorf("future prompts: %w", err)
		return nil
	}
	if len(sends) == 0 {
		return nil
	}

	// The plans sleep between keystrokes, so they run off the Update loop. The
	// whole fleet unblocks on the same reset, so sends are spaced: several
	// panes being typed into in the same instant is worth avoiding.
	//
	// A queue that fires is by construction aimed at a pane that is not
	// working, so nothing is interrupted: the busy-ness sampled here would be
	// seconds stale by the last send anyway, and a wrong Escape would clear
	// whatever the user had half-typed in the input box.
	return func() tea.Msg {
		for i, s := range sends {
			if i > 0 {
				time.Sleep(futureSendSpacing)
			}
			sendCannedOps(s.session, cannedSendPlan(false, s.text))
		}
		return nil
	}
}

const futureSendSpacing = 750 * time.Millisecond

// resumedSessions is the stricter question: which sessions have demonstrably
// carried on without hive, and so must not be fired into later.
//
// Generating alone is not enough. A session stalled on the quota can still
// read as generating, and disarming on that would cancel every queue the
// moment it was armed. A fresh telemetry snapshot is the positive evidence:
// the statusline is being rendered again, so the session is genuinely working.
func (m model) resumedSessions(generating map[string]bool, now time.Time) map[string]bool {
	if len(generating) == 0 {
		return map[string]bool{}
	}
	tcfg := m.telemetryConfig()
	// A zero stale window would mark every snapshot stale, and no session
	// would ever be seen resuming. Fall back to the default rather than
	// silently disabling the check.
	if tcfg.StaleAfterSeconds <= 0 {
		tcfg.StaleAfterSeconds = defaultTelemetryConfig().StaleAfterSeconds
	}
	snaps := readSessionSnapshots(tcfg, now)

	out := map[string]bool{}
	for session := range generating {
		if s, ok := snaps[session]; ok && !s.Stale {
			out[session] = true
		}
	}
	return out
}

func (m model) telemetryConfig() TelemetryConfig {
	if m.cfg == nil {
		return defaultTelemetryConfig()
	}
	return m.cfg.Telemetry
}
