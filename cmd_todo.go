package main

import (
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
	case "cost", "spend":
		return runTodoCost(args[1:])
	case "normalize", "resave":
		return runTodoNormalize()
	case "rm", "del", "delete":
		return runTodoRm(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown todo subcommand: %s\n", args[0])
		fmt.Fprintln(os.Stderr, "usage: hive todo [list|add|edit|show|done|reopen|current|defer|state|cost|reap|rm|statusline]")
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
			line += descSep + flattenLine(t.Description) // one row per task; `hive todo show` has the full body
		}
		if !t.Done && t.State != "" {
			line += "  ·" + t.State
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
       hive todo add --body-file <path|-> <subject>
       ... | hive todo add <subject>

The description is optional. Separate it from the subject with " - " (an
em-dash is accepted too), or pass it with --description/-d, or read it from a
file with --body-file ("-" means stdin). A body piped in on stdin is picked up
even without the flag, as long as no other body was given — giving two is an
error, never a silent drop. With a body flag the separator is not read at all,
so the subject keeps its dashes.

Prefer --body-file or a pipe for anything long: passing prose through argv
means quoting every apostrophe and backtick, and the shell mangling is silent.

Use "--" before a subject that starts with a dash.`

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
			if err = refuseSecondBody(flagged, a); err != nil {
				return "", "", err
			}
			desc, flagged = args[i+1], true
			i++
		case strings.HasPrefix(a, "--description="):
			if err = refuseSecondBody(flagged, "--description"); err != nil {
				return "", "", err
			}
			desc, flagged = strings.TrimPrefix(a, "--description="), true
		case a == "--body-file":
			if i+1 >= len(args) {
				return "", "", fmt.Errorf("%s needs a value", a)
			}
			if err = refuseSecondBody(flagged, a); err != nil {
				return "", "", err
			}
			if desc, err = readBody(args[i+1]); err != nil {
				return "", "", err
			}
			flagged = true
			i++
		case strings.HasPrefix(a, "--body-file="):
			if err = refuseSecondBody(flagged, "--body-file"); err != nil {
				return "", "", err
			}
			if desc, err = readBody(strings.TrimPrefix(a, "--body-file=")); err != nil {
				return "", "", err
			}
			flagged = true
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
	// The separator only separates when there is something to separate: once a
	// body has been named with a flag, a " - " in the subject is subject text.
	// Splitting anyway manufactured a second description and then refused the
	// add, so no scripted subject could contain a dash.
	subject := text
	if !flagged {
		var inline string
		if subject, inline = splitSubjectDesc(text); inline != "" {
			desc = inline
		}
	}
	// Read stdin whatever else was given. Taking it only as a fallback meant a
	// piped body was silently discarded when the subject happened to contain a
	// " - " separator — which ate a 3000-character ticket, reported success, and
	// is exactly the silent-data-loss this tool should never do.
	if piped := pipedBody(flagged || desc != ""); piped != "" {
		if desc != "" || flagged {
			return "", "", fmt.Errorf(
				"description given twice: once on stdin and once %s\n"+
					"pass only one — drop the separator, or use --body-file",
				givenWhere(flagged))
		}
		desc = piped
	}
	return subject, desc, nil
}

// givenWhere names the other place a body came from, for the given-twice error.
func givenWhere(flagged bool) string {
	if flagged {
		return "as a flag"
	}
	return "after the subject separator"
}

// refuseSecondBody rejects a body given more than once. Silently letting the
// last flag win would mean a body read from a file could be discarded by an
// earlier -d with nothing to show it had happened.
func refuseSecondBody(flagged bool, flag string) error {
	if !flagged {
		return nil
	}
	return fmt.Errorf("description given twice: %s conflicts with a body already given", flag)
}

// readBody reads a description from a file, or from stdin for "-". Passing prose
// through argv means quoting every apostrophe and backtick, and the shell
// mangles it silently; a file or a pipe carries the bytes untouched.
func readBody(path string) (string, error) {
	var (
		data []byte
		err  error
	)
	if path == "-" {
		data, err = io.ReadAll(os.Stdin)
	} else {
		data, err = os.ReadFile(path)
	}
	if err != nil {
		return "", fmt.Errorf("--body-file %s: %w", path, err)
	}
	return strings.Trim(string(data), " \t\n"), nil
}

// pipedBody returns a body piped in on stdin, or "" if stdin is a terminal or
// carries nothing.
//
// peek changes how long we are willing to wait. With no other body given, the
// pipe is the intended source and we block for it — a slow producer must not
// lose its body. With a body already given we are only looking for a conflict
// to refuse, and blocking there hangs every caller that inherited an open pipe
// it never writes to: a script, a CI step, an agent's shell. That hang is worse
// than the conflict it was looking for.
func pipedBody(peek bool) string {
	info, err := os.Stdin.Stat()
	if err != nil || info.Mode()&os.ModeCharDevice != 0 {
		return "" // a terminal, not a pipe
	}
	if !peek {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return ""
		}
		return strings.Trim(string(data), " \t\n")
	}
	ch := make(chan string, 1)
	go func() {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			ch <- ""
			return
		}
		ch <- strings.Trim(string(data), " \t\n")
	}()
	select {
	case v := <-ch:
		return v
	case <-time.After(250 * time.Millisecond):
		return "" // nothing waiting; treat the inherited pipe as empty
	}
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
	// Name the store. Every worktree of a repo shares one, and it lives under
	// hive's data directory rather than in the repo, so an add leaves no mark
	// on the working tree at all.
	fmt.Printf("added %s: %s\n  %s\n",
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
		ts[i].Subject = flattenLine(subj)
		ts[i].Description = strings.Trim(desc, " \t\n")
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
	rc := mutateOneVerb(cwd, "claim", ref, func(ts []Todo, i int) ([]Todo, string) {
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
	rc := mutateOneVerb(todoCwd(), "state", ref, func(ts []Todo, i int) ([]Todo, string) {
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
		return out, fmt.Sprintf("%s (%s) → %s", out[i].Subject, out[i].ID, label)
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

	todos := loadTodos(cwd)
	t, ok := resolveTodoForShow(todos, worktreeClaim(cwd), ref)
	if !ok {
		if ref != "" {
			fmt.Fprintf(os.Stderr, "%s\n", todoRefError(todos, ref))
			return 1
		}
		fmt.Println("(no task claimed in this worktree — run: hive todo claim <ref>)")
		return 0
	}
	fmt.Printf("id: %s\nsection: %s\nsubject: %s\ndescription: %s\n",
		t.ID, t.sectionOrDefault(), t.Subject, indentBody(t.Description))
	return 0
}

// indentBody lines a multi-line description up under the "description: " label
// so `hive todo show` reads as one field rather than running into the margin.
func indentBody(s string) string {
	if !strings.Contains(s, "\n") {
		return s
	}
	lines := strings.Split(s, "\n")
	for i, l := range lines[1:] {
		if l != "" {
			lines[i+1] = "             " + l
		}
	}
	return strings.Join(lines, "\n")
}

// mutateOne resolves ref and applies a change under the backlog lock. Resolution
// happens inside the closure, against the list as it is on disk: a position read
// from an earlier `list` may point at a different task by now. apply returns the
// message to print, or "" when it declined and reported the reason itself.
func mutateOne(cwd, ref string, apply func([]Todo, int) ([]Todo, string)) int {
	return mutateOneVerb(cwd, "", ref, apply)
}

// mutateOneVerb is mutateOne with the verb named, so ticket attribution can
// tell working from looking.
func mutateOneVerb(cwd, verb, ref string, apply func([]Todo, int) ([]Todo, string)) int {
	var msg, refErr string
	var missing bool
	_, err := withTodos(cwd, func(ts []Todo) []Todo {
		i, ok := resolveTodoRef(ts, ref)
		if !ok {
			missing, refErr = true, todoRefError(ts, ref)
			return ts
		}
		// Committing to a ticket is the moment hive learns which ticket the
		// caller is working, and where. Pipeline agents claim and move state as
		// a matter of course, so their sub-agents' spend attributes without any
		// pipeline change — they inherit the directory.
		if verbCommitsToTicket(verb) {
			recordTicketCwd(ts[i].ID, cwd, time.Now())
		}
		out, m := apply(ts, i)
		msg = m
		return out
	})
	switch {
	case missing:
		fmt.Fprintf(os.Stderr, "error: %s\n", refErr)
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
		fmt.Fprintln(os.Stderr, "error: need a task id, subject or number (see: hive todo list)")
		return "", false
	}
	return args[0], true
}

// runTodoStatusline prints the current task + progress for use as a Claude Code
// statusLine command, and collects session telemetry while it is here.
//
// Claude pipes a JSON payload on stdin carrying cost, context-window occupancy
// and rate-limit headroom for this session. hive is already wired in as the
// statusLine command, so this is the one place that data arrives for free —
// see docs/superpowers/specs/2026-08-27-session-telemetry-design.md.
//
// Telemetry is strictly secondary: any failure in it is swallowed and the line
// renders exactly as it did before.
func runTodoStatusline() int {
	payload, havePayload := readStatuslinePayload()
	cwd := payloadCwd(payload, havePayload)

	tel := ""
	if havePayload {
		tel = statuslineTelemetry(payload)
	}

	// The two halves are independent: an empty backlog must not hide the
	// verdict, which is when there is least else on the line to look at.
	out := joinStatusline(todoStatuslinePart(cwd), tel)
	if out == "" {
		return 0
	}
	fmt.Print(out)
	return 0
}

// todoStatuslinePart is the backlog's share of the line: progress only. The
// claimed task's subject used to lead here and no longer does — it is a long
// string competing for width with the numbers, and the drawer already shows it.
func todoStatuslinePart(cwd string) string {
	todos := loadTodos(cwd)
	done, active, deferred := todoProgress(todos)
	if active+deferred == 0 {
		return ""
	}
	out := fmt.Sprintf("%d/%d", done, active)
	if deferred > 0 {
		out += fmt.Sprintf(" · %d parked", deferred)
	}
	return out
}

func joinStatusline(parts ...string) string {
	var kept []string
	for _, p := range parts {
		if p != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, " · ")
}

// statuslineTelemetry collects and renders this session's verdict. It returns
// "" for every failure path, so the caller can append unconditionally.
func statuslineTelemetry(p statuslinePayload) string {
	cfg := loadTelemetryConfig()
	if !cfg.Enabled {
		return ""
	}
	snap, ok := collectTelemetry(p, cfg, time.Now())
	if !ok {
		return ""
	}
	return renderTelemetrySuffix(snap, statuslineColour())
}

// statuslineColour honours NO_COLOR, the de-facto standard, because a terminal
// that will not render escapes would otherwise show them as literal noise.
func statuslineColour() bool {
	return os.Getenv("NO_COLOR") == ""
}

// readStatuslinePayload consumes the JSON Claude Code sends on stdin. It is
// read exactly once: stdin is a pipe, so a second read gets nothing.
func readStatuslinePayload() (statuslinePayload, bool) {
	data, err := io.ReadAll(os.Stdin)
	if err != nil || len(data) == 0 {
		return statuslinePayload{}, false
	}
	return decodeStatuslinePayload(data)
}

// payloadCwd picks the session's working directory out of the payload. Prefer
// `cwd` — for a split running in a worktree it's that worktree, whereas
// `workspace.current_dir` can resolve to the project root (main), which would
// make every split claim as the same branch.
func payloadCwd(p statuslinePayload, ok bool) string {
	if ok {
		if p.Cwd != "" {
			return p.Cwd
		}
		if p.Workspace.CurrentDir != "" {
			return p.Workspace.CurrentDir
		}
	}
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return "."
}

// todoRefError explains why ref did not resolve. A fragment naming several
// tasks is a different failure from one naming none, and saying "no such task"
// for it sends the caller looking for a task that is right there.
func todoRefError(todos []Todo, ref string) string {
	m := subjectMatches(todos, ref)
	if len(m) < 2 {
		return fmt.Sprintf("no such task %q (see: hive todo list)", ref)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%q matches %d tasks — name one, or use its id:", ref, len(m))
	for _, i := range m {
		fmt.Fprintf(&b, "\n  %-4s %s", todos[i].ID, truncStr(todos[i].Subject, 60))
	}
	return b.String()
}
