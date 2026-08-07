package main

import (
	"crypto/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Todo is one task in the rich TODO.md format:
//
//   - [box] **subject** — description <!-- @claim -->
//
// grouped under "### section" headers inside a TASKS:BEGIN/END block. "In
// progress" is a per-worktree Claim (the claiming worktree's branch), so
// parallel worktrees each track their own current task and can see which items
// others have taken. box: ' ' open · '~' claimed · 'x' done.
type Todo struct {
	Done        bool
	Deferred    bool   // parked — kept out of "next" and sunk to the bottom
	Section     string // the "### " header this task lives under
	Subject     string // the bold subject (may carry a #NNN id prefix)
	Description string // free text after " — " (may be empty)
	Claim       string // branch/worktree that claimed it; "" = unclaimed
	ID          string // stable short id — addresses the task across peers
}

const (
	defaultSection = "Tasks"
	tasksBegin     = "<!-- TASKS:BEGIN (managed by hive — edit tasks via the drawer / `hive todo`, not by hand) -->"
	tasksEnd       = "<!-- TASKS:END -->"
	// descSep is the subject/description separator hive writes. A hyphen (not an
	// em-dash) so it survives a unicode normalizer unchanged — no git churn. The
	// parser (indexSeparator/trimSeparator) still accepts an em-dash on read.
	descSep = " - "
)

// mainWorktree resolves a repo's primary worktree — the first entry of
// `git worktree list`. The todo list lives there so all worktrees share one.
func mainWorktree(repoPath string) string {
	out, err := exec.Command("git", "-C", repoPath, "worktree", "list", "--porcelain").Output()
	if err != nil {
		return repoPath
	}
	for _, line := range strings.Split(string(out), "\n") {
		if rest, ok := strings.CutPrefix(line, "worktree "); ok {
			return strings.TrimSpace(rest)
		}
	}
	return repoPath
}

// worktreeClaim is the identity a worktree uses to claim tasks — its branch,
// or the worktree directory name when detached. "" if not a git repo.
func worktreeClaim(cwd string) string {
	out, err := exec.Command("git", "-C", cwd, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		return ""
	}
	branch := strings.TrimSpace(string(out))
	if branch == "" || branch == "HEAD" {
		if top, err := exec.Command("git", "-C", cwd, "rev-parse", "--show-toplevel").Output(); err == nil {
			return filepath.Base(strings.TrimSpace(string(top)))
		}
		return ""
	}
	return branch
}

// todoFilePath is the backlog file for a repo: docs/TODO.md if present, else a
// root TODO.md, else docs/TODO.md (created on save).
func todoFilePath(repoPath string) string {
	main := mainWorktree(repoPath)
	if docs := filepath.Join(main, "docs", "TODO.md"); fileExists(docs) {
		return docs
	}
	if root := filepath.Join(main, "TODO.md"); fileExists(root) {
		return root
	}
	return filepath.Join(main, "docs", "TODO.md")
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// loadTodos reads a repo's tasks from the TASKS block. Missing file/block → nil.
func loadTodos(repoPath string) []Todo {
	data, err := os.ReadFile(todoFilePath(repoPath))
	if err != nil {
		return nil
	}
	return parseTodos(extractBlock(string(data)))
}

// extractBlock returns the lines between the TASKS:BEGIN and TASKS:END markers
// (exclusive), or "" if there is no block.
func extractBlock(content string) string {
	lines := strings.Split(content, "\n")
	begin, end := blockBounds(lines)
	if begin < 0 || end <= begin {
		return ""
	}
	return strings.Join(lines[begin+1:end], "\n")
}

// replaceBlock swaps the body between the markers for blockBody, keeping the
// marker lines and everything outside them. If no block exists it appends a
// fresh one (scaffolding a minimal file when content is empty).
func replaceBlock(content, blockBody string) string {
	lines := strings.Split(content, "\n")
	begin, end := blockBounds(lines)
	body := strings.Split(blockBody, "\n")

	if begin >= 0 && end > begin {
		out := append([]string{}, lines[:begin+1]...)
		out = append(out, body...)
		out = append(out, lines[end:]...)
		return strings.Join(out, "\n")
	}

	block := append([]string{tasksBegin}, body...)
	block = append(block, tasksEnd)
	if strings.TrimSpace(content) == "" {
		return "# Open work\n\n" + strings.Join(block, "\n") + "\n"
	}
	sep := "\n"
	if !strings.HasSuffix(content, "\n") {
		sep = "\n\n"
	}
	return content + sep + "\n" + strings.Join(block, "\n") + "\n"
}

// blockBounds finds the TASKS:BEGIN/END marker lines. Only real HTML-comment
// marker lines (trimmed, starting with "<!--") qualify — so prose that merely
// mentions the marker names (a "how this file works" note, a changelog entry)
// can't latch a boundary onto the wrong line and eat everything between it and
// the real END. END is only accepted after BEGIN is found.
func blockBounds(lines []string) (begin, end int) {
	begin, end = -1, -1
	for i, l := range lines {
		if !strings.HasPrefix(strings.TrimSpace(l), "<!--") {
			continue
		}
		if begin < 0 {
			if strings.Contains(l, "TASKS:BEGIN") {
				begin = i
			}
			continue
		}
		if strings.Contains(l, "TASKS:END") {
			end = i
			break
		}
	}
	return begin, end
}

// parseTodos parses "### section" headers and task lines. Anything else (blank
// lines, a Last-sync line) is ignored.
func parseTodos(block string) []Todo {
	var todos []Todo
	section := defaultSection
	for _, line := range strings.Split(block, "\n") {
		trimmed := strings.TrimSpace(line)
		if h, ok := strings.CutPrefix(trimmed, "### "); ok {
			section = strings.TrimSpace(h)
			continue
		}
		if t, ok := parseTodoLine(trimmed); ok {
			t.Section = section
			todos = append(todos, t)
		}
	}
	return todos
}

// parseTodoMarker reads the trailing "<!-- @owner id:xyz -->" marker. Tokens are
// order-independent and each is optional; unrecognised tokens are ignored so a
// marker written by a newer hive still parses here. ok is false when no token was
// recognised, in which case the caller leaves the comment in the text — a plain
// HTML comment in a description is not ours to eat.
func parseTodoMarker(inner string) (claim, id string, ok bool) {
	for _, tok := range strings.Fields(inner) {
		switch {
		case strings.HasPrefix(tok, "@"):
			claim, ok = tok[1:], true
		case strings.HasPrefix(tok, "id:"):
			id, ok = tok[3:], true
		}
	}
	return claim, id, ok
}

func parseTodoLine(s string) (Todo, bool) {
	if !strings.HasPrefix(s, "- [") || len(s) < 6 || s[4] != ']' {
		return Todo{}, false
	}
	var t Todo
	switch s[3] {
	case ' ', '~': // open / claimed (owner from the marker below)
	case '-':
		t.Deferred = true
	case 'x', 'X':
		t.Done = true
	default:
		return Todo{}, false
	}
	rest := strings.TrimSpace(s[5:])
	if rest == "" {
		return Todo{}, false
	}

	// Trailing marker comment: <!-- @owner id:xyz -->
	if i := strings.LastIndex(rest, "<!--"); i >= 0 {
		if j := strings.Index(rest[i:], "-->"); j >= 0 {
			if claim, id, ok := parseTodoMarker(rest[i+4 : i+j]); ok {
				t.Claim, t.ID = claim, id
				rest = strings.TrimSpace(rest[:i])
			}
		}
	}

	subject, description := rest, ""
	if strings.HasPrefix(rest, "**") {
		if c := strings.Index(rest[2:], "**"); c >= 0 {
			subject = rest[2 : 2+c]
			description = trimSeparator(rest[2+c+2:])
		}
	} else if i := indexSeparator(rest); i >= 0 {
		subject = strings.TrimSpace(rest[:i])
		description = trimSeparator(rest[i:])
	}
	if subject == "" {
		return Todo{}, false
	}
	t.Subject, t.Description = subject, description
	return t, true
}

// formatTodos renders tasks back into the block body: a Last-sync line, then
// each section (first-appearance order) with its tasks.
func formatTodos(todos []Todo) string {
	var b strings.Builder
	b.WriteString("Last sync: **" + time.Now().Format("2006-01-02") + "**\n")

	var order []string
	seen := map[string]bool{}
	for _, t := range todos {
		sec := t.sectionOrDefault()
		if !seen[sec] {
			seen[sec] = true
			order = append(order, sec)
		}
	}
	writeTodo := func(t Todo) {
		b.WriteString("- [" + t.boxChar() + "] **" + t.Subject + "**")
		if t.Description != "" {
			b.WriteString(descSep + t.Description)
		}
		if mk := t.marker(); mk != "" {
			b.WriteString(" " + mk)
		}
		b.WriteByte('\n')
	}
	for _, sec := range order {
		b.WriteString("\n### " + sec + "\n\n")
		// Active first, then deferred sunk to the bottom of the section.
		for _, t := range todos {
			if t.sectionOrDefault() == sec && !t.Deferred {
				writeTodo(t)
			}
		}
		for _, t := range todos {
			if t.sectionOrDefault() == sec && t.Deferred {
				writeTodo(t)
			}
		}
	}
	return b.String()
}

func (t Todo) sectionOrDefault() string {
	if strings.TrimSpace(t.Section) == "" {
		return defaultSection
	}
	return t.Section
}

func (t Todo) boxChar() string {
	switch {
	case t.Done:
		return "x"
	case t.Deferred:
		return "-"
	case t.Claim != "":
		return "~"
	default:
		return " "
	}
}

// marker renders the trailing comment. The id is written in every state — a done
// task that lost its id could not be addressed to reopen it — while the claim is
// only meaningful while the task is live.
func (t Todo) marker() string {
	var toks []string
	if t.Claim != "" && !t.Done && !t.Deferred {
		toks = append(toks, "@"+t.Claim)
	}
	if t.ID != "" {
		toks = append(toks, "id:"+t.ID)
	}
	if len(toks) == 0 {
		return ""
	}
	return "<!-- " + strings.Join(toks, " ") + " -->"
}

func addTodo(todos []Todo, section, subject, description string) []Todo {
	subject = strings.TrimSpace(subject)
	if subject == "" {
		return todos
	}
	if strings.TrimSpace(section) == "" {
		section = defaultSection
	}
	return append(todos, Todo{
		Section:     section,
		Subject:     subject,
		Description: strings.TrimSpace(description),
	})
}

func toggleTodoDone(todos []Todo, i int) []Todo {
	if i < 0 || i >= len(todos) {
		return todos
	}
	todos[i].Done = !todos[i].Done
	if todos[i].Done {
		todos[i].Claim = "" // completing releases any claim
	}
	return todos
}

// claimTodo toggles owner's claim on task i: claims if free, releases if it's
// already owner's, and refuses (ok=false) if another worktree holds it. Claiming
// a deferred task un-parks it (you're picking it up).
func claimTodo(todos []Todo, i int, owner string) ([]Todo, bool) {
	if i < 0 || i >= len(todos) || owner == "" || todos[i].Done {
		return todos, false
	}
	if todos[i].Deferred {
		todos[i].Deferred = false
		todos[i].Claim = owner
		return todos, true
	}
	switch todos[i].Claim {
	case owner:
		todos[i].Claim = ""
	case "":
		todos[i].Claim = owner
	default:
		return todos, false // held by another worktree
	}
	return todos, true
}

// deferTodo toggles the parked state on task i. Parking releases any claim and
// clears done.
func deferTodo(todos []Todo, i int) []Todo {
	if i < 0 || i >= len(todos) {
		return todos
	}
	todos[i].Deferred = !todos[i].Deferred
	if todos[i].Deferred {
		todos[i].Claim = ""
		todos[i].Done = false
	}
	return todos
}

// releaseClaim drops every claim held by owner.
func releaseClaim(todos []Todo, owner string) []Todo {
	if owner == "" {
		return todos
	}
	for i := range todos {
		if todos[i].Claim == owner {
			todos[i].Claim = ""
		}
	}
	return todos
}

func deleteTodo(todos []Todo, i int) []Todo {
	if i < 0 || i >= len(todos) {
		return todos
	}
	return append(todos[:i], todos[i+1:]...)
}

// currentForClaim is what a worktree's statusline shows: the task this worktree
// has claimed, else the first unclaimed open task (the next thing to pick up),
// else nil.
func currentForClaim(todos []Todo, owner string) *Todo {
	if owner != "" {
		for i := range todos {
			if !todos[i].Done && !todos[i].Deferred && todos[i].Claim == owner {
				return &todos[i]
			}
		}
	}
	for i := range todos {
		if !todos[i].Done && !todos[i].Deferred && todos[i].Claim == "" {
			return &todos[i]
		}
	}
	return nil
}

// todoProgress counts completed, active (non-deferred), and parked tasks.
func todoProgress(todos []Todo) (done, active, deferred int) {
	for _, t := range todos {
		switch {
		case t.Deferred:
			deferred++
		case t.Done:
			done++
			active++
		default:
			active++
		}
	}
	return done, active, deferred
}

// indexSeparator finds the first subject/description separator — " — " (the
// em-dash hive writes) or " - " (what a unicode normalizer leaves behind) — or
// -1 if neither is present.
func indexSeparator(s string) int {
	em := strings.Index(s, " — ")
	hy := strings.Index(s, " - ")
	switch {
	case em < 0:
		return hy
	case hy < 0:
		return em
	case hy < em:
		return hy
	default:
		return em
	}
}

// trimSeparator strips a leading separator run (em-dashes, hyphens, spaces) so
// the description survives an em-dash→hyphen normalizer without leaking a "- ".
func trimSeparator(s string) string {
	return strings.TrimLeft(strings.TrimSpace(s), "—- ")
}

// idAlphabet is lowercase consonants. No digits, so an id can never be mistaken
// for a positional argument; no vowels, so an id never spells a word.
const idAlphabet = "bcdfghjklmnpqrstvwxyz"

// newTodoID returns a short id absent from taken, widening the id by a character
// if three proves crowded rather than looping forever.
func newTodoID(taken map[string]bool) string {
	for n := 3; ; n++ {
		for attempt := 0; attempt < 100; attempt++ {
			if id := randomID(n); !taken[id] {
				return id
			}
		}
	}
}

// randomID draws n characters from idAlphabet. The modulo bias across 21 symbols
// is irrelevant here — ids only need to not collide, not to be uniform.
func randomID(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b) // crypto/rand.Read panics rather than returning an error
	out := make([]byte, n)
	for i, v := range b {
		out[i] = idAlphabet[int(v)%len(idAlphabet)]
	}
	return string(out)
}

// backfillIDs stamps an id onto every task lacking one, leaving existing ids
// alone. Run on every write, so a hand-edited file heals itself.
func backfillIDs(todos []Todo) []Todo {
	taken := make(map[string]bool, len(todos))
	for _, t := range todos {
		if t.ID != "" {
			taken[t.ID] = true
		}
	}
	for i := range todos {
		if todos[i].ID == "" {
			todos[i].ID = newTodoID(taken)
			taken[todos[i].ID] = true
		}
	}
	return todos
}

// indexByID finds a task by exact id, case-insensitively. An empty id never
// matches, so tasks not yet backfilled are not addressable this way.
func indexByID(todos []Todo, id string) (int, bool) {
	if id == "" {
		return 0, false
	}
	lower := strings.ToLower(id)
	for i := range todos {
		if todos[i].ID != "" && strings.ToLower(todos[i].ID) == lower {
			return i, true
		}
	}
	return 0, false
}

// resolveTodoRef maps a CLI argument to an index: an id first, then a 1-based
// position. Ids contain no digits, so the two forms cannot collide. Callers must
// resolve inside withTodos, against the on-disk list — a position read from an
// earlier `list` may point at a different task by now.
func resolveTodoRef(todos []Todo, arg string) (int, bool) {
	arg = strings.TrimSpace(arg)
	if i, ok := indexByID(todos, arg); ok {
		return i, true
	}
	if v, err := strconv.Atoi(arg); err == nil && v >= 1 && v <= len(todos) {
		return v - 1, true
	}
	return 0, false
}

// truncStr shortens s to at most max display runes, adding an ellipsis.
func truncStr(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	if max <= 1 {
		return string(r[:max])
	}
	return string(r[:max-1]) + "…"
}
