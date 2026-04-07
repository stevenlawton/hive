package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
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

// NotifySessionEvent dispatches notifications for a session event based on config.
// Returns true if the KittyLauncher tab should flash.
func NotifySessionEvent(cfg *NotificationConfig, ev SessionEvent) bool {
	// Only notify on meaningful events
	switch ev.Event {
	case "completed", "ended":
		// Always notify
	default:
		return false
	}

	title := fmt.Sprintf("KittyLauncher: %s", ev.Repo)
	message := fmt.Sprintf("Claude session %s", ev.Event)

	if cfg.Sound {
		go playSound(cfg.SoundPath)
	}
	if cfg.Desktop {
		go sendDesktopNotification(title, message)
	}
	if cfg.WebhookURL != "" {
		go sendWebhook(cfg.WebhookURL, ev)
	}
	if cfg.NtfyTopic != "" {
		go sendNtfy(cfg.NtfyTopic, title, message)
	}
	if cfg.SlackWebhook != "" {
		go sendSlack(cfg.SlackWebhook, title, message)
	}

	return cfg.TabFlash
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

func sendDesktopNotification(title, message string) {
	exec.Command("notify-send", "--urgency=normal", title, message).Run()
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
