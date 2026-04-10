package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/stevenlawton/hive/bus"
)

// runBusCmd dispatches `hive bus <subcommand>` invocations. Returns the
// process exit code.
func runBusCmd(args []string) int {
	if len(args) == 0 {
		busUsage()
		return 2
	}

	switch args[0] {
	case "announce":
		return busAnnounceCmd(args[1:], "")
	case "intent", "working":
		return busAnnounceCmd(args[1:], string(bus.KindIntent))
	case "waiting", "blocked":
		return busAnnounceCmd(args[1:], string(bus.KindWaiting))
	case "done":
		return busAnnounceCmd(args[1:], string(bus.KindDone))
	case "ask", "question":
		return busAnnounceCmd(args[1:], string(bus.KindQuestion))
	case "fyi":
		return busAnnounceCmd(args[1:], string(bus.KindFYI))
	case "inbox":
		return busInboxCmd(args[1:])
	case "list":
		return busListCmd(args[1:])
	case "read":
		return busReadCmd(args[1:])
	case "reply":
		return busReplyCmd(args[1:])
	case "hook":
		return busHookCmd(args[1:])
	case "todo-hook":
		return busTodoHookCmd(args[1:])
	case "help", "-h", "--help":
		busUsage()
		return 0
	}
	fmt.Fprintf(os.Stderr, "unknown subcommand: %s\n\n", args[0])
	busUsage()
	return 2
}

func busUsage() {
	fmt.Fprintln(os.Stderr, `hive bus — inter-session message bus

Lifecycle verbs (preferred):
  hive bus intent  "refactoring auth middleware"       # 🔨 I'm working on it
  hive bus waiting "code review from Alice"            # ⏳ blocked on something
  hive bus done    "auth refactor, tests green"        # ✅ finished
  hive bus ask     "anyone using user.email?"          # ❓ need info from peers
  hive bus fyi     "switching to new tmux bell flag"   # 📢 just so you know

Reply to an existing message (threaded under its id):
  hive bus reply <id> <text>                           # 💬 reply

Reading:
  hive bus inbox              # unseen messages since last check
  hive bus list [-n N]        # last N messages (default 20)
  hive bus read <id>          # full body of one message

Setup:
  hive bus hook [--install]   # wire the inbox hook into Claude Code

All lifecycle verbs accept these optional flags:
  -b <body>   optional extended body
  -t <globs>  comma-separated file globs this work touches
  -r <id>     thread this under an existing message

Legacy:
  hive bus announce [flags] <headline>   # bare announce, kind defaults to fyi`)
}

func openBus() (*bus.Bus, error) {
	return bus.Open(bus.DetectSender())
}

// busAnnounceCmd handles all announcement-family subcommands. If `defaultKind`
// is non-empty, it's used as the Kind for this announcement (so the lifecycle
// verbs `hive bus intent`, `hive bus waiting`, etc. can share this handler).
func busAnnounceCmd(args []string, defaultKind string) int {
	fs := flag.NewFlagSet("announce", flag.ContinueOnError)
	replyTo := fs.String("r", "", "reply-to message id")
	body := fs.String("b", "", "optional body")
	touches := fs.String("t", "", "comma-separated file globs")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	headline := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if headline == "" {
		fmt.Fprintln(os.Stderr, "error: headline required")
		return 2
	}

	b, err := openBus()
	if err != nil {
		fmt.Fprintf(os.Stderr, "bus open: %v\n", err)
		return 1
	}
	msg := bus.Announcement{
		Kind:     bus.Kind(defaultKind),
		Headline: headline,
		Body:     *body,
		ReplyTo:  *replyTo,
	}
	if *touches != "" {
		msg.Touches = strings.Split(*touches, ",")
	}
	sent, err := b.Announce(msg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "announce: %v\n", err)
		return 1
	}
	fmt.Println(sent.ID)
	return 0
}

func busReplyCmd(args []string) int {
	fs := flag.NewFlagSet("reply", flag.ContinueOnError)
	body := fs.String("b", "", "optional body")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	pos := fs.Args()
	if len(pos) < 2 {
		fmt.Fprintln(os.Stderr, "usage: hive bus reply <id> <text>")
		return 2
	}
	// Reuse announce with -r set
	augmented := []string{"-r", pos[0]}
	if *body != "" {
		augmented = append(augmented, "-b", *body)
	}
	augmented = append(augmented, pos[1:]...)
	return busAnnounceCmd(augmented, "")
}

