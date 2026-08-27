package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Claude Code writes one JSONL transcript per session under
// ~/.claude/projects/<slug>/<session-id>.jsonl. Assistant records carry a
// message.usage block; nothing else in the tree reports token spend. The
// session hook payloads log-session.sh captures do NOT include usage, so this
// is the only source.
const tokensUsage = `usage: hive tokens [--window <dur>] [--limit <n>] [--sessions <n>] [--json]

Token spend over a rolling window, read from Claude Code transcripts.

  --window <dur>   rolling window, Go duration (default 5h — the usage-limit window)
  --limit <n>      token budget for the window; adds used/remaining
  --sessions <n>   how many busiest sessions to list (default 6, 0 to hide)
  --all            every record on disk, ignoring --window
  --json           machine-readable output`

type tokenUsage struct {
	Input       int64 `json:"input"`
	Output      int64 `json:"output"`
	CacheRead   int64 `json:"cache_read"`
	CacheCreate int64 `json:"cache_create"`
}

// Total is every component summed. Cache reads dominate real transcripts by
// two orders of magnitude, so a report that omits them is not a cost report.
func (u tokenUsage) Total() int64 {
	return u.Input + u.Output + u.CacheRead + u.CacheCreate
}

func (u *tokenUsage) add(o tokenUsage) {
	u.Input += o.Input
	u.Output += o.Output
	u.CacheRead += o.CacheRead
	u.CacheCreate += o.CacheCreate
}

type tokenSession struct {
	ID       string     `json:"session"`
	Project  string     `json:"project"`
	Usage    tokenUsage `json:"usage"`
	Requests int        `json:"requests"`
	Last     time.Time  `json:"last"`
}

type tokenReport struct {
	Since    time.Time      `json:"since"`
	Total    tokenUsage     `json:"total"`
	Requests int            `json:"requests"`
	Sessions []tokenSession `json:"sessions"`
}

type tokenRecord struct {
	At      time.Time
	Session string
	Project string
	Usage   tokenUsage
}

