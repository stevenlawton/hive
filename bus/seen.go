package bus

import (
	"os"
	"path/filepath"
	"strings"
)

// SeenStore tracks the last-seen message id per listener, so `hive bus inbox`
// can print only messages that arrived since the listener last checked.
//
// Storage: one file per listener at ~/.config/hive/seen/<key>.txt, containing
// just the last-seen message id.
type SeenStore struct {
	dir string
}

// OpenSeenStore returns a SeenStore rooted at ~/.config/hive/seen/.
func OpenSeenStore() (*SeenStore, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(home, ".config", "hive", "seen")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &SeenStore{dir: dir}, nil
}

// Get returns the last-seen id for `key`, or "" if none recorded.
func (s *SeenStore) Get(key string) string {
	data, err := os.ReadFile(s.path(key))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// Set records `id` as the last-seen message for `key`.
func (s *SeenStore) Set(key, id string) error {
	return os.WriteFile(s.path(key), []byte(id), 0o644)
}

func (s *SeenStore) path(key string) string {
	// Sanitize for filesystem safety
	safe := strings.NewReplacer("/", "_", ":", "_", " ", "_", "..", "_").Replace(key)
	return filepath.Join(s.dir, safe+".txt")
}
