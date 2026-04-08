package main

import (
	"fmt"
	"hive/ui"
	"os/exec"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/bubbles/v2/textinput"
)


type viewMode int

const (
	viewManager   viewMode = iota // main list + preview pane
	viewWorkspace                 // tabbed workspace with splits
	viewAttach                    // full-screen PTY attach
	viewHelp
	viewPromote
	viewEdit
	viewConfirm
	viewWorktree
)

type sessionStatus int

const (
	statusNone sessionStatus = iota
	statusClaude
	statusShell
	statusRemote
	statusDead
	statusWaiting
	statusTelegram // driven by Telegram bot
)

type repoItem struct {
	repo        Repo
	status      sessionStatus
	tmuxSes     string         // tmux session name if active
	title       string         // claude session title if available
	richStatus  *SessionStatus // from plugin status file (nil if no status file)
	bridgeEntry *BridgeEntry   // from bridge-sessions.json (nil if not bridged)
	diffStats   string         // "+42/-13" or ""
}

type model struct {
	cfg       *Config
	cfgPath   string
	items     []repoItem
	cursor    int
	mode      viewMode
	filter       textinput.Model
	filtering    bool
	filtered     []int // indices into items that match filter
	displayOrder []int            // indices into items, ordered: active > favourites > rest
	itemSection  map[int]string   // item index → section name ("active", "favourites", "repos", "archived")
	promote      textinput.Model
	keys      keyMap
	width     int
	height    int
	err       error
	alerts      map[string]string
	flashing    bool
	tabFlashing map[string]string // session tabs currently flashed (keyed by dirName, value: "bell" or "complete")

	showArchived bool // toggle to show/hide archived repos

	// New UI components
	manager   *ui.ManagerView
	workspace *ui.WorkspaceView
	chord     *ChordHandler

	// Worktree prompt state
	wtFields      []textinput.Model // 0=branch, 1=prompt
	wtYolo        bool
	wtFocus       int    // focused field index
	wtParent      string // DirName of parent repo
	wtSplitMode   bool   // true = add as split pane, false = return to manager
	wtOrientation ui.SplitOrientation // orientation for the split

	// Confirm dialog state
	confirmMsg    string
	confirmAction func()

	// Edit panel state
	editFields  []textinput.Model
	editToggles []bool   // remote, favourite, collection
	editFocus   int      // which field is focused (0-2 = text, 3-5 = toggles)
	editDirName string   // which repo we're editing
}

func newModel(cfg *Config, cfgPath string) model {
	repos := DiscoverRepos(cfg)
	scratches := DiscoverScratches(cfg)
	archived := DiscoverArchived(cfg)

	items := make([]repoItem, 0, len(repos)+len(scratches)+len(archived))
	for _, r := range repos {
		items = append(items, repoItem{repo: r})
		// Discover worktrees for this repo
		for _, wt := range DiscoverWorktrees(r) {
			items = append(items, repoItem{repo: wt})
		}
	}
	for _, r := range scratches {
		items = append(items, repoItem{repo: r})
	}
	for _, r := range archived {
		items = append(items, repoItem{repo: r})
	}

	fi := textinput.New()
	fi.Prompt = "/ "
	fi.Placeholder = "filter..."

	pr := textinput.New()
	pr.Prompt = "name: "
	pr.Placeholder = "new-project-name"

	m := model{
		cfg:         cfg,
		cfgPath:     cfgPath,
		items:       items,
		keys:        newKeyMap(),
		filter:      fi,
		promote:     pr,
		filtered:    allIndicesFor(len(items)),
		alerts:      make(map[string]string),
		tabFlashing: make(map[string]string),
		manager:     ui.NewManagerView(),
		workspace:   ui.NewWorkspaceView(),
		chord:       NewChordHandler(500 * time.Millisecond),
		mode:        viewManager,
	}
	m.rebuildDisplayOrder()
	return m
}

