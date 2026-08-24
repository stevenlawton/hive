package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	tmuxPrefix         = "hive-"
	tmuxRemotePrefix   = "hive-rc-"
	tmuxScratchPfx     = "hive-scratch-"
	legacyPrefix       = "kl-"
	legacyRemotePrefix = "kl-rc-"
	legacyScratchPfx   = "kl-scratch-"

	// tmuxControlSessionName is a dedicated tmux session that the
	// persistent `tmux -C` control client attaches to. Using our own
	// session keeps it out of reach of the pruning logic — no prefix
	// match so ParseTmuxSessions skips it, non-numeric so the
	// numbered-session prune skips it, and it has no cwd-dependent
	// zombie semantics. Lifetime is tied to hive: killed on clean
	// exit, and any leftover is killed at next startup.
	tmuxControlSessionName = "__hive_control__"
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

// tmuxTarget formats a session name as an exact-match target for tmux
// subcommands that take a target-session: has-session, kill-session,
// rename-session, attach-session, set-environment, show-environment.
// Without the leading "=", tmux treats the argument as a glob/prefix
// and `hive-foo` will match `hive-foo_com`, which causes cross-session
// bleed when two repos share a name prefix (e.g. `stevenlawton` and
// `stevenlawton.com`).
//
// For target-pane / target-window arguments (send-keys, capture-pane,
// display-message, copy-mode, list-panes, list-windows, resize-window)
// use tmuxPaneTarget — bare "=name" is rejected as "can't find pane"
// by those commands.
func tmuxTarget(name string) string {
	return "=" + name
}

// tmuxPaneTarget formats a session name as an exact-match target for
// tmux subcommands that take a target-pane or target-window. tmux's
// "=" prefix only applies to the session_arg portion of a pane target,
// so we append ":" to mark the rest as the default window/pane —
// "=name:" means "exact session match, first window, first pane".
//
// Without the trailing ":", commands like send-keys and capture-pane
// look up "=name" as a literal pane name and fail with "can't find
// pane: =name", which is what produced the "No session" placeholder
// in workspace tabs after the exact-match rollout.
func tmuxPaneTarget(name string) string {
	return "=" + name + ":"
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
	return []string{"send-keys", "-t", tmuxPaneTarget(sessionName), command, "Enter"}
}

func tmuxHasSessionArgs(sessionName string) []string {
	return []string{"has-session", "-t", tmuxTarget(sessionName)}
}

func tmuxKillSessionArgs(sessionName string) []string {
	return []string{"kill-session", "-t", tmuxTarget(sessionName)}
}

func tmuxListSessionsArgs() []string {
	return []string{"list-sessions"}
}

func tmuxPaneTitleArgs(sessionName string) []string {
	return []string{"display-message", "-t", tmuxPaneTarget(sessionName), "-p", "#{pane_title}"}
}

func tmuxCapturePaneArgs(sessionName string) []string {
	return []string{"capture-pane", "-p", "-e", "-t", tmuxPaneTarget(sessionName)}
}

func tmuxCapturePaneFullArgs(sessionName string) []string {
	return []string{"capture-pane", "-p", "-e", "-S", "-", "-E", "-", "-t", tmuxPaneTarget(sessionName)}
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

// tmuxControl is a persistent control-mode connection to tmux.
// Commands are sent over stdin — no process spawning per key.
var tmuxControl struct {
	sync.Mutex
	stdin   io.Writer
	cmd     *exec.Cmd
	started bool
	done    chan struct{}
}

// StartTmuxControl opens a persistent tmux control-mode connection
// attached to a dedicated hive-owned session (tmuxControlSessionName).
// Any leftover control session from a previous crashed run is killed
// first — that's how the session gets pruned without becoming
// immortal.
func StartTmuxControl() error {
	tmuxControl.Lock()
	defer tmuxControl.Unlock()
	if tmuxControl.started {
		return nil
	}

	_ = tmuxRun("kill-session", "-t", tmuxTarget(tmuxControlSessionName))
	if err := tmuxRun("new-session", "-d", "-s", tmuxControlSessionName); err != nil {
		return fmt.Errorf("create control session: %w", err)
	}

	// Control mode is an optimisation, not a requirement, and it can be
	// broken while ordinary tmux commands still work — a tmux server
	// outliving an ncurses upgrade will hang the attach handshake. Left
	// unchecked that silently swallows every keystroke, so probe once and
	// stay on plain commands if the probe doesn't come back.
	if !controlModeWorks() {
		fmt.Fprintln(os.Stderr,
			"hive: tmux control mode unavailable, using direct commands")
		return nil
	}

	cmd := exec.Command("tmux", "-C", "attach-session", "-t", tmuxTarget(tmuxControlSessionName))
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard

	if err := cmd.Start(); err != nil {
		return err
	}

	done := make(chan struct{})
	tmuxControl.cmd = cmd
	tmuxControl.stdin = stdin
	tmuxControl.started = true
	tmuxControl.done = done

	// The client can exit while its session lives on, and then it is only a
	// zombie holding a pipe nobody reads. Reap it and retire the connection
	// so later commands take the direct path instead of vanishing.
	go func() {
		cmd.Wait()
		retireTmuxControl()
		close(done)
	}()
	return nil
}

// retireTmuxControl marks the control connection unusable. Callers fall back
// to spawning tmux commands, which is how hive runs when control mode was
// never available in the first place.
func retireTmuxControl() {
	tmuxControl.Lock()
	defer tmuxControl.Unlock()
	tmuxControl.started = false
	tmuxControl.stdin = nil
}

// tmuxControlActive reports whether the persistent control connection is up.
func tmuxControlActive() bool {
	tmuxControl.Lock()
	defer tmuxControl.Unlock()
	return tmuxControl.started && tmuxControl.stdin != nil
}

// controlModeProbeTimeout bounds the startup probe. Control mode either
// answers immediately or is wedged; there is no slow-but-working case.
const controlModeProbeTimeout = 2 * time.Second

// controlModeWorks reports whether a control client can attach and run a
// command. A wedged handshake shows up as the probe timing out.
func controlModeWorks() bool {
	ctx, cancel := context.WithTimeout(context.Background(), controlModeProbeTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "tmux", "-C", "attach-session",
		"-t", tmuxTarget(tmuxControlSessionName))
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return false
	}
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return false
	}
	fmt.Fprint(stdin, "display-message -p probe\ndetach\n")
	stdin.Close()

	return cmd.Wait() == nil && ctx.Err() == nil
}

