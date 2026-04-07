package main

import (
	"testing"
)

func TestKittyLaunchTabArgs(t *testing.T) {
	args := kittyLaunchTabArgs("SliceWize", "tmux", "attach", "-t", "kl-slicewise")
	if args[0] != "@" || args[1] != "launch" {
		t.Errorf("expected '@ launch', got %v", args[:2])
	}
	foundType := false
	foundTitle := false
	for i, a := range args {
		if a == "--type=tab" {
			foundType = true
		}
		if a == "--tab-title" && i+1 < len(args) && args[i+1] == "SliceWize" {
			foundTitle = true
		}
	}
	if !foundType {
		t.Errorf("missing --type=tab: %v", args)
	}
	if !foundTitle {
		t.Errorf("missing --tab-title SliceWize: %v", args)
	}
}

func TestKittySetTabColorArgs(t *testing.T) {
	args := kittySetTabColorArgs("SliceWize", "#ff6b6b")
	hasMatch := false
	hasColor := false
	for i, a := range args {
		if a == "--match" && i+1 < len(args) && args[i+1] == "title:^SliceWize" {
			hasMatch = true
		}
		if a == "active_bg=#ff6b6b" {
			hasColor = true
		}
	}
	if !hasMatch || !hasColor {
		t.Errorf("missing match or color args: %v", args)
	}
}

func TestKittySetTabTitleArgs(t *testing.T) {
	args := kittySetTabTitleArgs("title:^SW", "SW — fixing auth")
	hasMatch := false
	hasTitle := false
	for i, a := range args {
		if a == "--match" && i+1 < len(args) && args[i+1] == "title:^SW" {
			hasMatch = true
		}
		if a == "SW — fixing auth" {
			hasTitle = true
		}
	}
	if !hasMatch || !hasTitle {
		t.Errorf("missing match or title: %v", args)
	}
}
