# Hive web submits a review six times from one click

**Ticket:** kdx
**Repo:** hive (github.com/stevenlawton/hive)
**Base commit:** 87acdeb0ae82594a1d3e69fa0c1dbd19e4f251f2
**Written:** 2026-08-27

---

## Current behaviour

**Client.** `web/app.js` is a single hand-written, unbundled file embedded into
the binary (`//go:embed web`, `cmd_serve.go:21-22`). There is no build step, no
package.json, and no JS test anywhere in the repo.

One delegated `click` listener on `document` (`web/app.js:306-333`) dispatches
every interaction. The verdict branch is `web/app.js:324-329`:

```js
const V=e.target.closest("[data-verdict]");
if(V){const n=cs(openPlan).length;
  if(V.dataset.verdict==="approve"&&n){alert(...);return}
  if(V.dataset.verdict==="changes"&&!n){alert(...);return}
  submitReview(V.dataset.verdict);return}
```

`submitReview` (`web/app.js:249-267`) is `async` and is called **without
`await`** — the handler returns immediately. Inside, it POSTs to
`/api/review/{repo}/{id}`, then `save()`, `await load()`, `closeReader()`,
`alert(...)`.

The two verdict buttons are static markup in `web/index.html:239-246`, outside
the `#rmain` subtree that `renderReader()` replaces, so they are the same DOM
nodes for the life of the page:

```html
<button class="primary" id="vApprove" data-verdict="approve">Approve</button>
<button data-verdict="changes">Request changes</button>
```

The only `disabled` handling in the whole file is `web/app.js:231-233`, inside
`renderReader()`: `ap.disabled=n>0` at **`web/app.js:232`**, where `n` is the
outstanding-comment count. **Nothing anywhere tracks a request in flight.** The
"Request changes" button has no `id` and no `disabled` logic at all.

**Server.** `apiReview` (`cmd_serve.go:277-366`) is registered at
`mux.HandleFunc("POST /api/review/{repo}/{id}", apiReview)` (`cmd_serve.go:154`).
There is no server struct: handlers are free functions that re-resolve
everything from disk per request (`repoByName`, `cmd_serve.go:183-195`, reads
`~/.config/hive/config.yaml` via `os.UserHomeDir()`). `net/http` serves one
goroutine per request, so any number of these run at once.

Per request, unconditionally and in this order:

1. `checkVerdict(post)` (`cmd_serve.go:370-390`).
2. Re-read the plan file (or re-diff the build branch) and re-hash it with
   `hashOf` (sha256 truncated to 12 hex, `cmd_serve.go:272-275`). If
   `now != post.Hash`, 409 (`cmd_serve.go:319-324`).
3. Mutate ticket state inside `withTodos` (`cmd_serve.go:326-346`), which takes
   an exclusive flock for the duration (`todo_store.go:17`, `:66-81`) and then
   releases it.
4. `os.WriteFile(docs/plans/<id>.review.md, ...)` (`cmd_serve.go:352-360`) —
   outside any lock.
5. `announceReview(...)` (`cmd_serve.go:361`, defined `cmd_serve.go:433-444`) —
   outside any lock.

**Bus.** `bus.Bus.Announce` (`bus/bus.go:45-60`) mints a random, time-seeded id
(`newID`, `bus/bus.go:107-112`) and appends to `~/.config/hive/bus.jsonl`. There
is no dedup, no rate limit, and no idempotency key anywhere in the bus layer:
`git grep dedup` and `git grep "already recorded"` return nothing; the only
`idempot` hits are in `bus/install.go` and are about config-file installation,
not messages.

## Root cause

