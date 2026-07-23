package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// TodoStatus is the state of a task in a repo's todo list.
type TodoStatus int

const (
	TodoPending TodoStatus = iota // "- [ ]"
	TodoCurrent                   // "- [~]" — the task you're on
	TodoDone                      // "- [x]"
)

// Todo is one task in the rich TODO.md format:
//
//   - [box] **subject** — description
//
// grouped under "### section" headers inside a TASKS:BEGIN/END block.
type Todo struct {
	Status      TodoStatus
	Section     string // the "### " header this task lives under
	Subject     string // the bold subject (may carry a #NNN id prefix)
	Description string // free text after " — " (may be empty)
}

const (
	defaultSection = "Tasks"
	tasksBegin     = "<!-- TASKS:BEGIN (managed by hive — edit tasks via the drawer / `hive todo`, not by hand) -->"
	tasksEnd       = "<!-- TASKS:END -->"
	emDash         = " — "
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

// saveTodos regenerates the TASKS block in the backlog file, preserving all
// content outside the markers.
func saveTodos(repoPath string, todos []Todo) error {
	path := todoFilePath(repoPath)
	existing := ""
	if data, err := os.ReadFile(path); err == nil {
		existing = string(data)
	}
	updated := replaceBlock(existing, formatTodos(todos))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(updated), 0o644)
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

func blockBounds(lines []string) (begin, end int) {
	begin, end = -1, -1
	for i, l := range lines {
		if begin < 0 && strings.Contains(l, "TASKS:BEGIN") {
			begin = i
		} else if strings.Contains(l, "TASKS:END") {
			end = i
			break
		}
	}
	return begin, end
}

// parseTodos parses "### section" headers and "- [box] **subject** — desc"
// task lines. Anything else (blank lines, a Last-sync line) is ignored.
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

func parseTodoLine(s string) (Todo, bool) {
	if !strings.HasPrefix(s, "- [") || len(s) < 6 || s[4] != ']' {
		return Todo{}, false
	}
	var status TodoStatus
	switch s[3] {
	case ' ':
		status = TodoPending
	case '~':
		status = TodoCurrent
	case 'x', 'X':
		status = TodoDone
	default:
		return Todo{}, false
	}
	rest := strings.TrimSpace(s[5:])
	if rest == "" {
		return Todo{}, false
	}
	subject, description := rest, ""
	// Bold subject: **subject**[ — description]
	if strings.HasPrefix(rest, "**") {
		if close := strings.Index(rest[2:], "**"); close >= 0 {
			subject = rest[2 : 2+close]
			tail := strings.TrimSpace(rest[2+close+2:])
			description = strings.TrimPrefix(tail, strings.TrimSpace(emDash))
			description = strings.TrimSpace(description)
		}
	} else if i := strings.Index(rest, emDash); i >= 0 {
		subject = strings.TrimSpace(rest[:i])
		description = strings.TrimSpace(rest[i+len(emDash):])
	}
	if subject == "" {
		return Todo{}, false
	}
	return Todo{Status: status, Subject: subject, Description: description}, true
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
	for _, sec := range order {
		b.WriteString("\n### " + sec + "\n\n")
		for _, t := range todos {
			if t.sectionOrDefault() != sec {
				continue
			}
			b.WriteString("- [" + t.boxChar() + "] **" + t.Subject + "**")
			if t.Description != "" {
				b.WriteString(emDash + t.Description)
			}
			b.WriteByte('\n')
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
	switch t.Status {
	case TodoCurrent:
		return "~"
	case TodoDone:
		return "x"
	default:
		return " "
	}
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
		Status:      TodoPending,
		Section:     section,
		Subject:     subject,
		Description: strings.TrimSpace(description),
	})
}

func toggleTodoDone(todos []Todo, i int) []Todo {
	if i < 0 || i >= len(todos) {
		return todos
	}
	if todos[i].Status == TodoDone {
		todos[i].Status = TodoPending
	} else {
		todos[i].Status = TodoDone
	}
	return todos
}

// setTodoCurrent marks i as the current task, demoting any other current task
// so there is at most one. Toggling the current task clears it.
func setTodoCurrent(todos []Todo, i int) []Todo {
	if i < 0 || i >= len(todos) {
		return todos
	}
	if todos[i].Status == TodoCurrent {
		todos[i].Status = TodoPending
		return todos
	}
	for j := range todos {
		if todos[j].Status == TodoCurrent {
			todos[j].Status = TodoPending
		}
	}
	todos[i].Status = TodoCurrent
	return todos
}

func deleteTodo(todos []Todo, i int) []Todo {
	if i < 0 || i >= len(todos) {
		return todos
	}
	return append(todos[:i], todos[i+1:]...)
}

// currentTodo is what the statusline shows: the current task, else the first
// pending one, else nil.
func currentTodo(todos []Todo) *Todo {
	for i := range todos {
		if todos[i].Status == TodoCurrent {
			return &todos[i]
		}
	}
	for i := range todos {
		if todos[i].Status == TodoPending {
			return &todos[i]
		}
	}
	return nil
}

func countDone(todos []Todo) (done, total int) {
	for _, t := range todos {
		total++
		if t.Status == TodoDone {
			done++
		}
	}
	return done, total
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
