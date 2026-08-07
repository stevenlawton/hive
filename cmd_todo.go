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
	case "defer", "park":
		return runTodoDefer(args[1:])
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
		handle := t.ID
		if handle == "" {
			handle = strconv.Itoa(i + 1) // not yet stamped; `hive todo normalize` fixes this
		}
		line := fmt.Sprintf("%-4s [%s] %s", handle, t.boxChar(), t.Subject)
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
	todos, err := withTodos(cwd, func(ts []Todo) []Todo {
		return addTodo(ts, defaultSection, subj, desc)
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	fmt.Printf("added %s: %s\n", todos[len(todos)-1].ID, subj)
	return 0
}

func runTodoSetDone(args []string, done bool) int {
	ref, ok := todoRef(args)
	if !ok {
		return 1
	}
	word := "done"
	if !done {
		word = "reopened"
	}
	return mutateOne(todoCwd(), ref, func(ts []Todo, i int) ([]Todo, string) {
		ts[i].Done = done
		if done {
			ts[i].Claim = ""
		}
		return ts, fmt.Sprintf("%s %s: %s", word, ts[i].ID, ts[i].Subject)
	})
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
	if len(args) > 0 && (args[0] == "clear" || args[0] == "none") {
		if _, err := withTodos(cwd, func(ts []Todo) []Todo {
			return releaseClaim(ts, owner)
		}); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return 1
		}
		fmt.Println("released your claims")
		return 0
	}
	ref, ok := todoRef(args)
	if !ok {
		return 1
	}
	var held string
	rc := mutateOne(cwd, ref, func(ts []Todo, i int) ([]Todo, string) {
		out, changed := claimTodo(ts, i, owner)
		if !changed {
			held = ts[i].Claim
			return ts, ""
		}
		verb := "claimed"
		if out[i].Claim == "" {
			verb = "released"
		}
		return out, fmt.Sprintf("%s %s: %s", verb, out[i].ID, out[i].Subject)
	})
	if held != "" {
		fmt.Fprintf(os.Stderr, "error: claimed by %s\n", held)
	}
	return rc
}

// runTodoDefer toggles the parked state on a task.
func runTodoDefer(args []string) int {
	ref, ok := todoRef(args)
	if !ok {
		return 1
	}
	return mutateOne(todoCwd(), ref, func(ts []Todo, i int) ([]Todo, string) {
		ts = deferTodo(ts, i)
		state := "deferred"
		if !ts[i].Deferred {
			state = "un-deferred"
		}
		return ts, fmt.Sprintf("%s %s: %s", state, ts[i].ID, ts[i].Subject)
	})
}

func runTodoRm(args []string) int {
	ref, ok := todoRef(args)
	if !ok {
		return 1
	}
	return mutateOne(todoCwd(), ref, func(ts []Todo, i int) ([]Todo, string) {
		removed := ts[i].Subject
		return deleteTodo(ts, i), "removed: " + removed
	})
}

// runTodoNormalize re-reads and re-writes the block, cleaning up formatting drift
// and stamping ids onto any task still lacking one.
func runTodoNormalize() int {
	todos, err := withTodos(todoCwd(), func(ts []Todo) []Todo { return ts })
	if err != nil {
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

// mutateOne resolves ref and applies a change under the backlog lock. Resolution
// happens inside the closure, against the list as it is on disk: a position read
// from an earlier `list` may point at a different task by now. apply returns the
// message to print, or "" when it declined and reported the reason itself.
func mutateOne(cwd, ref string, apply func([]Todo, int) ([]Todo, string)) int {
	var msg string
	var missing bool
	_, err := withTodos(cwd, func(ts []Todo) []Todo {
		i, ok := resolveTodoRef(ts, ref)
		if !ok {
			missing = true
			return ts
		}
		out, m := apply(ts, i)
		msg = m
		return out
	})
	switch {
	case missing:
		fmt.Fprintf(os.Stderr, "error: no such task %q (see: hive todo list)\n", ref)
		return 1
	case err != nil:
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	case msg == "":
		return 1
	}
	fmt.Println(msg)
	return 0
}

// todoRef pulls the task reference from a verb's arguments.
func todoRef(args []string) (string, bool) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "error: need a task id or number (see: hive todo list)")
		return "", false
	}
	return args[0], true
}

// runTodoStatusline prints the current task + progress for use as a Claude Code
// statusLine command. Claude pipes session JSON on stdin; we read cwd from it.
func runTodoStatusline() int {
	cwd := statuslineCwd()
	todos := loadTodos(cwd)
	done, active, deferred := todoProgress(todos)
	if active+deferred == 0 {
		return 0 // nothing to show
	}
	owner := worktreeClaim(cwd)
	label := "all done ✓"
	if cur := currentForClaim(todos, owner); cur != nil {
		if owner != "" && cur.Claim == owner {
			label = "▸ " + truncStr(cur.Subject, 55) // your claimed task
		} else {
			label = "next: " + truncStr(cur.Subject, 52) // unclaimed — up for grabs
		}
	}
	out := fmt.Sprintf("%s · %d/%d", label, done, active)
	if deferred > 0 {
		out += fmt.Sprintf(" · %d parked", deferred)
	}
	fmt.Print(out)
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