**The six posts were in flight at the same time.** This is not inferred from the
code shape — the incident is still on disk. `~/.config/hive/bus.jsonl` lines
1052-1057 are the six byte-identical announcements, stamped
`18:36:26.175`, `.502`, `.691`, `.852`, `18:36:27.105`, `.231` — 1.06s end to
end, 130-330ms apart. Six *completed sequential* round trips is impossible,
because `web/app.js:265` ends every successful submit with a blocking `alert()`
that a human would have had to dismiss five times inside that second. Six clicks
queued before the first response, each dispatching its own `fetch`, then
serialised server-side by the flock inside `withTodos` and by `backupStore`'s
git work (`todo_store.go:40`), fits the spacing exactly. The retraction is
`msg_6a900cd2cfe945b9f2e7`.

Three gaps stack.

**1. Nothing on the client stops a second submit.** `submitReview` is
fire-and-forget from a document-level delegated handler and sets no in-flight
state. Neither button is disabled while the POST runs, and `renderReader()`
would clobber it anyway (`ap.disabled=n>0`, `web/app.js:232`). A double tap, a
touch-plus-click pair, or a re-render swapping the node under the pointer each
enter the verdict branch and each start a fresh POST.

Everything else that could produce six POSTs was checked and ruled out:
`web/index.html:249` is the only `<script>` and there are no modules and no
service worker, so the `document` listener registers once per load;
`submitReview` has exactly one caller (`web/app.js:329`); the `keydown` handler
synthesises `.click()` only on `.num` elements (`web/app.js:335-338`); `api()`
(`web/app.js:2-8`) has no retry; there is no `setInterval`, no `EventSource`,
and no `<form>` around the buttons; `announceReview` is called once per request.
The incident ran `d2a104a`'s `app.js`, whose `submitReview` and click handler
are structurally identical to today's, so this analysis transfers.

**2. Nothing on the server stops a second submit either, and the one guard that
exists cannot.** The 409 staleness check compares the *artifact* hash. Posting a
review does not modify the plan file or the build branch, so every one of the
six identical posts passed the hash check and proceeded to write, transition,
and announce. This is the load-bearing point: the existing guard is a staleness
guard, not a duplicate guard, and no amount of tightening it would have caught
this.

**3. A repeated build acceptance actively corrupts state.**
`cmd_serve.go:337` does `ts = toggleTodoDone(ts, i)` for an accepted build, and
`toggleTodoDone` (`todo.go:454-463`) *flips* `Done`. Six accepts of a build
therefore leave the ticket **not done** — an even number of toggles returns it
to where it started. The reported incident was a plan approval, which happens to
be state-idempotent, so this did not bite; it is one POST away from doing so.

**Also found, in the same function, unfixed:** `announceReview` calls
`runBusCmd([]string{"announce", head, "--body", body})` (`cmd_serve.go:443`).
`busAnnounceCmd` registers the body flag as `-b` only (`cmd_bus.go:100`), and
Go's `flag.Parse` stops at the first non-flag argument — which here is `head`.
So the flag set is never consulted: `fs.Args()` comes back as
`[head, "--body", body]` and the three are joined with spaces into the headline.
`bus.jsonl:1052` shows the result verbatim: `… against plan-hash d99124e1b432
--body Reviewed from hive web. …`. `foldHeadline` (`bus/headline.go:37-73`)
then cuts the headline and moves the tail into `Body`, so the body is not empty
— it is a truncated fragment starting mid-sentence. Every review announcement on
the bus today is malformed this way. It is one line, it is in the function this
ticket already changes, and the ticket's own acceptance test cannot assert on an
announcement's content until it is fixed. It is in scope.

## The contract

Four files, plus one new test file. Server first — that is the defence that
holds regardless of what the browser does, and after §1 it actually does hold.

### 1. `cmd_serve.go` — serialise the handler

Add `"sync"` to the import block (`cmd_serve.go:3-19`), and a package-level
mutex immediately above `apiReview`:

```go
// reviewMu serialises review posts. apiReview is a read-modify-write across
// three stores — the todo file, the review doc, and the bus — and the
// duplicate check below is only worth anything if nothing gets between its
// read and its write. Six concurrent posts from one tap is the reported bug,
// not a hypothetical. One process serves every repo, so one mutex is enough;
// reviews are human-paced and contention is not a concern.
var reviewMu sync.Mutex
```

