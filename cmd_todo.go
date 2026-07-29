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
		return runTodoSetDone(args[1:], true)
	case "reopen", "undone":
		return runTodoSetDone(args[1:], false)
	case "current", "cur", "claim":
		return runTodoCurrent(args[1:])
	case "show", "mine":
		return runTodoShow()
	case "normalize", "resave":
		return runTodoNormalize()
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

func runTodoList() int {
	cwd := todoCwd()
	todos := loadTodos(cwd)
	if len(todos) == 0 {
		fmt.Println("(no tasks — hive todo add <subject — description>)")
		return 0
	}
	mine := worktreeClaim(cwd)
	section := ""
	for i, t := range todos {
		if s := t.sectionOrDefault(); s != section {
			section = s
			fmt.Printf("\n### %s\n", section)
		}
		line := fmt.Sprintf("%d  [%s] %s", i+1, t.boxChar(), t.Subject)
		if t.Description != "" {
			line += descSep + t.Description
		}
		if !t.Done && t.Claim != "" {
			if t.Claim == mine {
				line += "  (yours)"
			} else {
				line += "  🔒@" + t.Claim
			}
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

func runTodoSetDone(args []string, done bool) int {
	cwd := todoCwd()
	todos := loadTodos(cwd)
	i, ok := todoIndex(args, len(todos))
	if !ok {
		return 1
	}
	todos[i].Done = done
	if done {
		todos[i].Claim = ""
	}
	word := "done"
	if !done {
		word = "reopened"
	}
	return saveAndReport(cwd, todos, fmt.Sprintf("%s #%d: %s", word, i+1, todos[i].Subject))
}

// runTodoCurrent claims (or releases) a task for this worktree, so parallel
// worktrees don't all grab the same "next" item.
func runTodoCurrent(args []string) int {
	cwd := todoCwd()
	owner := worktreeClaim(cwd)
	if owner == "" {
		fmt.Fprintln(os.Stderr, "error: not in a git worktree — can't claim")
		return 1
	}
	todos := loadTodos(cwd)
	if len(args) > 0 && (args[0] == "clear" || args[0] == "none") {
		return saveAndReport(cwd, releaseClaim(todos, owner), "released your claims")
	}
	i, ok := todoIndex(args, len(todos))
	if !ok {
		return 1
	}
	updated, changed := claimTodo(todos, i, owner)
	if !changed {
		fmt.Fprintf(os.Stderr, "error: #%d is claimed by %s\n", i+1, todos[i].Claim)
		return 1
	}
	verb := "claimed"
	if updated[i].Claim == "" {
		verb = "released"
	}
	return saveAndReport(cwd, updated, fmt.Sprintf("%s #%d: %s", verb, i+1, updated[i].Subject))
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

// runTodoNormalize re-reads and re-writes the block, cleaning up formatting
// drift (e.g. separator artifacts left by an em-dash→hyphen normalizer).
func runTodoNormalize() int {
	cwd := todoCwd()
	todos := loadTodos(cwd)
	if err := saveTodos(cwd, todos); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	fmt.Printf("normalized %d tasks\n", len(todos))
	return 0
}

// runTodoShow prints this worktree's claimed task in a structured form for the
// /pickup skill to load context from. Nothing claimed → a hint.
func runTodoShow() int {
	cwd := todoCwd()
	owner := worktreeClaim(cwd)
	for _, t := range loadTodos(cwd) {
		if !t.Done && owner != "" && t.Claim == owner {
			fmt.Printf("section: %s\nsubject: %s\ndescription: %s\n", t.sectionOrDefault(), t.Subject, t.Description)
			return 0
		}
	}
	fmt.Println("(no task claimed in this worktree — run: hive todo claim <n>)")
	return 0
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
	cwd := statuslineCwd()
	todos := loadTodos(cwd)
	done, total := countDone(todos)
	if total == 0 {
		return 0 // nothing to show
	}
	owner := worktreeClaim(cwd)
	label := "all done ✓"
	if cur := currentForClaim(todos, owner); cur != nil {
		if owner != "" && cur.Claim == owner {
			label = "▸ " + truncStr(cur.Subject, 58) // your claimed task
		} else {
			label = "next: " + truncStr(cur.Subject, 55) // unclaimed — up for grabs
		}
	}
	fmt.Printf("%s · %d/%d", label, done, total)
	return 0
}

// statuslineCwd extracts the session's working directory from the JSON Claude
// Code sends on stdin. Prefer `cwd` — for a split running in a worktree it's
// that worktree, whereas `workspace.current_dir` can resolve to the project
// root (main), which would make every split claim as the same branch.
func statuslineCwd() string {
	if data, err := io.ReadAll(os.Stdin); err == nil && len(data) > 0 {
		var p struct {
			Cwd       string `json:"cwd"`
			Workspace struct {
				CurrentDir string `json:"current_dir"`
			} `json:"workspace"`
		}
		if json.Unmarshal(data, &p) == nil {
			if p.Cwd != "" {
				return p.Cwd
			}
			if p.Workspace.CurrentDir != "" {
				return p.Workspace.CurrentDir
			}
		}
	}
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return "."
}
