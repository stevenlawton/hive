package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// runTodoCmd handles `hive todo <sub>` CLI invocations (kept fast — it never
// opens the TUI). All verbs operate on the current directory's repo, resolving
// its main worktree, so they work from any checkout/branch.
func runTodoCmd(args []string) int {
	if len(args) == 0 {
		return runTodoList()
	}
	switch args[0] {
	case "statusline":
		return runTodoStatusline()
	case "list", "ls":
		return runTodoList()
	case "add":
		return runTodoAdd(args[1:])
	case "done":
		return runTodoSetStatus(args[1:], TodoDone)
	case "reopen", "undone":
		return runTodoSetStatus(args[1:], TodoPending)
	case "current", "cur":
		return runTodoCurrent(args[1:])
	case "rm", "del", "delete":
		return runTodoRm(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown todo subcommand: %s\n", args[0])
		fmt.Fprintln(os.Stderr, "usage: hive todo [list|add|done|reopen|current|rm|statusline]")
		return 1
	}
}

func todoCwd() string {
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return "."
}

func statusGlyph(s TodoStatus) string {
	switch s {
	case TodoCurrent:
		return "[~]"
	case TodoDone:
		return "[x]"
	default:
		return "[ ]"
	}
}

func runTodoList() int {
	todos := loadTodos(todoCwd())
	if len(todos) == 0 {
		fmt.Println("(no tasks — hive todo add <headline>)")
		return 0
	}
	section := ""
	for i, t := range todos {
		if s := t.sectionOrDefault(); s != section {
			section = s
			fmt.Printf("\n### %s\n", section)
		}
		line := fmt.Sprintf("%d  %s %s", i+1, statusGlyph(t.Status), t.Subject)
		if t.Description != "" {
			line += emDash + t.Description
		}
		fmt.Println(line)
	}
	return 0
}

func runTodoAdd(args []string) int {
	text := strings.TrimSpace(strings.Join(args, " "))
	if text == "" {
		fmt.Fprintln(os.Stderr, "usage: hive todo add <subject — optional description>")
		return 1
	}
	subj, desc := splitSubjectDesc(text)
	cwd := todoCwd()
	todos := addTodo(loadTodos(cwd), defaultSection, subj, desc)
	return saveAndReport(cwd, todos, fmt.Sprintf("added #%d: %s", len(todos), subj))
}

func runTodoSetStatus(args []string, status TodoStatus) int {
	cwd := todoCwd()
	todos := loadTodos(cwd)
	i, ok := todoIndex(args, len(todos))
	if !ok {
		return 1
	}
	todos[i].Status = status
	word := map[TodoStatus]string{TodoDone: "done", TodoPending: "reopened"}[status]
	return saveAndReport(cwd, todos, fmt.Sprintf("%s #%d: %s", word, i+1, todos[i].Subject))
}

func runTodoCurrent(args []string) int {
	cwd := todoCwd()
	todos := loadTodos(cwd)
	if len(args) > 0 && (args[0] == "clear" || args[0] == "none") {
		for j := range todos {
			if todos[j].Status == TodoCurrent {
				todos[j].Status = TodoPending
			}
		}
		return saveAndReport(cwd, todos, "cleared current task")
	}
	i, ok := todoIndex(args, len(todos))
	if !ok {
		return 1
	}
	for j := range todos {
		if todos[j].Status == TodoCurrent {
			todos[j].Status = TodoPending
		}
	}
	todos[i].Status = TodoCurrent
	return saveAndReport(cwd, todos, fmt.Sprintf("current → #%d: %s", i+1, todos[i].Subject))
}

func runTodoRm(args []string) int {
	cwd := todoCwd()
	todos := loadTodos(cwd)
	i, ok := todoIndex(args, len(todos))
	if !ok {
		return 1
	}
	removed := todos[i].Subject
	todos = deleteTodo(todos, i)
	return saveAndReport(cwd, todos, "removed: "+removed)
}

// todoIndex parses a 1-based task number from args and bounds-checks it.
func todoIndex(args []string, n int) (int, bool) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "error: need a task number (see: hive todo list)")
		return 0, false
	}
	v, err := strconv.Atoi(args[0])
	if err != nil || v < 1 || v > n {
		fmt.Fprintf(os.Stderr, "error: invalid task number %q (have %d tasks)\n", args[0], n)
		return 0, false
	}
	return v - 1, true
}

func saveAndReport(cwd string, todos []Todo, msg string) int {
	if err := saveTodos(cwd, todos); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	fmt.Println(msg)
	return 0
}

// runTodoStatusline prints the current task + progress for use as a Claude Code
// statusLine command. Claude pipes session JSON on stdin; we read cwd from it.
func runTodoStatusline() int {
	todos := loadTodos(statuslineCwd())
	done, total := countDone(todos)
	if total == 0 {
		return 0 // nothing to show
	}
	headline := "all done ✓"
	if cur := currentTodo(todos); cur != nil {
		headline = truncStr(cur.Subject, 60)
	}
	fmt.Printf("▸ %s · %d/%d", headline, done, total)
	return 0
}

// statuslineCwd extracts the working directory from the JSON Claude Code sends
// on stdin, falling back to the process cwd.
func statuslineCwd() string {
	if data, err := io.ReadAll(os.Stdin); err == nil && len(data) > 0 {
		var p struct {
			Cwd       string `json:"cwd"`
			Workspace struct {
				CurrentDir string `json:"current_dir"`
			} `json:"workspace"`
		}
		if json.Unmarshal(data, &p) == nil {
			if p.Workspace.CurrentDir != "" {
				return p.Workspace.CurrentDir
			}
			if p.Cwd != "" {
				return p.Cwd
			}
		}
	}
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return "."
}