func allIndicesFor(n int) []int {
	idx := make([]int, n)
	for i := range idx {
		idx[i] = i
	}
	return idx
}

func (m *model) allIndices() []int {
	return allIndicesFor(len(m.items))
}

// rebuildDisplayOrder groups filtered items into active > favourites > rest
// Children stay with their parent collection regardless of section
func (m *model) rebuildDisplayOrder() {
	// First, figure out which section each parent belongs to
	// A parent promotes to "active" if it or any child has an active session
	parentSection := make(map[string]int) // 0=active, 1=fav, 2=rest
	for _, idx := range m.filtered {
		item := m.items[idx]
		if item.repo.Parent != "" {
			continue // skip children in first pass
		}
		hasInteractiveTab := item.status == statusClaude || item.status == statusShell ||
			item.status == statusTelegram ||
			(item.status == statusRemote && TmuxHasSession(TmuxSessionName(item.repo.DirName, false)))
		switch {
		case hasInteractiveTab:
			parentSection[item.repo.DirName] = 0
		case item.repo.Favourite:
			parentSection[item.repo.DirName] = 1
		default:
			parentSection[item.repo.DirName] = 2
		}
	}
	var active, favourites, rest, archived []int
	for _, idx := range m.filtered {
		item := m.items[idx]

		if item.repo.IsArchived {
			if m.showArchived {
				archived = append(archived, idx)
			}
			continue
		}

		// Active children break out of their parent's section
		hasInteractiveTab := item.status == statusClaude || item.status == statusShell ||
			item.status == statusTelegram ||
			(item.status == statusRemote && TmuxHasSession(TmuxSessionName(item.repo.DirName, false)))

		if hasInteractiveTab {
			active = append(active, idx)
			continue
		}

		// Inactive children follow their parent's section
		if item.repo.Parent != "" {
			switch parentSection[item.repo.Parent] {
			case 0:
				active = append(active, idx)
			case 1:
				favourites = append(favourites, idx)
			default:
				rest = append(rest, idx)
			}
			continue
		}

		switch {
		case item.repo.Favourite:
			favourites = append(favourites, idx)
		default:
			rest = append(rest, idx)
		}
	}
	m.displayOrder = nil
	m.displayOrder = append(m.displayOrder, active...)
	m.displayOrder = append(m.displayOrder, favourites...)
	m.displayOrder = append(m.displayOrder, rest...)
	m.displayOrder = append(m.displayOrder, archived...)

	// Store section for each item so the view doesn't re-derive it
	m.itemSection = make(map[int]string, len(m.displayOrder))
	for _, idx := range active {
		m.itemSection[idx] = "active"
	}
	for _, idx := range favourites {
		m.itemSection[idx] = "favourites"
	}
	for _, idx := range rest {
		m.itemSection[idx] = "repos"
	}
	for _, idx := range archived {
		m.itemSection[idx] = "archived"
	}
}

// reloadItems rescans repos from disk, preserving session state
func (m *model) reloadItems() {
	// Save current session state by DirName
	sessionState := make(map[string]repoItem)
	for _, item := range m.items {
		if item.status != statusNone || item.tmuxSes != "" {
			sessionState[item.repo.DirName] = item
		}
	}

	// Rescan
	repos := DiscoverRepos(m.cfg)
	scratches := DiscoverScratches(m.cfg)

	m.items = make([]repoItem, 0, len(repos)+len(scratches))
	for _, r := range repos {
		item := repoItem{repo: r}
		// Restore session state
		if prev, ok := sessionState[r.DirName]; ok {
			item.status = prev.status
			item.tmuxSes = prev.tmuxSes
			item.title = prev.title
		}
		m.items = append(m.items, item)
	}
	for _, r := range scratches {
		item := repoItem{repo: r}
		if prev, ok := sessionState[r.DirName]; ok {
			item.status = prev.status
			item.tmuxSes = prev.tmuxSes
			item.title = prev.title
		}
		m.items = append(m.items, item)
	}

	m.filtered = m.allIndices()
	m.rebuildDisplayOrder()
	if m.cursor >= len(m.displayOrder) {
		m.cursor = max(0, len(m.displayOrder)-1)
	}
}

