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
)

type sessionStatus int

const (
	statusNone sessionStatus = iota
	statusClaude
	statusShell
	statusRemote
	statusDead
	statusWaiting
)

type repoItem struct {
	repo    Repo
	status  sessionStatus
	tmuxSes string // tmux session name if active
	title   string // claude session title if available
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
	displayOrder []int // indices into items, ordered: active > favourites > rest
	promote      textinput.Model
	keys      keyMap
	width     int
	height    int
	err       error
	alerts    map[string]string
	flashing  bool

	// Edit panel state
	editFields  []textinput.Model
	editToggles []bool   // remote, favourite, collection
	editFocus   int      // which field is focused (0-2 = text, 3-5 = toggles)
	editDirName string   // which repo we're editing
}

func newModel(cfg *Config, cfgPath string) model {
	repos := DiscoverRepos(cfg)
	scratches := DiscoverScratches(cfg)

	items := make([]repoItem, 0, len(repos)+len(scratches))
	for _, r := range repos {
		items = append(items, repoItem{repo: r})
	}
	for _, r := range scratches {
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
		alerts:   make(map[string]string),
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
func (m *model) rebuildDisplayOrder() {
	var active, favourites, rest []int
	for _, idx := range m.filtered {
		item := m.items[idx]
		hasInteractiveTab := item.status == statusClaude || item.status == statusShell ||
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

type tickMsg time.Time

func healthTick() tea.Cmd {
	return tea.Tick(5*time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

type flashRestoreMsg struct{}

func flashRestore() tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg {
		return flashRestoreMsg{}
	})
}

type reconnectMsg struct{}

// --- Tea interface ---

func (m model) Init() tea.Cmd {
	return tea.Batch(healthTick(), func() tea.Msg {
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
		KittySetTabColor("KittyLauncher", "#ff8c00")
		return m, nil
	case reconnectMsg:
		m.reconnectSessions()
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

	var cmds []tea.Cmd
	if hasNewHighSeverity {
		if m.cfg.Notifications.TabFlash && !m.flashing {
			m.flashing = true
			KittySetTabColor("KittyLauncher", "#ff0000")
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

