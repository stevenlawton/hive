package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Cost per ticket, so a small task that has eaten a lot of work is visible.
//
// A session is not a ticket — it chats, explores, and does several tickets'
// worth of things — so session cost cannot be attributed directly. The unit
// that IS the ticket is the agent run, plus every sub-agent it dispatches.
//
// Those sub-agents never touch hive, so they cannot be recognised from what
// they do. What they do inherit is a working directory. So hive records the
// cwd it sees whenever a ticket is named on its own CLI — `hive todo state
// <id>`, `claim <id>` and friends, which every pipeline agent runs — and from
// then on all agent work under that directory belongs to that ticket.
//
// Reconstructing this after the fact instead reaches about 42% of agent spend:
// only the agents that ran a hive command can be recognised, and those are not
// the ones doing the work.

type ticketSpend struct {
	NewTokens int `json:"new_tokens"`
	Runs      int `json:"runs"`
}

type ticketCwdEntry struct {
	Ticket string `json:"ticket"`
	Cwd    string `json:"cwd"`
	// SeenAt is informational and therefore a string, not a time.Time. Decoding
	// it as a time made one unparseable timestamp fail the whole array, and
	// every attribution disappeared with no error at all.
	SeenAt string `json:"seen_at"`
}

// verbCommitsToTicket reports whether naming a ticket on this verb means the
// caller is WORKING it, rather than merely looking at it.
//
// The distinction is load-bearing. Recording on `show` attributed 123k tokens
// of unrelated agent work to a ticket that had only been read, because a
// session reads many tickets and the last one read won the directory. A false
// figure here is worse than a missing one: the whole point is deciding whether
// a ticket cost too much.
//
// `done` is deliberately excluded — by then the work is over, and the caller is
// often a human in a different directory closing it out.
func verbCommitsToTicket(verb string) bool {
	switch verb {
	case "claim", "current", "cur", "state":
		return true
	}
	return false
}

func ticketCwdPath() string {
	return filepath.Join(hiveDataDir(), "ticket-cwds.json")
}

// recordTicketCwd notes that work on a ticket happens in a directory. It is
// called from the CLI verbs that name a ticket, so the pipeline needs no
// changes: agents already run these commands as a matter of course.
//
// Failure is silent. Losing an attribution is a gap in a report; breaking a
// todo command because a ledger would not write is not worth it.
func recordTicketCwd(ticket, cwd string, now time.Time) {
	if ticket == "" || cwd == "" {
		return
	}
	entries := loadTicketCwdEntries()
	for i, e := range entries {
		if e.Cwd == cwd {
			// Re-point rather than duplicate: a worktree gets reused, and the
			// most recent ticket to claim it is the one working there now.
			entries[i].Ticket = ticket
			entries[i].SeenAt = now.Format(time.RFC3339)
			writeTicketCwds(entries)
			return
		}
	}
	entries = append(entries, ticketCwdEntry{Ticket: ticket, Cwd: cwd, SeenAt: now.Format(time.RFC3339)})
	writeTicketCwds(entries)
}

func loadTicketCwdEntries() []ticketCwdEntry {
	b, err := os.ReadFile(ticketCwdPath())
	if err != nil {
		return nil
	}
	var out []ticketCwdEntry
	if json.Unmarshal(b, &out) != nil {
		return nil
	}
	return out
}

func writeTicketCwds(entries []ticketCwdEntry) {
	path := ticketCwdPath()
	if os.MkdirAll(filepath.Dir(path), 0o755) != nil {
		return
	}
	b, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return
	}
	tmp := path + ".tmp"
	if os.WriteFile(tmp, b, 0o644) != nil {
		return
	}
	_ = os.Rename(tmp, path)
}

func loadTicketCwds() map[string]string {
	out := map[string]string{}
	for _, e := range loadTicketCwdEntries() {
		out[e.Cwd] = e.Ticket
	}
	return out
}

// ticketForCwd resolves a working directory to a ticket by deepest match, so a
// worktree inside a repo wins over the repo itself and a sub-agent working in a
// subdirectory still attributes. Matching is on path boundaries: ".../wt1" must
// not swallow ".../wt10".
func ticketForCwd(m map[string]string, cwd string) string {
	best, bestLen := "", -1
	for dir, ticket := range m {
		if dir == "" {
			continue
		}
		if cwd != dir && !strings.HasPrefix(cwd, strings.TrimSuffix(dir, "/")+"/") {
			continue
		}
		if len(dir) > bestLen {
			best, bestLen = ticket, len(dir)
		}
	}
	return best
}

