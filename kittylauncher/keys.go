package main

import (
	"charm.land/bubbles/v2/key"
)

type keyMap struct {
	Open        key.Binding
	OpenShell   key.Binding
	Remote      key.Binding
	RemoteFlag  key.Binding
	FavFlag     key.Binding
	Edit        key.Binding
	Scratch     key.Binding
	Promote     key.Binding
	FocusTab    key.Binding
	Kill        key.Binding
	Detach      key.Binding
	Filter      key.Binding
	Help        key.Binding
	Quit        key.Binding
	Tab1        key.Binding
	Tab2        key.Binding
	Tab3        key.Binding
	Tab4        key.Binding
	Tab5        key.Binding
	Tab6        key.Binding
	Tab7        key.Binding
	Tab8        key.Binding
	Tab9        key.Binding
}

func newKeyMap() keyMap {
	return keyMap{
		Open: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "open + claude"),
		),
		OpenShell: key.NewBinding(
			key.WithKeys("shift+enter"),
			key.WithHelp("shift+enter", "open + shell"),
		),
		Remote: key.NewBinding(
			key.WithKeys("r"),
			key.WithHelp("r", "open remote"),
		),
		RemoteFlag: key.NewBinding(
			key.WithKeys("R"),
			key.WithHelp("R", "toggle remote flag"),
		),
		FavFlag: key.NewBinding(
			key.WithKeys("F"),
			key.WithHelp("F", "toggle favourite"),
		),
		Edit: key.NewBinding(
			key.WithKeys("E"),
			key.WithHelp("E", "edit repo"),
		),
		Scratch: key.NewBinding(
			key.WithKeys("s"),
			key.WithHelp("s", "new scratch"),
		),
		Promote: key.NewBinding(
			key.WithKeys("p"),
			key.WithHelp("p", "promote scratch"),
		),
		FocusTab: key.NewBinding(
			key.WithKeys("tab"),
			key.WithHelp("tab", "focus tab"),
		),
		Kill: key.NewBinding(
			key.WithKeys("x"),
			key.WithHelp("x", "kill session"),
		),
		Detach: key.NewBinding(
			key.WithKeys("d"),
			key.WithHelp("d", "detach tab"),
		),
		Filter: key.NewBinding(
			key.WithKeys("/"),
			key.WithHelp("/", "filter"),
		),
		Help: key.NewBinding(
			key.WithKeys("?"),
			key.WithHelp("?", "help"),
		),
		Quit: key.NewBinding(
			key.WithKeys("q"),
			key.WithHelp("q", "quit"),
		),
		Tab1: key.NewBinding(key.WithKeys("1"), key.WithHelp("1-9", "jump to tab")),
		Tab2: key.NewBinding(key.WithKeys("2")),
		Tab3: key.NewBinding(key.WithKeys("3")),
		Tab4: key.NewBinding(key.WithKeys("4")),
		Tab5: key.NewBinding(key.WithKeys("5")),
		Tab6: key.NewBinding(key.WithKeys("6")),
		Tab7: key.NewBinding(key.WithKeys("7")),
		Tab8: key.NewBinding(key.WithKeys("8")),
		Tab9: key.NewBinding(key.WithKeys("9")),
	}
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Open, k.OpenShell, k.Remote, k.Scratch, k.Help, k.Quit}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Open, k.OpenShell, k.Remote},
		{k.Scratch, k.Promote, k.FocusTab},
		{k.Kill, k.Detach, k.Filter},
		{k.Tab1, k.Help, k.Quit},
	}
}
