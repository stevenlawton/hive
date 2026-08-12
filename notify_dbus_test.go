package main

import (
	"strings"
	"testing"
)

// Real dbus-monitor output, captured from GNOME Shell 50.1 while clicking a
// notification. The id and action arrive on the two lines after the member
// line, and ActivationToken precedes ActionInvoked for the same click.
const dbusClickTrace = `signal time=1786549187.668112 sender=:1.28 -> destination=(null destination) serial=11050 path=/org/freedesktop/Notifications; interface=org.freedesktop.Notifications; member=ActivationToken
   uint32 31
   string "gnome-shell-1234"
signal time=1786549187.668131 sender=:1.28 -> destination=(null destination) serial=11051 path=/org/freedesktop/Notifications; interface=org.freedesktop.Notifications; member=ActionInvoked
   uint32 31
   string "default"
signal time=1786549187.668178 sender=:1.28 -> destination=(null destination) serial=11052 path=/org/freedesktop/Notifications; interface=org.freedesktop.Notifications; member=NotificationClosed
   uint32 31
   uint32 2
`

type sig struct {
	kind string // "action" or "closed"
	id   string
	name string
}

func collect(trace string) []sig {
	var got []sig
	scanNotificationSignals(strings.NewReader(trace),
		func(id, action string) { got = append(got, sig{"action", id, action}) },
		func(id string) { got = append(got, sig{"closed", id, ""}) })
	return got
}

func TestScanNotificationSignalsParsesAClick(t *testing.T) {
	got := collect(dbusClickTrace)

	var actions []sig
	for _, s := range got {
		if s.kind == "action" {
			actions = append(actions, s)
		}
	}
	if len(actions) != 1 {
		t.Fatalf("want exactly 1 action, got %d: %v", len(actions), got)
	}
	if actions[0].id != "31" || actions[0].name != "default" {
		t.Errorf("got id=%q action=%q, want 31/default", actions[0].id, actions[0].name)
	}
}

// ActivationToken carries the same id and a string payload, so a parser that
// keys off "the next uint32 then the next string" without checking the member
// would report every click twice.
func TestScanNotificationSignalsIgnoresActivationToken(t *testing.T) {
	for _, s := range collect(dbusClickTrace) {
		if s.kind == "action" && s.name == "gnome-shell-1234" {
			t.Fatal("ActivationToken was mistaken for an invoked action")
		}
	}
}

func TestScanNotificationSignalsParsesAClose(t *testing.T) {
	var closed []string
	for _, s := range collect(dbusClickTrace) {
		if s.kind == "closed" {
			closed = append(closed, s.id)
		}
	}
	if len(closed) != 1 || closed[0] != "31" {
		t.Errorf("want one close for id 31, got %v", closed)
	}
}

func TestScanNotificationSignalsIgnoresUnrelatedTraffic(t *testing.T) {
	noise := `signal time=1 sender=:1.28 path=/org/freedesktop/Notifications; interface=org.freedesktop.Notifications; member=SomethingElse
   uint32 5
   string "default"
`
	if got := collect(noise); len(got) != 0 {
		t.Errorf("unrelated signals should be ignored, got %v", got)
	}
}

func TestParseNotifyID(t *testing.T) {
	if got, err := parseNotifyID("(uint32 31,)\n"); err != nil || got != "31" {
		t.Errorf("got %q err=%v, want 31", got, err)
	}
	if _, err := parseNotifyID("nonsense"); err == nil {
		t.Error("unparseable reply should be an error, not a silent empty id")
	}
}
