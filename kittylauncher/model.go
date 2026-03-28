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
	items     []repoItem
	cursor    int
	mode      viewMode
	filter    textinput.Model
	filtering bool
	filtered  []int // indices into items that match filter
	promote   textinput.Model
	keys      keyMap
	width     int
	height    int
	err       error
	alerts    map[string]string
	flashing  bool
}

func newModel(cfg *Config) model {
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

	return model{
		cfg:      cfg,
		items:    items,
		keys:     newKeyMap(),
		filter:   fi,
		promote:  pr,
		filtered: allIndicesFor(len(items)),
		alerts:   make(map[string]string),
	}
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

func (m *model) selectedItem() *repoItem {
	if len(m.filtered) == 0 {
		return nil
	}
	if m.cursor < 0 || m.cursor >= len(m.filtered) {
		return nil
	}
	return &m.items[m.filtered[m.cursor]]
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

	return m, nil
}

func (m model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	// Global
	if key == "ctrl+c" {
		return m, tea.Quit
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
		if m.cursor < len(m.filtered)-1 {
			m.cursor++
		}
	case "enter":
		return m, m.openSelected(true)
	case "shift+enter":
		return m, m.openSelected(false)
	case "r":
		return m, m.toggleRemote()
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
	return m, healthTick()
}

func (m *model) applyFilter() {
	query := strings.ToLower(m.filter.Value())
	if query == "" {
		m.filtered = m.allIndices()
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
	if m.cursor >= len(m.filtered) {
		m.cursor = max(0, len(m.filtered)-1)
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

// --- Method stubs (implemented in Tasks 8 and 9) ---

func (m *model) openSelected(withClaude bool) tea.Cmd { return nil }
func (m *model) toggleRemote() tea.Cmd                { return nil }
func (m *model) createScratch() tea.Cmd               { return nil }
func (m *model) focusSelectedTab() tea.Cmd             { return nil }
func (m *model) killSelected() tea.Cmd                 { return nil }
func (m *model) detachSelected() tea.Cmd               { return nil }
func (m *model) reconnectSessions()                    {}
func (m *model) promoteSelected(name string)           {}