// controlWrite posts a command to the control connection. It reports false
// when the command was NOT delivered — the connection is down, or the write
// failed — and the caller must run the command directly instead. A failed
// write retires the connection: a pipe whose reader has gone will never come
// back, and silently swallowing keystrokes is worse than spawning processes.
func controlWrite(command string) bool {
	tmuxControl.Lock()
	w := tmuxControl.stdin
	if !tmuxControl.started || w == nil {
		tmuxControl.Unlock()
		return false
	}
	_, err := fmt.Fprintf(w, "%s\n", command)
	tmuxControl.Unlock()

	if err != nil {
		retireTmuxControl()
		return false
	}
	return true
}

// TmuxControlSend sends a command through the persistent control connection.
// Falls back to spawning a process if control mode isn't available.
func TmuxControlSend(command string) error {
	if controlWrite(command) {
		return nil
	}
	// Fallback: parse and run as regular command
	args := strings.Fields(command)
	return tmuxRun(args...)
}

// StopTmuxControl closes the persistent connection and tears down the
// dedicated control session, so nothing is left behind on clean exit.
func StopTmuxControl() {
	tmuxControl.Lock()
	if tmuxControl.stdin != nil {
		fmt.Fprintf(tmuxControl.stdin, "detach\n")
	}
	done := tmuxControl.done
	tmuxControl.Unlock()

	// The reaper goroutine owns Wait; block on it rather than calling Wait
	// again here, and give up rather than hang if the client ignores detach.
	if done != nil {
		select {
		case <-done:
		case <-time.After(controlModeProbeTimeout):
		}
	}
	_ = tmuxRun("kill-session", "-t", tmuxTarget(tmuxControlSessionName))
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

// TmuxCursorPos reports the pane's cursor position (0-based column, row from the
// top of the visible pane) and whether the cursor is currently visible. tmux's
// capture-pane omits the cursor entirely, so this is how hive learns where to
// draw a real cursor over a captured session. ok is false when the running app
// has hidden the cursor (cursor_flag == 0) or the query fails.
func TmuxCursorPos(sessionName string) (x, y int, ok bool) {
	out, err := tmuxOutput("display-message", "-p", "-t", tmuxPaneTarget(sessionName),
		"-F", "#{cursor_x} #{cursor_y} #{cursor_flag}")
	if err != nil {
		return 0, 0, false
	}
	fields := strings.Fields(out)
	if len(fields) < 3 || fields[2] == "0" {
		return 0, 0, false
	}
	cx, err1 := strconv.Atoi(fields[0])
	cy, err2 := strconv.Atoi(fields[1])
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	return cx, cy, true
}

// bubbleteaToTmuxKey translates a Bubbletea v2 key string to the
// equivalent tmux send-keys key name.
func bubbleteaToTmuxKey(key string) string {
	// Named keys
	switch key {
	case "backspace":
		return "BSpace"
	case "escape", "esc":
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

	// Shift combinations: shift+a → A, shift+enter → S-Enter, etc.
	if strings.HasPrefix(key, "shift+") {
		inner := strings.TrimPrefix(key, "shift+")
		// Single letter → uppercase
		if len(inner) == 1 && inner[0] >= 'a' && inner[0] <= 'z' {
			return strings.ToUpper(inner)
		}
		// Named keys → tmux S- prefix
		if named := bubbleteaToTmuxKey(inner); named != inner {
			return "S-" + named
		}
		return "S-" + inner
	}

	return key
}

func TmuxSendRawKeys(sessionName string, keys ...string) error {
	translated := make([]string, len(keys))
	for i, k := range keys {
		translated[i] = bubbleteaToTmuxKey(k)
	}
	if controlWrite("send-keys -t " + tmuxPaneTarget(sessionName) + " " +
		strings.Join(translated, " ")) {
		return nil
	}
	// Direct form: the generic fallback re-splits on whitespace, which is
	// wrong for anything quoted.
	return tmuxRun(append([]string{"send-keys", "-t", tmuxPaneTarget(sessionName)},
		translated...)...)
}

// TmuxSendWheel forwards SGR mouse wheel events to a pane, `count` notches at a
// time. Claude renders fullscreen on the alternate screen and scrolls its
// transcript only a few lines per wheel notch (scroll:lineUp/lineDown);
// PageUp/PageDown jump half a viewport, which is too coarse. We send the raw
// SGR sequence (ESC[<64;1;1M up / ESC[<65;1;1M down) as hex codepoints so tmux
// passes it straight through to the app, bypassing alternate-scroll's
// wheel→arrow translation.
func TmuxSendWheel(sessionName string, up bool, count int) error {
	if count < 1 {
		count = 1
	}
	// ESC [ < 6 4 ; 1 ; 1 M  — the '4' (0x34) becomes '5' (0x35) for wheel-down.
	seq := []string{"1b", "5b", "3c", "36", "34", "3b", "31", "3b", "31", "4d"}
	if !up {
		seq[4] = "35"
	}
	one := strings.Join(seq, " ")
	hexes := make([]string, count)
	for i := range hexes {
		hexes[i] = one
	}
	cmd := "send-keys -t " + tmuxPaneTarget(sessionName) + " -H " + strings.Join(hexes, " ")
	return TmuxControlSend(cmd)
}

func TmuxSendLiteral(sessionName, text string) error {
	// Quote it for the tmux command parser.
	if controlWrite(fmt.Sprintf("send-keys -t %s -l %q",
		tmuxPaneTarget(sessionName), text)) {
		return nil
	}
	// Direct form passes the text as one argv entry, so no quoting is
	// needed and none can be mis-parsed.
	return tmuxRun("send-keys", "-t", tmuxPaneTarget(sessionName), "-l", text)
}

// TmuxCopyModeScroll enters copy-mode (if needed) and scrolls up or down.
// The -e flag auto-exits copy-mode when scrolling hits the bottom.
func TmuxCopyModeScroll(sessionName string, up bool) {
	target := tmuxPaneTarget(sessionName)
	tmuxRun("copy-mode", "-t", target, "-e")
	if up {
		tmuxRun("send-keys", "-t", target, "-X", "scroll-up")
		tmuxRun("send-keys", "-t", target, "-X", "scroll-up")
		tmuxRun("send-keys", "-t", target, "-X", "scroll-up")
	} else {
		tmuxRun("send-keys", "-t", target, "-X", "scroll-down")
		tmuxRun("send-keys", "-t", target, "-X", "scroll-down")
		tmuxRun("send-keys", "-t", target, "-X", "scroll-down")
	}
}

func TmuxResizePane(sessionName string, width, height int) error {
	return tmuxRun("resize-window", "-t", tmuxPaneTarget(sessionName), "-x", fmt.Sprintf("%d", width), "-y", fmt.Sprintf("%d", height))
}

func TmuxRenameSession(oldName, newName string) error {
	return tmuxRun("rename-session", "-t", tmuxTarget(oldName), newName)
}

func TmuxSetEnv(sessionName, key, value string) {
	tmuxRun("set-environment", "-t", tmuxTarget(sessionName), key, value)
}

func TmuxGetEnv(sessionName, key string) string {
	out, err := tmuxOutput("show-environment", "-t", tmuxTarget(sessionName), key)
	if err != nil {
		return ""
	}
	// Output format: "KEY=value\n"
	s := strings.TrimSpace(out)
	if idx := strings.IndexByte(s, '='); idx >= 0 {
		return s[idx+1:]
	}
	return ""
}

func TmuxSessionCwd(sessionName string) (string, error) {
	out, err := tmuxOutput("display-message", "-t", tmuxPaneTarget(sessionName), "-p", "#{pane_current_path}")
	if err != nil {
		return "", fmt.Errorf("failed to get cwd for session %s: %w", sessionName, err)
	}
	return strings.TrimSpace(out), nil
}
