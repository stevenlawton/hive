package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// The five-hour quota is an account-level fact: every session reports the same
// window, and when it resets they all unblock together. So the reset time is
// read from the fleet rather than from the session a prompt is parked against
// — a session blocked on the limit stops rendering its statusline and its own
// snapshot goes stale, while its siblings keep reporting the same figure.
//
// A stale snapshot, or one whose window has already rolled over, is no use:
// arming against a reset in the past would fire on the very next tick, into a
// pane that may well still be blocked.
func fleetResetAt(snaps map[string]SessionSnapshot, now time.Time) (int64, bool) {
	var best SessionSnapshot
	found := false
	for _, s := range snaps {
		if !s.HasFiveHour || s.Stale || s.FiveHourResetsAt <= 0 {
			continue
		}
		if !time.Unix(s.FiveHourResetsAt, 0).After(now) {
			continue
		}
		if !found || s.CapturedAt.After(best.CapturedAt) {
			best, found = s, true
		}
	}
	if !found {
		return 0, false
	}
	return best.FiveHourResetsAt, true
}

// FutureQueue is what one session has parked: the prompts waiting to be sent,
// and whether hive should fire them itself when the quota window rolls over.
//
// ArmedFor is the reset timestamp the queue was armed against, captured when
// the box was ticked rather than read again at firing time. A session blocked
// on the limit stops reporting, so there may be nothing live to ask by then.
type FutureQueue struct {
	Prompts    []string `yaml:"prompts,omitempty"`
	AutoSend   bool     `yaml:"auto_send,omitempty"`
	AutoResume bool     `yaml:"auto_resume,omitempty"`
	ArmedFor   int64    `yaml:"armed_for,omitempty"`

	// Draining is set once the queue has fired at the reset. Only the first
	// prompt goes then; the rest follow as the session finishes each turn, so
	// a queue of four does not land in one input box at once.
	Draining bool `yaml:"draining,omitempty"`

	// AwaitingPickup is set the moment a prompt is sent and cleared when the
	// session is next seen generating. Without it the next tick would find a
	// draining, not-yet-running session and fire the following prompt into an
	// input box the first one is still being typed into.
	AwaitingPickup bool `yaml:"awaiting_pickup,omitempty"`

	// SentAt stamps the last send, so a pickup that is never observed does not
	// strand the rest of the queue. A turn can begin and end between two ticks,
	// and a dead pane never reports at all.
	SentAt int64 `yaml:"sent_at,omitempty"`
}

// FutureStore is the parked-prompt file, ~/.config/hive/future.yaml, keyed by
// tmux session name — the same key telemetry snapshots use, so a queue can be
// matched to the pane it belongs to.
//
// auto_resume_text lives here rather than in hive's top-level config because
// that one is hand-walked key by key in decodeConfigNode; a field added to the
// struct alone decodes to nothing.
type FutureStore struct {
	dir string
	mu  sync.Mutex
}

type futureFile struct {
	ResumeText string                 `yaml:"auto_resume_text,omitempty"`
	Queues     map[string]FutureQueue `yaml:"queues,omitempty"`
}

const defaultResumeText = "resume"

// OpenFutureStore returns a store rooted at ~/.config/hive/.
func OpenFutureStore() (*FutureStore, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(home, ".config", "hive")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return newFutureStore(dir), nil
}

func newFutureStore(dir string) *FutureStore {
	return &FutureStore{dir: dir}
}

// Queues returns what is currently parked, re-read from disk each call so a
// hand-edit lands without restarting hive. A file that cannot be read yields
// nothing; callers that are about to write must use LoadQueues instead, or a
// transient read failure would replace every other session's queue.
func (s *FutureStore) Queues() map[string]FutureQueue {
	file, err := s.load()
	if err != nil {
		return map[string]FutureQueue{}
	}
	return file.Queues
}

// LoadQueues is Queues for a caller that intends to write back. It reports the
// difference between "nothing parked yet" and "the file is there but could not
// be read" — saving over the latter would silently delete the lot.
func (s *FutureStore) LoadQueues() (map[string]FutureQueue, error) {
	file, err := s.load()
	if err != nil {
		return nil, err
	}
	return file.Queues, nil
}

// ResumeText is what "auto resume" sends.
func (s *FutureStore) ResumeText() string {
	file, err := s.load()
	if err != nil {
		return defaultResumeText
	}
	if text := flattenLines(file.ResumeText); text != "" {
		return text
	}
	return defaultResumeText
}