func busInboxCmd(args []string) int {
	fs := flag.NewFlagSet("inbox", flag.ContinueOnError)
	peek := fs.Bool("peek", false, "don't advance the seen cursor")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	b, err := openBus()
	if err != nil {
		fmt.Fprintf(os.Stderr, "bus open: %v\n", err)
		return 1
	}
	seen, err := bus.OpenSeenStore()
	if err != nil {
		fmt.Fprintf(os.Stderr, "seen store: %v\n", err)
		return 1
	}

	key := bus.SeenKey()
	cursor := seen.Get(key)
	// Filter out messages we sent ourselves — no point echoing.
	self := b.Self
	var unseen []bus.Announcement
	for _, msg := range b.Unseen(cursor) {
		if msg.From == self {
			continue
		}
		unseen = append(unseen, msg)
	}

	if len(unseen) == 0 {
		return 0
	}

	fmt.Printf("📬 %d new bus announcement(s) since your last check:\n\n", len(unseen))
	for _, msg := range unseen {
		printDigestLine(msg)
	}
	fmt.Println()
	fmt.Println("Use `hive bus read <id>` for full body.")
	fmt.Println("Use `hive bus announce <headline>` to broadcast, `hive bus reply <id> <text>` to thread.")

	if !*peek {
		if err := seen.Set(key, b.LatestID()); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to save seen cursor: %v\n", err)
		}
	}
	return 0
}

func busListCmd(args []string) int {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	n := fs.Int("n", 20, "number of messages")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	b, err := openBus()
	if err != nil {
		fmt.Fprintf(os.Stderr, "bus open: %v\n", err)
		return 1
	}
	msgs := b.Tail(*n)
	if len(msgs) == 0 {
		fmt.Println("(no messages)")
		return 0
	}
	for _, msg := range msgs {
		printDigestLine(msg)
	}
	return 0
}

func busReadCmd(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: hive bus read <id>")
		return 2
	}
	b, err := openBus()
	if err != nil {
		fmt.Fprintf(os.Stderr, "bus open: %v\n", err)
		return 1
	}
	msg := b.Find(args[0])
	if msg == nil {
		fmt.Fprintf(os.Stderr, "not found: %s\n", args[0])
		return 1
	}
	printFull(*msg)
	return 0
}

func printDigestLine(msg bus.Announcement) {
	icon := msg.Icon()
	if msg.ReplyTo != "" {
		icon += "→" + msg.ReplyTo
	}
	age := humanAge(time.Since(msg.At))
	fmt.Printf("  [%s] %s · %s · %s %s\n", msg.ID, age, msg.From, icon, msg.Headline)
}

func printFull(msg bus.Announcement) {
	kindLabel := string(msg.KindOrDefault())
	icon := fmt.Sprintf("%s %s", msg.Icon(), kindLabel)
	if msg.ReplyTo != "" {
		icon = "💬 reply to " + msg.ReplyTo
	}
	fmt.Printf("id:       %s\n", msg.ID)
	fmt.Printf("from:     %s\n", msg.From)
	fmt.Printf("at:       %s\n", msg.At.Format(time.RFC3339))
	fmt.Printf("kind:     %s\n", icon)
	fmt.Printf("headline: %s\n", msg.Headline)
	if len(msg.Touches) > 0 {
		fmt.Printf("touches:  %s\n", strings.Join(msg.Touches, ", "))
	}
	if msg.Body != "" {
		fmt.Println()
		fmt.Println(msg.Body)
	}
}

func humanAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

func busHookCmd(args []string) int {
	fs := flag.NewFlagSet("hook", flag.ContinueOnError)
	install := fs.Bool("install", false, "install the Stop hook into ~/.claude/settings.json")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *install {
		exe, err := os.Executable()
		if err != nil {
			fmt.Fprintf(os.Stderr, "resolve binary path: %v\n", err)
			return 1
		}
		if err := bus.InstallClaudeHook(exe); err != nil {
			fmt.Fprintf(os.Stderr, "install hook: %v\n", err)
			return 1
		}
		if err := bus.InstallClaudeMd(exe); err != nil {
			fmt.Fprintf(os.Stderr, "install CLAUDE.md section: %v\n", err)
			return 1
		}
		fmt.Printf("✓ bus hooks installed (UserPromptSubmit + SessionStart) → %s bus inbox\n", exe)
		fmt.Println("✓ Hive Bus section added to ~/.claude/CLAUDE.md")
		return 0
	}
	fmt.Println(`To wire Hive's bus into a Claude Code session, add this to the
project's .claude/settings.json (or ~/.claude/settings.json for global):

{
  "hooks": {
    "Stop": [
      {
        "matcher": "",
        "hooks": [
          { "type": "command", "command": "hive bus inbox" }
        ]
      }
    ]
  }
}

And add this to your CLAUDE.md so Claude learns the tools:

---
This workspace is part of a Hive message bus. Coordinate with peers:
  hive bus announce "<headline>"           broadcast fyi
  hive bus announce -q "<question>"        ask peers
  hive bus announce -t "services/auth/*" "<what you're touching>"
  hive bus reply <id> "<text>"             thread a reply
  hive bus list                            recent messages
  hive bus read <id>                       full body of one message
---`)
	return 0
}
