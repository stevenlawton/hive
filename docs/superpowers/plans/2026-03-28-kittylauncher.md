# KittyLauncher Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a Go TUI that lives in kitty tab 0 and manages Claude Code workspaces with tmux-backed session persistence.

**Architecture:** Three layers — kitty (tab chrome via IPC), tmux (session persistence), Bubbletea TUI (control hub). The TUI runs without tmux in tab 0; every workspace it opens is a kitty tab attached to a tmux session. Config is auto-discovered from `~/repos` with YAML overrides.

**Tech Stack:** Go, Bubbletea v2 (`charm.land/bubbletea/v2`), Bubbles v2, Lipgloss v2, `gopkg.in/yaml.v3`, kitty remote control IPC, tmux CLI.

**Note on Bubbletea v2:** We use v2 (not v1) because Shift+Enter detection requires the kitty keyboard protocol, which only v2 supports. Import paths: `charm.land/bubbletea/v2`, `github.com/charmbracelet/bubbles/v2/...`, `github.com/charmbracelet/lipgloss/v2`.

---

## File Structure

```
kittylauncher/
├── main.go              # Entry point — loads config, discovers repos, starts Bubbletea
├── config.go            # YAML config loading, defaults, repo auto-discovery
├── config_test.go       # Config loading + discovery tests
├── model.go             # Bubbletea model definition + Init/Update/View routing
├── view.go              # All rendering — repo list, sections, status bar, filter bar
├── keys.go              # Key bindings + help text definitions
├── kitty.go             # kitty @ IPC wrappers (launch, set-tab-title, set-tab-color, etc.)
├── kitty_test.go        # Tests for command construction (not execution)
├── tmux.go              # tmux CLI wrappers (new-session, has-session, list-sessions, etc.)
├── tmux_test.go         # Tests for command construction
├── session.go           # Session lifecycle — create, attach, kill, reconnect, detach
├── session_test.go      # Session logic tests
├── scratch.go           # Scratch creation + promotion
├── scratch_test.go      # Scratch logic tests
├── notify.go            # Health polling tick, alert state, desktop notifications, tab flash
├── notify_test.go       # Notification logic tests
├── launch-kl.sh         # Shell script to start kitty with TUI
└── config.example.yaml  # Example configuration file
```

---

### Task 1: Project Scaffold

**Files:**
- Create: `kittylauncher/main.go`
- Create: `kittylauncher/go.mod`

- [ ] **Step 1: Create go module**

```bash
mkdir -p kittylauncher && cd kittylauncher
go mod init kittylauncher
```

- [ ] **Step 2: Create minimal main.go**

```go
package main

import (
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"
)

type model struct{}

func (m model) Init() tea.Cmd { return nil }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if msg.String() == "q" || msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m model) View() string {
	return "⚡ KittyLauncher\n\nPress q to quit.\n"
}

func main() {
	p := tea.NewProgram(model{})
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
```

- [ ] **Step 3: Fetch dependencies and verify it runs**

```bash
cd kittylauncher
go mod tidy
go run .
```

Expected: TUI shows "⚡ KittyLauncher" and exits on `q`.

- [ ] **Step 4: Commit**

```bash
git init
echo "kittylauncher" > .gitignore
git add main.go go.mod go.sum .gitignore
git commit -m "feat: scaffold kittylauncher project with minimal bubbletea v2 app"
```

---

### Task 2: Configuration Loading & Repo Discovery

**Files:**
- Create: `kittylauncher/config.go`
- Create: `kittylauncher/config_test.go`

- [ ] **Step 1: Write failing tests for config loading**

```go
package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig_Defaults(t *testing.T) {
	cfg, err := LoadConfig("/nonexistent/path/config.yaml")
	if err != nil {
		t.Fatalf("expected no error for missing config, got: %v", err)
	}
	home, _ := os.UserHomeDir()
	if cfg.ReposDir != filepath.Join(home, "repos") {
		t.Errorf("expected default repos_dir %s/repos, got %s", home, cfg.ReposDir)
	}
	if cfg.ScratchDir != "/tmp/kl-scratch" {
		t.Errorf("expected default scratch_dir /tmp/kl-scratch, got %s", cfg.ScratchDir)
	}
	if cfg.DefaultAction != "claude" {
		t.Errorf("expected default_action claude, got %s", cfg.DefaultAction)
	}
}

func TestLoadConfig_ParsesYAML(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	content := []byte(`
repos_dir: /tmp/test-repos
scratch_dir: /tmp/test-scratch
default_action: shell
workspaces:
  myrepo:
    name: "My Repo"
    short: "MR"
    color: "#ff0000"
    remote: true
    favourite: true
`)
	os.WriteFile(cfgPath, content, 0644)

	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ReposDir != "/tmp/test-repos" {
		t.Errorf("expected /tmp/test-repos, got %s", cfg.ReposDir)
	}
	if cfg.DefaultAction != "shell" {
		t.Errorf("expected shell, got %s", cfg.DefaultAction)
	}
	ws, ok := cfg.Workspaces["myrepo"]
	if !ok {
		t.Fatal("expected myrepo workspace")
	}
	if ws.Name != "My Repo" || ws.Short != "MR" || ws.Color != "#ff0000" || !ws.Remote || !ws.Favourite {
		t.Errorf("workspace fields not parsed correctly: %+v", ws)
	}
}

func TestDiscoverRepos(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "repo-a"), 0755)
	os.MkdirAll(filepath.Join(dir, "repo-b"), 0755)
	os.WriteFile(filepath.Join(dir, "not-a-dir.txt"), []byte("hi"), 0644)

	cfg := &Config{
		ReposDir: dir,
		Workspaces: map[string]WorkspaceConfig{
			"repo-a": {Name: "Alpha", Short: "AL", Color: "#ff0000", Favourite: true},
		},
	}

	repos := DiscoverRepos(cfg)
	if len(repos) != 2 {
		t.Fatalf("expected 2 repos, got %d", len(repos))
	}

	// repo-a should have overrides
	var alpha, bravo *Repo
	for i := range repos {
		if repos[i].DirName == "repo-a" {
			alpha = &repos[i]
		}
		if repos[i].DirName == "repo-b" {
			bravo = &repos[i]
		}
	}
	if alpha == nil || bravo == nil {
		t.Fatal("missing expected repos")
	}
	if alpha.Name != "Alpha" || alpha.Short != "AL" {
		t.Errorf("alpha overrides not applied: %+v", alpha)
	}
	if bravo.Name != "repo-b" || bravo.Short != "REP" {
		t.Errorf("bravo defaults not applied: name=%s short=%s", bravo.Name, bravo.Short)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd kittylauncher
go test -run TestLoadConfig -v
go test -run TestDiscoverRepos -v
```

Expected: compilation errors — `LoadConfig`, `DiscoverRepos`, `Config`, `Repo` not defined.

- [ ] **Step 3: Implement config.go**

```go
package main

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type NotificationConfig struct {
	Desktop      bool `yaml:"desktop"`
	TabFlash     bool `yaml:"tab_flash"`
	PollInterval int  `yaml:"poll_interval"`
}

type WorkspaceConfig struct {
	Name      string `yaml:"name"`
	Short     string `yaml:"short"`
	Color     string `yaml:"color"`
	Remote    bool   `yaml:"remote"`
	Favourite bool   `yaml:"favourite"`
}

type Config struct {
	ReposDir      string                     `yaml:"repos_dir"`
	ScratchDir    string                     `yaml:"scratch_dir"`
	DefaultAction string                     `yaml:"default_action"`
	Notifications NotificationConfig         `yaml:"notifications"`
	Workspaces    map[string]WorkspaceConfig `yaml:"workspaces"`
}

func LoadConfig(path string) (*Config, error) {
	home, _ := os.UserHomeDir()
	cfg := &Config{
		ReposDir:      filepath.Join(home, "repos"),
		ScratchDir:    "/tmp/kl-scratch",
		DefaultAction: "claude",
		Notifications: NotificationConfig{
			Desktop:      true,
			TabFlash:     true,
			PollInterval: 5,
		},
		Workspaces: make(map[string]WorkspaceConfig),
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, err
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	// Expand ~ in paths
	if strings.HasPrefix(cfg.ReposDir, "~/") {
		cfg.ReposDir = filepath.Join(home, cfg.ReposDir[2:])
	}
	if strings.HasPrefix(cfg.ScratchDir, "~/") {
		cfg.ScratchDir = filepath.Join(home, cfg.ScratchDir[2:])
	}

	if cfg.Workspaces == nil {
		cfg.Workspaces = make(map[string]WorkspaceConfig)
	}

	return cfg, nil
}

type Repo struct {
	DirName   string // Directory name in repos_dir
	Path      string // Full path
	Name      string // Display name
	Short     string // Short name for tab title
	Color     string // Hex color for tab
	Remote    bool   // Auto-start remote-control
	Favourite bool   // Pin to favourites
	IsScratch bool   // Is a scratch instance
}

func defaultShort(dirName string) string {
	clean := strings.ToUpper(dirName)
	if len(clean) > 3 {
		clean = clean[:3]
	}
	return clean
}

func DiscoverRepos(cfg *Config) []Repo {
	entries, err := os.ReadDir(cfg.ReposDir)
	if err != nil {
		return nil
	}

	var repos []Repo
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dirName := entry.Name()
		repo := Repo{
			DirName: dirName,
			Path:    filepath.Join(cfg.ReposDir, dirName),
			Name:    dirName,
			Short:   defaultShort(dirName),
		}

		if ws, ok := cfg.Workspaces[dirName]; ok {
			if ws.Name != "" {
				repo.Name = ws.Name
			}
			if ws.Short != "" {
				repo.Short = ws.Short
			}
			repo.Color = ws.Color
			repo.Remote = ws.Remote
			repo.Favourite = ws.Favourite
		}

		repos = append(repos, repo)
	}

	sort.Slice(repos, func(i, j int) bool {
		return repos[i].DirName < repos[j].DirName
	})

	return repos
}
```

