package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
)

// claudeSessionMeta is the on-disk schema in ~/.claude/sessions/*.json.
// Only the fields we use are listed.
type claudeSessionMeta struct {
	PID       int    `json:"pid"`
	SessionID string `json:"sessionId"`
	CWD       string `json:"cwd"`
	Kind      string `json:"kind"` // "interactive" for human-driven sessions
}

// jsonlEvent is a single line in a session JSONL. Only the fields we need.
type jsonlEvent struct {
	Type    string `json:"type"`
	Message *struct {
		StopReason string `json:"stop_reason"`
		Content    []struct {
			Type string `json:"type"`
			Name string `json:"name"`
		} `json:"content"`
	} `json:"message,omitempty"`
}

// deriveEventsFromJSONL reads new content from offset to EOF, parses JSON
// lines, and returns the SessionEvents that should be emitted plus the new
// byte offset. Lines that don't map to a recognised state are skipped.
// Truncated files are restarted from offset 0.
func deriveEventsFromJSONL(path string, offset int64, repo, ses string) ([]SessionEvent, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, offset, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, offset, err
	}
	if info.Size() < offset {
		offset = 0
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return nil, offset, err
	}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)

	var events []SessionEvent
	var consumed int64
	for scanner.Scan() {
		line := scanner.Bytes()
		consumed += int64(len(line)) + 1 // +1 for the stripped newline
		if ev, ok := parseJSONLLine(line, repo, ses); ok {
			events = append(events, ev)
		}
	}
	if err := scanner.Err(); err != nil {
		return events, offset, err
	}
	return events, offset + consumed, nil
}

// parseJSONLLine maps one JSONL line to a SessionEvent. Returns ok=false for
// metadata entries (last-prompt, attachment, queue-operation, etc.) and
// anything that isn't a state-bearing message.
func parseJSONLLine(line []byte, repo, ses string) (SessionEvent, bool) {
	var ev jsonlEvent
	if err := json.Unmarshal(line, &ev); err != nil {
		return SessionEvent{}, false
	}
	out := SessionEvent{Repo: repo, Session: ses}
	switch ev.Type {
	case "assistant":
		if ev.Message == nil {
			return SessionEvent{}, false
		}
		switch ev.Message.StopReason {
		case "end_turn":
			out.Event = "completed"
			return out, true
		case "tool_use":
			out.Event = "tool"
			for _, c := range ev.Message.Content {
				if c.Type == "tool_use" {
					out.ToolName = c.Name
					break
				}
			}
			return out, true
		}
	case "user":
		// Both real user prompts and wrapped tool_result responses arrive
		// as "user" entries; both mean claude is about to do work, which
		// resets the wait state — handleSessionEvent treats started/tool
		// the same way downstream.
		out.Event = "started"
		return out, true
	}
	return SessionEvent{}, false
}

// readJSONLTail returns the most recent state-bearing SessionEvent in a file
// by reading the last tailBytes bytes and scanning forward through whole
// lines. Used at watcher startup to bootstrap initial state from existing
// JSONLs without replaying the entire history into the event channel.
// Returns ok=false when no recognisable event was found in the tail window.
func readJSONLTail(path string, repo, ses string) (SessionEvent, int64, bool) {
	const tailBytes = 32 * 1024

	f, err := os.Open(path)
	if err != nil {
		return SessionEvent{}, 0, false
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return SessionEvent{}, 0, false
	}
	size := info.Size()
	start := size - tailBytes
	if start < 0 {
		start = 0
	}
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return SessionEvent{}, size, false
	}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)

	// Skip the (probably partial) first line when we didn't start at 0.
	first := start > 0
	var last SessionEvent
	var found bool
	for scanner.Scan() {
		if first {
			first = false
			continue
		}
		line := scanner.Bytes()
		if ev, ok := parseJSONLLine(line, repo, ses); ok {
			last = ev
			found = true
		}
	}
	return last, size, found
}

// listClaudeSessions scans ~/.claude/sessions/*.json and returns metadata for
// every live, human-driven interactive session. PIDs that aren't running are
// filtered out, as are tmux remote-control sessions (claudes inside
// hive-rc-* / kl-rc-*), which are not user-facing.
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
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var m claudeSessionMeta
		if err := json.Unmarshal(data, &m); err != nil {
			continue
		}
		if m.Kind != "interactive" || m.PID == 0 || m.SessionID == "" {
			continue
		}
		if !pidAlive(m.PID) {
			continue
		}
		out = append(out, m)
	}
	return out
}

func pidAlive(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// On unix, signal 0 doesn't deliver but errors if the process is gone.
	return p.Signal(syscall.Signal(0)) == nil
}

// encodeProjectDir maps a cwd to claude code's project-dir naming convention,
// e.g. /home/steve/repos/workspace → -home-steve-repos-workspace.
func encodeProjectDir(cwd string) string {
	return strings.ReplaceAll(cwd, "/", "-")
}

// jsonlPathFor returns the JSONL path claude code writes for a given cwd +
// session id.
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

