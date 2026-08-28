package main

import (
	"os"
	"path/filepath"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

// CannedPrompt is one entry in the canned-response menu: a short label for the
// list and the text sent to the session.
type CannedPrompt struct {
	Label string `yaml:"label"`
	Text  string `yaml:"text"`
}

// CannedStore holds the canned prompts, backed by ~/.config/hive/canned.yaml.
//
// Prompts() re-reads the file on every call so hand-edits land without
// restarting hive; the menu opens rarely enough that the read costs nothing.
type CannedStore struct {
	dir string
	mu  sync.Mutex
}

type cannedFile struct {
	Prompts []CannedPrompt `yaml:"prompts"`
}

// OpenCannedStore returns a store rooted at ~/.config/hive/.
func OpenCannedStore() (*CannedStore, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(home, ".config", "hive")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return newCannedStore(dir), nil
}

func newCannedStore(dir string) *CannedStore {
	return &CannedStore{dir: dir}
}

// Prompts returns the current list. A missing file is seeded with the defaults
// so there is always something to edit; an unreadable one yields the defaults
// without touching what is on disk.
func (s *CannedStore) Prompts() []CannedPrompt {
	if s == nil {
		return cannedDefaults()
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.path())
	if err != nil {
		defaults := cannedDefaults()
		s.write(defaults)
		return defaults
	}
	var file cannedFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return cannedDefaults()
	}
	prompts := cleanCannedPrompts(file.Prompts)
	if len(prompts) == 0 {
		return cannedDefaults()
	}
	return prompts
}

// Save replaces the stored list.
func (s *CannedStore) Save(prompts []CannedPrompt) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.write(cleanCannedPrompts(prompts))
}

func (s *CannedStore) write(prompts []CannedPrompt) error {
	data, err := yaml.Marshal(cannedFile{Prompts: prompts})
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.dir, "canned-*.yaml")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), s.path())
}

func (s *CannedStore) path() string {
	return filepath.Join(s.dir, "canned.yaml")
}

// cleanCannedPrompts drops entries with no text and flattens each one to a
// single line. A newline in the text would submit the prompt half-typed, since
// sending it is a literal keystroke stream into Claude's input box.
func cleanCannedPrompts(in []CannedPrompt) []CannedPrompt {
	out := make([]CannedPrompt, 0, len(in))
	for _, p := range in {
		p.Text = flattenLines(p.Text)
		p.Label = flattenLines(p.Label)
		if p.Text == "" {
			continue
		}
		if p.Label == "" {
			p.Label = p.Text
		}
		out = append(out, p)
	}
	return out
}

func flattenLines(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func cannedDefaults() []CannedPrompt {
	return []CannedPrompt{
		{Label: "continue", Text: "continue"},
		{Label: "yes", Text: "yes, go ahead"},
		{Label: "tests", Text: "run the tests and fix whatever fails"},
		{Label: "commit", Text: "commit this with a sensible message"},
		{Label: "where are you", Text: "stop and summarise where you have got to"},
		{Label: "explain", Text: "explain what you just did and why"},
		{Label: "simplify", Text: "simplify what you just wrote, without changing behaviour"},
		{Label: "self-review", Text: "review your own diff for bugs before going further"},
		{Label: "check the bus", Text: "check the hive bus for anything relevant to your work"},
		{Label: "merge", Text: "merge to main once the suite is green"},
	}
}