- [ ] **Step 4: Add yaml dependency and run tests**

```bash
cd kittylauncher
go get gopkg.in/yaml.v3
go test -run TestLoadConfig -v
go test -run TestDiscoverRepos -v
```

Expected: all 3 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add config.go config_test.go go.mod go.sum
git commit -m "feat: add config loading with YAML parsing and repo auto-discovery"
```

---

### Task 3: tmux Wrapper

**Files:**
- Create: `kittylauncher/tmux.go`
- Create: `kittylauncher/tmux_test.go`

- [ ] **Step 1: Write failing tests for tmux command construction**

```go
package main

import (
	"testing"
)

func TestTmuxNewSessionArgs(t *testing.T) {
	args := tmuxNewSessionArgs("kl-slicewise", "/home/steve/repos/SliceWise")
	expected := []string{"new-session", "-d", "-s", "kl-slicewise", "-c", "/home/steve/repos/SliceWise"}
	if len(args) != len(expected) {
		t.Fatalf("expected %d args, got %d: %v", len(expected), len(args), args)
	}
	for i, arg := range expected {
		if args[i] != arg {
			t.Errorf("arg[%d]: expected %q, got %q", i, arg, args[i])
		}
	}
}

func TestTmuxSendKeysArgs(t *testing.T) {
	args := tmuxSendKeysArgs("kl-slicewise", "claude")
	expected := []string{"send-keys", "-t", "kl-slicewise", "claude", "Enter"}
	if len(args) != len(expected) {
		t.Fatalf("expected %d args, got %d: %v", len(expected), len(args), args)
	}
	for i, arg := range expected {
		if args[i] != arg {
			t.Errorf("arg[%d]: expected %q, got %q", i, arg, args[i])
		}
	}
}

func TestTmuxSessionName(t *testing.T) {
	if name := TmuxSessionName("SliceWise", false); name != "kl-SliceWise" {
		t.Errorf("expected kl-SliceWise, got %s", name)
	}
	if name := TmuxSessionName("tgbridge", true); name != "kl-rc-tgbridge" {
		t.Errorf("expected kl-rc-tgbridge, got %s", name)
	}
}