Take it in `apiReview` immediately after the `checkVerdict` error return
(i.e. after `cmd_serve.go:294`, before the artifact re-read comment at
`cmd_serve.go:296`):

```go
	reviewMu.Lock()
	defer reviewMu.Unlock()
```

**Do not** use `lockTodos(repo.Path)` here instead. `withTodos` takes `LOCK_EX`
on that same path further down (`todo_store.go:17`, `:66-81`); `flock` is per
open file description, so a second `Flock` from this process on a second fd
blocks the goroutine against itself.

### 2. `cmd_serve.go` — duplicate detection by comparing the rendered doc

`reviewDoc` (`cmd_serve.go:392-431`) is already a deterministic function of
everything that identifies a review — ticket, kind, artifact path/branch,
artifact hash, verdict, comments, and the artifact lines those comments quote.
The single time-varying byte in it is the `at:` line
(`nowFunc()`, `cmd_serve.go:407-408`). So the duplicate test is: render the doc
you were about to write, and compare it with the one already on disk, ignoring
that line. No new metadata field, no new persisted state, no back-compat branch.

Add next to `reviewDoc`:

```go
// sameReview reports whether two rendered review docs assert the same thing.
// Everything in a review doc is derived from the post and the artifact except
// the timestamp, so that one line is dropped before comparing.
func sameReview(a, b string) bool {
	return dropAtLine(a) == dropAtLine(b)
}

// dropAtLine removes the first "at: " metadata line from a review doc. Only
// the first: a comment body may legitimately begin with those characters, and
// dropping those too would let two different reviews compare equal.
func dropAtLine(doc string) string {
	lines := strings.Split(doc, "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, "at: ") {
			return strings.Join(append(append([]string{}, lines[:i]...), lines[i+1:]...), "\n")
		}
	}
	return doc
}
```

`reviewDoc` itself is **not** changed.

### 3. `cmd_serve.go` — extract the transition and make it idempotent

Add, next to `apiReview`:

```go
// applyReviewVerdict moves the ticket to where the verdict leaves it. It is
// idempotent by construction: applying the same verdict twice has to land in
// the same place, which is why an accepted build is set done rather than
// toggled.
func applyReviewVerdict(ts []Todo, i int, p reviewPost) []Todo {
	switch {
	case p.Kind == "plan" && p.Verdict == "approve":
		ts[i].State = StateReady // planned and approved; a builder may take it
	case p.Kind == "plan":
		ts[i].State = StateUnrefined // back to the planner
	case p.Verdict == "approve":
		ts[i].Done = true // the build is accepted
		ts[i].State = StateUnrefined
	default:
		ts[i].State = StateReady // the plan stands; the build does not
	}
	ts[i].Claim, ts[i].Since = "", ""
	return ts
}
```

`toggleTodoDone` (`todo.go:454-463`) is **not** modified — its other callers
still want toggle semantics. Dropping it from this one call site loses nothing:
it only cleared `Claim`/`Since` on the `Done == true` branch, and
`applyReviewVerdict` clears them unconditionally exactly as
`cmd_serve.go:344` does today.

### 4. `cmd_serve.go` — rewrite the tail of `apiReview`

Replace everything from `var subject string` (`cmd_serve.go:326`) to the end of
the function (`cmd_serve.go:365`) with:

```go
	subject := ""
	todos := loadTodos(repo.Path)
	if i, ok := indexByID(todos, id); ok {
		subject = todos[i].Subject
	}

	doc := reviewDoc(subject, id, post, text, where)
	out := filepath.Join(mainWorktree(repo.Path), "docs", "plans", id+".review.md")
	rel := strings.TrimPrefix(out, mainWorktree(repo.Path)+"/")

	// The same review posted twice says nothing new. Record it once, announce
	// it once, and hand the caller back what is already there — a duplicate is
	// not an error, it is a tap that arrived twice.
	if prev, err := os.ReadFile(out); err == nil && sameReview(string(prev), doc) {
		writeJSON(w, 200, map[string]string{
			"wrote": rel, "verdict": post.Verdict, "duplicate": "true"})
		return
	}

	if _, err := withTodos(repo.Path, func(ts []Todo) []Todo {
		i, ok := indexByID(ts, id)
		if !ok {
			return ts
		}
		return applyReviewVerdict(ts, i, post)
	}); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if err := os.WriteFile(out, []byte(doc), 0o644); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	announceReview(repo.DirName, id, subject, post)
	writeJSON(w, 200, map[string]string{"wrote": rel, "verdict": post.Verdict})
```

