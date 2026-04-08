package main

import (
	"fmt"
	"os/exec"
	"strings"
)

const (
	tmuxPrefix         = "hive-"
	tmuxRemotePrefix   = "hive-rc-"
	tmuxScratchPfx     = "hive-scratch-"
	legacyPrefix       = "kl-"
	legacyRemotePrefix = "kl-rc-"
	legacyScratchPfx   = "kl-scratch-"
)

type TmuxSession struct {
	Name      string
	IsRemote  bool
	IsScratch bool
	RepoKey   string
}

// sanitizeSessionName replaces chars that tmux doesn't allow in session names
func sanitizeSessionName(name string) string {
	return strings.NewReplacer(".", "_", ":", "_", " ", "_").Replace(name)
}

func TmuxSessionName(dirName string, remote bool) string {
	safe := sanitizeSessionName(dirName)
	if remote {
		return tmuxRemotePrefix + safe
	}
	return tmuxPrefix + safe
}

func tmuxNewSessionArgs(sessionName, cwd string) []string {
	return []string{"new-session", "-d", "-s", sessionName, "-c", cwd}
}

func tmuxNewSessionWithCmdArgs(sessionName, cwd, command string) []string {
	return []string{"new-session", "-d", "-s", sessionName, "-c", cwd, command}
}

func TmuxNewSessionWithCmd(sessionName, cwd, command string) error {
	return tmuxRun(tmuxNewSessionWithCmdArgs(sessionName, cwd, command)...)
}

func tmuxSendKeysArgs(sessionName, command string) []string {
	return []string{"send-keys", "-t", sessionName, command, "Enter"}
}

func tmuxHasSessionArgs(sessionName string) []string {
	return []string{"has-session", "-t", sessionName}
}

func tmuxKillSessionArgs(sessionName string) []string {
	return []string{"kill-session", "-t", sessionName}
}

func tmuxListSessionsArgs() []string {
	return []string{"list-sessions"}
}

func tmuxPaneTitleArgs(sessionName string) []string {
	return []string{"display-message", "-t", sessionName, "-p", "#{pane_title}"}
}

func tmuxCapturePaneArgs(sessionName string) []string {
	return []string{"capture-pane", "-p", "-e", "-t", sessionName}
}

func tmuxCapturePaneFullArgs(sessionName string) []string {
	return []string{"capture-pane", "-p", "-e", "-S", "-", "-E", "-", "-t", sessionName}
}

func ParseTmuxSessions(output string) []TmuxSession {
	var sessions []TmuxSession
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if line == "" {
			continue
		}
		name := strings.SplitN(line, ":", 2)[0]

		var ses TmuxSession
		ses.Name = name

		switch {
		case strings.HasPrefix(name, tmuxRemotePrefix):
			ses.IsRemote = true
			ses.RepoKey = strings.TrimPrefix(name, tmuxRemotePrefix)
		case strings.HasPrefix(name, legacyRemotePrefix):
			ses.IsRemote = true
			ses.RepoKey = strings.TrimPrefix(name, legacyRemotePrefix)
		case strings.HasPrefix(name, tmuxScratchPfx):
			ses.IsScratch = true
			ses.RepoKey = strings.TrimPrefix(name, tmuxScratchPfx)
		case strings.HasPrefix(name, legacyScratchPfx):
			ses.IsScratch = true
			ses.RepoKey = strings.TrimPrefix(name, legacyScratchPfx)
		case strings.HasPrefix(name, tmuxPrefix):
			ses.RepoKey = strings.TrimPrefix(name, tmuxPrefix)
		case strings.HasPrefix(name, legacyPrefix):
			ses.RepoKey = strings.TrimPrefix(name, legacyPrefix)
		default:
			continue
		}
		sessions = append(sessions, ses)
	}
	return sessions
}

// Exec helpers

func tmuxRun(args ...string) error {
	cmd := exec.Command("tmux", args...)
	return cmd.Run()
}

func tmuxOutput(args ...string) (string, error) {
	cmd := exec.Command("tmux", args...)
	out, err := cmd.Output()
	return string(out), err
}

func TmuxNewSession(sessionName, cwd string) error {
	return tmuxRun(tmuxNewSessionArgs(sessionName, cwd)...)
}

func TmuxSendKeys(sessionName, command string) error {
	return tmuxRun(tmuxSendKeysArgs(sessionName, command)...)
}

func TmuxHasSession(sessionName string) bool {
	return tmuxRun(tmuxHasSessionArgs(sessionName)...) == nil
}