func TestParseTmuxSessions(t *testing.T) {
	output := "kl-slicewise: 1 windows (created Fri Mar 28 10:00:00 2026)\nkl-rc-tgbridge: 1 windows (created Fri Mar 28 10:00:00 2026)\nother-session: 1 windows (created Fri Mar 28 10:00:00 2026)\n"
	sessions := ParseTmuxSessions(output)
	if len(sessions) != 2 {
		t.Fatalf("expected 2 kl- sessions, got %d: %v", len(sessions), sessions)
	}
	if sessions[0].Name != "kl-slicewise" {
		t.Errorf("expected kl-slicewise, got %s", sessions[0].Name)
	}
	if sessions[1].Name != "kl-rc-tgbridge" || !sessions[1].IsRemote {
		t.Errorf("expected kl-rc-tgbridge (remote), got %+v", sessions[1])
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd kittylauncher
go test -run TestTmux -v
```

Expected: compilation errors.

- [ ] **Step 3: Implement tmux.go**

```go
package main

import (
	"fmt"
	"os/exec"
	"strings"
)

const (
	tmuxPrefix       = "kl-"
	tmuxRemotePrefix = "kl-rc-"
	tmuxScratchPfx   = "kl-scratch-"
)

type TmuxSession struct {
	Name      string
	IsRemote  bool
	IsScratch bool
	RepoKey   string // DirName extracted from session name
}

func TmuxSessionName(dirName string, remote bool) string {
	if remote {
		return tmuxRemotePrefix + dirName
	}
	return tmuxPrefix + dirName
}

func tmuxNewSessionArgs(sessionName, cwd string) []string {
	return []string{"new-session", "-d", "-s", sessionName, "-c", cwd}
}

func tmuxSendKeysArgs(sessionName, command string) []string {
	return []string{"send-keys", "-t", sessionName, command, "Enter"}
}

func tmuxHasSessionArgs(sessionName string) []string {
	return []string{"has-session", "-t", sessionName}
}

func tmuxKillSessionArgs(sessionName string) []string {
	return []string{"kill-session", "-t", sessionName}
}

func tmuxListSessionsArgs() []string {
	return []string{"list-sessions"}
}

func tmuxCapturePaneArgs(sessionName string) []string {
	return []string{"display-message", "-t", sessionName, "-p", "#{pane_title}"}
}

func ParseTmuxSessions(output string) []TmuxSession {
	var sessions []TmuxSession
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if line == "" {
			continue
		}
		name := strings.SplitN(line, ":", 2)[0]
		if !strings.HasPrefix(name, tmuxPrefix) {
			continue
		}
		s := TmuxSession{Name: name}
		if strings.HasPrefix(name, tmuxRemotePrefix) {
			s.IsRemote = true
			s.RepoKey = strings.TrimPrefix(name, tmuxRemotePrefix)
		} else if strings.HasPrefix(name, tmuxScratchPfx) {
			s.IsScratch = true
			s.RepoKey = strings.TrimPrefix(name, tmuxPrefix)
		} else {
			s.RepoKey = strings.TrimPrefix(name, tmuxPrefix)
		}
		sessions = append(sessions, s)
	}
	return sessions
}

// Exec helpers — these actually run tmux commands

func tmuxRun(args ...string) error {
	cmd := exec.Command("tmux", args...)
	return cmd.Run()
}

func tmuxOutput(args ...string) (string, error) {
	cmd := exec.Command("tmux", args...)
	out, err := cmd.Output()
	return string(out), err
}

func TmuxNewSession(sessionName, cwd string) error {
	return tmuxRun(tmuxNewSessionArgs(sessionName, cwd)...)
}

func TmuxSendKeys(sessionName, command string) error {
	return tmuxRun(tmuxSendKeysArgs(sessionName, command)...)
}

func TmuxHasSession(sessionName string) bool {
	return tmuxRun(tmuxHasSessionArgs(sessionName)...) == nil
}

func TmuxKillSession(sessionName string) error {
	return tmuxRun(tmuxKillSessionArgs(sessionName)...)
}

func TmuxListSessions() ([]TmuxSession, error) {
	out, err := tmuxOutput(tmuxListSessionsArgs()...)
	if err != nil {
		// tmux returns error when no sessions exist
		if strings.Contains(err.Error(), "no server running") || strings.Contains(string(out), "no server") {
			return nil, nil
		}
		return nil, err
	}
	return ParseTmuxSessions(out), nil
}

func TmuxPaneTitle(sessionName string) (string, error) {
	out, err := tmuxOutput(tmuxCapturePaneArgs(sessionName)...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func TmuxRenameSession(oldName, newName string) error {
	return tmuxRun("rename-session", "-t", oldName, newName)
}

func TmuxSessionCwd(sessionName string) (string, error) {
	out, err := tmuxOutput("display-message", "-t", sessionName, "-p", "#{pane_current_path}")
	if err != nil {
		return "", fmt.Errorf("failed to get cwd for session %s: %w", sessionName, err)
	}
	return strings.TrimSpace(out), nil
}
```

- [ ] **Step 4: Run tests**

```bash
cd kittylauncher
go test -run TestTmux -v
```

Expected: all 4 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add tmux.go tmux_test.go
git commit -m "feat: add tmux CLI wrapper with session name conventions and parsing"
```

---

### Task 4: Kitty IPC Wrapper

**Files:**
- Create: `kittylauncher/kitty.go`
- Create: `kittylauncher/kitty_test.go`

- [ ] **Step 1: Write failing tests for kitty command construction**

```go
package main

import (
	"testing"
)

func TestKittyLaunchTabArgs(t *testing.T) {
	args := kittyLaunchTabArgs("SliceWize", "tmux", "attach", "-t", "kl-slicewise")
	// Should contain: @ launch --type=tab --tab-title SliceWize tmux attach -t kl-slicewise
	if args[0] != "@" || args[1] != "launch" {
		t.Errorf("expected '@ launch', got %v", args[:2])
	}
	found := false
	for i, a := range args {
		if a == "--type=tab" {
			found = true
		}
		if a == "--tab-title" && i+1 < len(args) && args[i+1] == "SliceWize" {
			found = found && true
		}
	}
	if !found {
		t.Errorf("missing --type=tab or --tab-title: %v", args)
	}
}

func TestKittySetTabColorArgs(t *testing.T) {
	args := kittySetTabColorArgs("SliceWize", "#ff6b6b")
	// Should contain: @ set-tab-color --match title:SliceWize active_bg=#ff6b6b
	hasMatch := false
	hasColor := false
	for i, a := range args {
		if a == "--match" && i+1 < len(args) && args[i+1] == "title:^SliceWize" {
			hasMatch = true
		}
		if a == "active_bg=#ff6b6b" {
			hasColor = true
		}
	}
	if !hasMatch || !hasColor {
		t.Errorf("missing match or color args: %v", args)
	}
}

func TestKittySetTabTitleArgs(t *testing.T) {
	args := kittySetTabTitleArgs("title:^SW", "SW — fixing auth")
	hasMatch := false
	hasTitle := false
	for i, a := range args {
		if a == "--match" && i+1 < len(args) && args[i+1] == "title:^SW" {
			hasMatch = true
		}
		if a == "SW — fixing auth" {
			hasTitle = true
		}
	}
	if !hasMatch || !hasTitle {
		t.Errorf("missing match or title: %v", args)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd kittylauncher
go test -run TestKitty -v
```

Expected: compilation errors.

- [ ] **Step 3: Implement kitty.go**

```go
package main

import (
	"encoding/json"
	"os/exec"
	"strings"
)

func kittyCmd(args ...string) *exec.Cmd {
	return exec.Command("kitten", args...)
}

func kittyRun(args ...string) error {
	return kittyCmd(args...).Run()
}

func kittyOutput(args ...string) (string, error) {
	out, err := kittyCmd(args...).Output()
	return string(out), err
}

// Command argument builders

func kittyLaunchTabArgs(tabTitle string, command ...string) []string {
	args := []string{"@", "launch", "--type=tab", "--tab-title", tabTitle}
	args = append(args, command...)
	return args
}

func kittySetTabColorArgs(tabTitle, color string) []string {
	return []string{"@", "set-tab-color", "--match", "title:^" + tabTitle, "active_bg=" + color}
}

func kittySetTabTitleArgs(match, title string) []string {
	return []string{"@", "set-tab-title", "--match", match, title}
}

func kittyFocusTabArgs(match string) []string {
	return []string{"@", "focus-tab", "--match", match}
}

func kittyCloseTabArgs(match string) []string {
	return []string{"@", "close-tab", "--match", match}
}

// Exec helpers

func KittyLaunchTab(tabTitle string, command ...string) error {
	return kittyRun(kittyLaunchTabArgs(tabTitle, command...)...)
}

func KittySetTabColor(tabTitle, color string) error {
	if color == "" {
		return nil
	}
	return kittyRun(kittySetTabColorArgs(tabTitle, color)...)
}

func KittySetTabTitle(match, title string) error {
	return kittyRun(kittySetTabTitleArgs(match, title)...)
}

func KittyFocusTab(match string) error {
	return kittyRun(kittyFocusTabArgs(match)...)
}

func KittyCloseTab(match string) error {
	return kittyRun(kittyCloseTabArgs(match)...)
}

// KittyTabInfo represents a tab from kitty @ ls output
type KittyTabInfo struct {
	ID    int    `json:"id"`
	Title string `json:"title"`
}

type kittyOSWindow struct {
	Tabs []KittyTabInfo `json:"tabs"`
}

func KittyListTabs() ([]KittyTabInfo, error) {
	out, err := kittyOutput("@", "ls")
	if err != nil {
		return nil, err
	}
	var osWindows []kittyOSWindow
	if err := json.Unmarshal([]byte(out), &osWindows); err != nil {
		return nil, err
	}
	var tabs []KittyTabInfo
	for _, osw := range osWindows {
		tabs = append(tabs, osw.Tabs...)
	}
	return tabs, nil
}

// KittyFlashTab toggles tab color between two values for visual alert
func KittyFlashTab(tabTitle, flashColor, restoreColor string) error {
	if err := kittyRun("@", "set-tab-color", "--match", "title:^"+tabTitle, "active_bg="+flashColor); err != nil {
		return err
	}
	// The caller should schedule the restore after a delay via tea.Tick
	return nil
}

func KittyRestoreTabColor(tabTitle, color string) error {
	colorVal := color
	if colorVal == "" {
		colorVal = "NONE"
	}
	return kittyRun("@", "set-tab-color", "--match", "title:^"+tabTitle, "active_bg="+colorVal)
}

func KittyFocusTabByIndex(index int) error {
	return kittyRun("@", "focus-tab", "--match", "index:"+strings.Repeat("", 0)+itoa(index))
}

func itoa(i int) string {
	return strings.TrimSpace(strings.Replace(string(rune('0'+i)), "\x00", "", -1))
}
```

Wait — that `itoa` is ugly. Let me fix:

```go
package main

import (
	"encoding/json"
	"fmt"
	"os/exec"
)

func kittyCmd(args ...string) *exec.Cmd {
	return exec.Command("kitten", args...)
}

func kittyRun(args ...string) error {
	return kittyCmd(args...).Run()
}

func kittyOutput(args ...string) (string, error) {
	out, err := kittyCmd(args...).Output()
	return string(out), err
}

// Command argument builders

func kittyLaunchTabArgs(tabTitle string, command ...string) []string {
	args := []string{"@", "launch", "--type=tab", "--tab-title", tabTitle}
	args = append(args, command...)
	return args
}

func kittySetTabColorArgs(tabTitle, color string) []string {
	return []string{"@", "set-tab-color", "--match", "title:^" + tabTitle, "active_bg=" + color}
}

func kittySetTabTitleArgs(match, title string) []string {
	return []string{"@", "set-tab-title", "--match", match, title}
}

func kittyFocusTabArgs(match string) []string {
	return []string{"@", "focus-tab", "--match", match}
}

func kittyCloseTabArgs(match string) []string {
	return []string{"@", "close-tab", "--match", match}
}

// Exec helpers

func KittyLaunchTab(tabTitle string, command ...string) error {
	return kittyRun(kittyLaunchTabArgs(tabTitle, command...)...)
}

func KittySetTabColor(tabTitle, color string) error {
	if color == "" {
		return nil
	}
	return kittyRun(kittySetTabColorArgs(tabTitle, color)...)
}

func KittySetTabTitle(match, title string) error {
	return kittyRun(kittySetTabTitleArgs(match, title)...)
}

func KittyFocusTab(match string) error {
	return kittyRun(kittyFocusTabArgs(match)...)
}

func KittyCloseTab(match string) error {
	return kittyRun(kittyCloseTabArgs(match)...)
}

// KittyTabInfo represents a tab from kitty @ ls output
type KittyTabInfo struct {
	ID    int    `json:"id"`
	Title string `json:"title"`
}

type kittyOSWindow struct {
	Tabs []KittyTabInfo `json:"tabs"`
}

func KittyListTabs() ([]KittyTabInfo, error) {
	out, err := kittyOutput("@", "ls")
	if err != nil {
		return nil, err
	}
	var osWindows []kittyOSWindow
	if err := json.Unmarshal([]byte(out), &osWindows); err != nil {
		return nil, err
	}
	var tabs []KittyTabInfo
	for _, osw := range osWindows {
		tabs = append(tabs, osw.Tabs...)
	}
	return tabs, nil
}

func KittyFocusTabByIndex(index int) error {
	return kittyRun("@", "focus-tab", "--match", fmt.Sprintf("index:%d", index))
}
```

- [ ] **Step 4: Run tests**

```bash
cd kittylauncher
go test -run TestKitty -v
```

Expected: all 3 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add kitty.go kitty_test.go
git commit -m "feat: add kitty IPC wrapper for tab management"
```

---

### Task 5: Keybindings

**Files:**
- Create: `kittylauncher/keys.go`

- [ ] **Step 1: Define keybindings**

```go
package main

import (
	"github.com/charmbracelet/bubbles/v2/key"
)

type keyMap struct {
	Open        key.Binding
	OpenShell   key.Binding
	Remote      key.Binding
	Scratch     key.Binding
	Promote     key.Binding
	FocusTab    key.Binding
	Kill        key.Binding
	Detach      key.Binding
	Filter      key.Binding
	Help        key.Binding
	Quit        key.Binding
	Tab1        key.Binding
	Tab2        key.Binding
	Tab3        key.Binding
	Tab4        key.Binding
	Tab5        key.Binding
	Tab6        key.Binding
	Tab7        key.Binding
	Tab8        key.Binding
	Tab9        key.Binding
}

func newKeyMap() keyMap {
	return keyMap{
		Open: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "open + claude"),
		),
		OpenShell: key.NewBinding(
			key.WithKeys("shift+enter"),
			key.WithHelp("shift+enter", "open + shell"),
		),
		Remote: key.NewBinding(
			key.WithKeys("r"),
			key.WithHelp("r", "toggle remote"),
		),
		Scratch: key.NewBinding(
			key.WithKeys("s"),
			key.WithHelp("s", "new scratch"),
		),
		Promote: key.NewBinding(
			key.WithKeys("p"),
			key.WithHelp("p", "promote scratch"),
		),
		FocusTab: key.NewBinding(
			key.WithKeys("tab"),
			key.WithHelp("tab", "focus tab"),
		),
		Kill: key.NewBinding(
			key.WithKeys("x"),
			key.WithHelp("x", "kill session"),
		),
		Detach: key.NewBinding(
			key.WithKeys("d"),
			key.WithHelp("d", "detach tab"),
		),
		Filter: key.NewBinding(
			key.WithKeys("/"),
			key.WithHelp("/", "filter"),
		),
		Help: key.NewBinding(
			key.WithKeys("?"),
			key.WithHelp("?", "help"),
		),
		Quit: key.NewBinding(
			key.WithKeys("q"),
			key.WithHelp("q", "quit"),
		),
		Tab1: key.NewBinding(key.WithKeys("1"), key.WithHelp("1-9", "jump to tab")),
		Tab2: key.NewBinding(key.WithKeys("2")),
		Tab3: key.NewBinding(key.WithKeys("3")),
		Tab4: key.NewBinding(key.WithKeys("4")),
		Tab5: key.NewBinding(key.WithKeys("5")),
		Tab6: key.NewBinding(key.WithKeys("6")),
		Tab7: key.NewBinding(key.WithKeys("7")),
		Tab8: key.NewBinding(key.WithKeys("8")),
		Tab9: key.NewBinding(key.WithKeys("9")),
	}
}

// ShortHelp returns keybindings for the compact help bar
func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Open, k.OpenShell, k.Remote, k.Scratch, k.Help, k.Quit}
}

