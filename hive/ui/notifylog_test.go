package ui

import (
	"testing"
	"time"
)

func TestNotifyLogAdd(t *testing.T) {
	log := NewNotifyLog(50)
	log.Add("SW", "completed", time.Now())
	log.Add("PB", "crashed", time.Now())

	if len(log.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(log.Entries))
	}
	if log.Entries[0].Repo != "PB" {
		t.Errorf("expected newest first, got %s", log.Entries[0].Repo)
	}
}

func TestNotifyLogMaxEntries(t *testing.T) {
	log := NewNotifyLog(3)
	for i := 0; i < 5; i++ {
		log.Add("repo", "event", time.Now())
	}
	if len(log.Entries) != 3 {
		t.Errorf("expected max 3 entries, got %d", len(log.Entries))
	}
}

func TestTimeAgo(t *testing.T) {
	result := TimeAgo(time.Now().Add(-2 * time.Minute))
	if result != "2m ago" {
		t.Errorf("expected '2m ago', got '%s'", result)
	}

	result = TimeAgo(time.Now().Add(-90 * time.Second))
	if result != "1m ago" {
		t.Errorf("expected '1m ago', got '%s'", result)
	}

	result = TimeAgo(time.Now().Add(-30 * time.Second))
	if result != "just now" {
		t.Errorf("expected 'just now', got '%s'", result)
	}

	result = TimeAgo(time.Now().Add(-2 * time.Hour))
	if result != "2h ago" {
		t.Errorf("expected '2h ago', got '%s'", result)
	}
}
