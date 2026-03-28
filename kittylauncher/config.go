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
	Name      string `yaml:"name,omitempty"`
	Short     string `yaml:"short,omitempty"`
	Color     string `yaml:"color,omitempty"`
	Remote    bool   `yaml:"remote,omitempty"`
	Favourite bool   `yaml:"favourite,omitempty"`
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
	DirName   string
	Path      string
	Name      string
	Short     string
	Color     string
	Remote    bool
	Favourite bool
	IsScratch bool
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
