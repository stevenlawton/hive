package bus

import "time"

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
	Kind     Kind      `json:"kind,omitempty"`     // defaults to KindFYI if empty
	Headline string    `json:"headline"`
	Body     string    `json:"body,omitempty"`
	Touches  []string  `json:"touches,omitempty"`  // file globs the work affects
	ReplyTo  string    `json:"reply_to,omitempty"` // parent message id
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
