package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
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
		return runTodoShow(args[1:])
	case "edit", "rename":
		return runTodoEdit(args[1:])
	case "defer", "park":
		return runTodoDefer(args[1:])
	case "state":
		return runTodoState(args[1:])
	case "reap":
		return runTodoReap(args[1:])
	case "normalize", "resave":
		return runTodoNormalize()
	case "rm", "del", "delete":
		return runTodoRm(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown todo subcommand: %s\n", args[0])
		fmt.Fprintln(os.Stderr, "usage: hive todo [list|add|edit|show|done|reopen|current|defer|state|reap|rm|statusline]")
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

const todoAddUsage = `usage: hive todo add <subject - description>
       hive todo add --description <text> <subject>

The description is optional. Separate it from the subject with " - " (an
em-dash is accepted too), or pass it with --description/-d. Use "--" before a
subject that starts with a dash.`

// parseTodoAddArgs turns argv into a subject and description.
//
// Unrecognised flags are refused rather than folded into the subject: add used
// to join every argument with spaces, so a plausible-looking --description or
// --body-file ended up as literal text inside the task, with nothing to show
// anything had gone wrong.
func parseTodoAddArgs(args []string) (subject, desc string, err error) {
	var positional []string
	flagged := false

	// Flags are read wherever they appear, not only before the subject.
	// Stopping at the first positional put a trailing -d and its value into
	// the task title, and made the unknown-flag check below unreachable.
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			positional = append(positional, args[i+1:]...)
			return finishTodoAdd(positional, desc, flagged)
		case a == "-d" || a == "--description":
			if i+1 >= len(args) {
				return "", "", fmt.Errorf("%s needs a value", a)
			}
			desc, flagged = args[i+1], true
			i++
		case strings.HasPrefix(a, "--description="):
			desc, flagged = strings.TrimPrefix(a, "--description="), true
		case strings.HasPrefix(a, "-") && len(a) > 1:
			return "", "", fmt.Errorf("unknown flag %s", a)
		default:
			positional = append(positional, a)
		}
	}
	return finishTodoAdd(positional, desc, flagged)
}

func finishTodoAdd(rest []string, desc string, flagged bool) (string, string, error) {
	text := strings.TrimSpace(strings.Join(rest, " "))
	if text == "" {
		return "", "", fmt.Errorf("a subject is required")
	}
	subject, inline := splitSubjectDesc(text)
	if inline != "" {
		if flagged {
			return "", "", fmt.Errorf(
				"description given twice: once with --description and once after the separator")
		}
		desc = inline
	}
	return subject, desc, nil
}

func runTodoAdd(args []string) int {
	subj, desc, err := parseTodoAddArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n\n%s\n", err, todoAddUsage)
		return 1
	}
	cwd := todoCwd()
	todos, err := withTodos(cwd, func(ts []Todo) []Todo {
		return addTodo(ts, defaultSection, subj, desc)
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	// Name the file. The list lives on the main worktree so every worktree
	// shares one, which means adding from a branch leaves an uncommitted
	// change in a checkout the caller may not be looking at — enough to abort
	// a deploy pre-flight that insists on a clean tree.
	fmt.Printf("added %s: %s\n  %s (uncommitted)\n",
		todos[len(todos)-1].ID, subj, todoFilePath(cwd))
	return 0
}

// runTodoEdit rewrites a task's text in place. rm+add would do the same job
// but mints a new id and drops the claim, and peers address tasks by id — so
// hand-editing the markdown was the only safe rewrite before this existed.
func runTodoEdit(args []string) int {
	if len(args) < 2 {
		fmt.Fprintf(os.Stderr, "usage: hive todo edit <ref> <subject - description>\n\n%s\n", todoAddUsage)
		return 1
	}
	subj, desc, err := parseTodoAddArgs(args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n\n%s\n", err, todoAddUsage)
		return 1
	}
	return mutateOne(todoCwd(), args[0], func(ts []Todo, i int) ([]Todo, string) {
		ts[i].Subject = subj
		ts[i].Description = desc
		return ts, fmt.Sprintf("edited %s: %s", ts[i].ID, subj)
	})
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

const todoStateUsage = `usage: hive todo state <ref> <state> [--note <text>]

States: plan-review | ready | triage | clear (back to unrefined).
Moving a task backwards requires --note explaining why.`

// runTodoState moves a task through the pipeline. Machine transitions are
// written by the worker that finished a stage; human ones come from the drawer
// or from here.
func runTodoState(args []string) int {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, todoStateUsage)
		return 1
	}
	ref, want := args[0], args[1]
	if want == "clear" {
		want = StateUnrefined
	}
	if !validTodoState(want) {
		fmt.Fprintf(os.Stderr, "error: unknown state %q\n\n%s\n", args[1], todoStateUsage)
		return 1
	}

	note := ""
	for i := 2; i < len(args); i++ {
		switch args[i] {
		case "-n", "--note":
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "error: %s needs a value\n", args[i])
				return 1
			}
			note = args[i+1]
			i++
		default:
			fmt.Fprintf(os.Stderr, "error: unexpected argument %q\n\n%s\n", args[i], todoStateUsage)
			return 1
		}
	}

	var refused string
	rc := mutateOne(todoCwd(), ref, func(ts []Todo, i int) ([]Todo, string) {
		if stateRank(want) < stateRank(ts[i].State) && strings.TrimSpace(note) == "" {
			refused = fmt.Sprintf("moving %s back to %q needs --note explaining why", ts[i].ID, want)
			return ts, ""
		}
		out, ok := setTodoState(ts, i, want, note)
		if !ok {
			refused = "could not set state"
			return ts, ""
		}
		label := want
		if label == StateUnrefined {
			label = "unrefined"
		}
		return out, fmt.Sprintf("%s → %s", out[i].ID, label)
	})
	if refused != "" {
		fmt.Fprintf(os.Stderr, "error: %s\n", refused)
		return 1
	}
	return rc
}

