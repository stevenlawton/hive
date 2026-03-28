package main

import (
	"encoding/json"
	"fmt"
	"os/exec"
)

func kittyCmd(args ...string) *exec.Cmd {
	return exec.Command("kitten", args...)
}

func kittyRun(args ...string) error {
	return kittyCmd(args...).Run()
}

func kittyOutput(args ...string) (string, error) {
	out, err := kittyCmd(args...).Output()
	return string(out), err
}

func kittyLaunchTabArgs(tabTitle string, command ...string) []string {
	args := []string{"@", "launch", "--type=tab", "--tab-title", tabTitle}
	args = append(args, command...)
	return args
}

func kittySetTabColorArgs(tabTitle, color string) []string {
	return []string{"@", "set-tab-color", "--match", "title:^" + tabTitle, "active_bg=" + color, "inactive_bg=" + color}
}

func kittySetTabTitleArgs(match, title string) []string {
	return []string{"@", "set-tab-title", "--match", match, title}
}

func kittyFocusTabArgs(match string) []string {
	return []string{"@", "focus-tab", "--match", match}
}

func kittyCloseTabArgs(match string) []string {
	return []string{"@", "close-tab", "--match", match}
}

func KittyLaunchTab(tabTitle string, command ...string) error {
	return kittyRun(kittyLaunchTabArgs(tabTitle, command...)...)
}

func KittySetTabColor(tabTitle, color string) error {
	if color == "" {
		return nil
	}
	return kittyRun(kittySetTabColorArgs(tabTitle, color)...)
}

func KittyResetTabColor(tabTitle string) error {
	return kittyRun("@", "set-tab-color", "--match", "title:^"+tabTitle, "active_bg=NONE", "inactive_bg=NONE")
}

func KittySetTabTitle(match, title string) error {
	return kittyRun(kittySetTabTitleArgs(match, title)...)
}

func KittyFocusTab(match string) error {
	return kittyRun(kittyFocusTabArgs(match)...)
}

func KittyCloseTab(match string) error {
	return kittyRun(kittyCloseTabArgs(match)...)
}

type KittyTabInfo struct {
	ID        int    `json:"id"`
	Title     string `json:"title"`
	IsFocused bool   `json:"is_focused"`
}

type kittyOSWindow struct {
	Tabs []KittyTabInfo `json:"tabs"`
}

func KittyListTabs() ([]KittyTabInfo, error) {
	out, err := kittyOutput("@", "ls")
	if err != nil {
		return nil, err
	}
	var osWindows []kittyOSWindow
	if err := json.Unmarshal([]byte(out), &osWindows); err != nil {
		return nil, err
	}
	var tabs []KittyTabInfo
	for _, osw := range osWindows {
		tabs = append(tabs, osw.Tabs...)
	}
	return tabs, nil
}

func KittyFocusTabByIndex(index int) error {
	return kittyRun("@", "focus-tab", "--match", fmt.Sprintf("index:%d", index))
}
