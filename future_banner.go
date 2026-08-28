package main

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Reading the limit banner out of a pane.
//
// Telemetry is the better source for the reset time and is tried first, but it
// has a blind spot: a session blocked on the quota stops rendering its
// statusline, so if hive starts (or every session stalls) after the window is
// already spent, nothing live is reporting and there is no clock to arm
// against. The pane itself still says so on screen.
//
// The exact wording of claude's message is not pinned here on purpose — it has
// changed before and is not worth guessing at. What is stable is the shape: a
// line that mentions a limit and when it resets. That is what is matched, and
// the clock is read out of it.

var (
	limitBannerLine  = regexp.MustCompile(`(?i)limit.*\breset`)
	limitBannerClock = regexp.MustCompile(`(?i)\b(\d{1,2})(?::(\d{2}))?\s*(am|pm)?\b`)
)

// paneShowsLimitBanner reports whether a captured pane is showing claude's
// usage-limit message.
func paneShowsLimitBanner(pane string) bool {
	return bannerLine(pane) != ""
}

// limitBannerReset reads the reset time claude is displaying, resolved against
// now: a clock time that has already passed today belongs to tomorrow, since
// the banner is naming the next reset rather than the last one.
func limitBannerReset(pane string, now time.Time) (int64, bool) {
	line := bannerLine(pane)
	if line == "" {
		return 0, false
	}
	return clockAfter(line, now)
}

// bannerLine returns the last line that reads like a limit banner. Panes hold
// scrollback, so an older banner from a window that has already rolled over
// may still be on screen above the current one.
func bannerLine(pane string) string {
	found := ""
	for _, line := range strings.Split(pane, "\n") {
		if limitBannerLine.MatchString(line) {
			found = line
		}
	}
	return found
}

// clockAfter pulls a wall-clock time out of a banner line and returns the next
// moment it names. The match is taken from after the word "reset", so a line
// that mentions some other number first does not mislead it.
func clockAfter(line string, now time.Time) (int64, bool) {
	idx := strings.Index(strings.ToLower(line), "reset")
	if idx < 0 {
		return 0, false
	}
	tail := line[idx:]

	for _, m := range limitBannerClock.FindAllStringSubmatch(tail, -1) {
		hour, err := strconv.Atoi(m[1])
		if err != nil {
			continue
		}
		minute := 0
		if m[2] != "" {
			minute, _ = strconv.Atoi(m[2])
		}
		meridiem := strings.ToLower(m[3])

		// A bare number with no meridiem and no minutes is only a clock if it
		// could be one; "reset" lines also carry stray numbers.
		if meridiem == "" && m[2] == "" && hour > 23 {
			continue
		}
		switch meridiem {
		case "pm":
			if hour < 12 {
				hour += 12
			}
		case "am":
			if hour == 12 {
				hour = 0
			}
		}
		if hour > 23 || minute > 59 {
			continue
		}

		at := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, now.Location())
		if !at.After(now) {
			at = at.AddDate(0, 0, 1)
		}
		return at.Unix(), true
	}
	return 0, false
}

// capturePane reads what a session's pane is currently showing. A var so tests
// can supply pane text without a live tmux.
var capturePane = TmuxCapturePane

// futureResetSource records where a popup's reset time came from, so the
// header can be honest about how much to trust it.
type futureResetSource int

const (
	futureResetNone futureResetSource = iota
	futureResetFromFleet
	futureResetFromBanner
)

// paneLimitReset reads the reset time off a session's own pane. This is the
// fallback for the case telemetry cannot cover: every session stalled, so
// nothing is rendering a statusline and the fleet has no clock to offer.
func paneLimitReset(session string, now time.Time) (int64, bool) {
	if session == "" {
		return 0, false
	}
	pane, err := capturePane(session)
	if err != nil {
		return 0, false
	}
	return limitBannerReset(pane, now)
}

// futureSendPlan builds the delivery for a parked note. A single line is typed
// as keystrokes, which is what the canned popup does and what claude's input
// box handles best. Anything longer goes as a bracketed paste: a typed newline
// would submit the prompt, so a note written across four lines would otherwise
// arrive as four half-finished ones.
func futureSendPlan(note string) []cannedOp {
	note = trimNote(note)
	if note == "" {
		return nil
	}
	if strings.Contains(note, "\n") {
		return []cannedOp{
			{paste: note},
			{key: "enter", delay: cannedSubmitPause},
		}
	}
	return []cannedOp{
		{literal: note},
		{key: "enter", delay: cannedSubmitPause},
	}
}

// trimNote drops surrounding blank space while leaving the shape of the note
// alone — the indentation of a list, say, is the user's and worth keeping.
func trimNote(note string) string {
	lines := strings.Split(note, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t\r")
	}
	return strings.Trim(strings.Join(lines, "\n"), "\n \t")
}