// SessionWatcher uses fsnotify to stream JSONL appends from every live claude
// session into the shared eventChan as SessionEvent values. It watches each
// JSONL for Write events and each parent project dir for Create events so
// new sessions are picked up without a restart. Per-file byte offsets keep
// re-reads cheap.
type SessionWatcher struct {
	fsw     *fsnotify.Watcher
	emit    func(SessionEvent)
	mu      sync.Mutex
	files   map[string]*watchedFile // jsonl path → state
	repoDir map[string]bool         // project dirs currently watched
}

type watchedFile struct {
	cwd       string
	sessionID string
	repo      string
	ses       string // tmux session name, e.g. hive-workspace
	offset    int64
}

func startSessionWatcher() error {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("fsnotify watcher: %w", err)
	}
	w := &SessionWatcher{
		fsw:     fsw,
		emit:    func(ev SessionEvent) { initEventChan() <- ev },
		files:   make(map[string]*watchedFile),
		repoDir: make(map[string]bool),
	}
	if err := w.bootstrap(); err != nil {
		return err
	}
	go w.run()
	go w.rediscoverLoop()
	return nil
}

// bootstrap discovers existing live sessions, reads each JSONL's tail to seed
// initial state into the event channel, and registers watches.
func (w *SessionWatcher) bootstrap() error {
	for _, m := range listClaudeSessions() {
		w.track(m)
	}
	return nil
}

func (w *SessionWatcher) track(m claudeSessionMeta) {
	path := jsonlPathFor(m.CWD, m.SessionID)
	w.mu.Lock()
	if _, ok := w.files[path]; ok {
		w.mu.Unlock()
		return
	}
	repo := repoFromCWD(m.CWD)
	ses := TmuxSessionName(repo, false)
	wf := &watchedFile{
		cwd:       m.CWD,
		sessionID: m.SessionID,
		repo:      repo,
		ses:       ses,
	}
	w.files[path] = wf

	projectDir := filepath.Dir(path)
	addProjectDir := !w.repoDir[projectDir]
	if addProjectDir {
		w.repoDir[projectDir] = true
	}
	w.mu.Unlock()

	// Seed initial state from the existing tail so the UI doesn't wait for
	// the next claude write to know whether this session is currently
	// waiting or working.
	if ev, offset, ok := readJSONLTail(path, repo, ses); ok {
		wf.offset = offset
		w.emit(ev)
	} else {
		if info, err := os.Stat(path); err == nil {
			wf.offset = info.Size()
		}
	}

	if addProjectDir {
		_ = w.fsw.Add(projectDir)
	}
	_ = w.fsw.Add(path)
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
	// File-level writes (most common): drain new content.
	if ev.Op&fsnotify.Write != 0 {
		w.drain(ev.Name)
		return
	}
	// New file in a project dir: a fresh session has started.
	if ev.Op&fsnotify.Create != 0 && strings.HasSuffix(ev.Name, ".jsonl") {
		w.adoptNewFile(ev.Name)
		return
	}
	if ev.Op&(fsnotify.Remove|fsnotify.Rename) != 0 {
		w.mu.Lock()
		delete(w.files, ev.Name)
		w.mu.Unlock()
		_ = w.fsw.Remove(ev.Name)
	}
}

// drain reads new content appended to a watched JSONL since the last offset
// and emits any SessionEvents.
func (w *SessionWatcher) drain(path string) {
	w.mu.Lock()
	wf, ok := w.files[path]
	w.mu.Unlock()
	if !ok {
		return
	}
	events, newOffset, err := deriveEventsFromJSONL(path, wf.offset, wf.repo, wf.ses)
	if err != nil && !os.IsNotExist(err) {
		return
	}
	w.mu.Lock()
	wf.offset = newOffset
	w.mu.Unlock()
	for _, ev := range events {
		w.emit(ev)
	}
}

// adoptNewFile is called when a project dir gains a new JSONL — typically a
// freshly-started claude session. We resolve the cwd from the parent dir,
// match it against ~/.claude/sessions/*.json to confirm liveness, and start
// tracking.
func (w *SessionWatcher) adoptNewFile(path string) {
	parent := filepath.Dir(path)
	// Decode the project dir back into a cwd path. The encoding strips
	// leading slash and replaces / with -, so decoding requires the original
	// path — we cross-check against listClaudeSessions().
	for _, m := range listClaudeSessions() {
		if filepath.Dir(jsonlPathFor(m.CWD, m.SessionID)) != parent {
			continue
		}
		if jsonlPathFor(m.CWD, m.SessionID) != path {
			continue
		}
		w.track(m)
		return
	}
}

// rediscoverLoop periodically re-scans ~/.claude/sessions/*.json in case a
// new session was created without a Write/Create event reaching the watcher
// (e.g. fsnotify queue overflow, or a session created before the parent
// project dir existed). Cheap — just stat + json parse a few small files.
func (w *SessionWatcher) rediscoverLoop() {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for range t.C {
		for _, m := range listClaudeSessions() {
			w.track(m)
		}
	}
}