func (m *model) selectedItem() *repoItem {
	if len(m.displayOrder) == 0 {
		return nil
	}
	if m.cursor < 0 || m.cursor >= len(m.displayOrder) {
		return nil
	}
	return &m.items[m.displayOrder[m.cursor]]
}

// --- Messages ---

// Mouse interaction messages
type itemClickMsg struct{ index int }
type keyBarClickMsg struct{ action string }
type scrollMsg struct{ dir int }

type tickMsg time.Time

func healthTick() tea.Cmd {
	return tea.Tick(5*time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

type flashRestoreMsg struct{}

type sessionFlashRestoreMsg struct {
	short string
	color string
}

func sessionFlashRestore(short, color string) tea.Cmd {
	return tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
		return sessionFlashRestoreMsg{short: short, color: color}
	})
}

func flashRestore() tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg {
		return flashRestoreMsg{}
	})
}

type reconnectMsg struct{}
type captureTickMsg struct{}

func captureTick() tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg {
		return captureTickMsg{}
	})
}

func gitDiff(repoPath string) (string, error) {
	cmd := exec.Command("git", "-C", repoPath, "diff")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// --- Tea interface ---

func (m model) Init() tea.Cmd {
	return tea.Batch(healthTick(), waitForEvent(), captureTick(), func() tea.Msg {
		return reconnectMsg{}
	})
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.manager.SetSize(msg.Width, msg.Height)
		m.workspace.SetSize(msg.Width, msg.Height)
		return m, nil
	case captureTickMsg:
		m.updateCaptures()
		return m, captureTick()
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	case tea.PasteMsg:
		if m.mode == viewWorkspace {
			if sesName := m.workspace.FocusedSessionName(); sesName != "" {
				TmuxSendLiteral(sesName, msg.Content)
			}
		}
		return m, nil
	case tickMsg:
		return m.handleTick()
	case flashRestoreMsg:
		m.flashing = false
		return m, nil
	case sessionFlashRestoreMsg:
		return m, nil
	case reconnectMsg:
		m.reconnectSessions()
		return m, nil
	case itemClickMsg:
		if msg.index >= 0 && msg.index < len(m.displayOrder) {
			m.cursor = msg.index
		}
		return m, nil
	case keyBarClickMsg:
		return m.handleAction(msg.action)
	case sessionEventMsg:
		cmd := m.handleSessionEvent(SessionEvent(msg))
		return m, tea.Batch(cmd, waitForEvent()) // listen for next event
	case scrollMsg:
		if m.mode == viewWorkspace {
			if term := m.focusedTerminal(); term != nil {
				if msg.dir < 0 {
					// First scroll up: grab full scrollback
					if !term.IsScrolledUp() {
						sesName := m.workspace.FocusedSessionName()
						if sesName != "" {
							if content, err := TmuxCapturePaneFull(sesName); err == nil {
								term.SetFullContent(content)
							}
						}
					}
					term.ScrollUp(3)
				} else {
					term.ScrollDown(3)
				}
			}
			return m, nil
		}
		if msg.dir < 0 && m.cursor > 0 {
			m.cursor--
		} else if msg.dir > 0 && m.cursor < len(m.displayOrder)-1 {
			m.cursor++
		}
		return m, nil
	}

	// Worktree panel
	if m.mode == viewWorktree {
		// Pass to text inputs first
		if m.wtFocus < wtFieldCount {
			var cmd tea.Cmd
			m.wtFields[m.wtFocus], cmd = m.wtFields[m.wtFocus].Update(msg)
			return m, cmd
		}
		return m, nil
	}

	// Pass through to sub-components
	if m.filtering {
		var cmd tea.Cmd
		m.filter, cmd = m.filter.Update(msg)
		m.applyFilter()
		return m, cmd
	}
	if m.mode == viewPromote {
		var cmd tea.Cmd
		m.promote, cmd = m.promote.Update(msg)
		return m, cmd
	}
	if m.mode == viewEdit && m.editFocus < len(m.editFields) {
		var cmd tea.Cmd
		m.editFields[m.editFocus], cmd = m.editFields[m.editFocus].Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	// Global
	if key == "ctrl+c" {
		return m, tea.Quit
	}

	// Workspace mode: chord handling and key forwarding
	if m.mode == viewWorkspace {
		if m.chord.Pending() {
			action, ok := m.chord.Complete(key)
			if ok {
				return m.handleChordAction(action)
			}
			// Unknown chord key — forward to session
			m.chord.Cancel()
			sesName := m.workspace.FocusedSessionName()
			if sesName != "" {
				TmuxSendRawKeys(sesName, key)
			}
			return m, nil
		}
		// Ctrl+Space (NUL byte) starts a chord
		if key == "ctrl+@" || key == "ctrl+space" {
			m.chord.Start()
			return m, nil
		}
		// If scrolled up, snap to live on any keypress
		if term := m.focusedTerminal(); term != nil && term.IsScrolledUp() {
			term.ScrollOffset = 0
		}
		// Forward all other keys to focused session
		sesName := m.workspace.FocusedSessionName()
		if sesName != "" {
			TmuxSendRawKeys(sesName, key)
		}
		return m, nil
	}

	// Edit mode
	if m.mode == viewEdit {
		return m.handleEditKey(msg)
	}

	// Worktree mode
	if m.mode == viewWorktree {
		return m.handleWorktreeKey(key)
	}

	// Help mode
	if m.mode == viewHelp {
		if key == "?" || key == "escape" {
			m.mode = viewManager
		}
		return m, nil
	}

	// Confirm mode (y/n)
	if m.mode == viewConfirm {
		switch key {
		case "y", "Y", "enter":
			if m.confirmAction != nil {
				m.confirmAction()
			}
			m.mode = viewManager
		case "n", "N", "escape":
			m.mode = viewManager
		}
		return m, nil
	}

	// Promote mode
	if m.mode == viewPromote {
		switch key {
		case "enter":
			name := m.promote.Value()
			if name != "" {
				m.promoteSelected(name)
			}
			m.mode = viewManager
			m.promote.SetValue("")
			return m, nil
		case "escape":
			m.mode = viewManager
			m.promote.SetValue("")
			return m, nil
		}
		var cmd tea.Cmd
		m.promote, cmd = m.promote.Update(msg)
		return m, cmd
	}

	// Filter mode
	if m.filtering {
		switch key {
		case "enter":
			m.filtering = false
			m.filter.Blur()
			return m, nil
		case "escape":
			m.filtering = false
			m.filter.SetValue("")
			m.filter.Blur()
			m.filtered = m.allIndices()
			m.rebuildDisplayOrder()
			m.cursor = 0
			return m, nil
		case "up":
			if m.cursor > 0 {
				m.cursor--
			}
			return m, nil
		case "down":
			if m.cursor < len(m.displayOrder)-1 {
				m.cursor++
			}
			return m, nil
		}
		var cmd tea.Cmd
		m.filter, cmd = m.filter.Update(msg)
		m.applyFilter()
		return m, cmd
	}

	// List mode
	switch key {
	case "q":
		return m, tea.Quit
	case "?":
		m.mode = viewHelp
	case "/":
		m.filtering = true
		return m, m.filter.Focus()
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.displayOrder)-1 {
			m.cursor++
		}
	case "enter":
		return m, m.openSelected(true)
	case "shift+enter":
		return m, m.openSelected(false)
	case "r":
		return m, m.toggleRemote()
	case "R":
		m.toggleRemoteFlag()
		return m, nil
	case "F":
		m.toggleFavouriteFlag()
		return m, nil
	case "Y":
		m.toggleYoloFlag()
		return m, nil
	case "w":
		return m, m.openWorktreePanel()
	case "E":
		return m, m.openEditPanel()
	case "s":
		return m, m.createScratch()
	case "p":
		item := m.selectedItem()
		if item != nil && item.repo.IsScratch {
			m.mode = viewPromote
			m.promote.SetValue("")
			return m, m.promote.Focus()
		}
	case "A":
		return m, m.archiveSelected()
	case "U":
		return m, m.unarchiveSelected()
	case "ctrl+a":
		m.showArchived = !m.showArchived
		m.filtered = m.allIndices()
		m.rebuildDisplayOrder()
		if m.cursor >= len(m.displayOrder) {
			m.cursor = max(0, len(m.displayOrder)-1)
		}
	case "tab":
		m.manager.Preview.ToggleTab()
		return m, nil
	case "x":
		return m, m.killSelected()
	case "d":
		return m, m.detachSelected()
	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		// Number keys reserved for future use
	}

	return m, nil
}