Two behavioural notes, both deliberate:

- `subject` is now read before the transition (`loadTodos`, `todo.go:131-137`)
  rather than captured inside it. Same value — no transition touches `Subject`.
  A ticket that is not in the store still yields `subject == ""`, which is what
  happens today.
- **A duplicate returns before the transition runs.** The ticket asks for "a
  no-op returning the existing result", and this is that. It also avoids the
  worse failure: the transition closure clears `Claim`/`Since`
  unconditionally, so a stale tab replaying an old approve against an unchanged
  plan would otherwise silently un-claim whichever worktree has since picked
  the ticket up — the same class of failure (unverifiable state change reaching
  shared state) that this ticket exists because of. A duplicate now changes
  nothing at all.

### 5. `cmd_serve.go` — fix the announce body flag

Replace `cmd_serve.go:443`:

```go
	_ = runBusCmd([]string{"announce", "-b", body, head})
```

Flags must precede the positional headline; `flag.Parse` stops at the first
non-flag argument. Nothing else in `announceReview` changes.

### 6. `web/index.html` — give the second verdict button an id

Line 243, add `id="vChanges"`, leaving everything else as it is:

```html
      <button id="vChanges" data-verdict="changes">Request changes</button>
```

### 7. `web/app.js` — an in-flight flag and a busy state

Add `submitting` to the module-level state declaration at `web/app.js:37`:

```js
let pal="quiet",fRepo="all",fState="open",q="",openPlan=null,mode="ren",composing=null,submitting=false;
```

Add a helper next to `submitReview`:

```js
function setVerdictBusy(on){
  const ap=document.getElementById("vApprove"),ch=document.getElementById("vChanges");
  if(ap)ap.disabled=on||cs(openPlan).length>0;
  if(ch)ch.disabled=on;}
```

Rewrite `submitReview` (`web/app.js:249-267`) as a guard and a `finally` around
the **existing, unchanged** body:

```js
async function submitReview(verdict){
  if(submitting)return;
  submitting=true;setVerdictBusy(true);
  try{
    const t=find(openPlan),p=PLANS[openPlan];
    const body={verdict,kind:p.kind||"plan",hash:p.hash,
      comments:cs(openPlan).map(c=>({line:c.line,text:c.text}))};
    try{
      const res=await api(`/api/review/${encodeURIComponent(t.repo)}/${encodeURIComponent(openPlan)}`,
        {method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify(body)});
      reviews[openPlan]={verdict:verdict==="approve"?"Approved":"Changes requested",
        at:new Date().toISOString().slice(0,16).replace("T"," "),hash:p.hash,n:cs(openPlan).length,
        wrote:res.wrote};
      delete comments[openPlan];save();
      await load();closeReader();
      alert(res.duplicate
        ? "Already recorded — this review was posted before, so nothing was written again."
        : (verdict==="approve"?(p.kind==="build"?"Accepted":"Approved"):"Sent back")+
          " — review written to "+res.wrote);
    }catch(e){
      if(e.status===409){
        alert("The "+(p.kind||"plan")+" changed while you were reviewing it.\n\nYour comments point at lines that may have moved, so the review was not recorded. Reopen it to see the new version.");
        delete PLANS[openPlan];closeReader();await load();return}
      alert("Review not saved: "+e.message)}
  }finally{submitting=false;setVerdictBusy(false);}}
```

