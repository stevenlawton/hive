package bus

import (
	"strings"
	"time"
)

// Kind identifies the lifecycle/purpose of an announcement.
type Kind string

const (
	KindFYI      Kind = "fyi"      // "just so you know..."
	KindIntent   Kind = "intent"   // "I'm working on X"
	KindWaiting  Kind = "waiting"  // "I'm blocked, waiting for X"
	KindDone     Kind = "done"     // "X is finished"
	KindQuestion Kind = "question" // "does anyone know X?"
)

// Announcement represents a single message on the Hive bus.
//
// Listeners (other Claude sessions, or the human) scan Headlines to decide
// relevance. Body is optional extra context they read only if interested.
// Touches lets a sender hint which files they're working on so receivers
// can instantly check overlap. Kind identifies the lifecycle state (intent,
// waiting, done, question, fyi). ReplyTo threads replies under a parent.
type Announcement struct {
	ID       string    `json:"id"`
	From     string    `json:"from"` // e.g. "steve", "wt:backend-auth"
	At       time.Time `json:"at"`
	Kind     Kind      `json:"kind,omitempty"` // defaults to KindFYI if empty
	Headline string    `json:"headline"`
	Body     string    `json:"body,omitempty"`
	Touches  []string  `json:"touches,omitempty"`  // file globs the work affects
	ReplyTo  string    `json:"reply_to,omitempty"` // parent message id
	Auto     bool      `json:"auto,omitempty"`     // posted by the bus auto-responder, not the worktree's own session
}

// AutoMarker returns a suffix identifying messages posted by the bus
// auto-responder. It shares the worktree's sender id, so without this a
// reader cannot tell it from the session a human is driving.
func (a Announcement) AutoMarker() string {
	if a.Auto {
		return " 🤖auto"
	}
	return ""
}

// MaxHeadline caps how much of a headline reaches a digest or listing.
const MaxHeadline = 160

// ShortHeadline reduces a headline to the single short line that listings
// promise. Headlines are meant to be one line; senders do paste whole
// multi-paragraph reports into the field, and one of those costs a reader more
// context than every other message in the digest combined. The full text is
// always a `hive bus read <id>` away.
func (a Announcement) ShortHeadline() string {
	headline, cut := a.Headline, false
	if i := strings.IndexAny(headline, "\r\n"); i >= 0 {
		headline, cut = headline[:i], true
	}
	if r := []rune(headline); len(r) > MaxHeadline {
		headline, cut = string(r[:MaxHeadline]), true
	}
	headline = strings.TrimRight(headline, " \t")
	if cut {
		headline += "…"
	}
	return headline
}

// KindOrDefault returns the announcement's Kind, defaulting to KindFYI if empty.
func (a Announcement) KindOrDefault() Kind {
	if a.Kind == "" {
		return KindFYI
	}
	return a.Kind
}

// Icon returns a one-character visual marker for the announcement's kind.
func (a Announcement) Icon() string {
	if a.ReplyTo != "" {
		return "💬"
	}
	switch a.KindOrDefault() {
	case KindIntent:
		return "🔨"
	case KindWaiting:
		return "⏳"
	case KindDone:
		return "✅"
	case KindQuestion:
		return "❓"
	default:
		return "📢"
	}
}
