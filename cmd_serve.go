package main

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

//go:embed web
var webFS embed.FS

const serveUsage = `usage: hive serve [--port N] [--bind ADDR]

Serves the backlog over HTTP so it can be read and reviewed from a phone.
Binds 0.0.0.0 by default so a tailnet address works; the URL it prints carries
a token you need once per browser.

Tailscale is the security boundary. Do not expose this to the internet.`

// serveTask is a task as the browser sees it: the store's fields plus the repo
// it came from, which the store file does not carry.
type serveTask struct {
	ID       string `json:"id"`
	Subject  string `json:"subject"`
	Desc     string `json:"desc"`
	Section  string `json:"section"`
	State    string `json:"state"`
	Claim    string `json:"claim"`
	Since    string `json:"since"`
	Done     bool   `json:"done"`
	Deferred bool   `json:"deferred"`
	HasPlan  bool   `json:"hasPlan"`
	HasBuild bool   `json:"hasBuild"`
}

type serveRepo struct {
	Repo  string      `json:"repo"`
	Tasks []serveTask `json:"tasks"`
}

type reviewComment struct {
	Line int    `json:"line"`
	Text string `json:"text"`
}

// reviewPost is a verdict on one artifact. Kind says which: a plan at
// plan-review, or a build's diff at triage. They re-hash different things and
// move the ticket differently, so the browser must say which it read.
type reviewPost struct {
	Verdict  string          `json:"verdict"` // "approve" | "changes"
	Kind     string          `json:"kind"`    // "plan" | "build"
	Hash     string          `json:"hash"`    // of the artifact as read
	Comments []reviewComment `json:"comments"`
}

func runServeCmd(args []string) int {
	port, bind := "8787", "0.0.0.0"
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--port":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "error: --port needs a value")
				return 1
			}
			port = args[i+1]
			i++
		case "--bind":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "error: --bind needs a value")
				return 1
			}
			bind = args[i+1]
			i++
		default:
			fmt.Fprintf(os.Stderr, "error: unexpected argument %q\n\n%s\n", args[i], serveUsage)
			return 1
		}
	}

	token, err := webToken()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	srv := &http.Server{
		Addr:              bind + ":" + port,
		Handler:           newServeMux(token),
		ReadHeaderTimeout: 10 * time.Second,
	}
	fmt.Printf("hive web on http://%s:%s/?t=%s\n", hostGuess(bind), port, token)
	fmt.Println("  token is stored at " + webTokenPath())
	fmt.Println("  Tailscale is the security boundary — do not expose this to the internet.")
	if err := srv.ListenAndServe(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	return 0
}

func hostGuess(bind string) string {
	if bind == "0.0.0.0" || bind == "" {
		if h, err := os.Hostname(); err == nil && h != "" {
			return h
		}
		return "localhost"
	}
	return bind
}

func webTokenPath() string { return filepath.Join(hiveDataDir(), "web-token") }

// webToken reads the shared token, minting one on first run. It is a latch, not
// a defence: anyone who can read this file, or who is on the tailnet, is in.
func webToken() (string, error) {
	path := webTokenPath()
	if b, err := os.ReadFile(path); err == nil {
		if t := strings.TrimSpace(string(b)); t != "" {
			return t, nil
		}
	}
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	t := hex.EncodeToString(raw)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(t+"\n"), 0o600); err != nil {
		return "", err
	}
	return t, nil
}

func newServeMux(token string) http.Handler {
	mux := http.NewServeMux()
	sub, _ := fs.Sub(webFS, "web")
	mux.Handle("GET /", http.FileServer(http.FS(sub)))
	mux.HandleFunc("GET /api/backlog", apiBacklog)
	mux.HandleFunc("GET /api/plan/{repo}/{id}", apiPlan)
	mux.HandleFunc("GET /api/build/{repo}/{id}", apiBuild)
	mux.HandleFunc("POST /api/review/{repo}/{id}", apiReview)
	mux.HandleFunc("POST /api/task/{repo}/{id}", apiTask)
	return withAuth(token, mux)
}

