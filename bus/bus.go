// Package bus implements Hive's inter-session message bus.
//
// The bus is a shared append-only log that any participant (a Claude session
// in a worktree, or the human via the Hive UI) can read and write. Listeners
// self-filter on headlines — the bus itself does no routing.
//
// Storage is a JSONL file at ~/.config/hive/bus.jsonl. Simple, zero-dep,
// survives restarts, works across worktrees via the shared filesystem.
package bus

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// Bus is the high-level message bus facade. Wrap a Store to gain ID
// generation, sender stamping, and a tidy API.
type Bus struct {
	store *Store
	// Self is this participant's sender ID (e.g. "steve" for the human, or
	// "wt:backend-auth" for a Claude session in that worktree).
	Self string
}

// New creates a Bus over the given store, identifying this participant as
// `self`.
func New(store *Store, self string) *Bus {
	return &Bus{store: store, Self: self}
}

// Open is a convenience: opens the default store and wraps it in a Bus.
func Open(self string) (*Bus, error) {
	store, err := OpenStore(DefaultPath())
	if err != nil {
		return nil, err
	}
	return New(store, self), nil
}

// Announce stamps and appends a new message. The caller supplies the
// message-specific fields (headline, body, etc.); the bus fills in ID, From,
// and At.
func (b *Bus) Announce(msg Announcement) (Announcement, error) {
	msg.ID = newID()
	msg.From = b.Self
	msg.At = time.Now()
	if err := b.store.Append(msg); err != nil {
		return Announcement{}, err
	}
	return msg, nil
}

// All returns every announcement in chronological order.
func (b *Bus) All() []Announcement {
	return b.store.All()
}

// Tail returns the last n announcements.
func (b *Bus) Tail(n int) []Announcement {
	return b.store.Tail(n)
}

// Find looks up an announcement by id.
func (b *Bus) Find(id string) *Announcement {
	return b.store.Find(id)
}

// Unseen returns every announcement that arrived after `cursor`. If cursor
// is empty or not found in the log, returns all announcements.
func (b *Bus) Unseen(cursor string) []Announcement {
	all := b.store.All()
	if cursor == "" {
		return all
	}
	for i, msg := range all {
		if msg.ID == cursor {
			return all[i+1:]
		}
	}
	// Cursor not found — treat as "no history", return everything.
	return all
}

// LatestID returns the id of the most recent message, or "" if empty.
func (b *Bus) LatestID() string {
	all := b.store.All()
	if len(all) == 0 {
		return ""
	}
	return all[len(all)-1].ID
}

// newID generates a short, url-safe, roughly-sortable message id.
func newID() string {
	var buf [6]byte
	_, _ = rand.Read(buf[:])
	return fmt.Sprintf("msg_%x%s", time.Now().Unix(), hex.EncodeToString(buf[:]))
}
