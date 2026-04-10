package bus

import (
	"context"
	"sync"
	"time"
)

// PeerSource is a callback that returns the current list of active peers.
// It's a function (rather than a slice) because peers may change as
// worktrees open/close — the watcher re-asks on every message.
type PeerSource func() []Peer

// OnNewMessage is invoked when the watcher observes a new announcement on
// the bus that wasn't written by an already-known peer in an echo-worthy
// context. Return an error if you want it logged; nothing else is done with
// the return value.
type OnNewMessage func(ctx context.Context, msg Announcement, peers []Peer)

// Watcher polls the bus store for new messages and fires OnNewMessage once
// per new announcement. It's a polling-based implementation (no fsnotify
// dependency) at ~500ms granularity, which is fine for the bus's expected
// traffic volume.
type Watcher struct {
	Bus       *Bus
	Peers     PeerSource
	OnMessage OnNewMessage
	Interval  time.Duration

	mu       sync.Mutex
	seen     map[string]bool
	started  bool
	stopOnce sync.Once
	stop     chan struct{}
}

// Start begins the poll loop in a new goroutine. It seeds the `seen` set
// with every message currently in the store, so only announcements that
// arrive AFTER Start will trigger OnMessage.
func (w *Watcher) Start(ctx context.Context) {
	w.mu.Lock()
	if w.started {
		w.mu.Unlock()
		return
	}
	w.started = true
	w.seen = make(map[string]bool)
	w.stop = make(chan struct{})
	for _, msg := range w.Bus.All() {
		w.seen[msg.ID] = true
	}
	interval := w.Interval
	if interval == 0 {
		interval = 500 * time.Millisecond
	}
	w.mu.Unlock()

	go w.loop(ctx, interval)
}

// Stop halts the poll loop. Safe to call multiple times.
func (w *Watcher) Stop() {
	w.stopOnce.Do(func() {
		close(w.stop)
	})
}

func (w *Watcher) loop(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-w.stop:
			return
		case <-ticker.C:
			// Re-open the store each tick so we pick up writes from other
			// processes (e.g. `hive bus announce` from a Claude session).
			// A cheaper approach would be to stat the file mtime first;
			// we can optimise later if this becomes hot.
			freshStore, err := OpenStore(DefaultPath())
			if err != nil {
				continue
			}
			all := freshStore.All()
			w.process(ctx, all)
		}
	}
}

func (w *Watcher) process(ctx context.Context, all []Announcement) {
	w.mu.Lock()
	var fresh []Announcement
	for _, msg := range all {
		if w.seen[msg.ID] {
			continue
		}
		w.seen[msg.ID] = true
		fresh = append(fresh, msg)
	}
	w.mu.Unlock()

	if len(fresh) == 0 || w.OnMessage == nil {
		return
	}

	peers := []Peer{}
	if w.Peers != nil {
		peers = w.Peers()
	}

	for _, msg := range fresh {
		w.OnMessage(ctx, msg, peers)
	}
}
