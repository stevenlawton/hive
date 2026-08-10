package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

const (
	wtFieldBranch = 0
	wtFieldPrompt = 1
	wtFieldCount  = 2 // text fields only; yolo is a toggle
)

// worktreeFieldWidth is the text-input width inside the centered modal box.
// editBoxStyle is 50 wide with a 1-cell border and 2-cell horizontal padding
// (inner 44); minus the "> " marker and the "Branch: " prompt leaves ~32. An
// explicit width is required — at width 0 bubbles renders only the first
// character of a placeholder, which is what made the modal look broken.
const worktreeFieldWidth = 32

// newWorktreeField builds a worktree-form text input sized for the modal so
// its placeholder/value render in full.
func newWorktreeField(prompt, placeholder, value string) textinput.Model {
	ti := textinput.New()
	ti.Prompt = prompt
	ti.Placeholder = placeholder
	ti.SetWidth(worktreeFieldWidth)
	if value != "" {
		ti.SetValue(value)
	}
	visibleCursorStyle(&ti)
	return ti
}

// defaultWorktreeBranch returns the next free wt-N branch name for a repo, so
// the modal opens pre-filled with a usable, collision-free default.
func defaultWorktreeBranch(repoPath string) string {
	base := filepath.Join(repoPath, ".worktrees")
	for n := 1; ; n++ {
		name := fmt.Sprintf("wt-%d", n)
		if _, err := os.Stat(filepath.Join(base, name)); err != nil {
			return name
		}
	}
}

// openWorktreePanel shows the worktree creation prompt for the selected repo.
func (m *model) openWorktreePanel() tea.Cmd {
	item := m.selectedItem()
	if item == nil || item.repo.IsScratch || item.repo.IsCollection || item.repo.IsWorktree || item.isTGSession {
		return nil
	}

	m.wtParent = item.repo.DirName

	defaultBranch := defaultWorktreeBranch(item.repo.Path)
	fields := make([]textinput.Model, wtFieldCount)
	fields[wtFieldBranch] = newWorktreeField("Branch: ", "feature-name", defaultBranch)
	fields[wtFieldPrompt] = newWorktreeField("Prompt: ", "optional task for Claude", "")

	m.wtFields = fields
	m.wtYolo = item.repo.Yolo // inherit parent's yolo setting
	m.wtFocus = 0
	m.mode = viewWorktree

	return m.wtFields[0].Focus()
}

// handleWorktreeKey handles keypresses in the worktree prompt panel.
func (m *model) handleWorktreeKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	switch key {
	case "ctrl+s", "ctrl+enter":
		return m, m.createWorktree()
	case "esc", "escape":
		if m.wtSplitMode {
			m.mode = viewWorkspace
			m.wtSplitMode = false
		} else {
			m.mode = viewManager
		}
		return m, nil
	case "tab", "down":
		if m.wtFocus < wtFieldCount {
			m.wtFields[m.wtFocus].Blur()
		}
		m.wtFocus++
		if m.wtFocus > wtFieldCount { // wtFieldCount = yolo toggle
			m.wtFocus = 0
		}
		if m.wtFocus < wtFieldCount {
			return m, m.wtFields[m.wtFocus].Focus()
		}
		return m, nil
	case "shift+tab", "up":
		if m.wtFocus < wtFieldCount {
			m.wtFields[m.wtFocus].Blur()
		}
		m.wtFocus--
		if m.wtFocus < 0 {
			m.wtFocus = wtFieldCount // yolo toggle
		}
		if m.wtFocus < wtFieldCount {
			return m, m.wtFields[m.wtFocus].Focus()
		}
		return m, nil
	case "enter":
		if m.wtFocus == wtFieldCount {
			// On yolo toggle, toggle it
			m.wtYolo = !m.wtYolo
			return m, nil
		}
		// Enter submits the form (branch has a default, prompt is optional)
		return m, m.createWorktree()
	case "space", " ":
		if m.wtFocus == wtFieldCount {
			m.wtYolo = !m.wtYolo
			return m, nil
		}
	}

	// Pass the real keypress to the focused text input so the user can type.
	if m.wtFocus < wtFieldCount {
		var cmd tea.Cmd
		m.wtFields[m.wtFocus], cmd = m.wtFields[m.wtFocus].Update(msg)
		return m, cmd
	}
	return m, nil
}