// FullHelp returns keybindings for the expanded help view
func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Open, k.OpenShell, k.Remote},
		{k.Scratch, k.Promote, k.FocusTab},
		{k.Kill, k.Detach, k.Filter},
		{k.Tab1, k.Help, k.Quit},
	}
}
```

- [ ] **Step 2: Verify it compiles**

```bash
cd kittylauncher
go mod tidy
go build ./...
```

Expected: compiles successfully.

- [ ] **Step 3: Commit**

```bash
git add keys.go go.mod go.sum
git commit -m "feat: add keybinding definitions with help text"
```

---

### Task 6: Core Model & View

**Files:**
- Create: `kittylauncher/model.go`
- Create: `kittylauncher/view.go`
- Modify: `kittylauncher/main.go`

This is the central task — the Bubbletea model that ties everything together. We build the list view with sections, the filter input, and the help overlay.

- [ ] **Step 1: Implement model.go**

```go
package main

import (
	"time"

	"github.com/charmbracelet/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

type viewMode int

const (
	viewList viewMode = iota
	viewHelp
	viewPromote // Inline text input for scratch promotion
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

	// Alert state
	alerts    map[string]string // repoKey -> alert message
	flashing  bool
}

type tickMsg time.Time

func healthTick(interval int) tea.Cmd {
	return tea.Tick(time.Duration(interval)*time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

type flashRestoreMsg struct{}

func flashRestore() tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg {
		return flashRestoreMsg{}
	})
}

func newModel(cfg *Config) model {
	repos := DiscoverRepos(cfg)
	items := make([]repoItem, len(repos))
	for i, r := range repos {
		items[i] = repoItem{repo: r}
	}

	// Also discover scratch instances
	scratches := DiscoverScratches(cfg)
	for _, s := range scratches {
		items = append(items, repoItem{repo: s})
	}

	fi := textinput.New()
	fi.Placeholder = "filter..."
	fi.CharLimit = 64
	fi.Width = 40

	pi := textinput.New()
	pi.Placeholder = "repo name..."
	pi.CharLimit = 64
	pi.Width = 40

	m := model{
		cfg:     cfg,
		items:   items,
		keys:    newKeyMap(),
		filter:  fi,
		promote: pi,
		alerts:  make(map[string]string),
	}
	m.filtered = m.allIndices()
	return m
}

func (m model) allIndices() []int {
	idx := make([]int, len(m.items))
	for i := range idx {
		idx[i] = i
	}
	return idx
}

func (m model) selectedItem() *repoItem {
	if m.cursor < 0 || m.cursor >= len(m.filtered) {
		return nil
	}
	return &m.items[m.filtered[m.cursor]]
}

func (m model) Init() tea.Cmd {
	return tea.Batch(
		healthTick(m.cfg.Notifications.PollInterval),
		m.reconnectExisting(),
	)
}

func (m model) reconnectExisting() tea.Cmd {
	return func() tea.Msg {
		return reconnectMsg{}
	}
}

type reconnectMsg struct{}
```

- [ ] **Step 2: Implement view.go**

```go
package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss/v2"
)

var (
	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#ff8c00")).Padding(1, 0, 0, 1)
	subtitleStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#888888")).Padding(0, 0, 0, 1)
	cursorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#00ff88")).Bold(true)
	nameStyle     = lipgloss.NewStyle().Bold(true).Width(24)
	statusStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#888888")).Width(20)
	remoteStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#ff8c00"))
	scratchStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#555555")).Italic(true)
	deadStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#ff0000")).Bold(true)
	waitStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#ffff00"))
	helpStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#555555")).Padding(1, 0, 0, 1)
	barKeyStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#ff8c00"))
	barValStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))
	sectionStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#555555")).Bold(true).Padding(0, 0, 0, 1)
	dividerStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#333333"))
)

func (m model) View() string {
	if m.mode == viewHelp {
		return m.viewHelp()
	}

	var b strings.Builder

	// Title
	b.WriteString(titleStyle.Render("⚡ KittyLauncher"))
	b.WriteString("\n")

	// Subtitle with counts
	activeCount := 0
	remoteCount := 0
	for _, idx := range m.filtered {
		item := m.items[idx]
		if item.status != statusNone {
			activeCount++
		}
		if item.status == statusRemote {
			remoteCount++
		}
	}
	sub := fmt.Sprintf("%s  ·  %d active  ·  %d remote", m.cfg.ReposDir, activeCount, remoteCount)
	b.WriteString(subtitleStyle.Render(sub))
	b.WriteString("\n")
	b.WriteString(dividerStyle.Render(strings.Repeat("─", min(m.width, 60))))
	b.WriteString("\n")

	// Repo list — grouped into sections
	active, favourites, rest := m.groupItems()

	if len(active) > 0 {
		b.WriteString(sectionStyle.Render("ACTIVE"))
		b.WriteString("\n")
		for _, vi := range active {
			b.WriteString(m.renderItem(vi))
		}
	}
	if len(favourites) > 0 {
		b.WriteString(sectionStyle.Render("FAVOURITES"))
		b.WriteString("\n")
		for _, vi := range favourites {
			b.WriteString(m.renderItem(vi))
		}
	}
	if len(rest) > 0 {
		b.WriteString(sectionStyle.Render("ALL"))
		b.WriteString("\n")
		for _, vi := range rest {
			b.WriteString(m.renderItem(vi))
		}
	}

	b.WriteString(dividerStyle.Render(strings.Repeat("─", min(m.width, 60))))
	b.WriteString("\n")

	// Filter bar or promote input
	if m.mode == viewPromote {
		b.WriteString(" Promote to ~/repos/")
		b.WriteString(m.promote.View())
		b.WriteString("\n")
	} else if m.filtering {
		b.WriteString(" Filter: ")
		b.WriteString(m.filter.View())
		b.WriteString("\n")
	} else {
		b.WriteString(m.renderKeyBar())
	}

	return b.String()
}

