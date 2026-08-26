package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// pipeStdin replaces os.Stdin with a pipe carrying s, the way a shell pipe or a
// heredoc would. Returns a restore func.
func pipeStdin(t *testing.T, s string) func() {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		_, _ = w.WriteString(s)
		w.Close()
	}()
	orig := os.Stdin
	os.Stdin = r
	return func() { os.Stdin = orig }
}

// The whole point of the ticket: a body full of apostrophes, backticks, quotes
// and non-ASCII must survive without any shell quoting at all.
func TestBodyFileTakesAwkwardTextVerbatim(t *testing.T) {
	dir := newTestRepo(t)
	chdir(t, dir)

	body := "It's a body with `backticks`, \"quotes\", £250 and an em-dash — all of it.\n\nA second paragraph."
	path := filepath.Join(dir, "body.md")
	if err := os.WriteFile(path, []byte(body+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if rc := runTodoAdd([]string{"awkward subject", "--body-file", path}); rc != 0 {
		t.Fatalf("add returned %d", rc)
	}
	got := loadTodos(dir)
	if len(got) != 1 {
		t.Fatalf("got %d tasks, want 1", len(got))
	}
	if got[0].Subject != "awkward subject" {
		t.Errorf("subject = %q", got[0].Subject)
	}
	if got[0].Description != body {
		t.Errorf("description round-tripped wrong:\n got %q\nwant %q", got[0].Description, body)
	}
}

func TestBodyFileDashReadsStdin(t *testing.T) {
	dir := newTestRepo(t)
	chdir(t, dir)
	defer pipeStdin(t, "from stdin, with an apostrophe's worth of trouble\n")()

	if rc := runTodoAdd([]string{"subj", "--body-file", "-"}); rc != 0 {
		t.Fatalf("add returned %d", rc)
	}
	if got := loadTodos(dir); len(got) != 1 ||
		got[0].Description != "from stdin, with an apostrophe's worth of trouble" {
		t.Errorf("got %+v", got)
	}
}

// Steve's choice: a bare pipe with no body flag is picked up automatically.
func TestPipedStdinIsPickedUpWithoutAFlag(t *testing.T) {
	dir := newTestRepo(t)
	chdir(t, dir)
	defer pipeStdin(t, "auto-detected body\n")()

	if rc := runTodoAdd([]string{"subj"}); rc != 0 {
		t.Fatalf("add returned %d", rc)
	}
	if got := loadTodos(dir); len(got) != 1 || got[0].Description != "auto-detected body" {
		t.Errorf("got %+v", got)
	}
}

// The guard on auto-detect: `hive todo add "subj" < /dev/null` is a pipe with
// nothing in it, and must not produce a task with an empty-but-present body or
// swallow anything.
func TestEmptyPipedStdinLeavesNoBody(t *testing.T) {
	dir := newTestRepo(t)
	chdir(t, dir)
	defer pipeStdin(t, "")()

	if rc := runTodoAdd([]string{"subj"}); rc != 0 {
		t.Fatalf("add returned %d", rc)
	}
	if got := loadTodos(dir); len(got) != 1 || got[0].Description != "" {
		t.Errorf("got %+v, want an empty description", got)
	}
}

// A body given twice is refused, including when one of them arrived on stdin.
// Preferring one and dropping the other is how a 3000-character ticket body was
// silently destroyed: the subject contained " - ", so the piped body lost, and
// the command still exited 0.
func TestPipedBodyPlusAnotherBodyIsRefused(t *testing.T) {
	for _, c := range []struct {
		name string
		args []string
	}{
		{"flag", []string{"subj", "-d", "explicit"}},
		{"separator", []string{"subj - explicit"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := newTestRepo(t)
			chdir(t, dir)
			defer pipeStdin(t, "from the pipe\n")()

			if rc := runTodoAdd(c.args); rc == 0 {
				t.Error("two bodies were accepted; one of them was silently dropped")
			}
			if got := loadTodos(dir); len(got) != 0 {
				t.Errorf("a refused add still wrote a task: %+v", got)
			}
		})
	}
}

// Giving the body twice is the existing error, and --body-file joins it.
func TestBodyGivenTwiceIsRefused(t *testing.T) {
	dir := newTestRepo(t)
	path := filepath.Join(dir, "b.md")
	if err := os.WriteFile(path, []byte("from file"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"subj", "-d", "inline", "--body-file", path},
		{"subj - inline", "--body-file", path},
	} {
		if _, _, err := parseTodoAddArgs(args); err == nil {
			t.Errorf("parseTodoAddArgs(%v) should have refused two bodies", args)
		}
	}
}

func TestBodyFileMissingIsAnError(t *testing.T) {
	_, _, err := parseTodoAddArgs([]string{"subj", "--body-file", "/nope/does/not/exist"})
	if err == nil {
		t.Fatal("a missing --body-file should be an error, not an empty body")
	}
	if !strings.Contains(err.Error(), "body-file") {
		t.Errorf("error should name the flag: %v", err)
	}
}

func TestBodyFileNeedsAValue(t *testing.T) {
	if _, _, err := parseTodoAddArgs([]string{"subj", "--body-file"}); err == nil {
		t.Error("--body-file with no value should be refused")
	}
}

// edit shares the parser, so it gets the same affordance — which is what makes
// rewriting a long body possible at all.
func TestEditTakesABodyFile(t *testing.T) {
	dir := newTestRepo(t)
	chdir(t, dir)
	if rc := runTodoAdd([]string{"subj - short"}); rc != 0 {
		t.Fatalf("add returned %d", rc)
	}
	id := loadTodos(dir)[0].ID

	body := "It's rewritten — with `backticks` and a £ sign.\n\nSecond para."
	path := filepath.Join(dir, "b.md")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if rc := runTodoEdit([]string{id, "subj", "--body-file", path}); rc != 0 {
		t.Fatalf("edit returned %d", rc)
	}
	got := loadTodos(dir)
	if got[0].ID != id {
		t.Errorf("id changed: %q -> %q", id, got[0].ID)
	}
	if got[0].Description != body {
		t.Errorf("description = %q, want %q", got[0].Description, body)
	}
}