// createWorktree creates a git worktree and tmux session.
func (m *model) createWorktree() tea.Cmd {
	branch := strings.TrimSpace(m.wtFields[wtFieldBranch].Value())
if branch == "" {
		m.err = fmt.Errorf("branch name required")
		return nil
	}

	prompt := strings.TrimSpace(m.wtFields[wtFieldPrompt].Value())
	yolo := m.wtYolo

	// Find parent item
	var parent *repoItem
	for i := range m.items {
		if m.items[i].repo.DirName == m.wtParent {
			parent = &m.items[i]
			break
		}
	}
	if parent == nil {
		m.err = fmt.Errorf("parent repo not found")
		m.mode = m.wtReturnMode()
		return nil
	}

	// Create git worktree
	wtDir := filepath.Join(parent.repo.Path, ".worktrees", branch)
	cmd := exec.Command("git", "-C", parent.repo.Path, "worktree", "add", "-b", branch, wtDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		// Branch might already exist, try without -b
		cmd = exec.Command("git", "-C", parent.repo.Path, "worktree", "add", wtDir, branch)
		if out2, err2 := cmd.CombinedOutput(); err2 != nil {
			m.err = fmt.Errorf("worktree: %s %s", string(out), string(out2))
			m.mode = m.wtReturnMode()
			return nil
		}
		_ = out
	}

	// Create tmux session in the worktree dir
	sessionName := TmuxSessionName(m.wtParent+"-wt-"+branch, false)
	if err := TmuxNewSession(sessionName, wtDir); err != nil {
		m.err = fmt.Errorf("tmux: %w", err)
		m.mode = m.wtReturnMode()
		return nil
	}

	// Launch Claude in the worktree
	args := ""
	if yolo {
		args = "--permission-mode bypassPermissions"
	}
	if prompt != "" {
		if args != "" {
			args += " "
		}
		args += "-p " + shellQuote(prompt)
	}
	TmuxSendKeys(sessionName, claudeCommand(args))

	// Add worktree item to the model
	wtRepo := Repo{
		DirName:        m.wtParent + "-wt-" + branch,
		Path:           wtDir,
		Name:           "wt: " + branch,
		Short:          parent.repo.Short + "/" + branch,
		Color:          parent.repo.Color,
		IsWorktree:     true,
		WorktreeBranch: branch,
		Parent:         m.wtParent,
		Yolo:           yolo,
	}

	m.items = append(m.items, repoItem{
		repo:    wtRepo,
		status:  statusClaude,
		tmuxSes: sessionName,
	})

	if m.wtSplitMode {
		if tab := m.workspace.ActiveTab(); tab != nil {
			tab.SplitPane.Orientation = m.wtOrientation
		}
		m.persistOrientation(parent.repo.DirName, parent.tmuxSes, m.wtOrientation)
		m.workspace.AddSplitToActive("wt:"+branch, sessionName)
		m.mode = viewWorkspace
		m.wtSplitMode = false
	} else {
		m.mode = viewManager
	}
	m.filtered = m.allIndices()
	m.rebuildDisplayOrder()

	return nil
}