const todoReapUsage = `usage: hive todo reap [--older-than <duration>]

Releases claims held by a branch with no live worktree, and claims older than
the cutoff (default 4h). States are left untouched.`

// runTodoReap releases claims nothing is working on anymore: an orchestrating
// session that died mid-batch leaves its tickets locked, and no other worktree
// can touch them until something clears the claim. The state marker is left
// alone — only the lock is stale.
func runTodoReap(args []string) int {
	older := 4 * time.Hour
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--older-than":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "error: --older-than needs a value")
				return 1
			}
			d, err := time.ParseDuration(args[i+1])
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: bad duration %q\n", args[i+1])
				return 1
			}
			older = d
			i++
		default:
			fmt.Fprintf(os.Stderr, "error: unexpected argument %q\n\n%s\n", args[i], todoReapUsage)
			return 1
		}
	}

	cwd := todoCwd()
	live := liveWorktreeBranches(mainWorktree(cwd))
	cutoff := nowFunc().UTC().Add(-older)

	var released []string
	if _, err := withTodos(cwd, func(ts []Todo) []Todo {
		out, rel := reapClaims(ts, live, cutoff)
		released = rel
		return out
	}); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	if len(released) == 0 {
		fmt.Println("nothing to reap")
		return 0
	}
	for _, r := range released {
		fmt.Println("released " + r)
	}
	return 0
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
// resolveTodoForShow picks the task to display: the one named by ref, or this
// worktree's claim when no ref is given. An unknown ref resolves to nothing
// rather than falling back to the claim — show used to ignore its argument
// entirely and print the claimed task, which reads as an answer to the question
// that was actually asked.
func resolveTodoForShow(ts []Todo, owner, ref string) (Todo, bool) {
	if ref != "" {
		if i, ok := resolveTodoRef(ts, ref); ok {
			return ts[i], true
		}
		return Todo{}, false
	}
	for _, t := range ts {
		if !t.Done && owner != "" && t.Claim == owner {
			return t, true
		}
	}
	return Todo{}, false
}

func runTodoShow(args []string) int {
	cwd := todoCwd()
	ref := ""
	if len(args) > 0 {
		ref = args[0]
	}

	t, ok := resolveTodoForShow(loadTodos(cwd), worktreeClaim(cwd), ref)
	if !ok {
		if ref != "" {
			fmt.Fprintf(os.Stderr, "no task %q — run: hive todo list\n", ref)
			return 1
		}
		fmt.Println("(no task claimed in this worktree — run: hive todo claim <ref>)")
		return 0
	}
	fmt.Printf("id: %s\nsection: %s\nsubject: %s\ndescription: %s\n",
		t.ID, t.sectionOrDefault(), t.Subject, t.Description)
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