// handleAction dispatches a named action (used by mouse click on key bar).
func (m model) handleAction(action string) (tea.Model, tea.Cmd) {
	switch action {
	case "q":
		return m, tea.Quit
	case "?":
		m.mode = viewHelp
	case "/":
		m.filtering = true
		return m, m.filter.Focus()
	case "enter":
		return m, m.openSelected(true)
	case "shift+enter":
		return m, m.openSelected(false)
	case "r":
		return m, m.toggleRemote()
	case "R":
		m.toggleRemoteFlag()
	case "F":
		m.toggleFavouriteFlag()
	case "Y":
		m.toggleYoloFlag()
	case "E":
		return m, m.openEditPanel()
	case "w":
		return m, m.openWorktreePanel()
	case "s":
		return m, m.createScratch()
	case "p":
		item := m.selectedItem()
		if item != nil && item.repo.IsScratch {
			m.mode = viewPromote
			m.promote.SetValue("")
			return m, m.promote.Focus()
		}
	case "tab":
		m.manager.Preview.ToggleTab()
		return m, nil
	case "A":
		return m, m.archiveSelected()
	case "U":
		return m, m.unarchiveSelected()
	case "x":
		return m, m.killSelected()
	case "d":
		return m, m.detachSelected()
	}
	return m, nil
}

