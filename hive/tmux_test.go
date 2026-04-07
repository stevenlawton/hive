package main

import (
	"testing"
)

func TestTmuxNewSessionArgs(t *testing.T) {
	args := tmuxNewSessionArgs("hive-slicewise", "/home/steve/repos/SliceWise")
	expected := []string{"new-session", "-d", "-s", "hive-slicewise", "-c", "/home/steve/repos/SliceWise"}
	if len(args) != len(expected) {
		t.Fatalf("expected %d args, got %d: %v", len(expected), len(args), args)
	}
	for i, arg := range expected {
		if args[i] != arg {
			t.Errorf("arg[%d]: expected %q, got %q", i, arg, args[i])
		}
	}
}

func TestTmuxSendKeysArgs(t *testing.T) {
	args := tmuxSendKeysArgs("hive-slicewise", "claude")
	expected := []string{"send-keys", "-t", "hive-slicewise", "claude", "Enter"}
	if len(args) != len(expected) {
		t.Fatalf("expected %d args, got %d: %v", len(expected), len(args), args)
	}
	for i, arg := range expected {
		if args[i] != arg {
			t.Errorf("arg[%d]: expected %q, got %q", i, arg, args[i])
		}
	}
}

func TestTmuxSessionName(t *testing.T) {
	if name := TmuxSessionName("SliceWise", false); name != "hive-SliceWise" {
		t.Errorf("expected hive-SliceWise, got %s", name)
	}
	if name := TmuxSessionName("tgbridge", true); name != "hive-rc-tgbridge" {
		t.Errorf("expected hive-rc-tgbridge, got %s", name)
	}
	if name := TmuxSessionName("stevenlawton.com", true); name != "hive-rc-stevenlawton_com" {
		t.Errorf("expected hive-rc-stevenlawton_com, got %s", name)
	}
}

func TestParseTmuxSessionsDualPrefix(t *testing.T) {
	output := "kl-workspace: 1 windows (created Mon Mar 30 09:56:02 2026)\n" +
		"hive-polybot: 1 windows (created Tue Mar 31 13:04:17 2026)\n" +
		"hive-rc-SliceWize: 1 windows (created Sat Mar 28 23:52:24 2026)\n" +
		"kl-rc-tgbridge: 1 windows (created Sun Mar 29 00:03:33 2026)\n" +
		"kl-scratch-001: 1 windows (created Mon Mar 30 10:00:00 2026)\n" +
		"hive-scratch-002: 1 windows (created Tue Mar 31 11:00:00 2026)\n" +
		"other-session: 1 windows (created Mon Mar 30 10:00:00 2026)\n"

	sessions := ParseTmuxSessions(output)

	if len(sessions) != 6 {
		t.Fatalf("expected 6 sessions, got %d: %v", len(sessions), sessions)
	}

	// kl- interactive
	if sessions[0].RepoKey != "workspace" || sessions[0].IsRemote || sessions[0].IsScratch {
		t.Errorf("expected workspace interactive, got %+v", sessions[0])
	}
	// hive- interactive
	if sessions[1].RepoKey != "polybot" || sessions[1].IsRemote {
		t.Errorf("expected polybot interactive, got %+v", sessions[1])
	}
	// hive-rc-
	if sessions[2].RepoKey != "SliceWize" || !sessions[2].IsRemote {
		t.Errorf("expected remote SliceWize, got %+v", sessions[2])
	}
	// kl-rc-
	if sessions[3].RepoKey != "tgbridge" || !sessions[3].IsRemote {
		t.Errorf("expected remote tgbridge, got %+v", sessions[3])
	}
	// kl-scratch-
	if sessions[4].RepoKey != "001" || !sessions[4].IsScratch {
		t.Errorf("expected scratch 001, got %+v", sessions[4])
	}
	// hive-scratch-
	if sessions[5].RepoKey != "002" || !sessions[5].IsScratch {
		t.Errorf("expected scratch 002, got %+v", sessions[5])
	}
}

func TestTmuxCapturePaneArgs(t *testing.T) {
	args := tmuxCapturePaneArgs("hive-workspace")
	expected := []string{"capture-pane", "-p", "-e", "-t", "hive-workspace"}
	if len(args) != len(expected) {
		t.Fatalf("expected %d args, got %d", len(expected), len(args))
	}
	for i, a := range args {
		if a != expected[i] {
			t.Errorf("arg[%d]: expected %q, got %q", i, expected[i], a)
		}
	}
}

func TestTmuxCapturePaneFullArgs(t *testing.T) {
	args := tmuxCapturePaneFullArgs("hive-workspace")
	expected := []string{"capture-pane", "-p", "-e", "-S", "-", "-E", "-", "-t", "hive-workspace"}
	if len(args) != len(expected) {
		t.Fatalf("expected %d args, got %d", len(expected), len(args))
	}
	for i, a := range args {
		if a != expected[i] {
			t.Errorf("arg[%d]: expected %q, got %q", i, expected[i], a)
		}
	}
}
