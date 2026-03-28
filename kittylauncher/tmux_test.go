package main

import (
	"testing"
)

func TestTmuxNewSessionArgs(t *testing.T) {
	args := tmuxNewSessionArgs("kl-slicewise", "/home/steve/repos/SliceWise")
	expected := []string{"new-session", "-d", "-s", "kl-slicewise", "-c", "/home/steve/repos/SliceWise"}
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
	args := tmuxSendKeysArgs("kl-slicewise", "claude")
	expected := []string{"send-keys", "-t", "kl-slicewise", "claude", "Enter"}
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
	if name := TmuxSessionName("SliceWise", false); name != "kl-SliceWise" {
		t.Errorf("expected kl-SliceWise, got %s", name)
	}
	if name := TmuxSessionName("tgbridge", true); name != "kl-rc-tgbridge" {
		t.Errorf("expected kl-rc-tgbridge, got %s", name)
	}
}

func TestParseTmuxSessions(t *testing.T) {
	output := "kl-slicewise: 1 windows (created Fri Mar 28 10:00:00 2026)\nkl-rc-tgbridge: 1 windows (created Fri Mar 28 10:00:00 2026)\nother-session: 1 windows (created Fri Mar 28 10:00:00 2026)\n"
	sessions := ParseTmuxSessions(output)
	if len(sessions) != 2 {
		t.Fatalf("expected 2 kl- sessions, got %d: %v", len(sessions), sessions)
	}
	if sessions[0].Name != "kl-slicewise" {
		t.Errorf("expected kl-slicewise, got %s", sessions[0].Name)
	}
	if sessions[1].Name != "kl-rc-tgbridge" || !sessions[1].IsRemote {
		t.Errorf("expected kl-rc-tgbridge (remote), got %+v", sessions[1])
	}
}
