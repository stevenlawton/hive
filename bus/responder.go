package bus

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Peer is a worktree that can participate in bus coordination.
// The Responder runs `claude -p` with the peer's worktree as the working
// directory and its Name as the sender identity.
type Peer struct {
	Name string // e.g. "wt:backend-auth" — used as bus sender id
	Path string // absolute path to the worktree root
}

// RespondOptions configures a single responder run.
type RespondOptions struct {
	Peer      Peer
	Trigger   Announcement // the new bus message to evaluate
	HiveBin   string       // absolute path to the hive binary
	Timeout   time.Duration
	ClaudeBin string // path to `claude` binary; defaults to "claude"
}

// Respond spawns `claude -p` with a coordination prompt and lets it decide
// whether the peer worktree should reply to, engage with, or ignore the
// trigger announcement. The `claude -p` process runs in the peer's working
// directory with the peer's identity (HIVE_SENDER env var) so any bus
// commands it invokes are stamped correctly.
//
// Loop prevention is the caller's job: don't fire Respond on reply-type
// messages or on messages whose From matches the peer.
func Respond(ctx context.Context, opts RespondOptions) error {
	if opts.Timeout == 0 {
		opts.Timeout = 120 * time.Second
	}
	if opts.ClaudeBin == "" {
		opts.ClaudeBin = "claude"
	}

	ctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	prompt := buildResponderPrompt(opts)
	cmd := exec.CommandContext(ctx, opts.ClaudeBin, "-p", prompt)
	cmd.Dir = opts.Peer.Path
	cmd.Env = append(cmd.Environ(),
		"HIVE_SENDER="+opts.Peer.Name,
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("claude -p failed: %w (stderr: %s)", err, stderr.String())
	}
	return nil
}

func buildResponderPrompt(opts RespondOptions) string {
	msg := opts.Trigger
	kind := string(msg.KindOrDefault())
	hive := opts.HiveBin
	if hive == "" {
		hive = "hive"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "You are the Hive bus coordination agent for worktree %q.\n", opts.Peer.Name)
	fmt.Fprintf(&b, "Your working directory is %q.\n\n", opts.Peer.Path)

	b.WriteString("A peer just posted this message on the shared Hive bus:\n\n")
	fmt.Fprintf(&b, "  id:       %s\n", msg.ID)
	fmt.Fprintf(&b, "  from:     %s\n", msg.From)
	fmt.Fprintf(&b, "  kind:     %s\n", kind)
	fmt.Fprintf(&b, "  headline: %s\n", msg.Headline)
	if msg.Body != "" {
		fmt.Fprintf(&b, "  body:     %s\n", msg.Body)
	}
	if len(msg.Touches) > 0 {
		fmt.Fprintf(&b, "  touches:  %s\n", strings.Join(msg.Touches, ", "))
	}
	b.WriteString("\n")

	b.WriteString(`Your job is to decide whether this message is relevant to the code in your
worktree, and if so, respond helpfully.

Use your normal tools (Grep, Glob, Read, Bash) to investigate the worktree if
you need to check whether the message affects files you're working on.

Respond using the hive bus CLI:

`)
	fmt.Fprintf(&b, "  %s bus reply %s \"your reply text\"\n", hive, msg.ID)
	fmt.Fprintf(&b, "  %s bus ask     \"follow-up question\"\n", hive)
	fmt.Fprintf(&b, "  %s bus intent  \"I'm about to X\"\n", hive)
	fmt.Fprintf(&b, "  %s bus waiting \"blocked on X\"\n", hive)
	fmt.Fprintf(&b, "  %s bus done    \"X finished\"\n", hive)

	b.WriteString(`
Guidelines:
- If the message is NOT relevant to your worktree, do nothing — just exit.
  Do not post anything; do not reply "not relevant". Silence is the default.
- If it IS relevant, post exactly one reply via 'hive bus reply'. Be concise.
- If you have useful information the sender should know (e.g. "yes I use
  that type in services/user.ts, please keep a compatibility shim"), share it.
- If the message is a question and you can answer it, answer it.
- If the message is an intent that conflicts with your own work, flag the
  conflict clearly.
- Do NOT post meta-commentary about your own decision-making process.
`)

	return b.String()
}