type viewItem struct {
	filteredIdx int
	item        repoItem
}

func (m model) groupItems() (active, favourites, rest []viewItem) {
	for i, idx := range m.filtered {
		item := m.items[idx]
		vi := viewItem{filteredIdx: i, item: item}
		if item.status != statusNone {
			active = append(active, vi)
		} else if item.repo.Favourite {
			favourites = append(favourites, vi)
		} else {
			rest = append(rest, vi)
		}
	}
	return
}

func (m model) renderItem(vi viewItem) string {
	item := vi.item
	cursor := "  "
	if vi.filteredIdx == m.cursor {
		cursor = cursorStyle.Render("▶ ")
	}

	name := nameStyle.Render(item.repo.Name)

	var status string
	switch item.status {
	case statusClaude:
		status = statusStyle.Render("claude interactive")
	case statusShell:
		status = statusStyle.Render("shell only")
	case statusRemote:
		status = statusStyle.Render("claude interactive") + "  " + remoteStyle.Render("⟳ remote")
	case statusDead:
		status = deadStyle.Render("✗ dead")
	case statusWaiting:
		status = waitStyle.Render("⏳ waiting")
	default:
		status = statusStyle.Render("")
	}

	var badges string
	if item.repo.IsScratch {
		badges += "  " + scratchStyle.Render("tmp")
	}
	if alert, ok := m.alerts[item.repo.DirName]; ok {
		badges += "  " + deadStyle.Render(alert)
	}

	return fmt.Sprintf("%s%s%s%s\n", cursor, name, status, badges)
}

func (m model) renderKeyBar() string {
	pairs := []struct{ key, val string }{
		{"Enter", "open+claude"},
		{"S+Enter", "shell"},
		{"r", "remote"},
		{"s", "scratch"},
		{"p", "promote"},
		{"?", "help"},
		{"q", "quit"},
	}
	var parts []string
	for _, p := range pairs {
		parts = append(parts, barKeyStyle.Render(p.key)+" "+barValStyle.Render(p.val))
	}
	return helpStyle.Render(strings.Join(parts, "  "))
}

func (m model) viewHelp() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("⚡ KittyLauncher — Keyboard Shortcuts"))
	b.WriteString("\n\n")

	bindings := []struct{ key, desc string }{
		{"Enter", "Open repo + start claude"},
		{"Shift+Enter", "Open repo + shell only"},
		{"r", "Toggle remote-control session"},
		{"s", "New scratch instance"},
		{"p", "Promote scratch to ~/repos/<name>"},
		{"Tab", "Focus kitty tab for selected repo"},
		{"x", "Kill tmux session + close tab"},
		{"d", "Detach tab (tmux persists)"},
		{"/", "Filter repos"},
		{"1-9", "Jump to kitty tab by number"},
		{"?", "Toggle this help"},
		{"q", "Quit TUI (sessions persist)"},
	}

	for _, bind := range bindings {
		key := barKeyStyle.Render(fmt.Sprintf("%-14s", bind.key))
		b.WriteString(fmt.Sprintf("  %s %s\n", key, barValStyle.Render(bind.desc)))
	}

	b.WriteString("\n")
	b.WriteString(helpStyle.Render("Press ? or Esc to close"))
	return b.String()
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
```

- [ ] **Step 3: Update main.go to wire model**

```go
package main

import (
	"fmt"
	"os"
	"path/filepath"

	tea "charm.land/bubbletea/v2"
)

