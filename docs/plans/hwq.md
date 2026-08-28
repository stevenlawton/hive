# Human decisions need a verifiable record, and nothing stops an agent fabricating one

**Ticket:** hwq
**Repo:** hive (`/home/steve/repos/workspace`)
**Base commit:** 87acdeb0ae82594a1d3e69fa0c1dbd19e4f251f2
**Written:** 2026-08-27

## Scope, and an honest statement of what this does not fix

The ticket describes a five-layer design (0 through 4). **This plan builds none
of them in full.** It fixes the one failure in the ticket that is *measured*,
in the one place a human decision actually enters this system, and it makes the
record that place writes honest. That is all.

Read this next sentence before anything else, because a first draft of this
plan got it wrong and two critics caught it:

> **This plan does not stop an agent fabricating a `## Decisions` section in a
> plan artifact.** That is the originating incident, it happens on a path this
> contract does not touch, and nothing here would have prevented it.

What it does fix is the *second* incident recorded on the ticket — the `dmy`
review, where the system put the wrong artifact in front of Steve, recorded a
correct hash of it, and wrote `reviewer: Steve, verdict: approved` for a
question he was never meaningfully asked. Every integrity property the design
promises held, and the record was still false.

An earlier draft of this plan proposed building the Layer 1 hash-chained ledger
now. It has been cut. The reasoning is in Critic findings and in Open question
1; the short version is that a ledger nothing is required to read, which the
documented CLI approval path bypasses entirely, and whose chain a same-uid
agent can recompute wholesale, is what the ticket itself calls decoration. It
should be planned after there is a reader for it.

## Current behaviour

### Where a human decision enters — and the door beside it

The hive web UI is the only place with a minting step. `web/app.js:11` defines
`GATES = new Set(["plan-review","triage"])`, renders a gate card, and POSTs a
verdict to `/api/review/{repo}/{id}`.

