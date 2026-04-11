package bus

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// busClaudeMdMarker is the start-of-section marker used to find a
// previously-installed Hive Bus section inside CLAUDE.md so we can
// idempotently replace it instead of appending duplicates.
const busClaudeMdMarker = "<!-- hive-bus:start -->"
const busClaudeMdEnd = "<!-- hive-bus:end -->"

// InstallClaudeMd ensures that ~/.claude/CLAUDE.md contains a section
// documenting the Hive bus commands, so every Claude session learns the
// tools exist without needing per-project setup. Idempotent — replaces a
// previously-installed section in place, or appends if none exists.
//
// The hiveBinary argument is substituted into example commands so Claude
// can copy-paste them even if `hive` isn't on PATH.
func InstallClaudeMd(hiveBinary string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	path := filepath.Join(home, ".claude", "CLAUDE.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	var existing string
	if data, err := os.ReadFile(path); err == nil {
		existing = string(data)
	}

	section := renderBusClaudeMdSection(hiveBinary)

	// If the file already has our section, replace it in place.
	if startIdx := strings.Index(existing, busClaudeMdMarker); startIdx != -1 {
		endIdx := strings.Index(existing, busClaudeMdEnd)
		if endIdx != -1 && endIdx > startIdx {
			endIdx += len(busClaudeMdEnd)
			// If the current content is already identical, do nothing.
			currentSection := existing[startIdx:endIdx]
			if currentSection == section {
				return nil
			}
			updated := existing[:startIdx] + section + existing[endIdx:]
			return os.WriteFile(path, []byte(updated), 0o644)
		}
	}

	// No existing section — append.
	var buf strings.Builder
	buf.WriteString(existing)
	if existing != "" && !strings.HasSuffix(existing, "\n") {
		buf.WriteString("\n")
	}
	if existing != "" {
		buf.WriteString("\n")
	}
	buf.WriteString(section)
	buf.WriteString("\n")
	return os.WriteFile(path, []byte(buf.String()), 0o644)
}

func renderBusClaudeMdSection(_ string) string {
	return busClaudeMdMarker + `
## Hive Bus — cross-session coordination

You are one of several Claude sessions potentially running in parallel
across different worktrees. A shared message bus lets you coordinate with
your peers and the human (Steve).

**Use these native MCP tools** — they appear in your tool inventory
alongside Bash/Read/Edit and should be preferred over the CLI:

- ` + "`hive_bus_intent`" + `  — announce you're about to do something
- ` + "`hive_bus_waiting`" + ` — announce you're blocked on something
- ` + "`hive_bus_done`" + `    — announce you finished something significant
- ` + "`hive_bus_ask`" + `     — ask peers for information you don't have
- ` + "`hive_bus_reply`" + `   — thread a reply to an existing message (by id)
- ` + "`hive_bus_list`" + `    — see recent bus activity
- ` + "`hive_bus_read`" + `    — read the full body of one message

**Be proactive:**
- Before making changes that touch shared types, interfaces, or APIs,
  call ` + "`hive_bus_ask`" + ` to check for conflicts or ` + "`hive_bus_intent`" + ` to
  put the plan on record.
- When you finish a significant change, call ` + "`hive_bus_done`" + ` so
  peers know the work is settled.
- When you're blocked, call ` + "`hive_bus_waiting`" + ` — another peer may
  know how to unblock you.

Hive automatically injects new unread bus messages at the start of each
turn (via UserPromptSubmit and SessionStart hooks). When you see a
'new bus announcement' block at the top of a prompt, read the headlines,
dig into bodies only if relevant, and reply or engage if you have
something useful to add.

The legacy CLI (` + "`hive bus intent …`" + `, etc.) still works and is
equivalent — use whichever is more convenient, but the MCP tools are
preferred because they're visible in your tool list and easier to discover.
` + busClaudeMdEnd
}

// InstallMCPServer idempotently registers the Hive bus MCP server into
// Claude Code's user config (~/.claude.json) so Claudes see native
// hive_bus_* tools in their inventory without any per-worktree setup.
//
// The config shape is: {"mcpServers":{"hive-bus":{"command":"...","args":[...]}}}
// We preserve all other fields and only touch the hive-bus entry.
func InstallMCPServer(hiveBinary string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	path := filepath.Join(home, ".claude.json")

	var settings map[string]any
	if data, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(data, &settings); err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
	}
	if settings == nil {
		settings = map[string]any{}
	}

	mcpServers, _ := settings["mcpServers"].(map[string]any)
	if mcpServers == nil {
		mcpServers = map[string]any{}
		settings["mcpServers"] = mcpServers
	}

	desired := map[string]any{
		"type":    "stdio",
		"command": hiveBinary,
		"args":    []any{"bus", "mcp-serve"},
	}

	// If the existing entry already matches, skip the write.
	if existing, ok := mcpServers["hive-bus"].(map[string]any); ok {
		if fmt.Sprintf("%v", existing["command"]) == hiveBinary {
			if args, ok := existing["args"].([]any); ok && len(args) == 2 &&
				fmt.Sprintf("%v", args[0]) == "bus" && fmt.Sprintf("%v", args[1]) == "mcp-serve" {
				return nil
			}
		}
	}
	mcpServers["hive-bus"] = desired

	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// InstallClaudeHook ensures that Claude Code's global settings file has
// UserPromptSubmit and SessionStart hooks wired up to `hive bus inbox`.
//
// Why not Stop? Claude Code's Stop hook stdout goes to debug logs only — it
// is NOT injected into the model context on the next turn. UserPromptSubmit
// and SessionStart are the only hooks whose stdout becomes context the model
// reads. So we use both: SessionStart surfaces the inbox when a Claude opens
// up, and UserPromptSubmit refreshes it on every user turn.
//
// This function is idempotent and also cleans up any legacy Stop-hook entries
// that earlier versions of the install wrote.
func InstallClaudeHook(hiveBinary string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	settingsDir := filepath.Join(home, ".claude")
	settingsPath := filepath.Join(settingsDir, "settings.json")

	if err := os.MkdirAll(settingsDir, 0o755); err != nil {
		return err
	}

	// Use absolute path so the hook works regardless of $PATH.
	hookCommand := fmt.Sprintf("%s bus inbox", hiveBinary)

	// Load existing settings (or start fresh).
	var settings map[string]any
	if data, err := os.ReadFile(settingsPath); err == nil {
		if err := json.Unmarshal(data, &settings); err != nil {
			return fmt.Errorf("parse %s: %w", settingsPath, err)
		}
	}
	if settings == nil {
		settings = map[string]any{}
	}

	hooks, _ := settings["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
		settings["hooks"] = hooks
	}

	// Clean up any legacy Stop-hook entries from the earlier broken install.
	if stopHooks, ok := hooks["Stop"].([]any); ok {
		cleaned := removeBusEntries(stopHooks)
		if len(cleaned) == 0 {
			delete(hooks, "Stop")
		} else {
			hooks["Stop"] = cleaned
		}
	}

	// Install (or update) the inbox digest hook on UserPromptSubmit and
	// SessionStart — both have stdout piped to model context, so new bus
	// messages appear to Claude at turn boundaries.
	ensureBusHook(hooks, "UserPromptSubmit", hookCommand, "")
	ensureBusHook(hooks, "SessionStart", hookCommand, "")

	// Install the TodoWrite watcher on PostToolUse — this is the main
	// auto-intent surface. Every time Claude updates its plan, we diff
	// against the previous snapshot and auto-announce intent/done events
	// on the bus.
	todoCommand := fmt.Sprintf("%s bus todo-hook", hiveBinary)
	ensureBusHook(hooks, "PostToolUse", todoCommand, "TodoWrite")

	// Write back with indentation so the user can read/edit it.
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(settingsPath, data, 0o644)
}

// ensureBusHook installs or updates a hook entry under the given event key
// in the hooks map. `match` is the tool-name matcher (or "" for events that
// don't use matchers). `hookCommand` is the shell command to run.
//
// Identity: a hook is "ours" if its command contains a marker derived from
// the command itself (last word: "inbox" / "todo-hook"). That way repeated
// installs update the binary path in place instead of duplicating.
func ensureBusHook(hooks map[string]any, event, hookCommand, matcher string) {
	entries, _ := hooks[event].([]any)
	marker := identityMarker(hookCommand)

	for _, entry := range entries {
		entryMap, _ := entry.(map[string]any)
		innerHooks, _ := entryMap["hooks"].([]any)
		for _, h := range innerHooks {
			hMap, _ := h.(map[string]any)
			cmd, _ := hMap["command"].(string)
			if strings.Contains(cmd, marker) {
				hMap["command"] = hookCommand
				if matcher != "" {
					entryMap["matcher"] = matcher
				}
				hooks[event] = entries
				return
			}
		}
	}

	newEntry := map[string]any{
		"matcher": matcher,
		"hooks": []any{
			map[string]any{
				"type":    "command",
				"command": hookCommand,
			},
		},
	}
	hooks[event] = append(entries, newEntry)
}

// identityMarker returns a substring that uniquely identifies a bus hook
// so future installs can find and update it. Currently the last word of
// the command, which is unique per hook type ("inbox", "todo-hook").
func identityMarker(command string) string {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return command
	}
	return fields[len(fields)-1]
}

// removeBusEntries returns a copy of entries with any `bus inbox` hook
// removed. Used to clean up legacy Stop-hook installs.
func removeBusEntries(entries []any) []any {
	var cleaned []any
	for _, entry := range entries {
		entryMap, _ := entry.(map[string]any)
		innerHooks, _ := entryMap["hooks"].([]any)
		var keepHooks []any
		for _, h := range innerHooks {
			hMap, _ := h.(map[string]any)
			cmd, _ := hMap["command"].(string)
			if !strings.Contains(cmd, "bus inbox") {
				keepHooks = append(keepHooks, h)
			}
		}
		if len(keepHooks) > 0 {
			entryMap["hooks"] = keepHooks
			cleaned = append(cleaned, entryMap)
		}
	}
	return cleaned
}
