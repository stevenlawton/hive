package bus

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The snapshot a Claude session leaves behind keeps colouring its pane until
// something deletes it, and only the SessionEnd hook knows the session is over.
// Installing it here means it arrives with the next hive restart rather than
// needing a separate step nobody would run.
func TestInstallClaudeHookInstallsTheSessionEndHook(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if err := InstallClaudeHook("/opt/hive/hive"); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(home, ".claude", "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var settings struct {
		Hooks map[string][]struct {
			Matcher string `json:"matcher"`
			Hooks   []struct {
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatal(err)
	}

	var got []string
	for _, entry := range settings.Hooks["SessionEnd"] {
		for _, h := range entry.Hooks {
			got = append(got, h.Command)
		}
	}
	want := "/opt/hive/hive session-end"
	if len(got) != 1 || got[0] != want {
		t.Fatalf("SessionEnd hooks = %v, want exactly [%q]", got, want)
	}
}

// Installing runs on every hive start, so a second pass must update the binary
// path in place rather than stack up another copy of the hook.
func TestInstallClaudeHookSessionEndIsIdempotent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if err := InstallClaudeHook("/old/path/hive"); err != nil {
		t.Fatal(err)
	}
	if err := InstallClaudeHook("/new/path/hive"); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(home, ".claude", "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(data), "session-end"); n != 1 {
		t.Errorf("session-end appears %d times, want 1:\n%s", n, data)
	}
	if strings.Contains(string(data), "/old/path/hive session-end") {
		t.Error("the old binary path survived a re-install")
	}
}
