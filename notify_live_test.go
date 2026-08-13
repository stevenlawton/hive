package main

import (
	"os"
	"os/exec"
	"testing"
	"time"
)

// These exercise the real session bus. They are the only way to catch the two
// things unit tests cannot: that dbus-monitor's output actually reaches us
// through a pipe (it is a different buffering regime from a file redirect),
// and that gdbus's reply parses into an id we can replace against later.
func requireSessionBus(t *testing.T) {
	t.Helper()
	// These post real notifications to the real desktop, so they stay out of
	// the way of a routine `go test -short ./...`.
	if testing.Short() {
		t.Skip("short mode: skipping tests that post to the desktop")
	}
	if os.Getenv("DBUS_SESSION_BUS_ADDRESS") == "" {
		t.Skip("no session bus")
	}
	for _, bin := range []string{"gdbus", "dbus-monitor"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("%s not installed", bin)
		}
	}
}

func TestLiveNotifyReturnsAnID(t *testing.T) {
	requireSessionBus(t)

	id, err := dbusNotify("", "hive self-test", "parsing the reply")
	if err != nil {
		t.Fatalf("dbusNotify: %v", err)
	}
	if id == "" {
		t.Fatal("no id returned — the slot would never be stored, so nothing coalesces")
	}
	t.Logf("first id = %s", id)

	// Replacing must come back with the same id, which is what makes one
	// notification per repo rather than one per alert.
	again, err := dbusNotify(id, "hive self-test", "replacing in place")
	if err != nil {
		t.Fatalf("replace: %v", err)
	}
	if again != id {
		t.Errorf("replace returned id %s, want %s — coalescing would break", again, id)
	}
	closeNotification(t, id)
}

// The scanner reads dbus-monitor over a pipe. A file redirect flushes
// differently, so this is the only check that the live path delivers promptly
// rather than sitting in a buffer.
func TestLiveScannerSeesSignalsThroughAPipe(t *testing.T) {
	requireSessionBus(t)

	cmd := exec.Command("dbus-monitor", "--session",
		"type='signal',interface='org.freedesktop.Notifications'")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { cmd.Process.Kill(); cmd.Wait() }()

	closed := make(chan string, 8)
	go scanNotificationSignals(stdout,
		func(id, action string) {},
		func(id string) { closed <- id })

	time.Sleep(300 * time.Millisecond) // let the monitor attach

	id, err := dbusNotify("", "hive self-test", "pipe delivery")
	if err != nil {
		t.Fatalf("dbusNotify: %v", err)
	}
	closeNotification(t, id)

	deadline := time.After(5 * time.Second)
	for {
		select {
		case got := <-closed:
			if got == id {
				return // delivered through the pipe
			}
		case <-deadline:
			t.Fatalf("no close signal for id %s reached the scanner in 5s — "+
				"dbus-monitor output is not arriving over the pipe", id)
		}
	}
}

func closeNotification(t *testing.T, id string) {
	t.Helper()
	exec.Command("gdbus", "call", "--session",
		"--dest", "org.freedesktop.Notifications",
		"--object-path", "/org/freedesktop/Notifications",
		"--method", "org.freedesktop.Notifications.CloseNotification",
		"--", id).Run()
}