func main() {
	home, _ := os.UserHomeDir()
	cfgPath := filepath.Join(home, ".config", "kittylauncher", "config.yaml")

	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error loading config: %v\n", err)
		os.Exit(1)
	}

	p := tea.NewProgram(newModel(cfg), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
```

- [ ] **Step 4: Verify it compiles** (scratch.go doesn't exist yet, so stub DiscoverScratches)

Add a temporary stub at the bottom of `model.go` or in scratch.go:

```go
// In scratch.go (create minimal stub)
package main

func DiscoverScratches(cfg *Config) []Repo {
	return nil
}
```

```bash
cd kittylauncher
go mod tidy
go build ./...
```

Expected: compiles.

- [ ] **Step 5: Commit**

```bash
git add model.go view.go main.go scratch.go go.mod go.sum
git commit -m "feat: add core model, sectioned repo list view, and help overlay"
```

---

### Task 7: Update Handler — Navigation, Filtering, Mode Switching

**Files:**
- Modify: `kittylauncher/model.go` — add the `Update` method

- [ ] **Step 1: Add Update method and filter logic to model.go**

Append to `model.go`:

```go
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

	// Global keys
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

	// List mode keys
	switch key {
	case "q":
		return m, tea.Quit
	case "?":
		m.mode = viewHelp
	case "/":
		m.filtering = true
		m.filter.Focus()
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

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
```

Note: `openSelected`, `toggleRemote`, `createScratch`, `focusSelectedTab`, `killSelected`, `detachSelected`, `reconnectSessions`, and `promoteSelected` are implemented in the next tasks (session.go, scratch.go). For now add stubs.

- [ ] **Step 2: Add method stubs to model.go temporarily**

```go
func (m *model) openSelected(withClaude bool) tea.Cmd   { return nil }
func (m *model) toggleRemote() tea.Cmd                   { return nil }
func (m *model) createScratch() tea.Cmd                  { return nil }
func (m *model) focusSelectedTab() tea.Cmd               { return nil }
func (m *model) killSelected() tea.Cmd                   { return nil }
func (m *model) detachSelected() tea.Cmd                 { return nil }
func (m *model) reconnectSessions()                      {}
func (m *model) promoteSelected(name string)             {}
```

- [ ] **Step 3: Add missing import to model.go**

Ensure `model.go` imports `"strings"`.

- [ ] **Step 4: Verify it compiles and runs**

```bash
cd kittylauncher
go build ./... && go run .
```

Expected: TUI shows repo list, j/k navigate, / filters, ? shows help, q quits.

- [ ] **Step 5: Commit**

```bash
git add model.go
git commit -m "feat: add Update handler with navigation, filtering, and mode switching"
```

---

### Task 8: Session Lifecycle

**Files:**
- Create: `kittylauncher/session.go`
- Create: `kittylauncher/session_test.go`
- Modify: `kittylauncher/model.go` — remove stubs

- [ ] **Step 1: Write failing test for session name and status mapping**

```go
package main

import (
	"testing"
)

func TestMapTmuxSessionsToItems(t *testing.T) {
	items := []repoItem{
		{repo: Repo{DirName: "SliceWise", Short: "SW"}},
		{repo: Repo{DirName: "lom2", Short: "LOM"}},
	}
	sessions := []TmuxSession{
		{Name: "kl-SliceWise", RepoKey: "SliceWise"},
		{Name: "kl-rc-SliceWise", RepoKey: "SliceWise", IsRemote: true},
	}

	MapSessionsToItems(items, sessions)

	if items[0].status != statusRemote {
		t.Errorf("SliceWise should be statusRemote (has both interactive + remote), got %d", items[0].status)
	}
	if items[0].tmuxSes != "kl-SliceWise" {
		t.Errorf("expected kl-SliceWise, got %s", items[0].tmuxSes)
	}
	if items[1].status != statusNone {
		t.Errorf("lom2 should have no session, got %d", items[1].status)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd kittylauncher
go test -run TestMapTmux -v
```

Expected: `MapSessionsToItems` not defined.

- [ ] **Step 3: Implement session.go**

```go
package main

import (
	tea "charm.land/bubbletea/v2"
)

// MapSessionsToItems updates repoItem status based on discovered tmux sessions
func MapSessionsToItems(items []repoItem, sessions []TmuxSession) {
	sessionMap := make(map[string][]TmuxSession)
	for _, s := range sessions {
		sessionMap[s.RepoKey] = append(sessionMap[s.RepoKey], s)
	}

	for i := range items {
		dirName := items[i].repo.DirName
		sess, ok := sessionMap[dirName]
		if !ok {
			items[i].status = statusNone
			items[i].tmuxSes = ""
			continue
		}

		hasInteractive := false
		hasRemote := false
		for _, s := range sess {
			if s.IsRemote {
				hasRemote = true
			} else {
				hasInteractive = true
				items[i].tmuxSes = s.Name
			}
		}

		if hasRemote && hasInteractive {
			items[i].status = statusRemote
		} else if hasRemote {
			items[i].status = statusRemote
			// Use the remote session name if no interactive
			for _, s := range sess {
				if s.IsRemote {
					items[i].tmuxSes = s.Name
				}
			}
		} else if hasInteractive {
			items[i].status = statusClaude // default; could be shell, detected later
		}
	}
}

// Real methods on model — replace the stubs

func (m *model) openSelected(withClaude bool) tea.Cmd {
	item := m.selectedItem()
	if item == nil {
		return nil
	}

	repo := item.repo
	sessionName := TmuxSessionName(repo.DirName, false)

	// If session already exists, just focus its tab
	if TmuxHasSession(sessionName) {
		KittyFocusTab("title:^" + repo.Short)
		return nil
	}

	// Create tmux session
	if err := TmuxNewSession(sessionName, repo.Path); err != nil {
		m.err = err
		return nil
	}

	// Send startup command
	if withClaude {
		TmuxSendKeys(sessionName, "claude")
		item.status = statusClaude
	} else {
		item.status = statusShell
	}
	item.tmuxSes = sessionName

	// Open kitty tab attached to tmux session
	KittyLaunchTab(repo.Short, "tmux", "attach", "-t", sessionName)
	KittySetTabColor(repo.Short, repo.Color)

	return nil
}

func (m *model) toggleRemote() tea.Cmd {
	item := m.selectedItem()
	if item == nil {
		return nil
	}

	repo := item.repo
	rcName := TmuxSessionName(repo.DirName, true)

	if TmuxHasSession(rcName) {
		// Kill remote session
		TmuxKillSession(rcName)
		// Close its kitty tab if it has one
		KittyCloseTab("title:^" + repo.Short + " ⟳")
		if item.status == statusRemote {
			if TmuxHasSession(TmuxSessionName(repo.DirName, false)) {
				item.status = statusClaude
			} else {
				item.status = statusNone
			}
		}
	} else {
		// Start remote session
		if err := TmuxNewSession(rcName, repo.Path); err != nil {
			m.err = err
			return nil
		}
		TmuxSendKeys(rcName, "claude remote-control")

		// Open kitty tab for remote
		tabTitle := repo.Short + " ⟳"
		KittyLaunchTab(tabTitle, "tmux", "attach", "-t", rcName)
		KittySetTabColor(tabTitle, repo.Color)

		if item.status == statusNone {
			item.status = statusRemote
		} else {
			item.status = statusRemote
		}
	}

	return nil
}

func (m *model) focusSelectedTab() tea.Cmd {
	item := m.selectedItem()
	if item == nil || item.status == statusNone {
		return nil
	}
	KittyFocusTab("title:^" + item.repo.Short)
	return nil
}

func (m *model) killSelected() tea.Cmd {
	item := m.selectedItem()
	if item == nil || item.status == statusNone {
		return nil
	}

	repo := item.repo

	// Kill interactive session
	interactiveName := TmuxSessionName(repo.DirName, false)
	if TmuxHasSession(interactiveName) {
		TmuxKillSession(interactiveName)
		KittyCloseTab("title:^" + repo.Short)
	}

	// Kill remote session
	rcName := TmuxSessionName(repo.DirName, true)
	if TmuxHasSession(rcName) {
		TmuxKillSession(rcName)
		KittyCloseTab("title:^" + repo.Short + " ⟳")
	}

	item.status = statusNone
	item.tmuxSes = ""
	item.title = ""
	delete(m.alerts, repo.DirName)

	return nil
}

func (m *model) detachSelected() tea.Cmd {
	item := m.selectedItem()
	if item == nil || item.status == statusNone {
		return nil
	}

	// Close kitty tab but leave tmux session alive
	KittyCloseTab("title:^" + item.repo.Short)
	return nil
}

func (m *model) reconnectSessions() {
	sessions, err := TmuxListSessions()
	if err != nil || len(sessions) == 0 {
		return
	}

	MapSessionsToItems(m.items, sessions)

	// For each session that has an active kitty tab already, skip.
	// For each session without a kitty tab, re-attach.
	tabs, err := KittyListTabs()
	if err != nil {
		return
	}
	tabTitles := make(map[string]bool)
	for _, tab := range tabs {
		tabTitles[tab.Title] = true
	}

	for i := range m.items {
		item := &m.items[i]
		if item.status == statusNone {
			continue
		}

		// Check if a kitty tab already exists for this repo
		if tabTitles[item.repo.Short] || tabTitles[item.repo.Short+" ⟳"] {
			continue
		}

		// Re-attach: open kitty tab for the tmux session
		if item.tmuxSes != "" {
			tabTitle := item.repo.Short
			if item.status == statusRemote && !TmuxHasSession(TmuxSessionName(item.repo.DirName, false)) {
				tabTitle = item.repo.Short + " ⟳"
			}
			KittyLaunchTab(tabTitle, "tmux", "attach", "-t", item.tmuxSes)
			KittySetTabColor(tabTitle, item.repo.Color)
		}
	}
}
```

- [ ] **Step 4: Remove stubs from model.go**

Delete these lines from `model.go`:

```go
func (m *model) openSelected(withClaude bool) tea.Cmd   { return nil }
func (m *model) toggleRemote() tea.Cmd                   { return nil }
func (m *model) createScratch() tea.Cmd                  { return nil }
func (m *model) focusSelectedTab() tea.Cmd               { return nil }
func (m *model) killSelected() tea.Cmd                   { return nil }
func (m *model) detachSelected() tea.Cmd                 { return nil }
func (m *model) reconnectSessions()                      {}
func (m *model) promoteSelected(name string)             {}
```

Note: `createScratch` and `promoteSelected` stubs should remain until Task 9.

Actually, keep these two stubs in model.go for now:
```go
func (m *model) createScratch() tea.Cmd       { return nil }
func (m *model) promoteSelected(name string)  {}
```

- [ ] **Step 5: Run tests**

```bash
cd kittylauncher
go test -run TestMapTmux -v
go build ./...
```

Expected: test PASSES, compiles.

- [ ] **Step 6: Commit**

```bash
git add session.go session_test.go model.go
git commit -m "feat: add session lifecycle — open, kill, detach, remote toggle, reconnect"
```

---

### Task 9: Scratch Instances & Promotion

**Files:**
- Modify: `kittylauncher/scratch.go`
- Create: `kittylauncher/scratch_test.go`
- Modify: `kittylauncher/model.go` — remove remaining stubs

- [ ] **Step 1: Write failing tests**

```go
package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverScratches(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "scratch-20260328-001"), 0755)
	os.MkdirAll(filepath.Join(dir, "scratch-20260328-002"), 0755)
	os.WriteFile(filepath.Join(dir, "not-a-scratch.txt"), []byte("hi"), 0644)

	cfg := &Config{ScratchDir: dir}
	scratches := DiscoverScratches(cfg)

	if len(scratches) != 2 {
		t.Fatalf("expected 2 scratches, got %d", len(scratches))
	}
	if !scratches[0].IsScratch {
		t.Error("expected IsScratch=true")
	}
	if scratches[0].Short != "SCR" {
		t.Errorf("expected short SCR, got %s", scratches[0].Short)
	}
}

func TestNextScratchName(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "scratch-20260328-001"), 0755)

	name := nextScratchDir(dir)
	// Should be scratch-<today>-002 or -001 depending on date
	if name == "" {
		t.Error("expected non-empty scratch name")
	}
}

