package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func usageLine(ts, session, cwd string, in, out, cr, cc int64) string {
	return fmt.Sprintf(`{"type":"assistant","timestamp":%q,"sessionId":%q,"cwd":%q,`+
		`"message":{"model":"claude-opus-4-7","usage":{"input_tokens":%d,"output_tokens":%d,`+
		`"cache_read_input_tokens":%d,"cache_creation_input_tokens":%d}}}`,
		ts, session, cwd, in, out, cr, cc)
}

func TestParseTokenRecord(t *testing.T) {
	rec, ok := parseTokenRecord([]byte(usageLine(
		"2026-08-26T13:38:17.164Z", "sess-1", "/home/steve/repos/workspace", 6, 183, 26993, 12645)))
	if !ok {
		t.Fatal("expected a usage record")
	}
	if rec.Session != "sess-1" {
		t.Errorf("session: got %q want sess-1", rec.Session)
	}
	if got := rec.Usage.Total(); got != 6+183+26993+12645 {
		t.Errorf("total: got %d want %d", got, 6+183+26993+12645)
	}
	if rec.At.UTC().Format("2006-01-02T15:04:05") != "2026-08-26T13:38:17" {
		t.Errorf("timestamp not parsed: %v", rec.At)
	}
}

// A transcript is an append-only log that may be read mid-write, so every
// non-usage or malformed shape must be skipped rather than erroring.
func TestParseTokenRecord_SkipsNonUsage(t *testing.T) {
	cases := []struct {
		name, line string
	}{
		{"user turn", `{"type":"user","timestamp":"2026-08-26T13:38:17.164Z","message":{"content":"hi"}}`},
		{"no usage key", `{"type":"assistant","timestamp":"2026-08-26T13:38:17.164Z","message":{"model":"x"}}`},
		{"truncated json", `{"type":"assistant","timestamp":"2026-08-26T13:3`},
		{"empty", ``},
		{"bad timestamp", usageLine("not-a-time", "s", "/tmp", 1, 1, 1, 1)},
		{"all zero", usageLine("2026-08-26T13:38:17.164Z", "s", "/tmp", 0, 0, 0, 0)},
	}
	for _, c := range cases {
		if _, ok := parseTokenRecord([]byte(c.line)); ok {
			t.Errorf("%s: expected skip, got a record", c.name)
		}
	}
}

func TestScanTokenUsage_WindowAndTotals(t *testing.T) {
	dir := t.TempDir()
	proj := filepath.Join(dir, "-home-steve-repos-workspace")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	inWin := now.Add(-1 * time.Hour).Format(time.RFC3339)
	outWin := now.Add(-9 * time.Hour).Format(time.RFC3339)

	body := strings.Join([]string{
		usageLine(inWin, "sess-a", "/home/steve/repos/workspace", 1, 100, 1000, 10),
		usageLine(inWin, "sess-b", "/home/steve/repos/he-events", 1, 50, 500, 5),
		usageLine(outWin, "sess-c", "/home/steve/repos/workspace", 1, 9999, 99999, 999),
		`{"type":"user","timestamp":"` + inWin + `","message":{"content":"hi"}}`,
	}, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(proj, "t.jsonl"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	rep, err := scanTokenUsage(dir, now.Add(-5*time.Hour))
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if rep.Requests != 2 {
		t.Errorf("requests: got %d want 2 (the 9h-old record must fall outside the window)", rep.Requests)
	}
	if got, want := rep.Total.Output, int64(150); got != want {
		t.Errorf("output: got %d want %d", got, want)
	}
	if got, want := rep.Total.Total(), int64(1+100+1000+10+1+50+500+5); got != want {
		t.Errorf("total: got %d want %d", got, want)
	}
	if len(rep.Sessions) != 2 {
		t.Fatalf("sessions: got %d want 2", len(rep.Sessions))
	}
	// Sorted by spend, biggest first.
	if rep.Sessions[0].ID != "sess-a" {
		t.Errorf("busiest session: got %q want sess-a", rep.Sessions[0].ID)
	}
	if rep.Sessions[0].Project != "workspace" {
		t.Errorf("project label: got %q want workspace", rep.Sessions[0].Project)
	}
}

// A line larger than bufio.Scanner's default token limit must not truncate the
// scan — real transcripts carry multi-megabyte records.
func TestScanTokenUsage_HandlesHugeLines(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "p"), 0o755); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	ts := now.Add(-time.Minute).Format(time.RFC3339)
	huge := fmt.Sprintf(`{"type":"user","timestamp":%q,"pad":%q}`, ts, strings.Repeat("x", 2<<20))
	body := huge + "\n" + usageLine(ts, "s", "/tmp/x", 1, 2, 3, 4) + "\n"
	if err := os.WriteFile(filepath.Join(dir, "p", "t.jsonl"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	rep, err := scanTokenUsage(dir, now.Add(-time.Hour))
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if rep.Requests != 1 || rep.Total.Total() != 10 {
		t.Errorf("record after a 2MB line was lost: requests=%d total=%d", rep.Requests, rep.Total.Total())
	}
}

func TestScanTokenUsage_EmptyRootIsNotAnError(t *testing.T) {
	rep, err := scanTokenUsage(t.TempDir(), time.Now().Add(-5*time.Hour))
	if err != nil {
		t.Fatalf("empty root should not error: %v", err)
	}
	if rep.Requests != 0 || len(rep.Sessions) != 0 {
		t.Errorf("expected an empty report, got %+v", rep)
	}
}

func TestCommas(t *testing.T) {
	for _, c := range []struct {
		in   int64
		want string
	}{{0, "0"}, {999, "999"}, {1000, "1,000"}, {1045356849, "1,045,356,849"}, {-1234, "-1,234"}} {
		if got := commas(c.in); got != c.want {
			t.Errorf("commas(%d): got %q want %q", c.in, got, c.want)
		}
	}
}

func TestFormatTokenReport_ShowsBudgetAndRatio(t *testing.T) {
	rep := tokenReport{
		Since:    time.Now().Add(-5 * time.Hour),
		Total:    tokenUsage{Input: 100, Output: 1000, CacheRead: 160000, CacheCreate: 500},
		Requests: 3,
		Sessions: []tokenSession{{ID: "abcdef1234", Project: "workspace", Usage: tokenUsage{Output: 1000}, Requests: 3}},
	}
	out := formatTokenReport(rep, 5*time.Hour, 1000000, 6, false)
	for _, want := range []string{"161,600", "16.2% of 1,000,000", "160x output", "abcdef12", "workspace"} {
		if !strings.Contains(out, want) {
			t.Errorf("report missing %q:\n%s", want, out)
		}
	}
}
