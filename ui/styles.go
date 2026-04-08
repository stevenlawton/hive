package ui

import "charm.land/lipgloss/v2"

// Colors
var (
	ColorOrange  = lipgloss.Color("#ff8c00")
	ColorGreen   = lipgloss.Color("#00ff88")
	ColorBlue    = lipgloss.Color("#00bbff")
	ColorRed     = lipgloss.Color("#ff0000")
	ColorYellow  = lipgloss.Color("#ffff00")
	ColorCyan    = lipgloss.Color("#0088cc")
	ColorGray    = lipgloss.Color("#555555")
	ColorDimGray = lipgloss.Color("#333333")
	ColorWhite   = lipgloss.Color("#ffffff")
)

// Layout styles
var (
	BorderStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorGray)

	FocusedBorderStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(ColorOrange)

	TabActiveStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#1a1a2e")).
			Background(ColorOrange).
			Padding(0, 1)

	TabInactiveStyle = lipgloss.NewStyle().
				Foreground(ColorGray).
				Background(lipgloss.Color("#1a1a2e")).
				Padding(0, 1)

	TabFlashStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorWhite).
			Background(ColorRed).
			Padding(0, 1)

	TabHomeStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorOrange).
			Padding(0, 1)

	SectionHeaderStyle = lipgloss.NewStyle().
				Foreground(ColorOrange).
				Bold(true)

	StatusBarStyle = lipgloss.NewStyle().
			Foreground(ColorGray)

	KeyStyle = lipgloss.NewStyle().
			Foreground(ColorOrange).
			Bold(true)

	ValStyle = lipgloss.NewStyle().
			Foreground(ColorGray)
)

// Status styles
var (
	ClaudeStyle   = lipgloss.NewStyle().Foreground(ColorOrange)
	ShellStyle    = lipgloss.NewStyle().Foreground(ColorBlue)
	RemoteStyle   = lipgloss.NewStyle().Foreground(ColorCyan)
	DeadStyle     = lipgloss.NewStyle().Foreground(ColorRed)
	WaitStyle     = lipgloss.NewStyle().Foreground(ColorYellow)
	IdleStyle     = lipgloss.NewStyle().Foreground(ColorGray)
	DiffAddStyle  = lipgloss.NewStyle().Foreground(ColorGreen)
	DiffDelStyle  = lipgloss.NewStyle().Foreground(ColorRed)
	DiffHunkStyle = lipgloss.NewStyle().Foreground(ColorCyan)
)
