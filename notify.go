package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

func DetectDeadSessions(items []repoItem, liveSessions map[string]bool) map[string]string {
	alerts := make(map[string]string)
	for _, item := range items {
		if item.status == statusNone || item.tmuxSes == "" {
			continue
		}
		if !liveSessions[item.tmuxSes] {
			alerts[item.repo.DirName] = "session crashed"
		}
	}
	return alerts
}

func DetectDeadRemotes(items []repoItem, liveSessions map[string]bool) map[string]string {
	alerts := make(map[string]string)
	for _, item := range items {
		if item.status != statusRemote {
			continue
		}
		rcName := TmuxSessionName(item.repo.DirName, true)
		if !liveSessions[rcName] {
			alerts[item.repo.DirName] = "remote died"
		}
	}
	return alerts
}

// NotifySessionEvent reports whether a session event should set the tab flash.
// Pure predicate — the actual desktop / sound / ntfy / slack / webhook
// notifications are driven by handleAttention's escalation levels (which have
// proper visibility-aware timing), so this function intentionally has no side
// effects. Letting both paths fire was the spam source: the JSONL watcher
// emits one "completed" per assistant content block (thinking, then text),
// so a single claude turn produced multiple back-to-back notifications.
func NotifySessionEvent(cfg *NotificationConfig, ev SessionEvent) bool {
	switch ev.Event {
	case "completed", "ended":
		return cfg.TabFlash
	}
	return false
}

func playSound(soundPath string) {
	if soundPath != "" {
		// Try paplay first (PulseAudio), fall back to aplay (ALSA)
		if err := exec.Command("paplay", soundPath).Run(); err != nil {
			exec.Command("aplay", soundPath).Run()
		}
	} else {
		// System bell — write directly to TTY (bubbletea owns stdout)
		if tty, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0); err == nil {
			tty.WriteString("\a")
			tty.Close()
		}
	}
}

// desktopNotifier keeps one notification slot per repo. Each repo's first
// alert records the id notify-send prints; later alerts for that repo pass it
// back as --replace-id so the existing drawer entry is rewritten in place
// rather than stacking a new one. Without this, five sessions completing a
// turn apiece buried the notification drawer.
//
// Worktrees fold into their parent's slot (see repoGroupKey), so a repo never
// occupies more than one entry no matter how many splits it is running.
// hiveWindowTitle is what hive names its terminal window, so a window
// manager can be asked to raise that exact window on notification click.
const hiveWindowTitle = "hive"

// hiveWindowClass is the WM_CLASS of the terminal hive runs in. Matching on
// it is the fallback for when the OSC title has been overwritten — tmux and
// the shell both rewrite the title, so it cannot be relied on alone.
const hiveWindowClass = "gnome-terminal-server.Gnome-terminal"

// notifySender starts a notification and returns its id plus a channel that
// yields action keys as the user invokes them. The channel closes when the
// notification is dismissed or replaced.
type notifySender func(args []string) (id string, actions <-chan string, err error)

type desktopNotifier struct {
	mu   sync.Mutex
	ids  map[string]string
	send notifySender
	// onClick fires with the repo key when the user clicks a notification.
	onClick func(repoKey string)
}

func newDesktopNotifier(send notifySender) *desktopNotifier {
	if send == nil {
		send = notifySend
	}
	return &desktopNotifier{ids: map[string]string{}, send: send}
}

// Notify raises or updates the notification for one repo.
func (n *desktopNotifier) Notify(repoKey, title, message string) {
	if n == nil {
		return
	}
	n.mu.Lock()
	prev := n.ids[repoKey]
	n.mu.Unlock()

	args := []string{
		"--urgency=normal",
		"--print-id",
		"--app-name=hive",
		"--action=default=Open",
	}
	if prev != "" {
		// A stale id (daemon restarted, user dismissed it) is harmless —
		// the spec says replace an unknown id by creating a new one.
		args = append(args, "--replace-id="+prev)
	}
	args = append(args, title, message)

	id, actions, err := n.send(args)
	if err != nil || id == "" {
		return
	}
	n.mu.Lock()
	n.ids[repoKey] = id
	n.mu.Unlock()

	if actions == nil {
		return
	}
	go func() {
		// Any action on this notification means the user clicked it; the
		// only one offered is "default".
		for range actions {
			if n.onClick != nil {
				n.onClick(repoKey)
			}
		}
	}()
}

