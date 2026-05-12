package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// claudeSessionMeta is the on-disk schema in ~/.claude/sessions/*.json.
// Only the fields we read are listed. The "status" field is the authoritative
// signal claude code writes for its current state:
//
//	"busy"  — claude is generating / using tools
//	"idle"  — claude is waiting for user input
//	""      — not an interactive session (e.g. SDK --print invocation)
type claudeSessionMeta struct {
	PID       int    `json:"pid"`
	SessionID string `json:"sessionId"`
	CWD       string `json:"cwd"`
	Kind      string `json:"kind"`   // "interactive" for human-driven sessions
	Status    string `json:"status"` // "busy", "idle", "" (missing for non-interactive)
}

// sessionStaleness is the cutoff for treating a session as currently active.
// If the session file hasn't been touched within this window the session is
// presumed idle/dead and skipped — claude updates the file's mtime
// periodically while a session is in use.
const sessionStaleness = 1 * time.Hour

// sessionCleanupAge is the minimum file age before pruneStaleSessionFiles
// will consider deleting a session file. Conservative — we never touch
// young files because a session may legitimately not have written yet.
const sessionCleanupAge = 24 * time.Hour

// listClaudeSessions returns metadata for every live human-driven interactive
// claude session, filtered to:
//   - kind == "interactive" AND a non-empty status (SDK --print invocations
//     have kind="interactive" but no status, which uniquely identifies them)
//   - /proc/<pid>/comm == "claude" (catches PID reuse to an unrelated program
//     and rejects SDK invocations whose comm is the version string)
//   - session file mtime within sessionStaleness
func listClaudeSessions() []claudeSessionMeta {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	dir := filepath.Join(home, ".claude", "sessions")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []claudeSessionMeta
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		if time.Since(info.ModTime()) > sessionStaleness {
			continue
		}
		m, ok := readSessionFile(path)
		if !ok {
			continue
		}
		if !isInteractiveClaude(m.PID) {
			continue
		}
		out = append(out, m)
	}
	return out
}

// readSessionFile parses one ~/.claude/sessions/<pid>.json. Returns ok=false
// when the file isn't a valid live interactive-session record (missing
// required fields, or no status — i.e. an SDK invocation).
func readSessionFile(path string) (claudeSessionMeta, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return claudeSessionMeta{}, false
	}
	var m claudeSessionMeta
	if err := json.Unmarshal(data, &m); err != nil {
		return claudeSessionMeta{}, false
	}
	if m.Kind != "interactive" || m.PID == 0 || m.SessionID == "" {
		return claudeSessionMeta{}, false
	}
	// SDK --print invocations have kind="interactive" but never publish a
	// status. Filter them here so the watcher only tracks real human-driven
	// sessions.
	if m.Status == "" {
		return claudeSessionMeta{}, false
	}
	return m, true
}

// isInteractiveClaude returns true iff /proc/<pid>/comm is "claude". This
// distinguishes the interactive CLI from the SDK --print mode (comm is the
// version string, e.g. "2.1.139") and from PID-reuse to an unrelated
// program. Linux-specific.
func isInteractiveClaude(pid int) bool {
	if pid <= 0 {
		return false
	}
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/comm", pid))
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(data)) == "claude"
}

// pruneStaleSessionFiles removes ~/.claude/sessions/*.json entries that are
// older than sessionCleanupAge AND demonstrably dead (no claude process at
// the PID, or the referenced JSONL doesn't exist). Never deletes young
// files — a session that started seconds ago may not have written
// anything else yet.
func pruneStaleSessionFiles() {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	dir := filepath.Join(home, ".claude", "sessions")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		if time.Since(info.ModTime()) < sessionCleanupAge {
			continue
		}
		m, ok := readSessionFile(path)
		if !ok {
			// Unparseable or non-interactive old file — safe to drop.
			_ = os.Remove(path)
			continue
		}
		alive := isInteractiveClaude(m.PID)
		_, jsonlErr := os.Stat(jsonlPathFor(m.CWD, m.SessionID))
		jsonlExists := jsonlErr == nil
		if !alive || !jsonlExists {
			_ = os.Remove(path)
		}
	}
}

