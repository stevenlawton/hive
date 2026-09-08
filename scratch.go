package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
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

	if err := moveDir(scratchPath, newPath); err != nil {
		return "", fmt.Errorf("failed to move scratch to repos: %w", err)
	}

	cmd := exec.Command("git", "init")
	cmd.Dir = newPath
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("moved to %s but git init failed: %w: %s",
			newPath, err, strings.TrimSpace(string(out)))
	}

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
		TmuxSendKeys(sessionName, claudeCommand(""))
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

// moveDir moves a directory tree, falling back to copy-then-delete when source
// and destination are on different filesystems.
//
// scratch_dir lives under /tmp, which is tmpfs, and repos_dir is on disk, so
// every promote on a normal setup is a cross-device move. The bare rename
// failed with EXDEV each time and promote could not work at all.
func moveDir(src, dst string) error {
	err := os.Rename(src, dst)
	if err == nil {
		return nil
	}
	if !errors.Is(err, syscall.EXDEV) {
		return err
	}
	if err := copyTree(src, dst); err != nil {
		// Leave the scratch where it is and take the half-copy away: a
		// failed move must lose nothing, and a partial repo in ~/repos
		// looks like a real one.
		os.RemoveAll(dst)
		return err
	}
	return os.RemoveAll(src)
}

// copyTree copies a directory tree, preserving permission bits. Symlinks are
// recreated rather than followed, so a link into the scratch does not become a
// second copy of the tree.
func copyTree(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)

		switch {
		case info.IsDir():
			return os.MkdirAll(target, info.Mode().Perm())
		case info.Mode()&os.ModeSymlink != 0:
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		case !info.Mode().IsRegular():
			return nil // sockets, devices and fifos are not scratch content
		}

		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()

		out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode().Perm())
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, in); err != nil {
			out.Close()
			return err
		}
		return out.Close()
	})
}
