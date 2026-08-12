package main

import (
	"errors"
	"strings"
	"testing"
)

// errWriter stands in for the control connection's stdin pipe after the
// client has exited: every write fails the way a broken pipe does.
type errWriter struct{ n int }

func (w *errWriter) Write(p []byte) (int, error) {
	w.n++
	return 0, errors.New("write |1: broken pipe")
}

// setControl installs a fake control connection and restores the real one.
func setControl(t *testing.T, w *strings.Builder, bad *errWriter) {
	t.Helper()
	tmuxControl.Lock()
	prevStdin, prevStarted := tmuxControl.stdin, tmuxControl.started
	tmuxControl.started = true
	if bad != nil {
		tmuxControl.stdin = bad
	} else {
		tmuxControl.stdin = w
	}
	tmuxControl.Unlock()

	t.Cleanup(func() {
		tmuxControl.Lock()
		tmuxControl.stdin, tmuxControl.started = prevStdin, prevStarted
		tmuxControl.Unlock()
	})
}

func TestControlWriteDeliversWhenHealthy(t *testing.T) {
	var buf strings.Builder
	setControl(t, &buf, nil)

	if !controlWrite("send-keys -t =hive-x: a") {
		t.Fatal("healthy connection should report delivery")
	}
	if got := buf.String(); got != "send-keys -t =hive-x: a\n" {
		t.Errorf("command not written verbatim: %q", got)
	}
	if !tmuxControlActive() {
		t.Error("connection should still be active after a good write")
	}
}

// The bug: the control client can exit while its session lives on. Until the
// write error takes the connection down, tmuxControlActive stays true and every
// keystroke is posted into a pipe with no reader — input vanishes silently
// while chords, which never reach tmux, keep working.
func TestControlWriteFailureRetiresConnection(t *testing.T) {
	bad := &errWriter{}
	setControl(t, nil, bad)

	if controlWrite("send-keys -t =hive-x: a") {
		t.Fatal("a failed write must not report delivery")
	}
	if tmuxControlActive() {
		t.Fatal("a failed write must retire the connection, not leave it latched active")
	}

	// Every later send must go straight to the direct path rather than
	// retrying a pipe that is known dead.
	if controlWrite("send-keys -t =hive-x: b") {
		t.Error("retired connection should keep reporting no delivery")
	}
	if bad.n != 1 {
		t.Errorf("dead pipe written %d times, want 1", bad.n)
	}
}

func TestControlWriteInactiveReportsNoDelivery(t *testing.T) {
	tmuxControl.Lock()
	prevStdin, prevStarted := tmuxControl.stdin, tmuxControl.started
	tmuxControl.started, tmuxControl.stdin = false, nil
	tmuxControl.Unlock()
	t.Cleanup(func() {
		tmuxControl.Lock()
		tmuxControl.stdin, tmuxControl.started = prevStdin, prevStarted
		tmuxControl.Unlock()
	})

	if controlWrite("display-message -p probe") {
		t.Error("inactive connection must report no delivery so the caller runs the command directly")
	}
}