`closeReader()` sets `openPlan=null` (`web/app.js:302`), so the `finally`'s
`cs(openPlan)` returns `[]` (`cs=id=>comments[id]||[]`, `web/app.js:48`) and the
approve button is simply re-enabled. That is correct — the reader is shut. The
`if(submitting)return` guard runs synchronously before the first `await`, so it
does stop rapid taps rather than merely narrowing the window.

In `renderReader()`, make the two disabled expressions respect the flag.
**`web/app.js:232`** (`ap.disabled=n>0;`) becomes:

```js
  ap.disabled=n>0||submitting;
```

and immediately after it add:

```js
  const ch=document.getElementById("vChanges");if(ch)ch.disabled=submitting;
```

The in-flight flag is deliberately a module-level variable and not only a
`disabled` attribute: a `disabled` button is a UI-level rule, and the guard has
to survive `renderReader()` running mid-flight.

### 8. `cmd_serve_review_test.go` — new file, package `main`

No existing test in this package drives `apiReview` end to end, so this
establishes the pattern. Three environment roots must be redirected, not one:
`HOME` (config discovery in `repoByName`, `cmd_serve.go:184`, and
`bus.DefaultPath`, `bus/store.go:41-45`), `XDG_DATA_HOME` (the todo store, via
`hiveDataDir`, `repo_key.go:127-145`), and `XDG_RUNTIME_DIR` (`todoLockPath`,
`todo_store.go:86-91`). Miss one and the test writes into the developer's real
hive data directory.

A shared fixture:

```go
// newReviewFixture stands up a throwaway hive: a temp HOME with a config, one
// git repo under repos_dir holding a plan, and one ticket in plan-review.
// Returns the mux, the repo path, the bus path, and the plan's hash.
func newReviewFixture(t *testing.T) (h http.Handler, repo, busPath, planHash string)
```

Setup, in order:

1. `tmp := t.TempDir()`; `t.Setenv("HOME", tmp)`;
   `t.Setenv("XDG_DATA_HOME", filepath.Join(tmp, ".local", "share"))`;
   `t.Setenv("XDG_RUNTIME_DIR", tmp)`; `t.Setenv("HIVE_SENDER", "wt:test")`;
   `t.Setenv("HIVE_AUTO_RESPONDER", "")`. The last matters: `CheckAutoVerb`
   (`bus/auto.go:34-41`) refuses non-question announcements when that variable
   is `"1"`, and this suite may be run from an agent shell.
2. `repo = filepath.Join(tmp, "repos", "demo")`, `os.MkdirAll`, then
   `git init`, `git config user.email` / `user.name`, write
   `docs/plans/tkt.md` with known content, `git add -A`, `git commit -m init`
   (all `exec.Command("git", "-C", repo, ...)`; `mainWorktree`,
   `todo.go:84-95`, shells out to `git worktree list --porcelain`).
3. Write `filepath.Join(tmp, ".config", "hive", "config.yaml")` containing
   `repos_dir: <tmp>/repos` (`Config.ReposDir`, `config.go:41`;
   `DiscoverRepos` enumerates that directory, `config.go:341-357`).
4. Seed the ticket:
   `withTodos(repo, func(ts []Todo) []Todo { return append(ts, Todo{ID: "tkt", Subject: "A ticket", State: StatePlanReview}) })`.
5. `busPath = filepath.Join(tmp, ".config", "hive", "bus.jsonl")`;
   `planHash = hashOf(planBytes)`; `h = newServeMux("tok")`.

Callers freeze the clock themselves where they need to: save `nowFunc`,
`defer` restore, assign a controlled value. That is the existing convention
(`todo_test.go:514-516`).

A `post` helper used by both HTTP tests:

```go
body := `{"verdict":"approve","kind":"plan","hash":"` + planHash + `"}`
post := func() *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/api/review/demo/tkt?t=tok",
		strings.NewReader(body)))
	return w
}
```

and a `busLines(t, busPath) []bus.Announcement` helper that reads the file,
skips blank lines, and `json.Unmarshal`s each one.