// encodeProjectDir maps a cwd to claude code's project-dir naming convention.
// Both '/' and '.' become '-', e.g. /home/steve/repos/stevenlawton.com →
// -home-steve-repos-stevenlawton-com, and /home/steve/.claude/worktrees →
// -home-steve--claude-worktrees (slash+dot collapses to two dashes).
func encodeProjectDir(cwd string) string {
	return strings.NewReplacer("/", "-", ".", "-").Replace(cwd)
}

// jsonlPathFor returns the JSONL path claude code writes for a given cwd +
// session id. Used by pruneStaleSessionFiles to verify a session file points
// to a real on-disk conversation.
func jsonlPathFor(cwd, sessionID string) string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "projects", encodeProjectDir(cwd), sessionID+".jsonl")
}

// repoFromCWD extracts the repo dir name from a cwd (last path component).
// Worktrees keep their distinct dir name and so map to their own tmux
// session, which is what the downstream matching expects.
func repoFromCWD(cwd string) string {
	return filepath.Base(strings.TrimRight(cwd, "/"))
}

// SessionWatcher tracks claude's per-PID session JSON files in
// ~/.claude/sessions/. Each file carries a `status` field — "busy" or "idle"
// — that claude updates as it transitions between working and waiting for
// user input. We watch the directory for Create events (new sessions) and
// each session file for Write events (status changes), and emit
// SessionEvents on busy↔idle transitions:
//
//	idle    → "completed" (claude finished a turn, waiting for user)
//	busy    → "started"   (claude is generating again)
//
// Per-file last-status is cached so duplicate writes with the same status
// don't produce duplicate events.
type SessionWatcher struct {
	fsw   *fsnotify.Watcher
	emit  func(SessionEvent)
	mu    sync.Mutex
	files map[string]*watchedSession // session file path → state
}

type watchedSession struct {
	cwd        string
	sessionID  string
	repo       string
	ses        string // tmux session name, e.g. hive-workspace
	lastStatus string
}

func startSessionWatcher() error {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("fsnotify watcher: %w", err)
	}
	w := &SessionWatcher{
		fsw:   fsw,
		emit:  func(ev SessionEvent) { initEventChan() <- ev },
		files: make(map[string]*watchedSession),
	}
	if err := w.bootstrap(); err != nil {
		return err
	}
	go w.run()
	go w.rediscoverLoop()
	return nil
}

// sessionsDir returns ~/.claude/sessions.
func sessionsDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "sessions")
}

// bootstrap prunes stale entries, then registers a directory watch on
// ~/.claude/sessions and a per-file watch for every currently-live session,
// seeding initial state from each session's current status field.
func (w *SessionWatcher) bootstrap() error {
	pruneStaleSessionFiles()
	_ = w.fsw.Add(sessionsDir())
	for _, m := range listClaudeSessions() {
		w.track(m, true)
	}
	return nil
}

// track adds a per-file watch for one session file and emits the initial
// event reflecting its current status. Idempotent: already-tracked files
// short-circuit. `initial` is true when the session was discovered already
// in this state (bootstrap or rediscovery) rather than via a live
// transition — the emitted event is tagged so downstream handlers can
// suppress attention escalation for state the user already knows about.
func (w *SessionWatcher) track(m claudeSessionMeta, initial bool) {
	path := filepath.Join(sessionsDir(), fmt.Sprintf("%d.json", m.PID))
	w.mu.Lock()
	if _, ok := w.files[path]; ok {
		w.mu.Unlock()
		return
	}
	repo := repoFromCWD(m.CWD)
	ws := &watchedSession{
		cwd:        m.CWD,
		sessionID:  m.SessionID,
		repo:       repo,
		ses:        TmuxSessionName(repo, false),
		lastStatus: m.Status,
	}
	w.files[path] = ws
	w.mu.Unlock()

	if ev, ok := statusToEvent(m.Status, ws.repo, ws.ses); ok {
		ev.Initial = initial
		w.emit(ev)
	}
	_ = w.fsw.Add(path)
}

