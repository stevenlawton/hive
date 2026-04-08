package main

import (
	"fmt"
	"os"
	"path/filepath"

	tea "charm.land/bubbletea/v2"
)

func main() {
	home, _ := os.UserHomeDir()
	cfgPath := filepath.Join(home, ".config", "hive", "config.yaml")

	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error loading config: %v\n", err)
		os.Exit(1)
	}

	if err := startServer(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: event server failed to start: %v\n", err)
	}

	p := tea.NewProgram(newModel(cfg, cfgPath))
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
