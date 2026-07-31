package bus

import (
	"errors"
	"strings"
	"testing"
)

// A bus auto-responder runs with its worktree's HIVE_SENDER, so anything it
// posts is stamped exactly like the session a human is driving. Coordination
// verbs from one are therefore indistinguishable from a real claim or release
// and peers act on them — which is how a responder's "SCOUT SLOT RELEASED"
// handed a peer a test-DB slot its owner was still using.
func TestCheckAutoVerbRefusesCoordinationVerbs(t *testing.T) {
	t.Setenv(AutoResponderEnv, "1")

	for _, kind := range []Kind{KindIntent, KindWaiting, KindDone, KindFYI} {
		err := CheckAutoVerb(Announcement{Kind: kind, Headline: "x"})
		if err == nil {
			t.Fatalf("kind %q: auto-responder must not be allowed to post it", kind)
		}
		var verbErr AutoVerbError
		if !errors.As(err, &verbErr) {
			t.Fatalf("kind %q: want AutoVerbError, got %T", kind, err)
		}
		if verbErr.Verb != string(kind) {
			t.Errorf("kind %q: error names verb %q", kind, verbErr.Verb)
		}
	}
}

// Replies and questions carry no authority over shared resources, so they stay
// open — they're the whole point of the responder.
func TestCheckAutoVerbAllowsReplyAndAsk(t *testing.T) {
	t.Setenv(AutoResponderEnv, "1")

	if err := CheckAutoVerb(Announcement{ReplyTo: "msg_abc", Headline: "x"}); err != nil {
		t.Errorf("reply must be allowed: %v", err)
	}
	if err := CheckAutoVerb(Announcement{Kind: KindQuestion, Headline: "x"}); err != nil {
		t.Errorf("ask must be allowed: %v", err)
	}
	// A coordination verb is fine when it *is* a reply — the authority problem
	// is starting a thread that peers read as a fresh claim.
	if err := CheckAutoVerb(Announcement{Kind: KindDone, ReplyTo: "msg_abc"}); err != nil {
		t.Errorf("done-as-reply must be allowed: %v", err)
	}
}

// Outside a responder the rule must not apply at all, or every ordinary
// session loses its lifecycle verbs.
func TestCheckAutoVerbInertForNormalSessions(t *testing.T) {
	t.Setenv(AutoResponderEnv, "")

	for _, kind := range []Kind{KindIntent, KindWaiting, KindDone, KindFYI, KindQuestion} {
		if err := CheckAutoVerb(Announcement{Kind: kind, Headline: "x"}); err != nil {
			t.Errorf("kind %q must be allowed for a normal session: %v", kind, err)
		}
	}
}

// The guard lives in Announce so that every write path — CLI, MCP, and
// anything added later — is covered without having to remember it.
func TestAnnounceEnforcesAutoVerbRule(t *testing.T) {
	t.Setenv(AutoResponderEnv, "1")
	b := newTestBus(t)

	if _, err := b.Announce(Announcement{Kind: KindDone, Headline: "SCOUT SLOT FREE"}); err == nil {
		t.Fatal("Announce must refuse a coordination verb from an auto-responder")
	}
	if got := len(b.All()); got != 0 {
		t.Fatalf("refused message must not be stored, found %d", got)
	}
}

// Anything a responder does post has to be visibly distinct, because the
// message that caused the outage was a reply — which the verb rule still
// permits.
func TestAnnounceStampsAutoOnResponderMessages(t *testing.T) {
	t.Setenv(AutoResponderEnv, "1")
	b := newTestBus(t)

	sent, err := b.Announce(Announcement{ReplyTo: "msg_abc", Headline: "confirmed idle"})
	if err != nil {
		t.Fatalf("reply from a responder must be allowed: %v", err)
	}
	if !sent.Auto {
		t.Error("responder message must be stamped Auto")
	}
	if sent.AutoMarker() == "" {
		t.Error("responder message must render an auto marker")
	}

	stored := b.All()
	if len(stored) != 1 || !stored[0].Auto {
		t.Error("Auto must survive the round-trip through the store")
	}
}

func TestAnnounceLeavesNormalMessagesUnmarked(t *testing.T) {
	t.Setenv(AutoResponderEnv, "")
	b := newTestBus(t)

	sent, err := b.Announce(Announcement{Kind: KindDone, Headline: "landed"})
	if err != nil {
		t.Fatalf("normal session must be able to post done: %v", err)
	}
	if sent.Auto || sent.AutoMarker() != "" {
		t.Error("a human-driven session's message must not be marked auto")
	}
}

// Withholding the tools stops the responder trying in the first place. The
// responder's `claude -p` inherits the MCP server too, so a CLI-only guard
// would be bypassed by simply calling the tool instead.
func TestToolDefinitionsWithholdCoordinationToolsFromResponder(t *testing.T) {
	banned := map[string]bool{"hive_bus_intent": true, "hive_bus_waiting": true, "hive_bus_done": true}
	required := []string{"hive_bus_reply", "hive_bus_ask", "hive_bus_list", "hive_bus_read"}

	t.Setenv(AutoResponderEnv, "1")
	names := map[string]bool{}
	for _, tool := range toolDefinitions() {
		names[tool["name"].(string)] = true
	}
	for tool := range banned {
		if names[tool] {
			t.Errorf("%s must not be offered to an auto-responder", tool)
		}
	}
	for _, tool := range required {
		if !names[tool] {
			t.Errorf("%s must remain available to an auto-responder", tool)
		}
	}

	t.Setenv(AutoResponderEnv, "")
	names = map[string]bool{}
	for _, tool := range toolDefinitions() {
		names[tool["name"].(string)] = true
	}
	for tool := range banned {
		if !names[tool] {
			t.Errorf("%s must still be offered to a normal session", tool)
		}
	}
}

// The prompt must not advertise verbs the code refuses, or the responder burns
// a turn discovering the rule.
func TestResponderPromptOffersOnlyReplyAndAsk(t *testing.T) {
	prompt := buildResponderPrompt(RespondOptions{
		Peer:    Peer{Name: "wt:demo", Path: "/tmp/demo"},
		Trigger: Announcement{ID: "msg_abc", From: "wt:peer", Headline: "taking the slot"},
		HiveBin: "hive",
	})

	for _, verb := range []string{"bus intent", "bus waiting", "bus done"} {
		if strings.Contains(prompt, verb) {
			t.Errorf("prompt must not offer %q", verb)
		}
	}
	for _, verb := range []string{"bus reply", "bus ask"} {
		if !strings.Contains(prompt, verb) {
			t.Errorf("prompt must still offer %q", verb)
		}
	}
}

func newTestBus(t *testing.T) *Bus {
	t.Helper()
	store, err := OpenStore(t.TempDir() + "/bus.jsonl")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	return New(store, "wt:test")
}
