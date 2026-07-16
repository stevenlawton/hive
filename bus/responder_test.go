package bus

import (
	"context"
	"path/filepath"
	"testing"
)

// A worktree directory can be deleted out from under a still-live peer (e.g.
// the tmux session outlives the worktree). When that happens the responder
// must NOT try to chdir into the missing directory — doing so makes the
// spawned `claude -p` die with "chdir <path>: no such file or directory" on
// every single bus announcement. Instead, Respond should quietly skip.
func TestRespondSkipsMissingWorktree(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "deleted-worktree")

	err := Respond(context.Background(), RespondOptions{
		Peer: Peer{Name: "wt:gone", Path: missing},
		Trigger: Announcement{
			ID:       "msgdeadbeef00",
			From:     "wt:someone",
			Headline: "hello",
		},
		// A bogus binary: if the guard is missing and we reach exec, the run
		// fails and Respond returns a non-nil error, failing this test.
		ClaudeBin: "/nonexistent/claude-binary",
	})
	if err != nil {
		t.Fatalf("Respond on a deleted worktree should be a no-op, got error: %v", err)
	}
}