// DiscoverWorktrees finds existing git worktrees for a repo.
func DiscoverWorktrees(parentRepo Repo) []Repo {
	wtDir := filepath.Join(parentRepo.Path, ".worktrees")
	entries, err := os.ReadDir(wtDir)
	if err != nil {
		return nil
	}

	var repos []Repo
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		branch := entry.Name()
		// Verify it's still a valid worktree
		wtPath := filepath.Join(wtDir, branch)
		if _, err := os.Stat(filepath.Join(wtPath, ".git")); err != nil {
			continue
		}
		repos = append(repos, Repo{
			DirName:        parentRepo.DirName + "-wt-" + branch,
			Path:           wtPath,
			Name:           "wt: " + branch,
			Short:          parentRepo.Short + "/" + branch,
			Color:          parentRepo.Color,
			IsWorktree:     true,
			WorktreeBranch: branch,
			Parent:         parentRepo.DirName,
		})
	}
	return repos
}

// killWorktreeSession kills the tmux session for a worktree.
// Does NOT remove the worktree from disk.
func (m *model) killWorktreeSession() tea.Cmd {
	item := m.selectedItem()
	if item == nil || !item.repo.IsWorktree {
		return nil
	}

	sessionName := TmuxSessionName(item.repo.DirName, false)
	if TmuxHasSession(sessionName) {
		TmuxKillSession(sessionName)
	}

	// Remove from items
	for i := range m.items {
		if m.items[i].repo.DirName == item.repo.DirName {
			m.items = append(m.items[:i], m.items[i+1:]...)
			break
		}
	}

	m.filtered = m.allIndices()
	m.rebuildDisplayOrder()
	if m.cursor >= len(m.displayOrder) {
		m.cursor = max(0, len(m.displayOrder)-1)
	}
	return nil
}

// worktreeStatus checks the state of a worktree for cleanup decisions.
type worktreeStatus struct {
	exists          bool
	hasUncommitted  bool
	hasUnmerged     bool
	uncommittedDesc string // e.g. "3 modified, 1 untracked"
	unmergedCount   int
	branch          string
	parentPath      string
}

func checkWorktreeStatus(repo Repo) worktreeStatus {
	ws := worktreeStatus{
		branch: repo.WorktreeBranch,
	}

	// Check if worktree directory still exists
	if _, err := os.Stat(repo.Path); err != nil {
		return ws
	}
	ws.exists = true

	// Find parent path
	for _, part := range []string{".worktrees", repo.WorktreeBranch} {
		_ = part
	}
	ws.parentPath = filepath.Dir(filepath.Dir(repo.Path)) // up from .worktrees/<branch>

	// Check for uncommitted changes
	out, err := exec.Command("git", "-C", repo.Path, "status", "--porcelain").Output()
	if err == nil && len(strings.TrimSpace(string(out))) > 0 {
		ws.hasUncommitted = true
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		ws.uncommittedDesc = fmt.Sprintf("%d changed files", len(lines))
	}

	// Check for unmerged commits (commits on branch not in main)
	out, err = exec.Command("git", "-C", repo.Path, "log", "--oneline", "main..HEAD").Output()
	if err == nil && len(strings.TrimSpace(string(out))) > 0 {
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		ws.unmergedCount = len(lines)
		ws.hasUnmerged = true
	}

	return ws
}

// mergeWorktree merges the worktree branch into main and cleans up.
func mergeWorktree(repo Repo) error {
	parentPath := filepath.Dir(filepath.Dir(repo.Path))

	// Merge branch into main from the parent repo
	out, err := exec.Command("git", "-C", parentPath, "merge", repo.WorktreeBranch, "--no-edit").CombinedOutput()
	if err != nil {
		return fmt.Errorf("merge failed: %s", strings.TrimSpace(string(out)))
	}

	return nil
}

// removeWorktree deletes the git worktree and its branch.
func removeWorktree(repo Repo) error {
	parentPath := filepath.Dir(filepath.Dir(repo.Path))

	// Remove worktree
	exec.Command("git", "-C", parentPath, "worktree", "remove", repo.Path, "--force").Run()

	// Delete branch
	exec.Command("git", "-C", parentPath, "branch", "-D", repo.WorktreeBranch).Run()

	return nil
}

func (m *model) wtReturnMode() viewMode {
	if m.wtSplitMode {
		m.wtSplitMode = false
		return viewWorkspace
	}
	return viewManager
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