func TestPromoteScratch(t *testing.T) {
	scratchDir := t.TempDir()
	reposDir := t.TempDir()

	scratchPath := filepath.Join(scratchDir, "scratch-20260328-001")
	os.MkdirAll(scratchPath, 0755)
	os.WriteFile(filepath.Join(scratchPath, "test.txt"), []byte("hello"), 0644)

	newPath, err := PromoteScratch(scratchPath, reposDir, "my-project")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := filepath.Join(reposDir, "my-project")
	if newPath != expected {
		t.Errorf("expected %s, got %s", expected, newPath)
	}

	// Check the file was moved
	data, err := os.ReadFile(filepath.Join(newPath, "test.txt"))
	if err != nil {
		t.Fatalf("file not found after promote: %v", err)
	}
	if string(data) != "hello" {
		t.Error("file content mismatch")
	}

	// Check old path doesn't exist
	if _, err := os.Stat(scratchPath); !os.IsNotExist(err) {
		t.Error("scratch dir should be removed after promotion")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd kittylauncher
go test -run TestDiscover -v
go test -run TestNextScratch -v
go test -run TestPromote -v
```

Expected: compilation errors — `nextScratchDir`, `PromoteScratch` not defined; `DiscoverScratches` returns nil.

- [ ] **Step 3: Implement scratch.go (replace the stub)**

```go
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func DiscoverScratches(cfg *Config) []Repo {
	entries, err := os.ReadDir(cfg.ScratchDir)
	if err != nil {
		return nil
	}

	var repos []Repo
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "scratch-") {
			continue
		}
		dirName := entry.Name()
		repos = append(repos, Repo{
			DirName:   dirName,
			Path:      filepath.Join(cfg.ScratchDir, dirName),
			Name:      dirName,
			Short:     "SCR",
			IsScratch: true,
		})
	}

	sort.Slice(repos, func(i, j int) bool {
		return repos[i].DirName > repos[j].DirName // newest first
	})

	return repos
}

func nextScratchDir(scratchDir string) string {
	date := time.Now().Format("20060102")
	prefix := fmt.Sprintf("scratch-%s-", date)

	entries, _ := os.ReadDir(scratchDir)
	maxNum := 0
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, prefix) {
			numStr := strings.TrimPrefix(name, prefix)
			var num int
			fmt.Sscanf(numStr, "%d", &num)
			if num > maxNum {
				maxNum = num
			}
		}
	}

	return fmt.Sprintf("%s%03d", prefix, maxNum+1)
}

func CreateScratch(cfg *Config) (Repo, error) {
	os.MkdirAll(cfg.ScratchDir, 0755)
	dirName := nextScratchDir(cfg.ScratchDir)
	path := filepath.Join(cfg.ScratchDir, dirName)

	if err := os.MkdirAll(path, 0755); err != nil {
		return Repo{}, fmt.Errorf("failed to create scratch dir: %w", err)
	}

	return Repo{
		DirName:   dirName,
		Path:      path,
		Name:      dirName,
		Short:     "SCR",
		IsScratch: true,
	}, nil
}

func PromoteScratch(scratchPath, reposDir, name string) (string, error) {
	newPath := filepath.Join(reposDir, name)

	if _, err := os.Stat(newPath); err == nil {
		return "", fmt.Errorf("repo %s already exists", name)
	}

	if err := os.Rename(scratchPath, newPath); err != nil {
		return "", fmt.Errorf("failed to move scratch to repos: %w", err)
	}

	// git init in new location
	cmd := exec.Command("git", "init")
	cmd.Dir = newPath
	cmd.Run() // best-effort

	return newPath, nil
}

// Model methods

func (m *model) createScratch() tea.Cmd {
	repo, err := CreateScratch(m.cfg)
	if err != nil {
		m.err = err
		return nil
	}

	item := repoItem{repo: repo}
	sessionName := TmuxSessionName(repo.DirName, false)

	if err := TmuxNewSession(sessionName, repo.Path); err != nil {
		m.err = err
		return nil
	}

	if m.cfg.DefaultAction == "claude" {
		TmuxSendKeys(sessionName, "claude")
		item.status = statusClaude
	} else {
		item.status = statusShell
	}
	item.tmuxSes = sessionName

	// Number the scratch tab
	scratchCount := 0
	for _, it := range m.items {
		if it.repo.IsScratch {
			scratchCount++
		}
	}
	tabTitle := fmt.Sprintf("SCR-%d", scratchCount+1)
	repo.Short = tabTitle

	KittyLaunchTab(tabTitle, "tmux", "attach", "-t", sessionName)

	m.items = append(m.items, item)
	m.filtered = m.allIndices()
	m.applyFilter()

	return nil
}

func (m *model) promoteSelected(name string) {
	item := m.selectedItem()
	if item == nil || !item.repo.IsScratch {
		return
	}

	newPath, err := PromoteScratch(item.repo.Path, m.cfg.ReposDir, name)
	if err != nil {
		m.err = err
		return
	}

	// Update the item
	oldSessionName := item.tmuxSes
	newSessionName := TmuxSessionName(name, false)

	item.repo.DirName = name
	item.repo.Path = newPath
	item.repo.Name = name
	item.repo.Short = defaultShort(name)
	item.repo.IsScratch = false

	// Rename tmux session
	if oldSessionName != "" {
		TmuxRenameSession(oldSessionName, newSessionName)
		item.tmuxSes = newSessionName
	}

	// Update kitty tab title
	KittySetTabTitle("title:^SCR", item.repo.Short)
}
```

- [ ] **Step 4: Add missing import to scratch.go**

Ensure `scratch.go` imports `tea "charm.land/bubbletea/v2"` and `"fmt"`.

- [ ] **Step 5: Remove remaining stubs from model.go**

Delete from `model.go`:
```go
func (m *model) createScratch() tea.Cmd       { return nil }
func (m *model) promoteSelected(name string)  {}
```

- [ ] **Step 6: Run tests**

```bash
cd kittylauncher
go test -run "TestDiscover|TestNextScratch|TestPromote" -v
go build ./...
```

Expected: all 3 tests PASS, compiles.

- [ ] **Step 7: Commit**

```bash
git add scratch.go scratch_test.go model.go
git commit -m "feat: add scratch instance creation and promotion to permanent repo"
```

---

### Task 10: Notifications & Health Polling

**Files:**
- Create: `kittylauncher/notify.go`
- Create: `kittylauncher/notify_test.go`

- [ ] **Step 1: Write failing tests**

```go
package main

import (
	"testing"
)

func TestDetectDeadSessions(t *testing.T) {
	items := []repoItem{
		{repo: Repo{DirName: "alive"}, status: statusClaude, tmuxSes: "kl-alive"},
		{repo: Repo{DirName: "dead"}, status: statusClaude, tmuxSes: "kl-dead"},
	}
	// Simulate: alive session exists, dead session doesn't
	liveSessions := map[string]bool{"kl-alive": true}

	alerts := DetectDeadSessions(items, liveSessions)

	if len(alerts) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(alerts))
	}
	if alerts["dead"] != "session crashed" {
		t.Errorf("expected 'session crashed' for dead, got %q", alerts["dead"])
	}
}

