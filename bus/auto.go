package bus

import (
	"fmt"
	"os"
)

// AutoResponderEnv marks a process spawned by the bus auto-responder (see
// Respond). Every write path reads it to decide what that process may post.
const AutoResponderEnv = "HIVE_AUTO_RESPONDER"

// IsAutoResponder reports whether this process is a bus auto-responder.
func IsAutoResponder() bool {
	return os.Getenv(AutoResponderEnv) == "1"
}

// AutoVerbError is returned when an auto-responder attempts to post a
// coordination message.
type AutoVerbError struct{ Verb string }

func (e AutoVerbError) Error() string {
	return fmt.Sprintf("auto-responder may not post %q (coordination verbs are parent-only)", e.Verb)
}

// CheckAutoVerb reports whether an auto-responder is allowed to post msg.
//
// A responder runs with its worktree's HIVE_SENDER, so its posts are stamped
// identically to the session a human is driving. That is fine for answering a
// peer, but a coordination verb from one is indistinguishable from a real
// claim or release and peers act on it — which desyncs shared resources such
// as a test-database slot. Replies and questions carry no such authority, so
// they stay open; everything else is refused here rather than merely omitted
// from the responder prompt, because a prompt is advice and this is a rule.
func CheckAutoVerb(msg Announcement) error {
	if !IsAutoResponder() {
		return nil
	}
	if msg.ReplyTo != "" || msg.Kind == KindQuestion {
		return nil
	}
	return AutoVerbError{Verb: string(msg.KindOrDefault())}
}