func (s *FutureStore) load() (futureFile, error) {
	if s == nil {
		return emptyFutureFile(), nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadLocked()
}

func (s *FutureStore) loadLocked() (futureFile, error) {
	data, err := os.ReadFile(s.path())
	if os.IsNotExist(err) {
		return emptyFutureFile(), nil
	}
	if err != nil {
		return emptyFutureFile(), err
	}

	file := emptyFutureFile()
	if err := yaml.Unmarshal(data, &file); err != nil {
		return emptyFutureFile(), fmt.Errorf("parse %s: %w", s.path(), err)
	}
	if file.Queues == nil {
		file.Queues = map[string]FutureQueue{}
	}
	return file, nil
}

func emptyFutureFile() futureFile {
	return futureFile{Queues: map[string]FutureQueue{}}
}

// Save replaces the parked queues, keeping the stored resume text. The lock is
// held across the whole read-modify-write: releasing it in between would let
// an interleaved writer's resume text be silently dropped.
func (s *FutureStore) Save(queues map[string]FutureQueue) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	file, err := s.loadLocked()
	if err != nil {
		return err
	}
	file.Queues = cleanFutureQueues(queues)
	return s.write(file)
}

func (s *FutureStore) write(file futureFile) error {
	data, err := yaml.Marshal(file)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.dir, "future-*.yaml")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), s.path()); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return nil
}

func (s *FutureStore) path() string {
	return filepath.Join(s.dir, "future.yaml")
}

// cleanFutureQueues flattens each prompt to one line — a newline mid-prompt
// would submit it half-typed, since sending is a literal keystroke stream —
// and drops queues holding nothing worth restoring.
func cleanFutureQueues(in map[string]FutureQueue) map[string]FutureQueue {
	out := make(map[string]FutureQueue, len(in))
	for session, q := range in {
		prompts := make([]string, 0, len(q.Prompts))
		for _, p := range q.Prompts {
			if p = flattenLines(p); p != "" {
				prompts = append(prompts, p)
			}
		}
		q.Prompts = prompts
		if len(q.Prompts) == 0 && !q.AutoResume {
			continue
		}
		out[session] = q
	}
	return out
}

// futureFireGrace is how long after the published reset hive waits before
// typing anything. A rolling window's resets_at has been seen landing slightly
// early, and a prompt typed into a session that is still blocked is swallowed
// with nothing to retry against — so the queue would quietly do nothing. Five
// minutes is cheap insurance against that.
const futureFireGrace = 5 * time.Minute

// futureStaleArming is how long past its reset an arming stays live. Hive can
// be down when a window rolls over; coming back a day later and firing
// yesterday's notes into whatever the session is doing now helps nobody, so a
// long-dead arming is left as notes for the user to send by hand.
const futureStaleArming = 12 * time.Hour

// futurePickupWait is how long a drain waits to see the session pick up the
// last prompt before carrying on regardless.
const futurePickupWait = 2 * time.Minute

// futureDue lists the sessions whose parked queues have come due, sorted so a
// wave of them fires in a stable order.
func futureDue(queues map[string]FutureQueue, now time.Time) []string {
	var due []string
	for session, q := range queues {
		if !q.AutoSend || q.ArmedFor <= 0 {
			continue
		}
		reset := time.Unix(q.ArmedFor, 0)
		if now.Before(reset.Add(futureFireGrace)) || now.After(reset.Add(futureStaleArming)) {
			continue
		}
		due = append(due, session)
	}
	sort.Strings(due)
	return due
}

// disarmResumed spends the arming of any session that has resumed by some
// other route — most often the user typing into the pane themselves while the
// window was still closed. Once a session is generating again it is plainly
// not blocked, and firing into it later would land a prompt in the middle of
// whatever it moved on to.
//
// Only a queue still waiting on the reset is disarmed. One that is already
// draining is generating *because* hive typed into it, and cancelling that
// would strand the rest of the queue.
//
// The parked prompts survive: only the firing is cancelled, so nothing typed
// is thrown away. Auto resume has no note to keep, so it clears outright.
//
// The bool reports whether anything actually changed, so a quiet tick does not
// rewrite future.yaml.
func disarmResumed(queues map[string]FutureQueue, running map[string]bool) (map[string]FutureQueue, bool) {
	out := make(map[string]FutureQueue, len(queues))
	changed := false
	for session, q := range queues {
		if running[session] && q.ArmedFor != 0 {
			q.AutoSend = false
			q.AutoResume = false
			q.ArmedFor = 0
			changed = true
		}
		out[session] = q
	}
	return out, changed
}