**Test A — `TestSixConcurrentReviewPostsRecordOneReview`.** This is the
reproduction; write it first and watch it fail. Fire six `post()` calls from six
goroutines under a `sync.WaitGroup`, wait, then assert:

- `busLines` has **exactly one** entry. On `87acdeb` this is six. Failure
  message should name the count and say the bus was told the same thing N
  times.
- Exactly one of the six responses lacks `"duplicate"`; the other five carry
  `"duplicate":"true"`.
- All six responses are 200.

**Test B — `TestReviewPostedTwiceIsWrittenOnce`.** Sequential, and it pins the
file and the announcement's shape:

- First `post()` returns 200; read `docs/plans/tkt.review.md` and keep the bytes.
- **Advance `nowFunc` by an hour.** This is what makes the file assertion
  meaningful: a second write would change the `at:` line.
- Second `post()` returns 200 and its body contains `"duplicate":"true"`.
- `docs/plans/tkt.review.md` is byte-identical to the bytes kept. Failure
  message: "the review was rewritten", not "bytes differ".
- `busLines` has exactly one entry; its `Headline` contains
  `plan review posted on tkt (demo) — approved, 0 comment(s), against hash `
  and does **not** contain `--body`; its `Body` contains
  `Reviewed from hive web.`. Those last two pin §5 and fail today.
- The ticket's state is `StateReady` (`loadTodos(repo)` + `indexByID`).

**Test C — `TestAcceptingTheSameBuildTwiceLeavesItDone`.** Pure, no fixture,
and it must call the production function so it is not a tautology:

```go
ts := []Todo{{ID: "tkt", Subject: "A ticket", State: StateTriage, Claim: "wt:x"}}
p := reviewPost{Verdict: "approve", Kind: "build", Hash: "abc"}
ts = applyReviewVerdict(ts, 0, p)
ts = applyReviewVerdict(ts, 0, p)
if !ts[0].Done { t.Error("accepting the same build twice un-accepted it") }
```

Also assert `ts[0].Claim == ""`. Add the plan-approve arm as a second case in
the same table if it reads better; the requirement is that the assertions go
through `applyReviewVerdict`, not through a copy of its body.

## Verification

```bash
cd /home/steve/repos/workspace          # or the worktree the build runs in
go build ./... && go vet ./...
go test ./... 2>&1 | tail -30
go test -race -run 'TestSixConcurrent|TestReviewPostedTwice|TestAcceptingTheSame' -v . 2>&1 | tail -60
```

Success: `go build` and `go vet` silent; `go test ./...` all `ok`; the three
named tests `--- PASS` under `-race`.

**The red step, before any production change.** Write Test A first and run it
against `87acdeb`. It must fail on the bus-line count — six lines, not one.
That is the reported bug reproduced without a browser, at the concurrency the
bus timestamps show it actually happened at. A version of this test that posts
sequentially would also fail today, but it would go green against a racy
implementation, so the concurrent form is the one that counts.

**Manual check of the client half** (the JS has no test harness, so this is the
only coverage it gets):

1. Set `web_port` in `~/.config/hive/config.yaml`, start hive, open the printed
   URL.
2. Open a `plan-review` ticket and hammer "Approve" — five or six fast clicks.
3. `hive bus list | grep "review posted"` shows **one** line for that ticket,
   and its headline does not contain `--body`.
4. `git -C <repo> status` — `docs/plans/<id>.review.md` written once.
5. Repeat with "Request changes" after adding one comment; both buttons should
   visibly grey out for the duration of the request.

## Blast radius

- **`reviewDoc`'s output is unchanged.** The duplicate check compares rendered
  docs rather than adding a metadata field, so no persisted format changes and
  no review doc already on disk needs migrating.
  `cmd_serve_test.go:43-80` and `:130-141` assert with `strings.Contains` and
  keep passing untouched.