// tokenLine is the subset of a transcript record this command reads.
type tokenLine struct {
	Timestamp string `json:"timestamp"`
	SessionID string `json:"sessionId"`
	CWD       string `json:"cwd"`
	Message   struct {
		Usage *struct {
			Input       int64 `json:"input_tokens"`
			Output      int64 `json:"output_tokens"`
			CacheRead   int64 `json:"cache_read_input_tokens"`
			CacheCreate int64 `json:"cache_creation_input_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

// parseTokenRecord reads one transcript line. ok is false for any line that is
// not an assistant record carrying usage — user turns, tool results, summaries
// and malformed lines all return false rather than an error, because a
// transcript is an append-only log that may be mid-write.
func parseTokenRecord(line []byte) (tokenRecord, bool) {
	if !bytes.Contains(line, []byte(`"usage"`)) {
		return tokenRecord{}, false
	}
	var tl tokenLine
	if err := json.Unmarshal(line, &tl); err != nil {
		return tokenRecord{}, false
	}
	if tl.Message.Usage == nil {
		return tokenRecord{}, false
	}
	at, err := time.Parse(time.RFC3339, tl.Timestamp)
	if err != nil {
		return tokenRecord{}, false
	}
	u := tokenUsage{
		Input:       tl.Message.Usage.Input,
		Output:      tl.Message.Usage.Output,
		CacheRead:   tl.Message.Usage.CacheRead,
		CacheCreate: tl.Message.Usage.CacheCreate,
	}
	if u.Total() == 0 {
		return tokenRecord{}, false
	}
	return tokenRecord{At: at, Session: tl.SessionID, Project: projectLabel(tl.CWD), Usage: u}, true
}

// projectLabel shortens a record's cwd to something readable in a table.
func projectLabel(cwd string) string {
	if cwd == "" {
		return "?"
	}
	if home, err := os.UserHomeDir(); err == nil {
		if rel, err := filepath.Rel(filepath.Join(home, "repos"), cwd); err == nil && !strings.HasPrefix(rel, "..") {
			return rel
		}
	}
	return filepath.Base(cwd)
}

// scanTokenUsage walks a Claude Code projects root and totals usage at or
// after since. Files last modified before since are skipped without being
// opened — the win that keeps this fast over a tree of hundreds of sessions.
func scanTokenUsage(root string, since time.Time) (tokenReport, error) {
	rep := tokenReport{Since: since}
	byID := map[string]*tokenSession{}

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // an unreadable dir is not fatal; report what we can
		}
		if d.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return nil
		}
		if info, err := d.Info(); err == nil && info.ModTime().Before(since) {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer f.Close()

		// A single transcript line can be megabytes, so bufio.Scanner's
		// token limit is not usable here.
		br := bufio.NewReaderSize(f, 1<<20)
		for {
			line, err := br.ReadBytes('\n')
			if len(line) > 0 {
				if rec, ok := parseTokenRecord(line); ok && !rec.At.Before(since) {
					rep.Total.add(rec.Usage)
					rep.Requests++
					s := byID[rec.Session]
					if s == nil {
						s = &tokenSession{ID: rec.Session, Project: rec.Project}
						byID[rec.Session] = s
					}
					s.Usage.add(rec.Usage)
					s.Requests++
					if rec.At.After(s.Last) {
						s.Last = rec.At
					}
				}
			}
			if err != nil {
				if err != io.EOF {
					return nil
				}
				break
			}
		}
		return nil
	})
	if err != nil {
		return rep, err
	}

	for _, s := range byID {
		rep.Sessions = append(rep.Sessions, *s)
	}
	sort.Slice(rep.Sessions, func(i, j int) bool {
		if rep.Sessions[i].Usage.Total() != rep.Sessions[j].Usage.Total() {
			return rep.Sessions[i].Usage.Total() > rep.Sessions[j].Usage.Total()
		}
		return rep.Sessions[i].ID < rep.Sessions[j].ID
	})
	return rep, nil
}

// commas renders n with thousands separators, right-aligned by the caller.
func commas(n int64) string {
	s := fmt.Sprintf("%d", n)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	var out []byte
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, c)
	}
	if neg {
		return "-" + string(out)
	}
	return string(out)
}

func formatTokenReport(rep tokenReport, window time.Duration, limit int64, topN int, all bool) string {
	var b strings.Builder
	head := fmt.Sprintf("%s rolling window", window)
	if all {
		head = "all time"
	}
	fmt.Fprintf(&b, "hive tokens — %s", head)
	if !all {
		fmt.Fprintf(&b, " (since %s)", rep.Since.Local().Format("15:04"))
	}
	b.WriteString("\n\n")

	rows := []struct {
		label string
		n     int64
	}{
		{"output", rep.Total.Output},
		{"cache write", rep.Total.CacheCreate},
		{"cache read", rep.Total.CacheRead},
		{"input", rep.Total.Input},
	}
	for _, r := range rows {
		fmt.Fprintf(&b, "  %-12s %16s\n", r.label, commas(r.n))
	}
	fmt.Fprintf(&b, "  %-12s %16s\n", "", strings.Repeat("─", 16))
	fmt.Fprintf(&b, "  %-12s %16s\n", "total", commas(rep.Total.Total()))

	if limit > 0 {
		pct := float64(rep.Total.Total()) / float64(limit) * 100
		left := limit - rep.Total.Total()
		if left < 0 {
			left = 0
		}
		fmt.Fprintf(&b, "  %-12s %16s  (%.1f%% of %s, %s left)\n",
			"budget", "", pct, commas(limit), commas(left))
	}

	// Cache reads dominating output is the signal that context is being
	// re-read rather than work being done, so surface the ratio.
	if rep.Total.Output > 0 && rep.Total.CacheRead > 0 {
		fmt.Fprintf(&b, "\n  cache read is %.0fx output — spend is re-reading context, not generating\n",
			float64(rep.Total.CacheRead)/float64(rep.Total.Output))
	}

	if topN > 0 && len(rep.Sessions) > 0 {
		fmt.Fprintf(&b, "\n  busiest sessions\n")
		for i, s := range rep.Sessions {
			if i >= topN {
				break
			}
			id := s.ID
			if len(id) > 8 {
				id = id[:8]
			}
			fmt.Fprintf(&b, "    %-8s  %-28s %14s\n", id, trunc(s.Project, 28), commas(s.Usage.Total()))
		}
	}

	fmt.Fprintf(&b, "\n  %d %s, %d %s\n",
		len(rep.Sessions), plural(len(rep.Sessions), "session"),
		rep.Requests, plural(rep.Requests, "request"))
	return b.String()
}

func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + "…"
}

func runTokensCmd(args []string) int {
	window := 5 * time.Hour
	var limit int64
	topN := 6
	all, asJSON := false, false

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--window":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "error: --window needs a value")
				return 1
			}
			d, err := time.ParseDuration(args[i+1])
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: bad --window %q: %v\n", args[i+1], err)
				return 1
			}
			window, i = d, i+1
		case "--limit":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "error: --limit needs a value")
				return 1
			}
			if _, err := fmt.Sscanf(args[i+1], "%d", &limit); err != nil {
				fmt.Fprintf(os.Stderr, "error: bad --limit %q\n", args[i+1])
				return 1
			}
			i++
		case "--sessions":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "error: --sessions needs a value")
				return 1
			}
			if _, err := fmt.Sscanf(args[i+1], "%d", &topN); err != nil {
				fmt.Fprintf(os.Stderr, "error: bad --sessions %q\n", args[i+1])
				return 1
			}
			i++
		case "--all":
			all = true
		case "--json":
			asJSON = true
		case "-h", "--help":
			fmt.Println(tokensUsage)
			return 0
		default:
			fmt.Fprintf(os.Stderr, "error: unexpected argument %q\n\n%s\n", args[i], tokensUsage)
			return 1
		}
	}

	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: no home dir: %v\n", err)
		return 1
	}
	root := filepath.Join(home, ".claude", "projects")
	if _, err := os.Stat(root); err != nil {
		fmt.Fprintf(os.Stderr, "error: no Claude Code transcripts at %s\n", root)
		return 1
	}

	since := time.Now().Add(-window)
	if all {
		since = time.Time{}
	}
	rep, err := scanTokenUsage(root, since)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(rep); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return 1
		}
		return 0
	}
	fmt.Print(formatTokenReport(rep, window, limit, topN, all))
	return 0
}