// futureNext takes the next thing to send from a queue and returns what is
// left of it. Auto resume ignores any parked prompts — the popup greys the
// editor out when it is ticked — and is spent by the one send.
func futureNext(q FutureQueue, resumeText string) (string, FutureQueue, bool) {
	if q.AutoResume {
		return resumeText, FutureQueue{Prompts: q.Prompts}, true
	}
	if len(q.Prompts) == 0 {
		return "", q, false
	}

	text := q.Prompts[0]
	q.Prompts = q.Prompts[1:]
	q.ArmedFor = 0
	q.Draining = len(q.Prompts) > 0
	if !q.Draining {
		q.AutoSend = false
	}
	return text, q, true
}

// futureDrainReady lists the draining sessions that are between turns, and so
// ready for the next prompt. A session still generating keeps its turn.
func futureDrainReady(queues map[string]FutureQueue, running map[string]bool) []string {
	var ready []string
	for session, q := range queues {
		if !q.Draining || !q.AutoSend || running[session] {
			continue
		}
		ready = append(ready, session)
	}
	sort.Strings(ready)
	return ready
}

// armFuture ticks auto send, capturing the reset the queue is armed against.
// With no reset time known there is nothing to fire against, so the queue is
// left as plain notes rather than armed against a zero it would fire on
// immediately.
func armFuture(q FutureQueue, resetAt int64) FutureQueue {
	if resetAt <= 0 {
		q.AutoSend = false
		q.ArmedFor = 0
		return q
	}
	q.AutoSend = true
	q.ArmedFor = resetAt
	return q
}

// futureSend is one prompt bound for one session.
type futureSend struct {
	session string
	text    string
}

// futureWork is futureWorkFor where generating and resumed are the same set.
func futureWork(
	queues map[string]FutureQueue,
	running map[string]bool,
	resumeText string,
	now time.Time,
) (map[string]FutureQueue, []futureSend, bool) {
	return futureWorkFor(queues, running, running, resumeText, now)
}

// futureWorkFor advances every parked queue by one tick and reports what should
// be typed where. It is pure so the ordering can be tested without tmux, and
// the caller persists the queues it hands back only when the bool says
// something actually changed.
//
// The two session sets are deliberately different questions:
//
//   - generating: is claude mid-turn? This is what holds a drain back, so the
//     next prompt does not land on top of the current one.
//   - resumed: has this session demonstrably carried on without hive? That
//     needs positive evidence — generating AND reporting fresh telemetry.
//     A session stalled on the quota can still read as generating while its
//     statusline has stopped updating, and treating that as a resume would
//     disarm the queue the moment it was armed and silently never fire.
//
// Order matters. Disarming comes first, so a session that resumed by some
// other route is never fired into; only then does anything come due.
func futureWorkFor(
	queues map[string]FutureQueue,
	generating map[string]bool,
	resumed map[string]bool,
	resumeText string,
	now time.Time,
) (map[string]FutureQueue, []futureSend, bool) {
	out, changed := disarmResumed(queues, resumed)

	// A session seen generating has picked up whatever was last sent.
	for session, q := range out {
		if q.AwaitingPickup && generating[session] {
			q.AwaitingPickup = false
			out[session] = q
			changed = true
		}
	}

	var sends []futureSend
	fire := func(session string) {
		text, rest, ok := futureNext(out[session], resumeText)
		if !ok {
			rest.AutoSend = false
			rest.ArmedFor = 0
			rest.Draining = false
			out[session] = rest
			changed = true
			return
		}
		rest.AwaitingPickup = true
		rest.SentAt = now.Unix()
		out[session] = rest
		sends = append(sends, futureSend{session: session, text: text})
		changed = true
	}

	for _, session := range futureDue(out, now) {
		fire(session)
	}
	for _, session := range futureDrainReady(out, generating) {
		if q := out[session]; q.AwaitingPickup && !futurePickupLapsed(q, now) {
			continue
		}
		fire(session)
	}
	return out, sends, changed
}

// futurePickupLapsed reports whether a queue has waited long enough to stop
// expecting the session to be seen picking up the last prompt.
func futurePickupLapsed(q FutureQueue, now time.Time) bool {
	if q.SentAt <= 0 {
		return true
	}
	return now.After(time.Unix(q.SentAt, 0).Add(futurePickupWait))
}
