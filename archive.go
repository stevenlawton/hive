package main

import (
	"fmt"
	"os"
	"path/filepath"

	tea "charm.land/bubbletea/v2"
)

func archiveDir(reposDir string) string {
	return filepath.Join(reposDir, ".archive")
}

func (m *model) archiveSelected() tea.Cmd {
	item := m.selectedItem()
	if item == nil || item.repo.IsScratch || item.repo.IsCollection {
		return nil
	}

	repo := item.repo
	m.confirmMsg = fmt.Sprintf("Archive %s? (y/n)", repo.Name)
	m.confirmAction = func() {
		m.doArchive(repo.DirName)
	}
	m.mode = viewConfirm
	return nil
}

func (m *model) doArchive(dirName string) {
	// Find the item
	var item *repoItem
	for i := range m.items {
		if m.items[i].repo.DirName == dirName {
			item = &m.items[i]
			break
		}
	}
	if item == nil {
		return
	}

	// Kill active sessions first
	interactiveName := TmuxSessionName(dirName, false)
	if TmuxHasSession(interactiveName) {
		TmuxKillSession(interactiveName)
	}
	rcName := TmuxSessionName(dirName, true)
	if TmuxHasSession(rcName) {
		TmuxKillSession(rcName)
	}

	// Move directory to .archive/
	archDir := archiveDir(m.cfg.ReposDir)
	os.MkdirAll(archDir, 0755)

	src := item.repo.Path
	dst := filepath.Join(archDir, filepath.Base(item.repo.Path))

	if err := os.Rename(src, dst); err != nil {
		m.err = fmt.Errorf("archive failed: %w", err)
		return
	}

	// Remove from items
	for i := range m.items {
		if m.items[i].repo.DirName == dirName {
			m.items = append(m.items[:i], m.items[i+1:]...)
			break
		}
	}

	delete(m.alerts, dirName)
	delete(m.tabFlashing, dirName)
	m.filtered = m.allIndices()
	m.rebuildDisplayOrder()
	if m.cursor >= len(m.displayOrder) {
		m.cursor = max(0, len(m.displayOrder)-1)
	}
}

func (m *model) unarchiveSelected() tea.Cmd {
	item := m.selectedItem()
	if item == nil || !item.repo.IsArchived {
		return nil
	}

	repo := item.repo

	// Move back from .archive/ to repos/
	archDir := archiveDir(m.cfg.ReposDir)
	src := filepath.Join(archDir, filepath.Base(repo.Path))
	dst := filepath.Join(m.cfg.ReposDir, filepath.Base(repo.Path))

	if err := os.Rename(src, dst); err != nil {
		m.err = fmt.Errorf("unarchive failed: %w", err)
		return nil
	}

	// Update the item in place
	item.repo.Path = dst
	item.repo.IsArchived = false

	m.filtered = m.allIndices()
	m.rebuildDisplayOrder()
	return nil
}

// DiscoverArchived finds repos in ~/repos/.archive/
func DiscoverArchived(cfg *Config) []Repo {
	archDir := archiveDir(cfg.ReposDir)
	entries, err := os.ReadDir(archDir)
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
			DirName:    dirName,
			Path:       filepath.Join(archDir, dirName),
			Name:       dirName,
			Short:      defaultShort(dirName),
			IsArchived: true,
		}
		if ws, ok := cfg.Workspaces[dirName]; ok {
			applyWorkspaceConfig(&repo, ws)
			repo.IsArchived = true // ensure it stays archived
		}
		repos = append(repos, repo)
	}
	return repos
}
