package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Steve, 2026-08-26: "im not gunna nit - either i want stuff changed - or not."
// The verdict is binary, and both halves of that are enforced here rather than
// only in the browser.
func TestReviewVerdictRules(t *testing.T) {
	one := []reviewComment{{Line: 3, Text: "wrong"}}
	cases := []struct {
		name string
		post reviewPost
		ok   bool
	}{
		{"approve with no comments", reviewPost{Verdict: "approve", Kind: "plan", Hash: "abc"}, true},
		{"approve holding a comment", reviewPost{Verdict: "approve", Kind: "plan", Hash: "abc", Comments: one}, false},
		{"changes with a comment", reviewPost{Verdict: "changes", Kind: "plan", Hash: "abc", Comments: one}, true},
		{"changes with nothing to say", reviewPost{Verdict: "changes", Kind: "plan", Hash: "abc"}, false},
		{"a third path", reviewPost{Verdict: "nits", Kind: "plan", Hash: "abc", Comments: one}, false},
		{"no hash", reviewPost{Verdict: "approve", Kind: "plan"}, false},
		{"no kind", reviewPost{Verdict: "approve", Hash: "abc"}, false},
		// Triage is not plan review: accepting a build while holding notes on
		// it is the ordinary outcome, so the comments ride along.
		{"accept a build holding a comment", reviewPost{Verdict: "approve", Kind: "build", Hash: "abc", Comments: one}, true},
		{"accept a build with nothing to say", reviewPost{Verdict: "approve", Kind: "build", Hash: "abc"}, true},
		{"send a build back with nothing to say", reviewPost{Verdict: "changes", Kind: "build", Hash: "abc"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := checkVerdict(c.post)
			if c.ok && err != nil {
				t.Errorf("rejected a valid review: %v", err)
			}
			if !c.ok && err == nil {
				t.Error("accepted a review that breaks the rules")
			}
		})
	}
}

// A comment must stay legible after the plan is rewritten and its line numbers
// stop meaning anything, so the source line is quoted into the review.
func TestReviewDocQuotesTheLineItIsAgainst(t *testing.T) {
	plan := "# Plan\n\nfirst para\n\nthe wrong bit\n"
	doc := reviewDoc("A subject", "abc", reviewPost{
		Verdict:  "changes",
		Kind:     "plan",
		Hash:     "deadbeef",
		Comments: []reviewComment{{Line: 5, Text: "this is wrong"}},
	}, plan, "docs/plans/abc.md")

	for _, want := range []string{
		"# Review — A subject", "ticket: abc", "hash: deadbeef", "reviewed: plan",
		"verdict: changes requested", "## Line 5", "> the wrong bit", "this is wrong",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("review doc missing %q:\n%s", want, doc)
		}
	}
}

func TestReviewDocOrdersCommentsByLine(t *testing.T) {
	plan := strings.Repeat("x\n", 20)
	doc := reviewDoc("S", "abc", reviewPost{Verdict: "changes", Kind: "plan", Hash: "h", Comments: []reviewComment{
		{Line: 12, Text: "c"}, {Line: 2, Text: "a"}, {Line: 7, Text: "b"},
	}}, plan, "p")
	i2, i7, i12 := strings.Index(doc, "Line 2"), strings.Index(doc, "Line 7"), strings.Index(doc, "Line 12")
	if !(i2 < i7 && i7 < i12) {
		t.Errorf("comments out of order: 2@%d 7@%d 12@%d", i2, i7, i12)
	}
}

// A line number past the end of the plan must not panic or silently vanish.
func TestReviewDocSurvivesAnOutOfRangeLine(t *testing.T) {
	doc := reviewDoc("S", "abc", reviewPost{Verdict: "changes", Kind: "plan", Hash: "h",
		Comments: []reviewComment{{Line: 9999, Text: "stale"}}}, "one\ntwo\n", "p")
	if !strings.Contains(doc, "Line 9999") || !strings.Contains(doc, "stale") {
		t.Errorf("out-of-range comment was lost:\n%s", doc)
	}
}

func TestAuthRejectsEverythingWithoutTheToken(t *testing.T) {
	h := newServeMux("sekret")
	for _, path := range []string{"/", "/api/backlog", "/api/plan/x/abc"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s returned %d without a token, want 401", path, w.Code)
		}
	}
}

func TestAuthAcceptsTheTokenAndSetsACookie(t *testing.T) {
	h := newServeMux("sekret")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/backlog?t=sekret", nil))
	if w.Code == http.StatusUnauthorized {
		t.Fatal("a correct token was rejected")
	}
	if !strings.Contains(w.Header().Get("Set-Cookie"), "hive=sekret") {
		t.Errorf("no cookie set, so a phone would re-authenticate every request: %q",
			w.Header().Get("Set-Cookie"))
	}
}

func TestAuthRejectsAWrongCookie(t *testing.T) {
	h := newServeMux("sekret")
	r := httptest.NewRequest("GET", "/api/backlog", nil)
	r.AddCookie(&http.Cookie{Name: "hive", Value: "guess"})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("a wrong cookie returned %d, want 401", w.Code)
	}
}

// The repo name arrives off the wire. It must be matched against the repos hive
// knows about, never joined into a path.
func TestUnknownRepoIsRefused(t *testing.T) {
	if _, ok := repoByName("../../etc"); ok {
		t.Error("a traversal-shaped repo name resolved")
	}
	if _, ok := repoByName("definitely-not-a-repo-xyzzy"); ok {
		t.Error("an unknown repo resolved")
	}
}

// A build review judges the diff, not the plan, and says so in the artifact an
// agent will read.
func TestReviewDocNamesTheBuildItJudged(t *testing.T) {
	doc := reviewDoc("Bootstrap a checkout", "dmy", reviewPost{
		Verdict: "changes", Kind: "build", Hash: "335f92aa4c1c",
		Comments: []reviewComment{{Line: 4, Text: "this hunk is wrong"}},
	}, "commit\nsubject\n\n+added line\n", "worktree-wt-init-bootstrap @ fa69ae26b2c3")
	for _, want := range []string{"reviewed: build", "build: worktree-wt-init-bootstrap @ fa69ae26b2c3",
		"hash: 335f92aa4c1c", "> +added line", "this hunk is wrong"} {
		if !strings.Contains(doc, want) {
			t.Errorf("build review missing %q:\n%s", want, doc)
		}
	}
}
