package ui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// ManagerView is the main overview screen with session list and preview.
type ManagerView struct {
	Preview   *PreviewPane
	NotifyLog *NotifyLog
	Width     int
	Height    int
	ListWidth int
}

// NewManagerView creates the manager view.
func NewManagerView() *ManagerView {
	return &ManagerView{
		Preview:   NewPreviewPane(),
		NotifyLog: NewNotifyLog(50),
	}
}

// SetSize updates layout dimensions.
func (mv *ManagerView) SetSize(w, h int) {
	mv.Width = w
	mv.Height = h

	mv.ListWidth = w * 35 / 100
	if mv.ListWidth < 28 {
		mv.ListWidth = 28
	}
	previewWidth := w - mv.ListWidth - 1
	if previewWidth < 10 {
		previewWidth = 10
	}

	statusBarHeight := 2
	contentHeight := h - statusBarHeight
	if contentHeight < 1 {
		contentHeight = 1
	}

	mv.Preview.SetSize(previewWidth, contentHeight)
}

// View renders the two-pane layout.
func (mv *ManagerView) View(listContent, statusBar string) string {
	previewContent := mv.Preview.View()

	notifyHeight := mv.Height / 5
	if notifyHeight > 6 {
		notifyHeight = 6
	}
	listHeight := mv.Height - 2 - notifyHeight
	if listHeight < 5 {
		listHeight = 5
		notifyHeight = 0
	}

	listLines := strings.Split(listContent, "\n")
	if len(listLines) > listHeight {
		listLines = listLines[:listHeight]
	}
	for len(listLines) < listHeight {
		listLines = append(listLines, "")
	}

	if notifyHeight > 0 {
		notifyContent := mv.NotifyLog.View(mv.ListWidth, notifyHeight)
		if notifyContent != "" {
			listLines = append(listLines, strings.Split(notifyContent, "\n")...)
		}
	}

	leftPane := strings.Join(listLines, "\n")
	leftPadded := padLines(leftPane, mv.ListWidth)
	rightPadded := padLines(previewContent, mv.Width-mv.ListWidth-1)

	separator := lipgloss.NewStyle().Foreground(ColorGray).Render("│")
	combined := joinHorizontalLines(leftPadded, separator, rightPadded)

	return combined + "\n" + statusBar
}

func padLines(content string, width int) string {
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		visible := lipgloss.Width(line)
		if visible < width {
			lines[i] = line + strings.Repeat(" ", width-visible)
		}
	}
	return strings.Join(lines, "\n")
}

func joinHorizontalLines(left, sep, right string) string {
	leftLines := strings.Split(left, "\n")
	rightLines := strings.Split(right, "\n")

	maxLen := len(leftLines)
	if len(rightLines) > maxLen {
		maxLen = len(rightLines)
	}

	var result []string
	for i := 0; i < maxLen; i++ {
		l := ""
		r := ""
		if i < len(leftLines) {
			l = leftLines[i]
		}
		if i < len(rightLines) {
			r = rightLines[i]
		}
		result = append(result, l+sep+r)
	}
	return strings.Join(result, "\n")
}
