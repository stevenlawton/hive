package bus

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// DetectSender returns the sender id to stamp on outgoing announcements.
//
// Priority:
//  1. $HIVE_SENDER env var — explicit override, always wins
//  2. tmux session name (if in tmux) — stable for the life of the session
//  3. basename of the working directory — fallback for non-tmux shells
func DetectSender() string {
	if v := os.Getenv("HIVE_SENDER"); v != "" {
		return v
	}

	if os.Getenv("TMUX") != "" {
		if out, err := exec.Command("tmux", "display-message", "-p", "#S").Output(); err == nil {
			name := strings.TrimSpace(string(out))
			if name != "" {
				// hive-foo / hive-rc-foo / hive-scratch-001 → wt:foo
				name = strings.TrimPrefix(name, "hive-")
				name = strings.TrimPrefix(name, "rc-")
				return "wt:" + name
			}
		}
	}

	if cwd, err := os.Getwd(); err == nil {
		return "wt:" + filepath.Base(cwd)
	}
	return "wt:unknown"
}

// SeenKey returns the key to use when looking up the seen cursor for the
// current session. Uses tmux session name when available, cwd otherwise.
func SeenKey() string {
	if os.Getenv("TMUX") != "" {
		if out, err := exec.Command("tmux", "display-message", "-p", "#S").Output(); err == nil {
			name := strings.TrimSpace(string(out))
			if name != "" {
				return name
			}
		}
	}
	if cwd, err := os.Getwd(); err == nil {
		return cwd
	}
	return "unknown"
}
