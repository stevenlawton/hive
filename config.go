package main

import (
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

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

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
