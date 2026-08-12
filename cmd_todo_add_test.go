package main

import (
	"strings"
	"testing"
)

func TestParseTodoAddPlainSubject(t *testing.T) {
	subj, desc, err := parseTodoAddArgs([]string{"fix", "the", "thing"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if subj != "fix the thing" || desc != "" {
		t.Errorf("got subj=%q desc=%q", subj, desc)
	}
}

func TestParseTodoAddSeparatorForms(t *testing.T) {
	for _, in := range []string{"subj — desc", "subj - desc"} {
		subj, desc, err := parseTodoAddArgs([]string{in})
		if err != nil {
			t.Fatalf("%q: unexpected error: %v", in, err)
		}
		if subj != "subj" || desc != "desc" {
			t.Errorf("%q: got subj=%q desc=%q", in, subj, desc)
		}
	}
}

func TestParseTodoAddDescriptionFlag(t *testing.T) {
	cases := [][]string{
		{"--description", "the long bit", "the subject"},
		{"-d", "the long bit", "the subject"},
		{"--description=the long bit", "the subject"},
	}
	for _, args := range cases {
		subj, desc, err := parseTodoAddArgs(args)
		if err != nil {
			t.Fatalf("%v: unexpected error: %v", args, err)
		}
		if subj != "the subject" || desc != "the long bit" {
			t.Errorf("%v: got subj=%q desc=%q", args, subj, desc)
		}
	}
}

// The reported bug: add joined every argument into the subject, so a flag it
// did not understand was silently embedded in the task text instead of being
// rejected. Failing loudly is the whole point.
func TestParseTodoAddRejectsUnknownFlag(t *testing.T) {
	_, _, err := parseTodoAddArgs([]string{"--body-file", "notes.txt", "the subject"})
	if err == nil {
		t.Fatal("an unrecognised flag must be an error, not silently part of the subject")
	}
	if !strings.Contains(err.Error(), "--body-file") {
		t.Errorf("error should name the offending flag, got %q", err)
	}
}

func TestParseTodoAddRejectsBothDescriptionForms(t *testing.T) {
	_, _, err := parseTodoAddArgs([]string{"--description", "one", "subj — two"})
	if err == nil {
		t.Fatal("giving a description twice is ambiguous and must be refused")
	}
}

// A subject may legitimately start with a dash; "--" ends flag parsing so it
// can be written without being mistaken for one.
func TestParseTodoAddDoubleDashEndsFlags(t *testing.T) {
	subj, desc, err := parseTodoAddArgs([]string{"--", "--weird-subject", "here"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if subj != "--weird-subject here" || desc != "" {
		t.Errorf("got subj=%q desc=%q", subj, desc)
	}
}

func TestParseTodoAddEmpty(t *testing.T) {
	if _, _, err := parseTodoAddArgs(nil); err == nil {
		t.Error("empty input should be an error")
	}
	if _, _, err := parseTodoAddArgs([]string{"--description", "only a body"}); err == nil {
		t.Error("a description with no subject should be an error")
	}
}

func TestParseTodoAddFlagValueMayLookLikeAFlag(t *testing.T) {
	subj, desc, err := parseTodoAddArgs([]string{"--description=--not-a-flag", "subj"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if subj != "subj" || desc != "--not-a-flag" {
		t.Errorf("got subj=%q desc=%q", subj, desc)
	}
}