func TmuxKillSession(sessionName string) error {
	return tmuxRun(tmuxKillSessionArgs(sessionName)...)
}

func TmuxListSessions() ([]TmuxSession, error) {
	out, err := tmuxOutput(tmuxListSessionsArgs()...)
	if err != nil {
		if strings.Contains(err.Error(), "no server running") || strings.Contains(string(out), "no server") {
			return nil, nil
		}
		return nil, err
	}
	return ParseTmuxSessions(out), nil
}

func TmuxPaneTitle(sessionName string) (string, error) {
	out, err := tmuxOutput(tmuxPaneTitleArgs(sessionName)...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func TmuxCapturePane(sessionName string) (string, error) {
	return tmuxOutput(tmuxCapturePaneArgs(sessionName)...)
}

func TmuxCapturePaneFull(sessionName string) (string, error) {
	return tmuxOutput(tmuxCapturePaneFullArgs(sessionName)...)
}

// bubbleteaToTmuxKey translates a Bubbletea v2 key string to the
// equivalent tmux send-keys key name.
func bubbleteaToTmuxKey(key string) string {
	// Named keys
	switch key {
	case "backspace":
		return "BSpace"
	case "escape":
		return "Escape"
	case "enter":
		return "Enter"
	case "tab":
		return "Tab"
	case "space":
		return "Space"
	case "up":
		return "Up"
	case "down":
		return "Down"
	case "left":
		return "Left"
	case "right":
		return "Right"
	case "home":
		return "Home"
	case "end":
		return "End"
	case "pgup":
		return "PPage"
	case "pgdown":
		return "NPage"
	case "delete":
		return "DC"
	case "insert":
		return "IC"
	}

	// Function keys: f1 → F1
	if strings.HasPrefix(key, "f") && len(key) >= 2 && len(key) <= 3 {
		if n := key[1:]; n[0] >= '1' && n[0] <= '9' {
			return "F" + n
		}
	}

	// Ctrl combinations: ctrl+a → C-a
	if strings.HasPrefix(key, "ctrl+") {
		return "C-" + strings.TrimPrefix(key, "ctrl+")
	}

	// Alt combinations: alt+a → M-a
	if strings.HasPrefix(key, "alt+") {
		return "M-" + strings.TrimPrefix(key, "alt+")
	}

	return key
}

func TmuxSendRawKeys(sessionName string, keys ...string) error {
	translated := make([]string, len(keys))
	for i, k := range keys {
		translated[i] = bubbleteaToTmuxKey(k)
	}
	args := append([]string{"send-keys", "-t", sessionName}, translated...)
	return tmuxRun(args...)
}

func TmuxSendLiteral(sessionName, text string) error {
	return tmuxRun("send-keys", "-t", sessionName, "-l", text)
}

// TmuxCopyModeScroll enters copy-mode (if needed) and scrolls up or down.
// The -e flag auto-exits copy-mode when scrolling hits the bottom.
func TmuxCopyModeScroll(sessionName string, up bool) {
	tmuxRun("copy-mode", "-t", sessionName, "-e")
	if up {
		tmuxRun("send-keys", "-t", sessionName, "-X", "scroll-up")
		tmuxRun("send-keys", "-t", sessionName, "-X", "scroll-up")
		tmuxRun("send-keys", "-t", sessionName, "-X", "scroll-up")
	} else {
		tmuxRun("send-keys", "-t", sessionName, "-X", "scroll-down")
		tmuxRun("send-keys", "-t", sessionName, "-X", "scroll-down")
		tmuxRun("send-keys", "-t", sessionName, "-X", "scroll-down")
	}
}

func TmuxResizePane(sessionName string, width, height int) error {
	return tmuxRun("resize-window", "-t", sessionName, "-x", fmt.Sprintf("%d", width), "-y", fmt.Sprintf("%d", height))
}

func TmuxWindowHasBell(sessionName string) bool {
	out, err := tmuxOutput("list-windows", "-t", sessionName, "-F", "#{window_bell_flag}")
	if err != nil {
		return false
	}
	return strings.TrimSpace(out) == "1"
}

func TmuxRenameSession(oldName, newName string) error {
	return tmuxRun("rename-session", "-t", oldName, newName)
}

func TmuxSessionCwd(sessionName string) (string, error) {
	out, err := tmuxOutput("display-message", "-t", sessionName, "-p", "#{pane_current_path}")
	if err != nil {
		return "", fmt.Errorf("failed to get cwd for session %s: %w", sessionName, err)
	}
	return strings.TrimSpace(out), nil
}
