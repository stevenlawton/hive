package bus

import "strings"

// ParseCompose turns a compose-line string into an Announcement-ready payload
// using keyword and sigil prefixes.
//
// Grammar:
//
//	"working: refactoring auth middleware"   → kind=intent
//	"waiting: code review from Alice"         → kind=waiting
//	"done: auth refactor, all tests green"    → kind=done
//	"? anyone using user.email?"              → kind=question
//	"r:msg_abc I'll handle it"                → reply to msg_abc (kind inherits fyi)
//	"! hotfix shipping, freeze pushes"        → headline prefixed with ⚠ (kind fyi)
//	"refactored User.email → emailAddress"    → kind=fyi
//
// The returned Announcement has ID, From, At left empty — Bus.Announce fills
// those in.
func ParseCompose(line string) Announcement {
	line = strings.TrimSpace(line)
	msg := Announcement{}

	// Reply prefix: "r:<id> <text>"
	if strings.HasPrefix(line, "r:") {
		rest := strings.TrimPrefix(line, "r:")
		if idx := strings.IndexAny(rest, " \t"); idx > 0 {
			msg.ReplyTo = rest[:idx]
			line = strings.TrimSpace(rest[idx+1:])
		} else {
			msg.ReplyTo = rest
			msg.Headline = ""
			return msg
		}
	}

	// Lifecycle keyword prefixes (case-insensitive)
	lower := strings.ToLower(line)
	switch {
	case strings.HasPrefix(lower, "working:"), strings.HasPrefix(lower, "working on"):
		msg.Kind = KindIntent
		line = stripKeyword(line, []string{"working:", "working on"})
	case strings.HasPrefix(lower, "intent:"):
		msg.Kind = KindIntent
		line = stripKeyword(line, []string{"intent:"})
	case strings.HasPrefix(lower, "waiting:"), strings.HasPrefix(lower, "waiting for"), strings.HasPrefix(lower, "blocked:"), strings.HasPrefix(lower, "blocked on"):
		msg.Kind = KindWaiting
		line = stripKeyword(line, []string{"waiting:", "waiting for", "blocked:", "blocked on"})
	case strings.HasPrefix(lower, "done:"), strings.HasPrefix(lower, "done "):
		msg.Kind = KindDone
		line = stripKeyword(line, []string{"done:", "done "})
	}

	// Question prefix: "? text"
	if msg.Kind == "" {
		if strings.HasPrefix(line, "? ") {
			msg.Kind = KindQuestion
			line = strings.TrimSpace(strings.TrimPrefix(line, "?"))
		} else if strings.HasPrefix(line, "?") && len(line) > 1 {
			msg.Kind = KindQuestion
			line = strings.TrimSpace(line[1:])
		}
	}

	// Urgent prefix: "! text" — stored as a headline starting with "⚠ "
	if strings.HasPrefix(line, "! ") || (strings.HasPrefix(line, "!") && len(line) > 1 && line[1] != '=') {
		line = "⚠ " + strings.TrimSpace(strings.TrimPrefix(line, "!"))
	}

	msg.Headline = line
	return msg
}

func stripKeyword(line string, keywords []string) string {
	lower := strings.ToLower(line)
	for _, kw := range keywords {
		if strings.HasPrefix(lower, kw) {
			return strings.TrimSpace(line[len(kw):])
		}
	}
	return line
}