- **`docs/plans/<id>.review.md` is a tracked file in the main worktree** —
  nothing in `.gitignore` covers it, and it is now load-bearing for duplicate
  detection. A branch switch, revert, or `git clean` in the main worktree that
  removes it turns dedup off until the next review is written; one that restores
  an older identical one suppresses a genuine re-review. Both are acceptable —
  the failure is bounded to one extra or one missing write of identical content
  — but the next reader should not be surprised by it. See Open question 1.
- **`toggleTodoDone` is not modified**, only one of its call sites. Its other
  callers (drawer and CLI done-toggling) are untouched and still want toggle
  semantics.
- **`apiReview`'s success response gains an optional `duplicate` key.** It stays
  `map[string]string`, so no struct changes. `web/app.js` is the only consumer.
- **`announceReview`'s bus output changes shape** — headlines stop carrying
  `--body <the whole body>` and bodies stop being truncated fragments. Nothing
  parses bus headlines programmatically; `bus list` and `bus inbox` render them.
- **Nothing else reads a review doc.** `git grep "\.review\.md"` hits only
  `cmd_serve.go:352`, `cmd_serve.go:440`, and
  `docs/superpowers/specs/2026-08-26-hive-web-design.md:131`. No TUI drawer
  code, no CLI command.
- **`reviewMu` serialises review posts across every repo**, not just the one
  being reviewed. With a human at the keyboard this is invisible; if hive ever
  grows automated review posting it becomes a throughput ceiling worth
  revisiting.
- **`web/index.html` and `web/app.js` are `go:embed`ed** (`cmd_serve.go:21`), so
  a rebuild is required for the client fix to reach a browser. There is no
  separate asset pipeline and no cache-busting query on
  `<script src="/app.js">` (`index.html:249`) — a phone holding the old
  `app.js` may need a hard reload. Not worth solving here, because the server
  guard holds on its own.
- **No persisted data shape changes** — the todo store, the `bus.jsonl` schema,
  and the `/api/plan` and `/api/build` responses are all untouched.

## Critic findings

Two critics ran against the first draft. Both independently found the same
blocking defect, and it changed the plan's core.

- **The server guard was a TOCTOU and would not have stopped the reported bug.**
  The draft placed an `os.ReadFile` duplicate check at the top of `apiReview`
  and the write at the bottom with nothing serialising them, while billing it as
  "the defence that holds regardless of what the browser does". Six concurrent
  requests all read "no previous review" and all proceed. One critic noted six
  is also the HTTP/1.1 per-origin connection limit; the other went further and
  read `~/.config/hive/bus.jsonl:1052-1057`, where the six timestamps span 1.06s
  at 130-330ms intervals — spacing that only makes sense if they were in flight
  together and serialised by the flock inside `withTodos`. **Added §1, the
  `reviewMu` mutex, and Test A, six concurrent posts.** Both critics also warned
  against reaching for `lockTodos` instead: `withTodos` flocks the same path on
  a second fd and the goroutine would block against itself. That warning is
  recorded in §1.
- **The fingerprint scheme was over-engineered.** The draft added a
  `reviewFingerprint` function, a `sortedComments` extraction, a `fingerprintIn`
  parser, and a new `fingerprint:` line in the review doc — a persisted-format
  change with a back-compat branch. One critic pointed out that `reviewDoc` is
  already a deterministic function of exactly those inputs and that the only
  time-varying byte is the `at:` line, so comparing rendered docs with that line
  dropped gives the identical guarantee with none of the machinery. **Rewrote §2
  accordingly**; the diff and the blast radius both shrank.
- **The second test was a tautology.** The draft said to "call `withTodos` twice
  with the build-accept arm's new body" and left the factoring optional, which
  an agent that cannot ask questions would read as "paste the two assignments
  into the test" — asserting that `true` assigned twice is `true`, without
  touching `cmd_serve.go`. **Both critics flagged it. §3 now mandates extracting
  `applyReviewVerdict`, and Test C calls it.**
