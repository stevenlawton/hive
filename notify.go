package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"regexp"
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

// notifySender posts a notification and returns the id the server assigned.
// replaceID is the id of the repo's previous notification, or "" if it has
// none, in which case a new entry is created.
type notifySender func(replaceID, title, body string) (id string, err error)

type desktopNotifier struct {
	mu     sync.Mutex
	slots  map[string]string // repo key -> notification id
	owners map[string]string // notification id -> repo key
	send   notifySender
	// onClick fires with the repo key when the user clicks a notification.
	onClick func(repoKey string)
}

func newDesktopNotifier(send notifySender) *desktopNotifier {
	if send == nil {
		send = dbusNotify
	}
	return &desktopNotifier{
		slots:  map[string]string{},
		owners: map[string]string{},
		send:   send,
	}
}

// Notify raises or updates the notification for one repo.
func (n *desktopNotifier) Notify(repoKey, title, message string) {
	if n == nil {
		return
	}
	n.mu.Lock()
	prev := n.slots[repoKey]
	n.mu.Unlock()

	// A stale id (server restarted, user dismissed it) is harmless — the
	// spec says replace an unknown id by creating a new one. That is why the
	// slot is never dropped on close: forgetting it is what turns one entry
	// per repo back into one entry per alert.
	id, err := n.send(prev, title, message)
	if err != nil || id == "" {
		n.mu.Lock()
		delete(n.owners, prev)
		delete(n.slots, repoKey)
		n.mu.Unlock()
		return
	}

	n.mu.Lock()
	if prev != "" && prev != id {
		delete(n.owners, prev)
	}
	n.slots[repoKey] = id
	n.owners[id] = repoKey
	n.mu.Unlock()
}

// handleAction routes an invoked action to the repo that owns the
// notification. "default" is the only action offered, and GNOME fires it when
// the notification body is clicked.
func (n *desktopNotifier) handleAction(id, action string) {
	if n == nil || action == "" {
		return
	}
	n.mu.Lock()
	repoKey, known := n.owners[id]
	n.mu.Unlock()
	if !known {
		return
	}
	if n.onClick != nil {
		n.onClick(repoKey)
	}
}

// handleClosed records that a notification is gone. The slot deliberately
// survives: see Notify.
func (n *desktopNotifier) handleClosed(string) {}

// dbusNotify posts a notification straight to the notification server and
// returns the id it assigned.
//
// hive talks to the bus rather than going through notify-send because
// libnotify 0.8.8 decides GNOME Shell 50 has no action support — it prints
// "Displaying non-interactively" and exits at once, even though the server
// advertises "actions" and does deliver ActionInvoked. That instant exit took
// the click reporting with it, and made hive drop the per-repo slot on every
// alert, which is what buried the drawer.
func dbusNotify(replaceID, title, body string) (string, error) {
	if replaceID == "" {
		replaceID = "0"
	}
	out, err := exec.Command("gdbus", "call", "--session",
		"--dest", "org.freedesktop.Notifications",
		"--object-path", "/org/freedesktop/Notifications",
		"--method", "org.freedesktop.Notifications.Notify",
		// "--" or gdbus reads the -1 expire timeout as one of its own flags.
		"--", "hive", replaceID, "", title, body,
		"['default', 'Open']", "{}", "-1").Output()
	if err != nil {
		return notifySendFallback(replaceID, title, body)
	}
	return parseNotifyID(string(out))
}

// notifyIDPattern matches the id in a gdbus reply such as "(uint32 31,)".
var notifyIDPattern = regexp.MustCompile(`uint32 (\d+)`)

func parseNotifyID(reply string) (string, error) {
	m := notifyIDPattern.FindStringSubmatch(reply)
	if m == nil {
		return "", fmt.Errorf("unrecognised notify reply: %q", strings.TrimSpace(reply))
	}
	return m[1], nil
}

// notifySendFallback is the display-only path for a box without gdbus. The
// notification still shows and still collapses per repo; it just cannot report
// a click, because that is the part notify-send no longer does.
func notifySendFallback(replaceID, title, body string) (string, error) {
	args := []string{"--app-name=hive", "--print-id"}
	if replaceID != "" && replaceID != "0" {
		args = append(args, "--replace-id="+replaceID)
	}
	out, err := exec.Command("notify-send", append(args, title, body)...).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// watchNotificationActions streams signals from the notification server for as
// long as hive runs, routing clicks back to the repo that owns each
// notification. Best-effort: without dbus-monitor the notifications still
// appear and still collapse, they just are not clickable.
func (n *desktopNotifier) watchNotificationActions() {
	if n == nil {
		return
	}
	path, err := exec.LookPath("dbus-monitor")
	if err != nil {
		return
	}
	go func() {
		cmd := exec.Command(path, "--session",
			"type='signal',interface='org.freedesktop.Notifications'")
		stdout, err := cmd.StdoutPipe()
		if err != nil || cmd.Start() != nil {
			return
		}
		defer cmd.Wait()
		scanNotificationSignals(stdout, n.handleAction, n.handleClosed)
	}()
}

// scanNotificationSignals parses dbus-monitor output, which prints a header
// line naming the member and then one line per argument:
//
//	... member=ActionInvoked
//	   uint32 31
//	   string "default"
//
// The member has to gate the argument lines: ActivationToken carries the same
// id-then-string shape for the same click, so parsing on shape alone would
// report every click twice.
func scanNotificationSignals(r io.Reader, onAction func(id, action string), onClosed func(id string)) {
	const (
		idle = iota
		wantActionID
		wantActionName
		wantClosedID
	)
	state := idle
	id := ""

	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if strings.HasPrefix(line, "signal ") {
			switch {
			case strings.HasSuffix(line, "member=ActionInvoked"):
				state = wantActionID
			case strings.HasSuffix(line, "member=NotificationClosed"):
				state = wantClosedID
			default:
				state = idle
			}
			continue
		}

		switch state {
		case wantActionID:
			if v, ok := strings.CutPrefix(line, "uint32 "); ok {
				id, state = v, wantActionName
			}
		case wantActionName:
			if v, ok := strings.CutPrefix(line, "string "); ok {
				onAction(id, strings.Trim(v, `"`))
				state = idle
			}
		case wantClosedID:
			if v, ok := strings.CutPrefix(line, "uint32 "); ok {
				onClosed(v)
				state = idle
			}
		}
	}
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
