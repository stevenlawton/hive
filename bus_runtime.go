package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/stevenlawton/hive/bus"
)

// busRuntime owns the lifecycle of the background bus watcher and the
// claude-p responder fleet. It's created once per Hive process and knows
// how to build a Peer list from the current model state.
type busRuntime struct {
	bus     *bus.Bus
	hiveBin string

	mu       sync.Mutex
	watcher  *bus.Watcher
	cancel   context.CancelFunc
	peerFn   func() []bus.Peer
	inFlight map[string]bool // peer name → responder currently running
}

func newBusRuntime(b *bus.Bus) *busRuntime {
	exe, _ := os.Executable()
	return &busRuntime{
		bus:      b,
		hiveBin:  exe,
		inFlight: make(map[string]bool),
	}
}

// SetPeerSource installs the callback used to list currently-active
// worktrees. It's a late-binding because the model is constructed after
// the runtime in newModel().
func (r *busRuntime) SetPeerSource(fn func() []bus.Peer) {
	r.mu.Lock()
	r.peerFn = fn
	r.mu.Unlock()
}

// Start kicks off the watcher goroutine. Safe to call multiple times.
func (r *busRuntime) Start() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.bus == nil || r.watcher != nil {
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	r.watcher = &bus.Watcher{
		Bus: r.bus,
		Peers: func() []bus.Peer {
			r.mu.Lock()
			fn := r.peerFn
			r.mu.Unlock()
			if fn == nil {
				return nil
			}
			return fn()
		},
		OnMessage: r.handleNewMessage,
	}
	r.watcher.Start(ctx)
}

// Stop halts the watcher and any in-flight responders.
func (r *busRuntime) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cancel != nil {
		r.cancel()
		r.cancel = nil
	}
	if r.watcher != nil {
		r.watcher.Stop()
		r.watcher = nil
	}
}

// handleNewMessage is called by the watcher for every new announcement.
// For each active peer (excluding the sender), it spawns a one-shot
// `claude -p` responder. Loop guard: replies don't trigger responders.
func (r *busRuntime) handleNewMessage(ctx context.Context, msg bus.Announcement, peers []bus.Peer) {
	// Skip replies — they're responses, not triggers for another round of
	// responders. Without this guard we'd get reply chains forever.
	if msg.ReplyTo != "" {
		return
	}

	for _, peer := range peers {
		// Don't respond to your own message.
		if peer.Name == msg.From {
			continue
		}

		// Coalesce: if a responder for this peer is already running, skip
		// this message. Claude has the full bus history anyway, so the
		// current responder can choose to look at more than one message.
		r.mu.Lock()
		if r.inFlight[peer.Name] {
			r.mu.Unlock()
			continue
		}
		r.inFlight[peer.Name] = true
		r.mu.Unlock()

		go r.runResponder(ctx, peer, msg)
	}
}

func (r *busRuntime) runResponder(ctx context.Context, peer bus.Peer, msg bus.Announcement) {
	defer func() {
		r.mu.Lock()
		delete(r.inFlight, peer.Name)
		r.mu.Unlock()
	}()

	err := bus.Respond(ctx, bus.RespondOptions{
		Peer:    peer,
		Trigger: msg,
		HiveBin: r.hiveBin,
	})
	if err != nil {
		// Soft failure — log to stderr so the TUI isn't disrupted.
		fmt.Fprintf(os.Stderr, "bus responder [%s → %s]: %v\n", peer.Name, msg.ID[:12], err)
	}
}

// peerFromRepo converts a repoItem into a bus.Peer if it has an active
// session and a usable path. Returns zero Peer + false if not eligible.
func peerFromRepo(item repoItem) (bus.Peer, bool) {
	if item.tmuxSes == "" || item.repo.Path == "" {
		return bus.Peer{}, false
	}
	// Derive a readable sender name from the tmux session, matching what
	// DetectSender would produce if invoked inside that session.
	name := item.tmuxSes
	name = strings.TrimPrefix(name, "hive-")
	name = strings.TrimPrefix(name, "rc-")
	return bus.Peer{
		Name: "wt:" + name,
		Path: item.repo.Path,
	}, true
}