func (m *model) handleSessionEvent(ev SessionEvent) tea.Cmd {
	shouldNotify := NotifySessionEvent(&m.cfg.Notifications, ev)

	// Find the item matching this session
	for i := range m.items {
		item := &m.items[i]
		interactiveName := TmuxSessionName(item.repo.DirName, false)
		if interactiveName != ev.Session {
			continue
		}
		if item.richStatus == nil {
			item.richStatus = &SessionStatus{}
		}
		rs := item.richStatus
		rs.Session = ev.Session
		rs.Repo = ev.Repo

		switch ev.Event {
		case "started":
			rs.Status = "running"
			rs.ToolCount = 0
			// User started new work — clear completion flash
			if m.tabFlashing[item.repo.DirName] == "complete" {
				delete(m.tabFlashing, item.repo.DirName)
			}
		case "tool":
			rs.Status = "running"
			rs.ToolCount++
			rs.LastTool = ev.ToolName
			// Tool use means user responded — clear completion flash
			if m.tabFlashing[item.repo.DirName] == "complete" {
				delete(m.tabFlashing, item.repo.DirName)
			}
		case "completed":
			rs.Status = "completed"
			if shouldNotify {
				m.tabFlashing[item.repo.DirName] = "complete"
				m.manager.NotifyLog.Add(item.repo.DirName, "completed", time.Now())
				m.workspace.TabBar.SetFlashing(item.repo.DirName, true)
			}
		case "ended":
			rs.Status = "ended"
			m.manager.NotifyLog.Add(item.repo.DirName, "ended", time.Now())
		}
		return nil
	}
	return nil
}