- **A duplicate still mutated state.** The draft deliberately re-ran the
  transition on a duplicate, arguing it let a hand-moved ticket be brought back
  into line. One critic pointed out the closure clears `Claim`/`Since`
  unconditionally, so an unbounded window meant a stale replay could un-claim a
  builder mid-build; the other said the choice deserved an explicit decision
  rather than a parenthesis. **Changed: a duplicate now returns before the
  transition and changes nothing at all**, which is also the plainer reading of
  the ticket's "a no-op returning the existing result". The former open question
  about a time-bounded window is gone with it.
- **Wrong line number.** Both critics caught the draft citing `web/app.js:230`
  for `ap.disabled=n>0`; line 230 is a comment and the assignment is 232.
  Corrected throughout.
- **A misleading justification in the test setup.** The draft claimed "a single
  `t.Setenv("HOME", tmp)` redirects the config, the todo store, and `bus.jsonl`
  at once". False — `hiveDataDir` prefers `XDG_DATA_HOME` and `todoLockPath`
  uses `XDG_RUNTIME_DIR`. The recipe set all three, but the prose invited an
  executor to drop the "redundant" ones and write into the real
  `~/.local/share/hive/todos`. **§8 now names the three roots and what each
  covers.**
- **One draft claim was wrong on its own terms and is corrected in Root cause:**
  the draft said the `--body` bug left the announcement's `Body` empty. It does
  not — `foldHeadline` moves the headline overflow into `Body`, which is why the
  real message on the bus has a body starting mid-sentence. The test assertion
  built on it still holds (the fold cuts after the phrase being asserted), but
  the reasoning was wrong.
- **Where a critic was wrong:** one suggested the `-b` fix might be scope creep
  to cut. The other checked it and concluded the opposite — it is one line in
  the function this ticket already edits, and the ticket's own acceptance
  criterion ("assert exactly one bus announcement") cannot assert on an
  announcement's content while every announcement is malformed. Kept, and the
  reasoning is now stated in Root cause rather than left implicit. Open
  question 2 still offers it up for cutting.
- Everything else both critics checked came back sound: the `flag.Parse`
  behaviour and the `{"announce","-b",body,head}` fix; that dropping
  `toggleTodoDone` at this call site loses nothing; that the JS `finally` is
  safe against both the 409 early return and `closeReader()` nulling `openPlan`;
  that the `if(submitting)return` guard runs before the first `await`; that no
  existing test asserts on `reviewDoc`'s exact shape and no test in the package
  uses `t.Parallel`; that the click listener cannot be registered twice; and
  that nothing outside `cmd_serve.go` reads a review doc or parses a bus
  headline.

## Open questions

1. **Is the review doc on disk a good enough store for "already recorded"?**
   Duplicate detection now depends entirely on `docs/plans/<id>.review.md`
   being present and unchanged. It is a git-tracked file in the main worktree,
   so a revert, branch switch, or `git clean` can remove it (dedup silently
   stops until the next review) or restore an identical older copy (a genuine
   re-review of an unchanged plan is silently suppressed). **I chose to accept
   this**, because the blast radius is one extra or one missing write of
   identical content, and because the alternative — a separate state file
   outside git — adds a store to keep in sync for a failure mode nobody has
   hit. Say so if you would rather the guard were independent of git.

2. **Should the `-b` / `--body` announce fix ride along, or be its own ticket?**
   One line in `announceReview`, in the function this ticket already edits, and
   the acceptance test cannot assert on an announcement's content until it is
   fixed — so **I folded it in** and said so explicitly in Root cause. If you
   would rather keep this ticket to the duplicate-submit fix alone, drop §5 and
   drop the `Body` and `--body` assertions from Test B; the ticket's stated goal
   still lands.

3. **Should a duplicate be a 200 or a 409?** As specified it is a 200 carrying
   `"duplicate":"true"`, on the ticket's own wording: *"a no-op returning the
   existing result"*. A 409 would be more RESTful and would let a client
   distinguish without inspecting the body, but it would also make an accidental
   double-tap look like an error to a human, which is the opposite of what a
   silent, harmless deduplication should feel like. **I chose 200.**