It is **not** the only way a ticket clears a gate. `hive todo state <id> ready`
is documented as the human approval path in two places —
`docs/claude/commands/refine.md:104` (*"read the plan, then `hive todo state
<id> ready` to approve it for building"*) and `docs/claude/commands/next.md:47`.
`runTodoState` (`cmd_todo.go:404-459`) has no auth, no gate check and no
record, and any agent in any worktree can run it. This plan does not close that
door — see Open question 3 — but any claim that the web gate is "the" decision
surface is false and must not be written into the code.

### What the web gate records

`apiReview` (`cmd_serve.go:277-366`):

- `reviewPost` (`cmd_serve.go:58-66`) carries `Verdict`, `Kind`, `Hash`,
  `Comments`. **`Kind` is supplied by the browser.**
- `checkVerdict` (`cmd_serve.go:370-390`) validates the verdict, the comment
  rules, that `Hash` is non-empty, and that `Kind` is one of two strings.
- The artifact is re-read and re-hashed (`cmd_serve.go:296-323`); 409 if it
  changed. `hashOf` (`cmd_serve.go:272-275`) is sha256 **truncated to 12 hex
  characters**.
- The ticket is transitioned inside `withTodos` (`cmd_serve.go:326-350`).
- `reviewDoc` (`cmd_serve.go:395-431`) renders markdown to
  `docs/plans/<id>.review.md`. **That git-tracked file is the only durable
  record of any human decision in this system.**
- `announceReview` (`cmd_serve.go:433`) posts a headline to the bus.

### The three defects

**1. Nothing checks the question was the right one for the ticket's state.**
`apiReview` branches on `post.Kind` and never reads `Todo.State`. The rule
exists only in the browser, at `web/app.js:285`:
`const kindOf=t=>t.state==="triage"?"build":"plan";`. The comment above
`checkVerdict` (`cmd_serve.go:368-369`) says *"The browser enforces them too,
but a rule that only exists in the UI is not a rule"* — about the rules it does
enforce. This one it does not. `5362d1d` fixed the browser half; the server
half is recorded as still open in
`docs/superpowers/specs/2026-08-26-hive-web-design.md:212-227`.

**2. The record asserts an identity nobody checked.** `reviewDoc` writes the
literal string `reviewer: Steve` (`cmd_serve.go:407`). `withAuth`
(`cmd_serve.go:161-176`) is one shared bearer token at
`~/.config/hive/data/web-token`; its own comment (`cmd_serve.go:124-126`) says
*"It is a latch, not a defence."* The record also omits the question entirely,
so a reader cannot tell what was asked.

**3. A review against a ticket that does not exist returns 200.**
`cmd_serve.go:327-331`: `indexByID` misses, the closure returns `ts` unchanged,
and execution falls through — the review doc is written and 200 returned. Same
disease as `hive todo claim clear` reporting success having released nothing.

### Facts the contract depends on, all verified at 87acdeb

- `loadTodos` (`todo.go:131`) is **not** a free read. It is
  `withTodos(repoPath, identity)`, so it takes `lockTodos` and, on a store
  whose bytes change under `backfillIDs`, writes the file and calls
  `backupStore()` (`todo_store.go:31-41`). It also returns `nil` on error.
- `withTodos` has a no-op guard (`todo_store.go:29-31`): if the rendered bytes
  match the existing bytes, it returns without writing. A closure that returns
  `ts` unmodified therefore does not write.
- `fmt` is already imported in `cmd_serve.go:10`. `StatePlanReview` and
  `StateTriage` are package consts (`todo.go:52,54`).
- **There is no `xdgRuntimeDir()` helper.** Every site inlines
  `os.Getenv("XDG_RUNTIME_DIR")` with an `os.TempDir()` fallback
  (`todo_store.go:86-92`, `repo_key.go:81-85`).
- `repoByName` (`cmd_serve.go:183-195`) resolves via `LoadConfig` on
  `$HOME/.config/hive/config.yaml`, which on a missing file returns defaults
  with `ReposDir = $HOME/repos` (`config.go:55, 68-72`), then `DiscoverRepos`
  (`config.go:341`) reads that directory. `os.UserHomeDir()` reads `$HOME`, so
  `t.Setenv("HOME", …)` fully redirects it.
- `newTestRepo(t)` (`todo_store_test.go:175`) redirects `XDG_DATA_HOME` and
  `XDG_RUNTIME_DIR` but **not `HOME`**.
- **No existing test drives `apiReview`.** `httptest` appears in
  `cmd_serve_test.go` only in the `withAuth` tests (lines 82, 93, 106) and
  `TestUnknownRepoIsRefused` (119). `cmd_serve_test.go` is **141 lines long**.
- `reviewDoc` has **four** test callers: `cmd_serve_test.go:45, 64, 75, 131`.
  `announceReview` has one production caller, `cmd_serve.go:361`.
- `go build ./...`, `go vet ./...` and `go test ./... -count=1` are all clean at
  87acdeb. Any breakage is attributable to this work.

## Root cause

A decision has three parts. This codebase persists a degraded form of one.

1. **The question asked**, and whether it was the right question for the state
   the work is in — not recorded, and not checked.
2. **The artifact read** — recorded, as a 48-bit truncated hash.
3. **The answer** — recorded, in prose, attributed to a hardcoded name.

The `dmy` incident is a wrong (1) with a sound (2) and (3). It is undetectable
because the only thing that knows which artifact a state calls for is a
JavaScript arrow function in the browser, and the server takes the browser's
word for it. Fixing that requires the state→artifact rule to exist once,
server-side, and the question to be derived from state rather than supplied by
the caller.

## The contract

Two files edited, one test file extended, two documents corrected. **No new
package, no new store, no new CLI command.**

### 1. `cmd_serve.go` — the state→artifact rule, in one place

Add next to `checkVerdict`:

```go
// requiredKind is the single source of truth for which artifact a human gate
// puts in front of a person. States with no human gate return ok=false.
// web/app.js:285 mirrors this; when they disagree, this one wins.
func requiredKind(state string) (kind string, ok bool) {
	switch state {
	case StatePlanReview:
		return "plan", true
	case StateTriage:
		return "build", true
	}
	return "", false
}
```

### 2. `cmd_serve.go` — the question, derived from state

```go
// gateQuestion is the question the gate actually put. It is derived from the
// ticket's state so that no caller — browser or agent — can choose what a
// human is recorded as having been asked.
func gateQuestion(state, subject, artifact string) string
```

Exact return values, so tests can pin them:

- `StatePlanReview` → `Does this plan describe the change to make for "<subject>"? (plan: <artifact>)`
- `StateTriage` → `Does this built change do what the plan promised for "<subject>"? (build: <artifact>)`
- any other state → `""`.

### 3. `cmd_serve.go` — a full-width hash for the record

```go
// fullHashOf is hashOf without the truncation. The record carries the full
// digest; the 12-char form stays on the wire, where a human eyeballs it.
func fullHashOf(b []byte) string
```

Leave `hashOf` and the existing `post.Hash` 409 comparison **exactly as they
are**. Changing their width breaks every open browser tab.

### 4. `cmd_serve.go` — `apiReview` refuses incoherent and unanchored reviews

**4a. Pre-check.** Insert immediately after the `checkVerdict(post)` block
(which is at `cmd_serve.go:289-292`) and before the artifact re-read at
`cmd_serve.go:296`:

```go
// A verdict is only meaningful against the artifact the ticket's state calls
// for. Trusting the browser's `kind` is how a plan came to be approved for a
// ticket sitting at triage. This costs a backlog lock — loadTodos is
// withTodos with an identity mutate, not a free read — but the transition
// below takes the same lock anyway, and answering before doing the git work
// is worth one extra acquisition.
todos := loadTodos(repo.Path)
ti, found := indexByID(todos, id)
if !found {
	// loadTodos returns nil on a store read failure too, so this 404 is
	// "not found or not readable". Better a refusal than the 200 this
	// used to return.
	http.Error(w, "no such ticket: "+id, 404)
	return
}
if want, gated := requiredKind(todos[ti].State); !gated {
	http.Error(w, fmt.Sprintf("ticket %s is not at a human gate (state %q)", id, todos[ti].State), 409)
	return
} else if post.Kind != want {
	http.Error(w, fmt.Sprintf("ticket %s is at %q, which calls for the %s — refusing a %s verdict",
		id, todos[ti].State, want, post.Kind), 409)
	return
}
```

`http.Error` writes plain text, consistent with every other refusal in this
handler. 409 rather than 400 because the request is well formed and the
conflict is with server state.

**4b. Re-assert inside the transition, and capture the state from there.** The
pre-check is a separate lock acquisition from the transition, so the state can
change in between. The recorded state must be the one the transition actually
acted on. In the existing `withTodos` closure (`cmd_serve.go:326-350`), after
`subject = ts[i].Subject`:

```go
priorState = ts[i].State // captured for the record; declared outside the closure
if want, gated := requiredKind(priorState); !gated || post.Kind != want {
	raced = true  // declared outside the closure
	return ts     // unchanged, so withTodos' no-op guard writes nothing
}
```

Declare `var priorState string` and `var raced bool` alongside the existing
`var subject string` at `cmd_serve.go:325`. After the `withTodos` call returns
successfully, before anything is written:

```go
if raced {
	http.Error(w, "the ticket moved while you were reviewing it — reload and try again", 409)
	return
}
```

Leave the existing `indexByID` miss inside the closure exactly as it is; 4a
already refuses that case earlier.

### 5. `cmd_serve.go` — `reviewDoc` records all three parts

New signature:

```go
func reviewDoc(subject, id string, p reviewPost, artifact, planRel, state, question, artifactSHA string) string
```

Replace the header `Fprintf` (`cmd_serve.go:407-408`). The emitted block becomes,
in this order, one field per line:

```
ticket: <id>
state: <state>
reviewed: <kind>
<kind>: <planRel>
question: <question>
hash: <p.Hash>
artifact-sha256: <artifactSHA>
answered-via: hive-web
verdict: <verdict>
comments: <n>
at: <RFC3339>
```

Three changes of substance, everything else preserved:

- `reviewer: Steve` **is deleted** and replaced by `answered-via: hive-web`.
  The code does not know who typed it and must stop saying it does. See Open
  question 2.
- `question:` and `state:` are new — this is part (1) of the record, which was
  missing entirely.
- `artifact-sha256:` is new and full width. `hash:` keeps the 12-char value so
  it still matches what the UI showed.

Update the sole production caller at `cmd_serve.go:351` to pass `priorState`,
`gateQuestion(priorState, subject, where)` and `fullHashOf([]byte(text))`.

Update **all four** test callers — `cmd_serve_test.go:45, 64, 75, 131` — with
the three new arguments. Their existing assertions must keep passing unchanged.

### 6. `cmd_serve.go` — the bus headline carries the state

In `announceReview` (`cmd_serve.go:433`), add a `state string` parameter and
include `at <state>` in the headline, so a peer reading the bus can see which
gate was answered rather than inferring it. Update the caller at
`cmd_serve.go:361`.

### 7. `web/app.js` — the preview must not contradict the file

`web/app.js:243-244` renders a client-side preview of the review doc. It
currently hardcodes `plan:`, `plan-hash:` and `reviewer: Steve`. After §5 the
server writes something different, so the preview would lie about a document
the human is being shown before they commit to it. Update lines 243-244 to
emit the same field names and order as §5, using `p.kind` for the label and
omitting `artifact-sha256` (the browser does not have it).

**Also in scope, one line:** `web/app.js:156` labels the inline button "Review
the plan" whenever `t.hasPlan`, including for a ticket at `triage`. Change it
to use the same conditional the gate card already uses at `web/app.js:117-118`
(`t.state==="triage"?t.hasBuild:t.hasPlan`, labelled "Review the build" at
triage). This is the *same class* of mislabel as the incident and is one
expression away.

**Also in scope, and not optional — the 409 handler is now wrong.**
`web/app.js:264-266` treats *every* 409 as the stale-artifact case and alerts
*"The plan changed while you were reviewing it."* After §4 there are three new
409s that mean nothing of the sort, and a reviewer refused for a gate mismatch
would be told their plan changed. That is a misleading message about a decision
gate, in a ticket about misleading records.

No server change is needed. The stale-artifact 409 is emitted with `writeJSON`
and carries `was` and `now` fields (`cmd_serve.go:319-323`); the new refusals
use `http.Error`, so they are plain text with no such fields. The `api` helper
(`web/app.js:3-9`) already attaches the parsed body to the thrown error as
`.data` and the text as `.message`. So narrow the condition:

```js
if(e.status===409 && e.data && e.data.now){   // was: if(e.status===409){
```

Everything else in that branch is unchanged. The `else` already does
`alert("Review not saved: "+e.message)`, which surfaces the server's plain-text
refusal verbatim — which is why §4's messages must read well to a human.

### 8. Documentation corrections

- `docs/superpowers/specs/2026-08-26-hive-web-design.md:138-152` shows the
  review-doc format including `reviewer: Steve`. Update the fenced example to
  match §5 exactly. The base commit is *"docs: bring the hive web design in
  line with what shipped"*; landing this without touching it re-drifts it on
  day one.
- In the same file, the *"What building it taught"* section (lines 214-227)
  says the state/kind cross-check is *"Recorded in full on the 'human decisions
  need a verifiable record' ticket"*. Append one sentence noting it is now
  enforced server-side by `requiredKind`, and that the remaining gap is the
  `hive todo state` CLI path.

### 9. Tests — `cmd_serve_test.go`

**First, the seam. It does not exist and must be built, or five of the tests
below cannot be written.** Add to `cmd_serve_test.go`:

```go
// newServeTestRepo redirects HOME as well as the store env vars, so
// repoByName -> LoadConfig -> DiscoverRepos resolves inside the test. It
// creates $HOME/repos/<name> as a git repo, and returns the repo's display
// name and its path.
func newServeTestRepo(t *testing.T, name string) (repoName, repoPath string)
```

It must: `t.Setenv("HOME", t.TempDir())`, `t.Setenv("XDG_DATA_HOME", …)` and
`t.Setenv("XDG_RUNTIME_DIR", …)` to fresh temp dirs; `os.MkdirAll` on
`$HOME/repos/<name>`; run `git init` there (`mainWorktree` and `repoKey` both
shell out to git); and write no config file, relying on the documented
defaults. Drive handlers with `httptest.NewRequest` plus `req.SetPathValue("repo", …)`
and `req.SetPathValue("id", …)`, calling `apiReview` directly — the existing
`withAuth` tests show the `httptest` idiom.

Then, matching the package's plain `func TestX(t *testing.T)` style:

- `TestRequiredKindMapsEveryGate` — `plan-review`→`plan`, `triage`→`build`,
  and `""`/`ready` return `ok=false`. Pure function, no seam needed.
- `TestGateQuestionNamesTheArtifactAndSubject` — both gate strings pinned
  verbatim; a non-gate state returns `""`.
- `TestReviewRefusesAPlanVerdictOnATriageTicket` — ticket at `triage`, POST
  `kind:"plan"`; assert 409, assert the ticket's state is **still** `triage`,
  and assert no `<id>.review.md` was written. **This is the `dmy` incident,
  pinned. If this test is dropped or weakened, the build has failed.**
- `TestReviewRefusesAnUnknownTicket` — assert 404 and that no review doc exists.
- `TestReviewRefusesATicketNotAtAGate` — state `ready`; assert 409.
- `TestReviewAtTheRightGateSucceeds` — ticket at `plan-review`, `kind:"plan"`,
  correct `hashOf` of the plan file; assert 200 and state `ready`. Without this
  the four refusal tests would pass on a handler that refuses everything.
- `TestReviewDocRecordsQuestionStateAndFullHash` — the rendered doc contains
  `state: plan-review`, a `question: ` line matching `gateQuestion`, a
  64-character `artifact-sha256:` value, and `answered-via: hive-web`; and does
  **not** contain `reviewer: Steve`.

## Verification

From `/home/steve/repos/workspace/.worktrees/split-3`:

```
go build ./... && go vet ./...
```
Success is no output.

```
go test . -run 'TestRequiredKind|TestGateQuestion|TestReview' -v
```
Note `go test .`, not `./...` — the `bus` and `ui` packages match none of these
and would print `[no tests to run]`. Success is every named test `--- PASS`.

```
go test ./... -count=1
```
Success is `ok` for all three packages and no `FAIL`. This must be run:
`reviewDoc` and `announceReview` both change signature and four existing tests
call `reviewDoc` directly.

**The behavioural check, which is the point of the whole change.** Unit tests
alone would not have caught the `dmy` incident, so drive the real binary:

```
export HOME=$(mktemp -d) XDG_DATA_HOME=$(mktemp -d) XDG_RUNTIME_DIR=$(mktemp -d)
mkdir -p "$HOME/repos/demo" && git -C "$HOME/repos/demo" init -q
go build -o /tmp/hive-hwq . || exit 1
cd "$HOME/repos/demo"
/tmp/hive-hwq todo add "A demo ticket - body text"
id=$(/tmp/hive-hwq todo list | sed 's/\x1b\[[0-9;]*m//g' | awk 'NR==1{print $1}')
mkdir -p docs/plans && echo "# plan" > "docs/plans/$id.md"
/tmp/hive-hwq todo state "$id" triage
/tmp/hive-hwq serve --port 8899 &
sleep 1
tok=$(cat "$HOME/.config/hive/data/web-token")
curl -s -o /dev/stderr -w '%{http_code}\n' -X POST \
  "http://127.0.0.1:8899/api/review/demo/$id?t=$tok" \
  -H 'content-type: application/json' \
  -d '{"verdict":"approve","kind":"plan","hash":"whatever","comments":[]}'
```

**Expected: `409`, with a body naming `triage` and refusing the `plan` verdict.**
A `200`, a `404`, or a `409` about the hash changing all mean the guard is not
in the path — the hash check must never be reached, because the coherence
refusal comes first. Confirm afterwards that `docs/plans/$id.review.md` does
not exist and that `hive todo show $id` still reports `triage`.

If `hive serve` does not accept `--port`, read `runServeCmd` and use whatever
flag it does take; do not skip this step.

## Blast radius

- **`reviewDoc` gains three parameters.** Callers: `cmd_serve.go:351` and
  `cmd_serve_test.go:45, 64, 75, 131`. All five must be updated or the package
  does not compile. No existing test asserts `reviewer: Steve`, so none should
  fail on content — only on arity.
- **`announceReview` gains one parameter.** One caller, `cmd_serve.go:361`.
- **`docs/plans/*.review.md` changes format.** Existing review docs on disk are
  not migrated and will keep their old fields. Nothing parses them — confirmed,
  no Go code reads the body of anything under `docs/plans/` — so this is a
  readability change only.
- **`hive tokens`, the TUI, the bus and the backlog are untouched.**
- **Interaction with ticket `kdx` (one click posts six reviews), which is live
  and unfixed.** After this change the first POST moves the ticket out of its
  gate; the remaining five hit `requiredKind` and return 409. **One mobile tap
  will therefore raise up to five error dialogs after a successful approval**,
  reading `ticket <id> is not at a human gate (state "ready")`. This is a real,
  user-visible regression in a mobile flow, and §7's narrowed 409 condition
  makes the message accurate but does not make it stop appearing. Do **not**
  paper over it by making the handler idempotent — that is `kdx`'s job and
  doing it here would collide. Note it in the commit message and on `kdx`.
  If it proves intolerable before `kdx` lands, the smallest honest mitigation
  is for the browser to disable the verdict buttons on first submit; that is a
  `web/app.js` change and still belongs on `kdx`, not here.
- **The extra `loadTodos` in 4a takes the backlog flock** and, on a store that
  needs an id backfill, may write and trigger `backupStore()`. In steady state
  the no-op guard makes it a read. Acceptable, and documented in the comment
  the contract specifies.
- **Not touched, deliberately:** `planner.md`, `builder.md`, the plan-artifact
  template, `runTodoState`, and anything to do with `## Decisions` sections.
  See Open questions 1 and 3.

## Critic findings

Two `plan-critic` agents attacked the draft. Both returned a blocking verdict
and the plan was substantially rewritten. What they found:

**Upheld, and the reason the ledger was cut from this plan:**

1. *The ledger could not contain the class of record that was fabricated.* The
   incident is a `## Decisions` section written by hand into a plan artifact
   per `docs/claude/commands/refine.md:76-84`. The web gate writes verdicts to
   `<id>.review.md` and never writes `## Decisions`. A ledger fed only by the
   web gate would, after a month, hold N approve/changes verdicts and zero
   records of the thing that got invented. The draft conceded this in an open
   question while its Root cause section claimed otherwise. Correct, and
   damning. The scope statement at the top of this plan now leads with it.
2. *The CLI path bypasses the gate entirely.* `hive todo state <id> ready` is
   the documented human approval path
   (`docs/claude/commands/refine.md:104`, `docs/claude/commands/next.md:47`),
   has no auth and no record, and any agent can run it. Verified. Against the
   ticket's SETTLED scope — "scope this for a deliberate forger" — a bypass
   requiring no forgery at all is disqualifying. Now Open question 3, and
   stated in Current behaviour rather than buried.
3. *`verifyChain` gave nothing against the stated threat model.* The ledger
   JSONL and the code that verifies it are both same-uid writable; a forger
   rewrites record 4 and recomputes hashes 4..N. The draft called it "a
   tamper-evidence hash" with no qualifier — an overclaim of exactly the kind
   the ticket exists to stop. The critic further noted that `<id>.review.md` is
   already git-tracked, so today's prose record has a *better* integrity story
   than the untracked JSONL that would have replaced it as the citable
   artifact. That observation is what settled the rescope.
4. *A ~25-line change gets the only measured win.* Correct. That change is now
   the whole contract.
5. *TOCTOU on `priorState`.* The draft read the state via an unlocked
   `loadTodos` and then recorded that stale value after the transition. Fixed:
   §4b re-asserts inside the `withTodos` closure and records from there.
6. *`loadTodos` is not read-only.* Verified — it is `withTodos` with an
   identity mutate (`todo.go:131`), takes the lock, and can write and call
   `backupStore()`. It also returns `nil` on error, so the new 404 is
   "missing or unreadable". Both facts are now written into the contract.
7. *`xdgRuntimeDir()` does not exist.* Verified by grep; every site inlines the
   lookup. The draft told the builder to call a function that is not there.
   The ledger it was needed for is gone, and the fact is recorded in Current
   behaviour so the next plan does not repeat it.
8. *No test seam exists for driving `apiReview`.* Verified: `repoByName` goes
   through `$HOME`, `newTestRepo` does not redirect `HOME`, and no existing
   test exercises the handler. Five of the proposed tests were unwritable. §9
   now specifies `newServeTestRepo` before listing them.
9. *Citation drift.* All corrected: `cmd_serve_test.go` is 141 lines, not 409
   (`TestReviewDocNamesTheBuildItJudged` is at 130-141); `reviewDoc` has four
   test callers, not one; the "Review the plan" label is at `web/app.js:156`
   and `:118`, not 149; the `checkVerdict` block is 289-292; the `reviewDoc`
   call is 351.
10. *The end-to-end recipe was a wish, not a recipe*, and its `sed` step would
    have exited 0 on a no-op — "tests green, feature broken" reproduced inside
    the verification section meant to prevent it. Replaced with a runnable
    sequence whose expected result is a specific HTTP status.
11. *`web/app.js:244` becomes wrong because of this change*, not before it. The
    draft classified it as a pre-existing cosmetic issue to flag and not fix.
    Wrong: changing the server's output is what makes the preview lie. Now in
    scope, §7.
12. *The `kdx` interaction was described backwards.* The draft said six clicks
    would mint six records and called that correct. With the coherence check in
    place, clicks 2-6 get 409s and raise error dialogs — a user-visible
    regression, not correct behaviour. Now stated as such in Blast radius.

**Where a critic was wrong:**

- One critic flagged a hash-stability hazard around `Comments []reviewComment`
  with `omitempty`, suspecting a nil-vs-empty-slice mismatch across a
  write/read round trip. I checked it with a scratch program: `omitempty` omits
  both `nil` and `[]reviewComment{}`, and unmarshalling an absent key yields
  `nil`, so the round trip is stable and there was no hazard. The other critic
  independently reached the same conclusion. Moot now that the ledger is cut,
  but recorded because the next plan will face the question again — and the
  *related* point that critic made is sound: a "mutate every field and assert
  the hash changes" test is false for a slice field mutated from `nil` to
  empty.
- The same critic suggested `-run` regexes would be fine with `./...`. True for
  matching, but it produces `[no tests to run]` lines for two packages, which
  contradicts "every named test PASS" as a success criterion. The contract now
  says `go test .` for the targeted run.

## Open questions

### 1. What comes next — the Layer 0/3 prose lint, or the Layer 1 ledger?

This plan deliberately builds neither. The ticket's SUGGESTED ORDER is *"0 and
3 first, no crypto, stops the observed failure. Then 4, then 1, then 2."* My
draft inverted that to build the ledger first; both critics rejected the
inversion and I now agree.

The two candidates for the next ticket:

- **(a) The Layer 3 prose lint.** *"A lint or pre-commit hook should fail on
  decision-shaped prose (Settled, Do not re-ask, Decided by, carried in from)
  that is not a rendered reference."* This is a grep. It needs no store, and it
  fires on `docs/plans/dmy.md:808-810` today. It targets the actual originating
  incident, which this plan does not. Caveat, and it is a real one: the ticket's
  own CORRECTION says a guard that blocks is routable, and a lint is a block —
  it shapes the default path, it is not a boundary. It would also need care not
  to break `planner.md:101`'s instruction to carry a `## Decisions` section
  forward verbatim.
- **(b) The Layer 1 ledger**, planned properly — with the CLI approval path
  brought inside it, with the store git-backed so a rewrite becomes a visible
  force-push, and with the chain's limit against a same-uid forger written into
  the contract rather than an open question.

**I would do (a) next**, then (b). (a) is hours, targets the reported failure,
and its weakness is honestly known. (b) is days and should wait until Layer 0's
renderer exists to consume it, so the store's shape is driven by a real reader
rather than guessed. But this is a priority call and it is yours.

### 2. Should the record keep a human name at all?

§5 deletes `reviewer: Steve` and writes `answered-via: hive-web`, because
`withAuth` is one shared token and the server genuinely does not know who
clicked.

- **(a) `answered-via: hive-web` — what this plan does.** Records the channel,
  claims no identity.
- **(b) Keep a name with a qualifier**, e.g. `reviewer: Steve (unverified)`.
  More readable. The risk is precisely this ticket's subject: the name is what
  a downstream agent quotes and the parenthetical is what gets dropped one hop
  later.
- **(c) Add real identity** — Layer 2, out of scope.

**I would take (a)**, on the ticket's own principle that a record must not
assert what it cannot check. Flagging it because it changes a file you read.

### 3. Should `hive todo state <id> ready` stay an unrecorded approval path?

This is the hole this plan leaves open, and it is bigger than the one it
closes. Today an agent can move a ticket through either human gate with one
unauthenticated CLI call, and the documentation tells it to
(`docs/claude/commands/refine.md:104`, `docs/claude/commands/next.md:47`).

- **(a) Leave it.** The CLI is how a human at a terminal works, and gating it
  would break `/refine` and `/next`.
- **(b) Record but do not block:** any `state` transition *out of* a gate
  writes a marker saying it was made from the CLI with no artifact review, so a
  later reader can tell a reviewed approval from an unreviewed one. Cheap, and
  it fits this plan's "record and verify first" posture.
- **(c) Require a flag** such as `--reviewed` or `--unreviewed` to leave a gate,
  forcing the caller to say which it is. Explicit, but an agent that wants to
  proceed will simply pass the flag — the ticket's own point about routing
  around guards.

**I would take (b)**, as a small follow-on ticket. I have not put it in this
contract because it touches `runTodoState`, which is on the path of every
`/refine` and `/next`, and because it is a design choice about the CLI's
contract rather than a bug fix.

### 4. A Layer 2 finding, recorded now so it is not discovered halfway through

Not blocking — Layer 2 is out of scope — but it changes whether the Telegram
option is cheap.

`~/.claude/telegram.env` holds only `TELEGRAM_BOT_TOKEN` and
`TELEGRAM_CHAT_ID`, and **the chat id is negative**, meaning a group, not a
private chat with Steve. The ticket's argument for Telegram is sound as far as
it goes — an agent holding the bot token can send *as* the bot but cannot
fabricate an inbound message *from* Steve — but verifying that requires his
numeric Telegram **user** id, which is not in the creds file and would have to
be captured once, out of band. There is also no `getUpdates` code anywhere in
`llm-tools`; `hooks/notify-telegram.sh` only calls `sendMessage`. The inbound
half is entirely new construction.

Separately, and relevant to Layer 3: **`judge.sh` is not registered in
`~/.claude/settings.json` at all.** The only hooks there are `log-session.sh`,
`guard.sh` and three `hive bus` hooks. Its whole layer, nonce included, is dead
code box-wide. The ticket already says wiring it back in is a precondition
rather than an extra; this confirms it is still true at the time of writing.
Note that hive already owns hook installation — `bus.InstallClaudeHook`
(`bus/install.go:197`) rewrites `~/.claude/settings.json` idempotently on every
TUI start — so hive is the natural place for both the wiring and the Layer 4
"assert every hook resolves" check.