func (m model) handleTick() (tea.Model, tea.Cmd) {
	sessions, err := TmuxListSessions()
	if err != nil {
		return m, healthTick()
	}

	liveSessions := make(map[string]bool)
	for _, s := range sessions {
		liveSessions[s.Name] = true
	}

	deadAlerts := DetectDeadSessions(m.items, liveSessions)
	remoteAlerts := DetectDeadRemotes(m.items, liveSessions)

	newAlerts := make(map[string]string)
	for k, v := range deadAlerts {
		newAlerts[k] = v
	}
	for k, v := range remoteAlerts {
		newAlerts[k] = v
	}

	hasNewHighSeverity := false
	for k, v := range newAlerts {
		if _, existed := m.alerts[k]; !existed {
			hasNewHighSeverity = true
			m.manager.NotifyLog.Add(k, "crashed", time.Now())
			m.workspace.TabBar.SetFlashing(k, true)
			for i := range m.items {
				if m.items[i].repo.DirName == k && v == "session crashed" {
					m.items[i].status = statusDead
				}
			}
		}
	}
	m.alerts = newAlerts

	// Clear rich status for dead sessions
	for i := range m.items {
		item := &m.items[i]
		if item.status == statusNone || item.status == statusDead {
			item.richStatus = nil
			item.bridgeEntry = nil
		}
	}

	// Read bridge registry and mark telegram-driven sessions
	bridge := ReadBridgeRegistry()
	for i := range m.items {
		item := &m.items[i]
		dirName := item.repo.DirName
		if entry, ok := bridge[dirName]; ok && entry.Driver == "telegram" {
			item.bridgeEntry = &entry
			// Only show as telegram if not already running interactively
			if item.status == statusNone || item.status == statusShell {
				item.status = statusTelegram
				item.tmuxSes = TmuxSessionName(dirName, false)
			}
		} else {
			// Clear stale bridge entries
			if item.status == statusTelegram {
				item.status = statusNone
			}
			item.bridgeEntry = nil
		}
	}

	// Update tab titles from tmux pane titles (interactive sessions only)
	for i := range m.items {
		item := &m.items[i]
		if item.tmuxSes == "" {
			continue
		}
		// Only update interactive session tabs, not remote-control tabs
		interactiveName := TmuxSessionName(item.repo.DirName, false)
		if !TmuxHasSession(interactiveName) {
			continue
		}
		title, err := TmuxPaneTitle(interactiveName)
		if err == nil && title != item.title {
			item.title = title
		}
	}

	// Track bell state for sessions
	for i := range m.items {
		item := &m.items[i]
		flashReason := m.tabFlashing[item.repo.DirName]

		if item.status != statusClaude && item.status != statusTelegram {
			if flashReason != "" {
				m.clearFlash(item)
			}
			continue
		}

		interactiveName := TmuxSessionName(item.repo.DirName, false)
		if TmuxWindowHasBell(interactiveName) {
			if flashReason != "bell" {
				m.tabFlashing[item.repo.DirName] = "bell"
				m.manager.NotifyLog.Add(item.repo.DirName, "waiting", time.Now())
				m.workspace.TabBar.SetFlashing(item.repo.DirName, true)
			}
		} else if flashReason == "bell" {
			m.clearFlash(item)
		}
	}

	var cmds []tea.Cmd
	if hasNewHighSeverity {
		if m.cfg.Notifications.TabFlash && !m.flashing {
			m.flashing = true
			cmds = append(cmds, flashRestore())
		}
		if m.cfg.Notifications.Desktop {
			for k, v := range newAlerts {
				if _, existed := m.alerts[k]; !existed {
					sendDesktopNotification(k, v)
				}
			}
		}
	}

	cmds = append(cmds, healthTick())
	return m, tea.Batch(cmds...)
}