func TestDetectDeadRemote(t *testing.T) {
	items := []repoItem{
		{repo: Repo{DirName: "myrepo", Remote: true}, status: statusRemote, tmuxSes: "kl-myrepo"},
	}
	// Interactive session alive, but remote is gone
	liveSessions := map[string]bool{"kl-myrepo": true}

	alerts := DetectDeadRemotes(items, liveSessions)

	if len(alerts) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(alerts))
	}
	if alerts["myrepo"] != "remote died" {
		t.Errorf("expected 'remote died', got %q", alerts["myrepo"])
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd kittylauncher
go test -run TestDetectDead -v
```

Expected: `DetectDeadSessions`, `DetectDeadRemotes` not defined.

- [ ] **Step 3: Implement notify.go**

```go
package main

import (
	"fmt"
	"os/exec"

	tea "charm.land/bubbletea/v2"
)

// DetectDeadSessions checks which items have tmux sessions that no longer exist
func DetectDeadSessions(items []repoItem, liveSessions map[string]bool) map[string]string {
	alerts := make(map[string]string)
	for _, item := range items {
		if item.status == statusNone || item.tmuxSes == "" {
			continue
		}
		if !liveSessions[item.tmuxSes] {
			alerts[item.repo.DirName] = "session crashed"
		}
	}
	return alerts
}

// DetectDeadRemotes checks which items expect a remote session but it's gone
func DetectDeadRemotes(items []repoItem, liveSessions map[string]bool) map[string]string {
	alerts := make(map[string]string)
	for _, item := range items {
		if !item.repo.Remote && item.status != statusRemote {
			continue
		}
		rcName := TmuxSessionName(item.repo.DirName, true)
		if !liveSessions[rcName] {
			alerts[item.repo.DirName] = "remote died"
		}
	}
	return alerts
}

func (m model) handleTick() (tea.Model, tea.Cmd) {
	sessions, err := TmuxListSessions()
	if err != nil {
		return m, healthTick(m.cfg.Notifications.PollInterval)
	}

	// Build live session set
	liveSessions := make(map[string]bool)
	for _, s := range sessions {
		liveSessions[s.Name] = true
	}

	// Check for dead sessions
	deadAlerts := DetectDeadSessions(m.items, liveSessions)
	remoteAlerts := DetectDeadRemotes(m.items, liveSessions)

	// Merge alerts
	newAlerts := make(map[string]string)
	for k, v := range deadAlerts {
		newAlerts[k] = v
	}
	for k, v := range remoteAlerts {
		newAlerts[k] = v
	}

	// Check for new high-severity alerts
	hasNewHighSeverity := false
	for k, v := range newAlerts {
		if _, existed := m.alerts[k]; !existed {
			hasNewHighSeverity = true
			// Update item status
			for i := range m.items {
				if m.items[i].repo.DirName == k {
					if v == "session crashed" {
						m.items[i].status = statusDead
					}
				}
			}
		}
	}
	m.alerts = newAlerts

	// Update tab titles from tmux pane titles
	for i := range m.items {
		item := &m.items[i]
		if item.status == statusClaude || item.status == statusRemote {
			if item.tmuxSes != "" {
				title, err := TmuxPaneTitle(item.tmuxSes)
				if err == nil && title != item.title {
					item.title = title
					// Update kitty tab title
					newTabTitle := item.repo.Short
					if title != "" {
						newTabTitle = item.repo.Short + " — " + title
					}
					KittySetTabTitle("title:^"+item.repo.Short, newTabTitle)
				}
			}
		}
	}

	// Handle alerts: flash + desktop notification
	var cmds []tea.Cmd
	if hasNewHighSeverity {
		if m.cfg.Notifications.TabFlash && !m.flashing {
			m.flashing = true
			KittySetTabColor("KittyLauncher", "#ff0000")
			cmds = append(cmds, flashRestore())
		}
		if m.cfg.Notifications.Desktop {
			for k, v := range newAlerts {
				sendDesktopNotification(k, v)
			}
		}

		// Add alert badge to affected tab titles
		for k := range newAlerts {
			for _, item := range m.items {
				if item.repo.DirName == k {
					KittySetTabTitle("title:^"+item.repo.Short, "⚠ "+item.repo.Short)
				}
			}
		}
	}

	cmds = append(cmds, healthTick(m.cfg.Notifications.PollInterval))
	return m, tea.Batch(cmds...)
}

func sendDesktopNotification(repoName, message string) {
	title := fmt.Sprintf("KittyLauncher: %s", repoName)
	exec.Command("notify-send", "--urgency=critical", title, message).Run()
}
```

- [ ] **Step 4: Run tests**

```bash
cd kittylauncher
go test -run TestDetectDead -v
go build ./...
```

Expected: tests PASS, compiles.

- [ ] **Step 5: Commit**

```bash
git add notify.go notify_test.go
git commit -m "feat: add health polling with dead session detection, tab flash, and desktop notifications"
```

---

### Task 11: Launch Script

**Files:**
- Create: `kittylauncher/launch-kl.sh`

- [ ] **Step 1: Write the launch script**

```bash
#!/usr/bin/env bash
set -euo pipefail

# KittyLauncher — launch script
# Starts kitty with the TUI in tab 0 (orange), IPC enabled.

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
BINARY="${SCRIPT_DIR}/kittylauncher"
SOCKET_PATH="/tmp/kl-kitty-$$"

# Check if KL is already running
EXISTING_SOCKET=$(ls /tmp/kl-kitty-* 2>/dev/null | head -1)
if [ -n "${EXISTING_SOCKET:-}" ] && [ -S "$EXISTING_SOCKET" ]; then
    echo "KittyLauncher already running. Focusing existing window."
    kitten @ --to "unix:${EXISTING_SOCKET}" focus-window 2>/dev/null || true
    exit 0
fi

# Build if needed
if [ ! -f "$BINARY" ]; then
    echo "Building kittylauncher..."
    (cd "$SCRIPT_DIR" && go build -o kittylauncher .)
fi

# Start kitty with:
# - Remote control enabled via socket
# - TUI as the initial tab
# - allow_remote_control set to socket-only
kitty \
    --listen-on "unix:${SOCKET_PATH}" \
    -o allow_remote_control=socket-only \
    -o tab_bar_style=powerline \
    -o tab_title_template="{title}" \
    --title "KittyLauncher" \
    "$BINARY"

# Cleanup socket on exit
rm -f "$SOCKET_PATH" 2>/dev/null
```

- [ ] **Step 2: Make it executable**

```bash
chmod +x kittylauncher/launch-kl.sh
```

- [ ] **Step 3: Test the script starts kitty (manual)**

```bash
cd kittylauncher
go build -o kittylauncher .
./launch-kl.sh
```

Expected: kitty opens with the TUI visible in tab 0. Press `q` to quit.

- [ ] **Step 4: Commit**

```bash
git add launch-kl.sh
git commit -m "feat: add launch script for starting kitty with TUI and IPC"
```

---

### Task 12: Example Config

**Files:**
- Create: `kittylauncher/config.example.yaml`

- [ ] **Step 1: Write example config**

```yaml
# KittyLauncher configuration
# Place at: ~/.config/kittylauncher/config.yaml

# Global settings
repos_dir: ~/repos              # Directory to scan for repos
scratch_dir: /tmp/kl-scratch    # Where scratch instances are created
default_action: claude           # "claude" (start claude on open) or "shell" (just a shell)

# Notification settings
notifications:
  desktop: true                  # Send desktop notifications for high-severity events
  tab_flash: true                # Flash TUI tab on alerts
  poll_interval: 5               # Seconds between health checks

# Per-workspace overrides
# Key = directory name inside repos_dir
# All fields are optional — unconfigured repos use defaults
workspaces:
  SliceWise:
    name: "SliceWize"            # Display name in TUI list
    short: "SW"                  # Short name for kitty tab title
    color: "#ff6b6b"             # Tab color (hex)
    remote: true                 # Auto-start claude remote-control session
    favourite: true              # Pin to favourites section
  tgclaudebridge:
    name: "Telegram Bridge"
    short: "TGB"
    color: "#0088cc"
    remote: true
    favourite: true
  lom2:
    name: "Legend of Mir"
    short: "LOM"
    color: "#c792ea"
    favourite: true
```

- [ ] **Step 2: Commit**

```bash
git add config.example.yaml
git commit -m "docs: add example configuration file"
```

---

### Task 13: Integration Smoke Test

This task verifies everything works end-to-end.

- [ ] **Step 1: Run all unit tests**

```bash
cd kittylauncher
go test ./... -v
```

Expected: all tests PASS.

- [ ] **Step 2: Build the binary**

```bash
cd kittylauncher
go build -o kittylauncher .
```

Expected: binary created with no errors.

- [ ] **Step 3: Create a test config**

```bash
mkdir -p ~/.config/kittylauncher
cp kittylauncher/config.example.yaml ~/.config/kittylauncher/config.yaml
```

Edit the config to match actual repos.

- [ ] **Step 4: Manual smoke test**

Launch the app:
```bash
cd kittylauncher
./launch-kl.sh
```

Test checklist:
- [ ] TUI renders with repo list grouped by Active / Favourites / All
- [ ] Arrow keys / j/k navigate the list
- [ ] `/` activates filter, typing narrows the list, Esc clears
- [ ] `?` shows help overlay, `?` or Esc dismisses
- [ ] `Enter` on a repo opens a kitty tab with claude
- [ ] `Shift+Enter` opens a tab with just a shell
- [ ] `r` toggles remote-control session
- [ ] `s` creates a scratch instance
- [ ] `p` on a scratch prompts for name and promotes
- [ ] `Tab` focuses the kitty tab for the selected repo
- [ ] `x` kills the session and closes the tab
- [ ] `d` closes the tab but tmux session persists
- [ ] `1-9` jump to kitty tab by number
- [ ] Tab titles show `<short> — <claude title>` format
- [ ] Killing a tmux session externally triggers alert (flash + notification)
- [ ] `q` quits TUI, tmux sessions persist

- [ ] **Step 5: Fix any issues found in smoke test**

- [ ] **Step 6: Final commit**

```bash
git add -A
git commit -m "feat: kittylauncher v1 — complete TUI workspace manager for kitty + claude"
```