// notifySend runs notify-send and streams back any action the user invokes.
//
// --action implies --wait, so the process lives until the notification is
// clicked, dismissed or replaced. That makes stdbuf necessary: without it
// libnotify's stdout is fully buffered when piped and the id would not
// surface until exit, long after it is needed for --replace-id.
func notifySend(args []string) (string, <-chan string, error) {
	if _, err := exec.LookPath("stdbuf"); err != nil {
		return notifySendNoActions(args)
	}
	cmd := exec.Command("stdbuf", append([]string{"-oL", "notify-send"}, args...)...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", nil, err
	}
	if err := cmd.Start(); err != nil {
		return "", nil, err
	}

	reader := bufio.NewReader(stdout)
	line, err := reader.ReadString('\n')
	if err != nil {
		cmd.Process.Kill()
		cmd.Wait()
		return "", nil, err
	}

	actions := make(chan string, 1)
	go func() {
		defer close(actions)
		defer cmd.Wait()
		for {
			l, err := reader.ReadString('\n')
			if key := strings.TrimSpace(l); key != "" {
				actions <- key
			}
			if err != nil {
				return
			}
		}
	}()
	return strings.TrimSpace(line), actions, nil
}

// notifySendNoActions is the degraded path for systems without stdbuf: the
// notification still shows and still collapses per repo, but clicking it
// cannot reach hive.
func notifySendNoActions(args []string) (string, <-chan string, error) {
	filtered := args[:0:0]
	for _, a := range args {
		if strings.HasPrefix(a, "--action=") {
			continue
		}
		filtered = append(filtered, a)
	}
	out, err := exec.Command("notify-send", filtered...).Output()
	if err != nil {
		return "", nil, err
	}
	return strings.TrimSpace(string(out)), nil, nil
}

// setHiveWindowTitle names the hosting terminal window via OSC 0, giving
// raiseHiveWindow something to match on. Written straight to the tty because
// bubbletea owns stdout — the same route playSound uses for the bell.
func setHiveWindowTitle() {
	tty, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0)
	if err != nil {
		return
	}
	defer tty.Close()
	tty.WriteString("\033]0;" + hiveWindowTitle + "\007")
}

// raiseHiveWindow asks the window manager to focus hive's terminal window,
// by title first and window class second. Best-effort and silent: with
// neither helper installed the click still switches the hive tab, it just
// cannot bring the window forward.
func raiseHiveWindow() {
	if path, err := exec.LookPath("wmctrl"); err == nil {
		if exec.Command(path, "-a", hiveWindowTitle).Run() == nil {
			return
		}
		exec.Command(path, "-x", "-a", hiveWindowClass).Run()
		return
	}
	if path, err := exec.LookPath("xdotool"); err == nil {
		if exec.Command(path, "search", "--name", "^"+hiveWindowTitle+"$",
			"windowactivate").Run() == nil {
			return
		}
		exec.Command(path, "search", "--class", "gnome-terminal",
			"windowactivate").Run()
	}
}

// repoGroupKey is the notification slot a repo or worktree belongs to.
func repoGroupKey(r Repo) string {
	if r.IsWorktree && r.Parent != "" {
		return r.Parent
	}
	return r.DirName
}

func sendWebhook(url string, ev SessionEvent) {
	body, err := json.Marshal(ev)
	if err != nil {
		return
	}
	client := &http.Client{Timeout: 5 * time.Second}
	client.Post(url, "application/json", bytes.NewReader(body))
}

func sendNtfy(topic, title, message string) {
	url := "https://ntfy.sh/" + topic
	req, err := http.NewRequest("POST", url, bytes.NewBufferString(message))
	if err != nil {
		return
	}
	req.Header.Set("Title", title)
	client := &http.Client{Timeout: 5 * time.Second}
	client.Do(req)
}

func sendSlack(webhookURL, title, message string) {
	payload := map[string]string{
		"text": fmt.Sprintf("*%s*\n%s", title, message),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return
	}
	client := &http.Client{Timeout: 5 * time.Second}
	client.Post(webhookURL, "application/json", bytes.NewReader(body))
}