func (m *model) applyFilter() {
	query := strings.ToLower(m.filter.Value())
	if query == "" {
		m.filtered = m.allIndices()
		m.rebuildDisplayOrder()
		m.cursor = 0
		return
	}
	var matched []int
	for i, item := range m.items {
		name := strings.ToLower(item.repo.Name)
		dirName := strings.ToLower(item.repo.DirName)
		if fuzzyMatch(query, name) || fuzzyMatch(query, dirName) {
			matched = append(matched, i)
		}
	}
	m.filtered = matched
	m.rebuildDisplayOrder()
	if m.cursor >= len(m.displayOrder) {
		m.cursor = max(0, len(m.displayOrder)-1)
	}
}

func fuzzyMatch(query, target string) bool {
	qi := 0
	for i := 0; i < len(target) && qi < len(query); i++ {
		if target[i] == query[qi] {
			qi++
		}
	}
	return qi == len(query)
}

// handleChordAction executes a workspace chord action.
func (m model) handleChordAction(action ChordAction) (tea.Model, tea.Cmd) {
	switch action {
	case ChordReturnManager:
		m.mode = viewManager
		m.manager.Preview.Terminal.InvalidateResize()
	case ChordNextTab:
		m.workspace.TabBar.Next()
	case ChordPrevTab:
		m.workspace.TabBar.Prev()
	case ChordJumpTab:
		m.workspace.TabBar.SetActive(m.chord.TabIndex - 1)
	case ChordFocusLeft:
		if tab := m.workspace.ActiveTab(); tab != nil {
			tab.SplitPane.FocusLeft()
		}
	case ChordFocusRight:
		if tab := m.workspace.ActiveTab(); tab != nil {
			tab.SplitPane.FocusRight()
		}
	case ChordVSplit, ChordHSplit:
		tab := m.workspace.ActiveTab()
		if tab == nil {
			m.err = fmt.Errorf("no active tab")
			break
		}
		// Find the repo for this tab — look for the tab ID or its parent
		var item *repoItem
		for i := range m.items {
			if m.items[i].repo.DirName == tab.ID {
				item = &m.items[i]
				break
			}
		}
		// If tab is for a worktree, use its parent for creating new worktrees
		if item != nil && item.repo.IsWorktree && item.repo.Parent != "" {
			for i := range m.items {
				if m.items[i].repo.DirName == item.repo.Parent {
					item = &m.items[i]
					break
				}
			}
		}
		if item == nil {
			m.err = fmt.Errorf("repo not found for tab %q", tab.ID)
			break
		}

		m.wtSplitMode = true
		m.wtParent = item.repo.DirName
		if action == ChordHSplit {
			m.wtOrientation = ui.SplitHorizontal
		} else {
			m.wtOrientation = ui.SplitVertical
		}

		wtCount := 0
		for _, it := range m.items {
			if it.repo.Parent == item.repo.DirName && it.repo.IsWorktree {
				wtCount++
			}
		}
		defaultBranch := fmt.Sprintf("split-%d", wtCount+1)

		fields := make([]textinput.Model, wtFieldCount)
		branchInput := textinput.New()
		branchInput.Prompt = "Branch: "
		branchInput.Placeholder = defaultBranch
		branchInput.SetValue(defaultBranch)
		fields[wtFieldBranch] = branchInput
		promptInput := textinput.New()
		promptInput.Prompt = "Prompt: "
		promptInput.Placeholder = "optional task for Claude"
		fields[wtFieldPrompt] = promptInput
		m.wtFields = fields
		m.wtYolo = item.repo.Yolo
		m.wtFocus = 0
		m.mode = viewWorktree
		return m, m.wtFields[0].Focus()
	case ChordCloseSplit:
		if tab := m.workspace.ActiveTab(); tab != nil {
			if split := tab.SplitPane.FocusedSplit(); split != nil {
				// Kill the tmux session
				if split.SessionName != "" {
					TmuxKillSession(split.SessionName)
				}
				// Update item status
				for i := range m.items {
					if m.items[i].tmuxSes == split.SessionName {
						m.items[i].status = statusNone
						m.items[i].tmuxSes = ""
						break
					}
				}
				tab.SplitPane.RemoveSplit(split.Label)
			}
			if len(tab.SplitPane.Splits) == 0 {
				m.workspace.CloseTab(tab.ID)
				m.rebuildDisplayOrder()
				if len(m.workspace.Tabs) == 0 {
					m.mode = viewManager
					m.manager.Preview.Terminal.InvalidateResize()
				}
			}
		}
	case ChordFullScreen:
		// Full-screen attach is handled separately
		sesName := m.workspace.FocusedSessionName()
		if sesName != "" {
			m.mode = viewAttach
			return m, func() tea.Msg {
				ui.AttachSession(sesName)
				return reconnectMsg{} // return to workspace after detach
			}
		}
	}
	return m, nil
}