// scanAgentSpend totals every subagent transcript, attributed by the directory
// it ran in. Claude Code writes one file per agent under
// <project>/<session-id>/subagents/, so the tree walk is the whole join.
func scanAgentSpend(projectsDir string, cwds map[string]string) map[string]ticketSpend {
	out := map[string]ticketSpend{}
	files, err := filepath.Glob(filepath.Join(projectsDir, "*", "subagents", "agent-*.jsonl"))
	if err != nil || len(files) == 0 {
		more, _ := filepath.Glob(filepath.Join(projectsDir, "*", "*", "subagents", "agent-*.jsonl"))
		files = append(files, more...)
	}
	for _, f := range files {
		ticket, tokens := agentFileSpend(f, cwds)
		if ticket == "" || tokens == 0 {
			continue
		}
		s := out[ticket]
		s.NewTokens += tokens
		s.Runs++
		out[ticket] = s
	}
	return out
}

// agentFileSpend returns the ticket this agent worked and the new tokens it
// produced. New tokens are output plus cache writes: cache reads are re-reads
// of context already paid for, and counting them would say a long agent did
// more work than a productive one.
func agentFileSpend(path string, cwds map[string]string) (string, int) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0
	}
	defer f.Close()

	ticket, tokens := "", 0
	dec := json.NewDecoder(f)
	for {
		var r struct {
			Type    string `json:"type"`
			Cwd     string `json:"cwd"`
			Message struct {
				Model string `json:"model"`
				Usage struct {
					OutputTokens int `json:"output_tokens"`
					CacheCreate  int `json:"cache_creation_input_tokens"`
				} `json:"usage"`
			} `json:"message"`
		}
		if dec.Decode(&r) != nil {
			break
		}
		if ticket == "" && r.Cwd != "" {
			ticket = ticketForCwd(cwds, r.Cwd)
		}
		if r.Type == "assistant" && r.Message.Model != "<synthetic>" {
			tokens += r.Message.Usage.OutputTokens + r.Message.Usage.CacheCreate
		}
	}
	return ticket, tokens
}

// sortedTicketSpend orders a report by spend, heaviest first.
func sortedTicketSpend(m map[string]ticketSpend) []string {
	ids := make([]string, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return m[ids[i]].NewTokens > m[ids[j]].NewTokens })
	return ids
}

// claudeProjectsDir is where Claude Code keeps transcripts, one directory per
// project and one file per subagent beneath each session.
func claudeProjectsDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude", "projects")
}

const todoCostUsage = `usage: hive todo cost [<ref>]

Agent tokens spent per ticket — output plus cache writes, which is work
produced rather than context re-read.

Attribution comes from the working directory: hive records the cwd whenever a
ticket is named on this CLI, and every agent that ran under that directory
counts, including sub-agents that never touched hive themselves.

Only work done since hive started recording appears. There is no back-fill.`

func runTodoCost(args []string) int {
	for _, a := range args {
		if a == "-h" || a == "--help" {
			fmt.Println(todoCostUsage)
			return 0
		}
	}
	cwds := loadTicketCwds()
	if len(cwds) == 0 {
		fmt.Println("(nothing recorded yet — spend attributes once agents run hive todo commands)")
		return 0
	}
	spend := scanAgentSpend(claudeProjectsDir(), cwds)
	if len(spend) == 0 {
		fmt.Println("(no agent work attributed yet)")
		return 0
	}

	todos := loadTodos(todoCwd())
	subject := map[string]string{}
	for _, t := range todos {
		subject[t.ID] = t.Subject
	}

	ref := ""
	if len(args) > 0 {
		ref = args[0]
	}
	if ref != "" {
		if i, ok := resolveTodoRef(todos, ref); ok {
			ref = todos[i].ID
		}
	}

	for _, id := range sortedTicketSpend(spend) {
		if ref != "" && id != ref {
			continue
		}
		s := spend[id]
		name := subject[id]
		if name == "" {
			name = "(not in this repo's backlog)"
		}
		fmt.Printf("%-4s %9s tokens  %2d run(s)  %s\n",
			id, humanTokens(s.NewTokens), s.Runs, truncStr(name, 60))
	}
	return 0
}

func humanTokens(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 1_000:
		return fmt.Sprintf("%.0fk", float64(n)/1e3)
	default:
		return strconv.Itoa(n)
	}
}
