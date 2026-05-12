package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type NotificationConfig struct {
	Desktop      bool   `yaml:"desktop"`
	TabFlash     bool   `yaml:"tab_flash"`
	Sound        bool   `yaml:"sound"`
	SoundPath    string `yaml:"sound_path,omitempty"`    // custom sound file, empty = system bell
	PollInterval int    `yaml:"poll_interval"`
	WebhookURL   string `yaml:"webhook_url,omitempty"`   // POST JSON on events
	NtfyTopic    string `yaml:"ntfy_topic,omitempty"`    // ntfy.sh topic
	SlackWebhook string `yaml:"slack_webhook,omitempty"` // Slack incoming webhook URL
}

type WorkspaceConfig struct {
	Name        string `yaml:"name,omitempty"`
	Short       string `yaml:"short,omitempty"`
	Color       string `yaml:"color,omitempty"`
	Description string `yaml:"description,omitempty"`
	Yolo        bool   `yaml:"yolo,omitempty"`
	Remote      bool   `yaml:"remote,omitempty"`
	Favourite  bool   `yaml:"favourite,omitempty"`
	Collection bool   `yaml:"collection,omitempty"`
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
		ScratchDir:    "/tmp/hive-scratch",
		DefaultAction: "claude",
		Notifications: NotificationConfig{
			Desktop:      true,
			TabFlash:     true,
			Sound:        true,
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

	// Parse to a yaml.Node tree first: this tolerates duplicate mapping keys
	// (which yaml.v3's struct/map decoder rejects outright), so one bad entry
	// can't prevent hive from booting.
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, err
	}

	rawEntries, err := decodeConfigNode(&root, cfg)
	if err != nil {
		return nil, err
	}

	if strings.HasPrefix(cfg.ReposDir, "~/") {
		cfg.ReposDir = filepath.Join(home, cfg.ReposDir[2:])
	}
	if strings.HasPrefix(cfg.ScratchDir, "~/") {
		cfg.ScratchDir = filepath.Join(home, cfg.ScratchDir[2:])
	}

	cleaned, mergeLog := cleanupWorkspaces(rawEntries, cfg.ReposDir)
	cfg.Workspaces = cleaned

	if len(cleaned) != len(rawEntries) {
		if err := rewriteConfigWithBackup(path, cfg); err != nil {
			fmt.Fprintf(os.Stderr, "warning: config auto-cleanup failed: %v\n", err)
		} else {
			fmt.Fprintf(os.Stderr, "config auto-cleaned: %d duplicate workspace key(s) merged (backup: %s.bak)\n", len(rawEntries)-len(cleaned), path)
			for _, m := range mergeLog {
				fmt.Fprintf(os.Stderr, "  %s\n", m)
			}
		}
	}

	return cfg, nil
}

// decodeConfigNode walks the top-level mapping manually instead of relying on
// yaml.v3's map decoder, so duplicate keys anywhere in the tree survive as-is
// and can be resolved by cleanupWorkspaces.
func decodeConfigNode(root *yaml.Node, cfg *Config) ([]wsEntry, error) {
	if len(root.Content) == 0 {
		return nil, nil
	}
	doc := root.Content[0]
	if doc.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("config root is not a mapping")
	}

	var entries []wsEntry
	for i := 0; i+1 < len(doc.Content); i += 2 {
		key := doc.Content[i].Value
		val := doc.Content[i+1]
		switch key {
		case "repos_dir":
			cfg.ReposDir = val.Value
		case "scratch_dir":
			cfg.ScratchDir = val.Value
		case "default_action":
			cfg.DefaultAction = val.Value
		case "notifications":
			if err := val.Decode(&cfg.Notifications); err != nil {
				return nil, fmt.Errorf("notifications: %w", err)
			}
		case "workspaces":
			if val.Kind != yaml.MappingNode {
				continue
			}
			for j := 0; j+1 < len(val.Content); j += 2 {
				wsKey := val.Content[j].Value
				var ws WorkspaceConfig
				if err := val.Content[j+1].Decode(&ws); err != nil {
					return nil, fmt.Errorf("workspaces[%s]: %w", wsKey, err)
				}
				entries = append(entries, wsEntry{key: wsKey, cfg: ws})
			}
		}
	}
	return entries, nil
}

type wsEntry struct {
	key string
	cfg WorkspaceConfig
}

// canonicalKey groups workspace keys that differ only by casing or separators.
// "react-learning", "reactLearning", and "React_Learning" all collapse to
// "reactlearning" and are treated as duplicates.
func canonicalKey(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r + 32)
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
		}
	}
	return b.String()
}