// updateCaptures polls tmux capture-pane for visible sessions.
func (m *model) updateCaptures() {
	// Update preview in manager view
	if m.mode == viewManager {
		if sel := m.selectedItem(); sel != nil && sel.tmuxSes != "" {
			m.manager.Preview.SetSession(sel.tmuxSes)
			// Resize tmux pane to match preview dimensions
			tp := m.manager.Preview.Terminal
			if tp.NeedsResize() {
				TmuxResizePane(sel.tmuxSes, tp.InnerWidth(), tp.InnerHeight())
				tp.MarkResized()
			}
			if content, err := TmuxCapturePane(sel.tmuxSes); err == nil {
				tp.SetContent(content)
			}
			if sel.repo.Path != "" {
				if diff, err := gitDiff(sel.repo.Path); err == nil {
					m.manager.Preview.DiffView.SetDiff(diff)
				}
			}
		}
	}

	// Update workspace splits
	if m.mode == viewWorkspace {
		tab := m.workspace.ActiveTab()
		if tab != nil {
			for i := range tab.SplitPane.Splits {
				s := &tab.SplitPane.Splits[i]
				// Resize tmux pane to match split dimensions
				if s.Terminal.NeedsResize() {
					iw, ih := s.Terminal.InnerWidth(), s.Terminal.InnerHeight()
					TmuxResizePane(s.SessionName, iw, ih)
					s.Terminal.MarkResized()
				}
				if content, err := TmuxCapturePane(s.SessionName); err == nil {
					s.Terminal.SetContent(content)
				}
				// Fetch full scrollback when scrolled up
				if s.Terminal.IsScrolledUp() {
					if content, err := TmuxCapturePaneFull(s.SessionName); err == nil {
						s.Terminal.SetFullContent(content)
					}
				}
			}
		}
	}

	// Update diff stats for active sessions
	for i := range m.items {
		item := &m.items[i]
		if item.status == statusClaude || item.status == statusShell {
			if diff, err := gitDiff(item.repo.Path); err == nil {
				added, removed := ui.ParseDiffStats(diff)
				if added > 0 || removed > 0 {
					item.diffStats = fmt.Sprintf("+%d/-%d", added, removed)
				} else {
					item.diffStats = ""
				}
			}
		} else {
			item.diffStats = ""
		}
	}
}

// focusedTerminal returns the TerminalPane of the focused split, or nil.
func (m *model) focusedTerminal() *ui.TerminalPane {
	tab := m.workspace.ActiveTab()
	if tab == nil {
		return nil
	}
	split := tab.SplitPane.FocusedSplit()
	if split == nil {
		return nil
	}
	return split.Terminal
}

// renderWorkspaceStatusBar renders the status bar for workspace view.
func (m model) renderWorkspaceStatusBar() string {
	keys := []string{
		"Ctrl+Space q:manager",
		"n:next-tab",
		"p:prev-tab",
		"←→:focus",
		"x:close",
		"f:fullscreen",
	}
	status := strings.Join(keys, "  ")
	if m.err != nil {
		status += "  " + ui.DeadStyle.Render(m.err.Error())
	}
	return ui.StatusBarStyle.Render(status)
}