// statusToEvent maps a claude session status string to the SessionEvent the
// rest of hive consumes. Returns ok=false for statuses we don't translate
// (e.g. transient "starting" values, or unknown future strings).
//
// Both "waiting" and "idle" mean "claude is not generating and the next
// move is on the user" — claude code uses them somewhat interchangeably
// (likely a fresh-wait vs been-here-a-while distinction, or version skew).
// Treat them the same. Long-dormant idle sessions are kept out of the
// watch set entirely by the sessionStaleness mtime filter in
// listClaudeSessions, so they don't spuriously flash.
func statusToEvent(status, repo, ses string) (SessionEvent, bool) {
	switch status {
	case "waiting", "idle":
		return SessionEvent{Repo: repo, Session: ses, Event: "completed"}, true
	case "busy":
		return SessionEvent{Repo: repo, Session: ses, Event: "started"}, true
	}
	return SessionEvent{}, false
}

func (w *SessionWatcher) run() {
	for {
		select {
		case ev, ok := <-w.fsw.Events:
			if !ok {
				return
			}
			w.handleFSEvent(ev)
		case err, ok := <-w.fsw.Errors:
			if !ok {
				return
			}
			fmt.Fprintf(os.Stderr, "session watcher: %v\n", err)
		}
	}
}

func (w *SessionWatcher) handleFSEvent(ev fsnotify.Event) {
	// New file in ~/.claude/sessions: a session may have just started.
	if ev.Op&fsnotify.Create != 0 && strings.HasSuffix(ev.Name, ".json") {
		w.adoptNew(ev.Name)
		return
	}
	if ev.Op&fsnotify.Write != 0 {
		w.handleStatusUpdate(ev.Name)
		return
	}
	if ev.Op&(fsnotify.Remove|fsnotify.Rename) != 0 {
		// Two scenarios here:
		//  (a) Atomic rename-replace: claude rewrote the file via tmp+mv,
		//      so the path still exists with a new inode and our per-file
		//      inotify watch (on the old, now-deleted inode) has gone
		//      silent. Re-arm and process as a status write.
		//  (b) Real session end: the file is gone, claude exited.
		if _, err := os.Stat(ev.Name); err == nil {
			_ = w.fsw.Add(ev.Name)
			w.handleStatusUpdate(ev.Name)
			return
		}
		w.mu.Lock()
		ws, ok := w.files[ev.Name]
		delete(w.files, ev.Name)
		w.mu.Unlock()
		_ = w.fsw.Remove(ev.Name)
		if ok {
			w.emit(SessionEvent{Repo: ws.repo, Session: ws.ses, Event: "ended"})
		}
	}
}

// handleStatusUpdate is called when a watched session file is rewritten.
// Re-parse it, compare the status to what we last saw, and emit on
// transitions. Writes that don't change status produce no event.
func (w *SessionWatcher) handleStatusUpdate(path string) {
	w.mu.Lock()
	ws, ok := w.files[path]
	w.mu.Unlock()
	if !ok {
		return
	}
	m, ok := readSessionFile(path)
	if !ok {
		return
	}
	if m.Status == ws.lastStatus {
		return
	}
	w.mu.Lock()
	ws.lastStatus = m.Status
	w.mu.Unlock()
	if ev, ok := statusToEvent(m.Status, ws.repo, ws.ses); ok {
		w.emit(ev)
	}
}

// adoptNew handles fsnotify Create events on the sessions directory. The
// freshly-created file may not be readable / complete yet, so the actual
// tracking decision goes through readSessionFile which short-circuits if
// the file isn't a valid live interactive session. Not flagged Initial
// because a real Create event means a freshly-spawned claude — a real
// transition the user should be alerted to.
func (w *SessionWatcher) adoptNew(path string) {
	m, ok := readSessionFile(path)
	if !ok {
		return
	}
	if !isInteractiveClaude(m.PID) {
		return
	}
	w.track(m, false)
}

// rediscoverLoop periodically re-scans ~/.claude/sessions/*.json in case a
// new session was created without a fsnotify event reaching us, and runs
// the stale-file pruner so cleanup keeps happening for long-running hive
// sessions.
func (w *SessionWatcher) rediscoverLoop() {
	t := time.NewTicker(5 * time.Minute)
	defer t.Stop()
	for range t.C {
		pruneStaleSessionFiles()
		for _, m := range listClaudeSessions() {
			// rediscovery picks up sessions we missed via fsnotify (rare).
			// Treat as initial — we can't know how long the session has
			// been in its current state, so don't fire escalation now.
			w.track(m, true)
		}
	}
}
