package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/stevenlawton/hive/bus"

	tea "charm.land/bubbletea/v2"
)

func main() {
	// Drop any Claude Code session vars inherited from a parent claude
	// process, so sessions hive spawns start as top-level, not children.
	for _, kv := range os.Environ() {
		if k, _, ok := strings.Cut(kv, "="); ok && strings.HasPrefix(k, "CLAUDE_CODE_") {
			os.Unsetenv(k)
		}
	}

	// CLI subcommand dispatch — must come before the TUI opens.
	if len(os.Args) > 1 && os.Args[1] == "bus" {
		os.Exit(runBusCmd(os.Args[2:]))
	}
	if len(os.Args) > 1 && os.Args[1] == "todo" {
		os.Exit(runTodoCmd(os.Args[2:]))
	}
	if len(os.Args) > 1 && os.Args[1] == "serve" {
		os.Exit(runServeCmd(os.Args[2:]))
	}
	if len(os.Args) > 1 && os.Args[1] == "tokens" {
		os.Exit(runTokensCmd(os.Args[2:]))
	}

	home, _ := os.UserHomeDir()
	cfgPath := filepath.Join(home, ".config", "hive", "config.yaml")

	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error loading config: %v\n", err)
		os.Exit(1)
	}

	if err := startSessionWatcher(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: session watcher failed to start: %v\n", err)
	}

	// Auto-wire the bus into Claude Code: hooks, CLAUDE.md section, and
	// the native MCP server. All three installers are idempotent and
	// update the binary path in place if it has changed.
	if exe, err := os.Executable(); err == nil {
		if err := bus.InstallClaudeHook(exe); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to install bus hook: %v\n", err)
		}
		if err := bus.InstallClaudeMd(exe); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to install CLAUDE.md section: %v\n", err)
		}
		if err := bus.InstallMCPServer(exe); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to install bus MCP server: %v\n", err)
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
