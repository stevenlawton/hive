package main

import (
	"fmt"
	"os/exec"
	"strings"
)

const (
	tmuxPrefix       = "kl-"
	tmuxRemotePrefix = "kl-rc-"
	tmuxScratchPfx   = "kl-scratch-"
)

type TmuxSession struct {
	Name      string
	IsRemote  bool
	IsScratch bool
	RepoKey   string
}

func TmuxSessionName(dirName string, remote bool) string {
	if remote {
		return tmuxRemotePrefix + dirName
	}
	return tmuxPrefix + dirName
}

func tmuxNewSessionArgs(sessionName, cwd string) []string {
	return []string{"new-session", "-d", "-s", sessionName, "-c", cwd}
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

func tmuxCapturePaneArgs(sessionName string) []string {
	return []string{"display-message", "-t", sessionName, "-p", "#{pane_title}"}
}

func ParseTmuxSessions(output string) []TmuxSession {
	var sessions []TmuxSession
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if line == "" {
			continue
		}
		name := strings.SplitN(line, ":", 2)[0]
		if !strings.HasPrefix(name, tmuxPrefix) {
			continue
		}
		s := TmuxSession{Name: name}
		if strings.HasPrefix(name, tmuxRemotePrefix) {
			s.IsRemote = true
			s.RepoKey = strings.TrimPrefix(name, tmuxRemotePrefix)
		} else if strings.HasPrefix(name, tmuxScratchPfx) {
			s.IsScratch = true
			s.RepoKey = strings.TrimPrefix(name, tmuxPrefix)
		} else {
			s.RepoKey = strings.TrimPrefix(name, tmuxPrefix)
		}
		sessions = append(sessions, s)
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
	out, err := tmuxOutput(tmuxCapturePaneArgs(sessionName)...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
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
