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

// openWorktreePanel shows the worktree creation prompt for the selected repo.
func (m *model) openWorktreePanel() tea.Cmd {
	item := m.selectedItem()
	if item == nil || item.repo.IsScratch || item.repo.IsCollection || item.repo.IsWorktree {
		return nil
	}

	m.wtParent = item.repo.DirName

	fields := make([]textinput.Model, wtFieldCount)

	branchInput := textinput.New()
	branchInput.Prompt = "Branch: "
	branchInput.Placeholder = "feature-name"
	fields[wtFieldBranch] = branchInput

	promptInput := textinput.New()
	promptInput.Prompt = "Prompt: "
	promptInput.Placeholder = "optional task for Claude"
	fields[wtFieldPrompt] = promptInput

	m.wtFields = fields
	m.wtYolo = item.repo.Yolo // inherit parent's yolo setting
	m.wtFocus = 0
	m.mode = viewWorktree

	return m.wtFields[0].Focus()
}

// handleWorktreeKey handles keypresses in the worktree prompt panel.
func (m *model) handleWorktreeKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "ctrl+s", "ctrl+enter":
		return m, m.createWorktree()
	case "escape":
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
	case " ":
		if m.wtFocus == wtFieldCount {
			m.wtYolo = !m.wtYolo
			return m, nil
		}
	}

	// Pass to text input
	if m.wtFocus < wtFieldCount {
		var cmd tea.Cmd
		m.wtFields[m.wtFocus], cmd = m.wtFields[m.wtFocus].Update(tea.KeyPressMsg{})
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
	claudeCmd := "claude"
	if yolo {
		claudeCmd = "claude --permission-mode bypassPermissions"
	}
	if prompt != "" {
		claudeCmd += " -p " + shellQuote(prompt)
	}
	TmuxSendKeys(sessionName, claudeCmd)

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
