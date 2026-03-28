package main

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/bubbles/v2/textinput"
)

type viewMode int

const (
	viewList viewMode = iota
	viewHelp
	viewPromote
	viewEdit
	viewConfirm
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
	repo      Repo
	status    sessionStatus
	tmuxSes   string         // tmux session name if active
	title     string         // claude session title if available
	richStatus  *SessionStatus // from plugin status file (nil if no status file)
	bridgeEntry *BridgeEntry   // from bridge-sessions.json (nil if not bridged)
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
		cfg:      cfg,
		cfgPath:  cfgPath,
		items:    items,
		keys:     newKeyMap(),
		filter:   fi,
		promote:  pr,
		filtered: allIndicesFor(len(items)),
		alerts:      make(map[string]string),
		tabFlashing: make(map[string]string),
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

		// Archived repos go to their own section (or get filtered out)
		if item.repo.IsArchived {
			if m.showArchived {
				archived = append(archived, idx)
			}
			continue
		}

		// Children follow their parent's section
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

		hasInteractiveTab := item.status == statusClaude || item.status == statusShell ||
			item.status == statusTelegram ||
			(item.status == statusRemote && TmuxHasSession(TmuxSessionName(item.repo.DirName, false)))
		switch {
		case hasInteractiveTab:
			active = append(active, idx)
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

// --- Tea interface ---

func (m model) Init() tea.Cmd {
	return tea.Batch(healthTick(), waitForEvent(), func() tea.Msg {
		return reconnectMsg{}
	})
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	case tickMsg:
		return m.handleTick()
	case flashRestoreMsg:
		m.flashing = false
		kittyRun("@", "set-tab-color", "--self", "active_bg=#ff8c00")
		return m, nil
	case sessionFlashRestoreMsg:
		if msg.color != "" {
			KittySetTabColor(msg.short, msg.color)
		} else {
			KittyResetTabColor(msg.short)
		}
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
		if msg.dir < 0 && m.cursor > 0 {
			m.cursor--
		} else if msg.dir > 0 && m.cursor < len(m.displayOrder)-1 {
			m.cursor++
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

	// Edit mode
	if m.mode == viewEdit {
		return m.handleEditKey(msg)
	}

	// Help mode
	if m.mode == viewHelp {
		if key == "?" || key == "escape" {
			m.mode = viewList
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
			m.mode = viewList
		case "n", "N", "escape":
			m.mode = viewList
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
			m.mode = viewList
			m.promote.SetValue("")
			return m, nil
		case "escape":
			m.mode = viewList
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
		return m, m.focusSelectedTab()
	case "x":
		return m, m.killSelected()
	case "d":
		return m, m.detachSelected()
	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		idx := int(key[0] - '0')
		KittyFocusTabByIndex(idx)
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
		return m, m.focusSelectedTab()
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
				if item.repo.Color != "" {
					KittySetTabColor(item.repo.Short, item.repo.Color)
				} else {
					KittyResetTabColor(item.repo.Short)
				}
			}
		case "tool":
			rs.Status = "running"
			rs.ToolCount++
			rs.LastTool = ev.ToolName
			// Tool use means user responded — clear completion flash
			if m.tabFlashing[item.repo.DirName] == "complete" {
				delete(m.tabFlashing, item.repo.DirName)
				if item.repo.Color != "" {
					KittySetTabColor(item.repo.Short, item.repo.Color)
				} else {
					KittyResetTabColor(item.repo.Short)
				}
			}
		case "completed":
			rs.Status = "completed"
			// Flash the session's tab green — stays until next interaction
			if shouldNotify {
				KittySetTabColor(item.repo.Short, "#00ff88")
				m.tabFlashing[item.repo.DirName] = "complete"
			}
		case "ended":
			rs.Status = "ended"
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
			newTabTitle := item.repo.Short
			if title != "" {
				newTabTitle = item.repo.Short + " — " + title
			}
			KittySetTabTitle("title:^"+item.repo.Short, newTabTitle)
		}
	}

	// Flash session tabs: red for bell (waiting for input), green for completion
	// Check which tab is focused to auto-clear flashes
	var focusedTabTitle string
	if len(m.tabFlashing) > 0 {
		if tabs, err := KittyListTabs(); err == nil {
			for _, tab := range tabs {
				if tab.IsFocused {
					focusedTabTitle = tab.Title
					break
				}
			}
		}
	}

	for i := range m.items {
		item := &m.items[i]
		flashReason := m.tabFlashing[item.repo.DirName]

		if item.status != statusClaude && item.status != statusTelegram {
			if flashReason != "" {
				m.clearFlash(item)
			}
			continue
		}

		// Clear flash if user is currently on this tab
		if flashReason != "" && focusedTabTitle != "" {
			if focusedTabTitle == item.repo.Short || strings.HasPrefix(focusedTabTitle, item.repo.Short+" ") {
				m.clearFlash(item)
				continue
			}
		}

		interactiveName := TmuxSessionName(item.repo.DirName, false)
		if TmuxWindowHasBell(interactiveName) {
			if flashReason != "bell" {
				m.tabFlashing[item.repo.DirName] = "bell"
				KittySetTabColor(item.repo.Short, "#ff0000")
			}
		} else if flashReason == "bell" {
			m.clearFlash(item)
		}
	}

	var cmds []tea.Cmd
	if hasNewHighSeverity {
		if m.cfg.Notifications.TabFlash && !m.flashing {
			m.flashing = true
			kittyRun("@", "set-tab-color", "--self", "active_bg=#ff0000")
			cmds = append(cmds, flashRestore())
		}
		if m.cfg.Notifications.Desktop {
			for k, v := range newAlerts {
				if _, existed := m.alerts[k]; !existed {
					sendDesktopNotification(k, v)
				}
			}
		}
		for k := range newAlerts {
			if _, existed := m.alerts[k]; !existed {
				for _, item := range m.items {
					if item.repo.DirName == k {
						KittySetTabTitle("title:^"+item.repo.Short, "⚠ "+item.repo.Short)
					}
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

