package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

func DiscoverScratches(cfg *Config) []Repo {
	entries, err := os.ReadDir(cfg.ScratchDir)
	if err != nil {
		return nil
	}

	var repos []Repo
	scratchNum := 0
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "scratch-") {
			continue
		}
		scratchNum++
		dirName := entry.Name()
		repos = append(repos, Repo{
			DirName:   dirName,
			Path:      filepath.Join(cfg.ScratchDir, dirName),
			Name:      dirName,
			Short:     fmt.Sprintf("SCR-%d", scratchNum),
			IsScratch: true,
		})
	}

	sort.Slice(repos, func(i, j int) bool {
		return repos[i].DirName > repos[j].DirName
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
			var num int
			fmt.Sscanf(strings.TrimPrefix(name, prefix), "%d", &num)
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

	cmd := exec.Command("git", "init")
	cmd.Dir = newPath
	cmd.Run()

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

	scratchCount := 0
	for _, it := range m.items {
		if it.repo.IsScratch {
			scratchCount++
		}
	}
	tabTitle := fmt.Sprintf("SCR-%d", scratchCount+1)
	item.repo.Short = tabTitle

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

	oldSessionName := item.tmuxSes
	oldShort := item.repo.Short // e.g. "SCR-1" — unique per scratch
	newSessionName := TmuxSessionName(name, false)

	item.repo.DirName = name
	item.repo.Path = newPath
	item.repo.Name = name
	item.repo.Short = defaultShort(name)
	item.repo.IsScratch = false

	if oldSessionName != "" {
		TmuxRenameSession(oldSessionName, newSessionName)
		item.tmuxSes = newSessionName
	}

	_ = oldShort // previously used for kitty tab title
}
