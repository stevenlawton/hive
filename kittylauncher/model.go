package main

import (
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
	case tea.KeyPressMsg:
		if msg.String() == "q" || msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	}
	return m, nil
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
