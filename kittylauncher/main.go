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

	p := tea.NewProgram(newModel(cfg))
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