// cleanupWorkspaces resolves duplicate keys by canonical form and returns the
// clean map plus a human-readable log of what was merged.
func cleanupWorkspaces(entries []wsEntry, reposDir string) (map[string]WorkspaceConfig, []string) {
	onDisk := map[string]bool{}
	if ents, err := os.ReadDir(reposDir); err == nil {
		for _, e := range ents {
			if e.IsDir() {
				onDisk[e.Name()] = true
			}
		}
	}

	groups := map[string][]wsEntry{}
	order := []string{}
	for _, e := range entries {
		ck := canonicalKey(e.key)
		if ck == "" {
			continue
		}
		if _, seen := groups[ck]; !seen {
			order = append(order, ck)
		}
		groups[ck] = append(groups[ck], e)
	}

	result := make(map[string]WorkspaceConfig, len(order))
	var log []string
	for _, ck := range order {
		grp := groups[ck]
		if len(grp) == 1 {
			result[grp[0].key] = grp[0].cfg
			continue
		}
		keys := make([]string, len(grp))
		merged := grp[0].cfg
		for i, e := range grp {
			keys[i] = e.key
			if i > 0 {
				merged = mergeWorkspace(merged, e.cfg)
			}
		}
		chosen := chooseKey(keys, onDisk)
		result[chosen] = merged
		log = append(log, fmt.Sprintf("merged %v → %q", keys, chosen))
	}
	return result, log
}

// chooseKey picks the surface key for a dedup cluster. Prefer any key whose
// literal spelling exists as a directory under repos_dir — that's the one
// DiscoverRepos will actually match. Otherwise fall back to the longest key
// (typically the more descriptive kebab-case form).
func chooseKey(keys []string, onDisk map[string]bool) string {
	for _, k := range keys {
		if onDisk[k] {
			return k
		}
	}
	best := keys[0]
	for _, k := range keys[1:] {
		if len(k) > len(best) {
			best = k
		}
	}
	return best
}

// mergeWorkspace combines two WorkspaceConfig entries whose keys collided
// after canonicalization. Policy: non-empty strings from the later entry
// override; booleans are OR'd so any "true" survives. Adjust here if you want
// different semantics (e.g. first-write-wins, or preserve longer descriptions).
func mergeWorkspace(a, b WorkspaceConfig) WorkspaceConfig {
	out := a
	if b.Name != "" {
		out.Name = b.Name
	}
	if b.Short != "" {
		out.Short = b.Short
	}
	if b.Color != "" {
		out.Color = b.Color
	}
	if b.Description != "" {
		out.Description = b.Description
	}
	out.Yolo = a.Yolo || b.Yolo
	out.Remote = a.Remote || b.Remote
	out.Favourite = a.Favourite || b.Favourite
	out.Collection = a.Collection || b.Collection
	return out
}

func rewriteConfigWithBackup(path string, cfg *Config) error {
	if data, err := os.ReadFile(path); err == nil {
		if err := os.WriteFile(path+".bak", data, 0644); err != nil {
			return fmt.Errorf("backup: %w", err)
		}
	}
	return SaveConfig(path, cfg)
}

func SaveConfig(path string, cfg *Config) error {
	dir := filepath.Dir(path)
	os.MkdirAll(dir, 0755)

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

type Repo struct {
	DirName      string
	Path         string
	Name         string
	Short        string
	Color        string
	Description  string
	Yolo         bool
	Remote       bool
	Favourite    bool
	IsScratch    bool
	IsCollection bool
	IsArchived     bool
	IsWorktree     bool
	WorktreeBranch string
	Parent         string // DirName of parent collection or worktree parent, empty if top-level
}

func defaultShort(dirName string) string {
	clean := strings.ToUpper(dirName)
	if len(clean) > 3 {
		clean = clean[:3]
	}
	return clean
}

func applyWorkspaceConfig(repo *Repo, ws WorkspaceConfig) {
	if ws.Name != "" {
		repo.Name = ws.Name
	}
	if ws.Short != "" {
		repo.Short = ws.Short
	}
	repo.Color = ws.Color
	repo.Description = ws.Description
	repo.Yolo = ws.Yolo
	repo.Remote = ws.Remote
	repo.Favourite = ws.Favourite
	repo.IsCollection = ws.Collection
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
			applyWorkspaceConfig(&repo, ws)
		}

		if repo.IsCollection {
			// Add the collection header (not selectable for opening)
			repos = append(repos, repo)
			// Scan children
			children, err := os.ReadDir(repo.Path)
			if err != nil {
				continue
			}
			for _, child := range children {
				if !child.IsDir() {
					continue
				}
				childKey := dirName + "/" + child.Name()
				childRepo := Repo{
					DirName: childKey,
					Path:    filepath.Join(repo.Path, child.Name()),
					Name:    child.Name(),
					Short:   defaultShort(child.Name()),
					Parent:  dirName,
				}
				if ws, ok := cfg.Workspaces[childKey]; ok {
					applyWorkspaceConfig(&childRepo, ws)
				}
				repos = append(repos, childRepo)
			}
		} else {
			repos = append(repos, repo)
		}
	}

	sort.Slice(repos, func(i, j int) bool {
		// Keep collections and their children together
		keyI := repos[i].DirName
		keyJ := repos[j].DirName
		if repos[i].Parent != "" {
			keyI = repos[i].Parent + "/" + filepath.Base(repos[i].Path)
		}
		if repos[j].Parent != "" {
			keyJ = repos[j].Parent + "/" + filepath.Base(repos[j].Path)
		}
		return keyI < keyJ
	})

	return repos
}
