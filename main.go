package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/stevenlawton/hive/bus"

	tea "charm.land/bubbletea/v2"
)

func main() {
	// CLI subcommand dispatch — must come before the TUI opens.
	if len(os.Args) > 1 && os.Args[1] == "bus" {
		os.Exit(runBusCmd(os.Args[2:]))
	}

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

	// Auto-wire the bus hooks + CLAUDE.md section into Claude's global
	// settings so every Claude session joins the bus without manual setup.
	// Idempotent — updates the binary path if it has changed.
	if exe, err := os.Executable(); err == nil {
		if err := bus.InstallClaudeHook(exe); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to install bus hook: %v\n", err)
		}
		if err := bus.InstallClaudeMd(exe); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to install CLAUDE.md section: %v\n", err)
		}
	}

	StartTmuxControl()
	defer StopTmuxControl()

	p := tea.NewProgram(newModel(cfg, cfgPath))
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