// withAuth gates everything on the shared token. It arrives once as ?t= and is
// kept in a cookie, so a phone pays the cost only on its first visit.
func withAuth(token string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if t := r.URL.Query().Get("t"); t != "" && eq(t, token) {
			http.SetCookie(w, &http.Cookie{Name: "hive", Value: token, Path: "/",
				HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: 60 * 60 * 24 * 365})
			if r.URL.Path == "/" {
				http.Redirect(w, r, "/", http.StatusFound)
				return
			}
		} else if c, err := r.Cookie("hive"); err != nil || !eq(c.Value, token) {
			http.Error(w, "unauthorised — open the URL hive printed, token and all", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func eq(a, b string) bool { return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1 }

// repoByName resolves a repo the browser named against the repos hive knows
// about. Anything unknown is a 404 and never reaches the filesystem — the name
// comes off the wire and must not be joined into a path.
func repoByName(name string) (Repo, bool) {
	home, _ := os.UserHomeDir()
	cfg, err := LoadConfig(filepath.Join(home, ".config", "hive", "config.yaml"))
	if err != nil {
		return Repo{}, false
	}
	for _, r := range DiscoverRepos(cfg) {
		if r.DirName == name || r.Name == name {
			return r, true
		}
	}
	return Repo{}, false
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func apiBacklog(w http.ResponseWriter, r *http.Request) {
	home, _ := os.UserHomeDir()
	cfg, err := LoadConfig(filepath.Join(home, ".config", "hive", "config.yaml"))
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	out := []serveRepo{}
	for _, repo := range DiscoverRepos(cfg) {
		todos := loadTodos(repo.Path)
		if len(todos) == 0 {
			continue
		}
		sr := serveRepo{Repo: repo.DirName, Tasks: make([]serveTask, 0, len(todos))}
		for _, t := range todos {
			sr.Tasks = append(sr.Tasks, serveTask{
				ID: t.ID, Subject: t.Subject, Desc: t.Description,
				Section: t.sectionOrDefault(), State: t.State, Claim: t.Claim,
				Since: t.Since, Done: t.Done, Deferred: t.Deferred,
				HasPlan:  planPath(repo.Path, t.ID) != "",
				HasBuild: t.State == StateTriage && hasBuild(repo.Path, t.ID),
			})
		}
		out = append(out, sr)
	}
	writeJSON(w, 200, out)
}

// hasBuild says whether a triage ticket has an unmerged branch to look at. Kept
// cheap: this runs for every task on every backlog load.
func hasBuild(repoPath, id string) bool {
	b, _, _, _, _ := buildFor(repoPath, id)
	return b != ""
}

// planPath is the plan document for a ticket, or "" when it has none.
func planPath(repoPath, id string) string {
	if id == "" {
		return ""
	}
	p := filepath.Join(mainWorktree(repoPath), "docs", "plans", id+".md")
	if fileExists(p) {
		return p
	}
	return ""
}

func apiPlan(w http.ResponseWriter, r *http.Request) {
	repo, ok := repoByName(r.PathValue("repo"))
	if !ok {
		http.Error(w, "no such repo", 404)
		return
	}
	p := planPath(repo.Path, r.PathValue("id"))
	if p == "" {
		http.Error(w, "no plan for that ticket", 404)
		return
	}
	b, err := os.ReadFile(p)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, 200, map[string]any{
		"text": string(b), "hash": hashOf(b), "lines": strings.Count(string(b), "\n") + 1,
		"path": strings.TrimPrefix(p, mainWorktree(repo.Path)+"/"),
	})
}

func hashOf(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])[:12]
}

func apiReview(w http.ResponseWriter, r *http.Request) {
	repo, ok := repoByName(r.PathValue("repo"))
	if !ok {
		http.Error(w, "no such repo", 404)
		return
	}
	id := r.PathValue("id")
	var post reviewPost
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&post); err != nil {
		http.Error(w, "bad request body", 400)
		return
	}
	if err := checkVerdict(post); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	// Re-read whatever was reviewed and re-hash it. A plan rewritten, or a
	// branch pushed to, under the reviewer makes their line numbers meaningless.
	var text, where string
	if post.Kind == "plan" {
		p := planPath(repo.Path, id)
		if p == "" {
			http.Error(w, "no plan for that ticket", 404)
			return
		}
		b, err := os.ReadFile(p)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		text = string(b)
		where = strings.TrimPrefix(p, mainWorktree(repo.Path)+"/")
	} else {
		branch, commit, _, diff, _ := buildFor(repo.Path, id)
		if branch == "" {
			http.Error(w, "no unmerged branch carries a commit for this ticket", 404)
			return
		}
		text = diff
		where = branch + " @ " + commit[:12]
	}
	if now := hashOf([]byte(text)); now != post.Hash {
		writeJSON(w, http.StatusConflict, map[string]string{
			"error": "the " + post.Kind + " changed while you were reviewing it",
			"was":   post.Hash, "now": now})
		return
	}

	var subject string
	if _, err := withTodos(repo.Path, func(ts []Todo) []Todo {
		i, ok := indexByID(ts, id)
		if !ok {
			return ts
		}
		subject = ts[i].Subject
		switch {
		case post.Kind == "plan" && post.Verdict == "approve":
			ts[i].State = StateReady // planned and approved; a builder may take it
		case post.Kind == "plan":
			ts[i].State = StateUnrefined // back to the planner
		case post.Verdict == "approve":
			ts = toggleTodoDone(ts, i) // the build is accepted
			ts[i].State = StateUnrefined
		default:
			ts[i].State = StateReady // the plan stands; the build does not
		}
		ts[i].Claim, ts[i].Since = "", ""
		return ts
	}); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	doc := reviewDoc(subject, id, post, text, where)
	out := filepath.Join(mainWorktree(repo.Path), "docs", "plans", id+".review.md")
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if err := os.WriteFile(out, []byte(doc), 0o644); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	announceReview(repo.DirName, id, subject, post)
	writeJSON(w, 200, map[string]string{
		"wrote":   strings.TrimPrefix(out, mainWorktree(repo.Path)+"/"),
		"verdict": post.Verdict,
	})
}

