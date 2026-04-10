package bus

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// Store is a JSONL-backed append log of announcements. Thread-safe.
//
// The file lives at ~/.config/hive/bus.jsonl (or whatever path is passed in)
// and is written append-only. Reads scan the whole file into memory — fine for
// thousands of messages, swap for an index if we ever hit millions.
type Store struct {
	path string
	mu   sync.Mutex
	msgs []Announcement
}

// OpenStore loads the JSONL file at path (creating parent dirs as needed) and
// returns a Store ready for Append/All calls.
func OpenStore(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	s := &Store{path: path}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

// DefaultPath returns ~/.config/hive/bus.jsonl.
func DefaultPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "hive", "bus.jsonl")
}

func (s *Store) load() error {
	f, err := os.Open(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024) // allow long bodies
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var msg Announcement
		if err := json.Unmarshal(line, &msg); err != nil {
			continue // skip corrupt lines rather than refuse to load
		}
		s.msgs = append(s.msgs, msg)
	}
	return scanner.Err()
}

// Append writes a new announcement to the log and keeps it in memory.
func (s *Store) Append(msg Announcement) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := os.OpenFile(s.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(data, '\n')); err != nil {
		return err
	}
	s.msgs = append(s.msgs, msg)
	return nil
}

// All returns a copy of every announcement in chronological order.
func (s *Store) All() []Announcement {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Announcement, len(s.msgs))
	copy(out, s.msgs)
	return out
}

// Tail returns the last n announcements (or all if there are fewer than n).
func (s *Store) Tail(n int) []Announcement {
	s.mu.Lock()
	defer s.mu.Unlock()
	if n <= 0 || n >= len(s.msgs) {
		out := make([]Announcement, len(s.msgs))
		copy(out, s.msgs)
		return out
	}
	start := len(s.msgs) - n
	out := make([]Announcement, n)
	copy(out, s.msgs[start:])
	return out
}

// Find returns the announcement with the given id, or nil if not found.
func (s *Store) Find(id string) *Announcement {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.msgs {
		if s.msgs[i].ID == id {
			msg := s.msgs[i]
			return &msg
		}
	}
	return nil
}
