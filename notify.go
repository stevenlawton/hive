package main

import (
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
type desktopNotifier struct {
	mu   sync.Mutex
	ids  map[string]string
	send func(args []string) (string, error)
}

func newDesktopNotifier(send func([]string) (string, error)) *desktopNotifier {
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

	args := []string{"--urgency=normal", "--print-id"}
	if prev != "" {
		// A stale id (daemon restarted, user dismissed it) is harmless —
		// the spec says replace an unknown id by creating a new one.
		args = append(args, "--replace-id="+prev)
	}
	args = append(args, title, message)

	id, err := n.send(args)
	if err != nil || id == "" {
		return
	}
	n.mu.Lock()
	n.ids[repoKey] = id
	n.mu.Unlock()
}

func notifySend(args []string) (string, error) {
	out, err := exec.Command("notify-send", args...).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
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