// checkVerdict enforces the review rules server-side. The browser enforces them
// too, but a rule that only exists in the UI is not a rule.
func checkVerdict(p reviewPost) error {
	switch p.Verdict {
	case "approve":
		if len(p.Comments) > 0 {
			return fmt.Errorf("a plan you hold %d comment(s) against cannot be approved — send it back, or clear them", len(p.Comments))
		}
	case "changes":
		if len(p.Comments) == 0 {
			return fmt.Errorf("requesting changes needs at least one comment — say what is wrong")
		}
	default:
		return fmt.Errorf("verdict must be \"approve\" or \"changes\"")
	}
	if p.Hash == "" {
		return fmt.Errorf("a review must carry the hash of the artifact it read")
	}
	if p.Kind != "plan" && p.Kind != "build" {
		return fmt.Errorf("kind must be \"plan\" or \"build\"")
	}
	return nil
}

// reviewDoc renders the review an agent will read. Each comment quotes its
// source line verbatim, so it stays legible after a rewrite has moved the line
// numbers out from under it.
func reviewDoc(subject, id string, p reviewPost, artifact, planRel string) string {
	lines := strings.Split(artifact, "\n")
	verdict := "changes requested"
	if p.Verdict == "approve" {
		verdict = "approved"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# Review — %s\n\n", subject)
	kind := "plan"
	if p.Kind == "build" {
		kind = "build"
	}
	fmt.Fprintf(&b, "ticket: %s\nreviewed: %s\n%s: %s\nhash: %s\nreviewer: Steve\nverdict: %s\ncomments: %d\nat: %s\n\n",
		id, kind, kind, planRel, p.Hash, verdict, len(p.Comments), nowFunc().UTC().Format(time.RFC3339))
	if len(p.Comments) == 0 {
		b.WriteString("_No comments._\n")
		return b.String()
	}
	sorted := append([]reviewComment(nil), p.Comments...)
	for i := 1; i < len(sorted); i++ {
		for j := i; j > 0 && sorted[j].Line < sorted[j-1].Line; j-- {
			sorted[j], sorted[j-1] = sorted[j-1], sorted[j]
		}
	}
	for _, c := range sorted {
		src := "(line out of range)"
		if c.Line >= 1 && c.Line <= len(lines) {
			if s := strings.TrimSpace(lines[c.Line-1]); s != "" {
				src = s
			} else {
				src = "(blank line)"
			}
		}
		fmt.Fprintf(&b, "## Line %d\n\n> %s\n\n%s\n\n", c.Line, src, strings.TrimSpace(c.Text))
	}
	return b.String()
}

func announceReview(repo, id, subject string, p reviewPost) {
	verdict := "changes requested"
	if p.Verdict == "approve" {
		verdict = "approved"
	}
	head := fmt.Sprintf("%s review posted on %s (%s) — %s, %d comment(s), against hash %s",
		p.Kind, id, repo, verdict, len(p.Comments), p.Hash)
	body := fmt.Sprintf("Reviewed from hive web. Subject: %s\nThe review is at docs/plans/%s.review.md, "+
		"and the ticket has moved to %s.\nEach comment quotes the plan line it is against.",
		subject, id, map[string]string{"approve": "ready", "changes": "unrefined"}[p.Verdict])
	_ = runBusCmd([]string{"announce", head, "--body", body})
}

func apiTask(w http.ResponseWriter, r *http.Request) {
	repo, ok := repoByName(r.PathValue("repo"))
	if !ok {
		http.Error(w, "no such repo", 404)
		return
	}
	id := r.PathValue("id")
	var body struct {
		Op      string `json:"op"`
		State   string `json:"state"`
		Subject string `json:"subject"`
		Desc    string `json:"desc"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		http.Error(w, "bad request body", 400)
		return
	}
	var found bool
	if _, err := withTodos(repo.Path, func(ts []Todo) []Todo {
		i, ok := indexByID(ts, id)
		if !ok {
			return ts
		}
		found = true
		switch body.Op {
		case "done":
			ts = toggleTodoDone(ts, i)
		case "defer":
			ts = deferTodo(ts, i)
		case "state":
			if validTodoState(body.State) {
				ts[i].State = body.State
			}
		case "release":
			ts[i].Claim, ts[i].Since = "", ""
		case "edit":
			if s := flattenLine(body.Subject); s != "" {
				ts[i].Subject = s
				ts[i].Description = strings.Trim(body.Desc, " \t\n")
			}
		}
		return ts
	}); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if !found {
		http.Error(w, "no such task", 404)
		return
	}
	writeJSON(w, 200, map[string]string{"ok": body.Op})
}

// buildFor locates the work a triage ticket is waiting on: an unmerged branch
// carrying a commit for it. There is no recorded link from ticket to branch, so
// this looks for the plan document the builder commits alongside its work —
// docs/plans/<id>.md — and falls back to naming every unmerged branch rather
// than guessing. A wrong branch shown confidently is worse than none.
func buildFor(repoPath, id string) (branch, commit, stat, diff string, others []string) {
	main := mainWorktree(repoPath)
	git := func(args ...string) string {
		out, err := runGit(main, args...)
		if err != nil {
			return ""
		}
		return strings.TrimSpace(out)
	}
	raw := git("branch", "--format=%(refname:short)", "--no-merged", "main")
	if raw == "" {
		return "", "", "", "", nil
	}
	for _, b := range strings.Split(raw, "\n") {
		b = strings.TrimSpace(b)
		if b == "" {
			continue
		}
		others = append(others, b)
		if branch != "" {
			continue
		}
		// The commit that introduced this ticket's plan is the build's commit.
		if c := git("log", "-1", "--format=%H", b, "--", "docs/plans/"+id+".md"); c != "" {
			branch, commit = b, c
		}
	}
	if branch == "" {
		return "", "", "", "", others
	}
	stat = git("show", "--stat", "--oneline", commit)
	diff = git("show", "--format=%H%n%s%n", commit)
	return branch, commit, stat, diff, others
}

func runGit(dir string, args ...string) (string, error) {
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
	return string(out), err
}

func apiBuild(w http.ResponseWriter, r *http.Request) {
	repo, ok := repoByName(r.PathValue("repo"))
	if !ok {
		http.Error(w, "no such repo", 404)
		return
	}
	id := r.PathValue("id")
	branch, commit, stat, diff, others := buildFor(repo.Path, id)
	if branch == "" {
		writeJSON(w, 404, map[string]any{
			"error":    "no unmerged branch carries a commit for this ticket",
			"unmerged": others})
		return
	}
	writeJSON(w, 200, map[string]any{
		"branch": branch, "commit": commit[:12], "stat": stat,
		"text": diff, "hash": hashOf([]byte(diff)),
		"lines": strings.Count(diff, "\n") + 1,
		"path":  branch + " @ " + commit[:12],
	})
}
