package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stevenlawton/hive/ui"
)

func testTelemetryConfig() TelemetryConfig {
	return TelemetryConfig{
		Enabled:           true,
		WrapupAtPct:       50,
		HandoffAtPct:      70,
		WrapupGrowth:      6,
		ColdGrowth:        5,
		ParkAtPct:         90,
		CacheTTLMinutes:   60,
		StaleAfterSeconds: 30,
		PruneAfterHours:   24,
	}
}

// A healthy session: room to spare, recently active, quota fine.
func healthySnapshot(now time.Time) SessionSnapshot {
	return SessionSnapshot{
		SessionID:      "s1",
		Model:          "claude-opus-5",
		CtxPct:         22.8,
		CtxTokens:      227793,
		OpenedAtTokens: 49485,
		CostUSD:        25.07,
		LastChangeAt:   now.Add(-1 * time.Minute),
		CapturedAt:     now,
	}
}

func TestVerdictKeepGoing(t *testing.T) {
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	v, reason := computeVerdict(healthySnapshot(now), testTelemetryConfig(), now)
	if v != VerdictKeepGoing {
		t.Fatalf("got %q, want keep_going (reason %q)", v, reason)
	}
	if !strings.Contains(reason, "23%") {
		t.Errorf("reason %q should quote the context percentage", reason)
	}
}

func TestVerdictWrapUpOnContext(t *testing.T) {
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	s := healthySnapshot(now)
	s.CtxPct = 58
	if v, reason := computeVerdict(s, testTelemetryConfig(), now); v != VerdictWrapUp {
		t.Fatalf("58%% context: got %q (%q), want wrap_up", v, reason)
	}
}

func TestVerdictHandOffOnContext(t *testing.T) {
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	s := healthySnapshot(now)
	s.CtxPct = 74
	v, reason := computeVerdict(s, testTelemetryConfig(), now)
	if v != VerdictHandOff {
		t.Fatalf("74%% context: got %q, want hand_off", v)
	}
	if !strings.Contains(reason, "compaction") {
		t.Errorf("reason %q should name the compaction cliff", reason)
	}
}

// The measured case: a session left overnight has its cache evicted, so the
// next turn re-writes the whole window. Big + cold is the strongest hand-off
// candidate there is.
func TestVerdictHandOffWhenCacheColdAndBig(t *testing.T) {
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	s := healthySnapshot(now)
	s.CtxPct = 40 // below the hand-off threshold on its own
	s.OpenedAtTokens = 40000
	s.CtxTokens = 400000 // 10x growth
	s.LastChangeAt = now.Add(-16 * time.Hour)
	v, reason := computeVerdict(s, testTelemetryConfig(), now)
	if v != VerdictHandOff {
		t.Fatalf("cold + 10x growth: got %q (%q), want hand_off", v, reason)
	}
	if !strings.Contains(reason, "cold") {
		t.Errorf("reason %q should say the cache is cold", reason)
	}
}

func TestVerdictColdButSmallKeepsGoing(t *testing.T) {
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	s := healthySnapshot(now)
	s.CtxPct = 12
	s.OpenedAtTokens = 40000
	s.CtxTokens = 48000 // 1.2x — cheap to resume, cold or not
	s.LastChangeAt = now.Add(-16 * time.Hour)
	if v, reason := computeVerdict(s, testTelemetryConfig(), now); v == VerdictHandOff {
		t.Errorf("a small cold session should not be handed off: got %q (%q)", v, reason)
	}
}

// Quota override. Waiting for a reset outlasts the cache, so a BIG session near
// the limit must hand off while it can still afford to — parking traps the work.
func TestQuotaNearLimitBigSessionHandsOffRatherThanParks(t *testing.T) {
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	s := healthySnapshot(now)
	s.CtxPct = 74
	s.CtxTokens = 555680
	s.HasFiveHour = true
	s.FiveHourPct = 94
	s.FiveHourResetsAt = now.Add(4 * time.Hour).Unix() // well past the cache TTL
	v, reason := computeVerdict(s, testTelemetryConfig(), now)
	if v != VerdictHandOff {
		t.Fatalf("big session at 94%% quota: got %q (%q), want hand_off", v, reason)
	}
	if !strings.Contains(reason, "5h") {
		t.Errorf("reason %q should name the quota as the driver", reason)
	}
}

func TestQuotaNearLimitSmallSessionParks(t *testing.T) {
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	s := healthySnapshot(now)
	s.CtxPct = 20
	s.HasFiveHour = true
	s.FiveHourPct = 94
	s.FiveHourResetsAt = now.Add(4 * time.Hour).Unix()
	v, reason := computeVerdict(s, testTelemetryConfig(), now)
	if v != VerdictPark {
		t.Fatalf("small session at 94%% quota: got %q (%q), want park", v, reason)
	}
	if !strings.Contains(reason, "resets") {
		t.Errorf("reason %q should give the reset time", reason)
	}
}

// The 5-hour window rolls, so a reset can land inside the cache TTL. When it
// does, waiting costs nothing and even a big session should just park.
func TestQuotaResetInsideCacheTTLParksEvenWhenBig(t *testing.T) {
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	s := healthySnapshot(now)
	s.CtxPct = 74
	s.CtxTokens = 555680
	s.HasFiveHour = true
	s.FiveHourPct = 94
	s.FiveHourResetsAt = now.Add(20 * time.Minute).Unix() // inside the 60m TTL
	v, reason := computeVerdict(s, testTelemetryConfig(), now)
	if v != VerdictPark {
		t.Fatalf("reset inside the TTL: got %q (%q), want park", v, reason)
	}
	if !strings.Contains(reason, "cache holds") {
		t.Errorf("reason %q should say the cache survives the wait", reason)
	}
}

func TestQuotaAbsentIsNotZero(t *testing.T) {
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	s := healthySnapshot(now)
	s.HasFiveHour = false
	s.FiveHourPct = 0
	if v, _ := computeVerdict(s, testTelemetryConfig(), now); v != VerdictKeepGoing {
		t.Errorf("a missing quota figure must not be read as 0%%: got %q", v)
	}
}

// growth is unknowable until a second snapshot exists, and for a session
// already running when this ships the baseline is simply wrong. The verdict
// must still work from ctx_pct alone, and must not quote a bogus multiple.
func TestVerdictWithoutGrowthBaseline(t *testing.T) {
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	s := healthySnapshot(now)
	s.OpenedAtTokens = 0
	v, reason := computeVerdict(s, testTelemetryConfig(), now)
	if v != VerdictKeepGoing {
		t.Fatalf("got %q, want keep_going", v)
	}
	if strings.Contains(reason, "×") {
		t.Errorf("reason %q must not quote a multiple with no baseline", reason)
	}
}

// The resume penalty is quoted as a MULTIPLE, never in currency.
//
// A price table put the cost of a real session at $194.78 when Claude's own
// total_cost_usd for it was $93.38 — 2.09x out, with the cache-read term alone
// exceeding the true total. The earlier "validation" of that table compared a
// prediction against an observation computed from the same table, which proved
// arithmetic consistency and nothing else.
//
// The RATIO survives that error where the absolute does not: it is
// write-price over read-price, so any uniform mispricing cancels.
func TestResumePenaltyIsAMultipleNotMoney(t *testing.T) {
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	s := healthySnapshot(now)
	s.CtxPct = 74
	s.CtxTokens = 555680
	s.HasFiveHour = true
	s.FiveHourPct = 94
	s.FiveHourResetsAt = now.Add(4 * time.Hour).Unix()

	v, reason := computeVerdict(s, testTelemetryConfig(), now)
	if v != VerdictHandOff {
		t.Fatalf("got %q, want hand_off", v)
	}
	if strings.Contains(reason, "$") {
		t.Errorf("reason %q must not quote money we cannot verify", reason)
	}
	if !strings.Contains(reason, "×") {
		t.Errorf("reason %q should quote the resume penalty as a multiple", reason)
	}
}

func TestRenderBar(t *testing.T) {
	cases := []struct {
		pct  float64
		want string
	}{
		{0, "░░░░░░░░░░"},
		{50, "█████░░░░░"},
		{100, "██████████"},
		{999, "██████████"}, // clamped
		{-5, "░░░░░░░░░░"},  // clamped
	}
	for _, c := range cases {
		if got := renderBar(c.pct, 10); got != c.want {
			t.Errorf("renderBar(%v) = %q, want %q", c.pct, got, c.want)
		}
	}
}

func TestDecodePayloadFull(t *testing.T) {
	raw := []byte(`{
	  "session_id":"abc","cwd":"/tmp/x","transcript_path":"/t.jsonl",
	  "model":{"id":"claude-opus-5","display_name":"Opus"},
	  "workspace":{"current_dir":"/tmp/x","project_dir":"/tmp/p"},
	  "cost":{"total_cost_usd":25.07,"total_duration_ms":1000,"total_api_duration_ms":500},
	  "context_window":{"used_percentage":22.8,"context_window_size":1000000,
	                    "current_usage":{"input_tokens":2,"cache_creation_input_tokens":1791,"cache_read_input_tokens":226000}},
	  "rate_limits":{"five_hour":{"used_percentage":81.0,"resets_at":1787685000}}
	}`)
	p, ok := decodeStatuslinePayload(raw)
	if !ok {
		t.Fatal("a well-formed payload should decode")
	}
	if p.SessionID != "abc" || p.Model.ID != "claude-opus-5" {
		t.Errorf("identity fields wrong: %+v", p)
	}
	if p.ContextWindow.UsedPercentage != 22.8 {
		t.Errorf("used_percentage = %v, want 22.8", p.ContextWindow.UsedPercentage)
	}
	if p.RateLimits.FiveHour == nil || p.RateLimits.FiveHour.UsedPercentage != 81.0 {
		t.Errorf("five_hour not decoded: %+v", p.RateLimits)
	}
	if got := p.contextTokens(); got != 227793 {
		t.Errorf("contextTokens = %d, want 227793", got)
	}
}

// Version drift: a future build that drops fields must degrade, never break.
func TestDecodePayloadMinimal(t *testing.T) {
	p, ok := decodeStatuslinePayload([]byte(`{"cwd":"/tmp/x"}`))
	if !ok {
		t.Fatal("a payload with only cwd should still decode")
	}
	if p.RateLimits.FiveHour != nil {
		t.Error("absent five_hour must stay nil, not a zero struct")
	}
	if p.contextTokens() != 0 {
		t.Error("absent context usage should be 0 tokens, not a panic")
	}
}

func TestDecodePayloadGarbage(t *testing.T) {
	if _, ok := decodeStatuslinePayload([]byte(`not json`)); ok {
		t.Error("garbage must not decode")
	}
}

func TestSnapshotRoundTripCarriesTheBaseline(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s1.json")
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)

	first := healthySnapshot(now)
	first.OpenedAtTokens = 49485
	if err := writeSnapshot(path, first); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, ok := readSnapshot(path)
	if !ok {
		t.Fatal("snapshot should read back")
	}
	if got.OpenedAtTokens != 49485 || got.CtxTokens != first.CtxTokens {
		t.Errorf("round trip lost data: %+v", got)
	}
	if _, ok := readSnapshot(filepath.Join(dir, "nope.json")); ok {
		t.Error("a missing snapshot must report not-ok")
	}
}

func TestSnapshotStaleness(t *testing.T) {
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	cfg := testTelemetryConfig()
	fresh := healthySnapshot(now)
	fresh.CapturedAt = now.Add(-5 * time.Second)
	if snapshotStale(fresh, cfg, now) {
		t.Error("a 5s-old snapshot is not stale at a 30s threshold")
	}
	old := healthySnapshot(now)
	old.CapturedAt = now.Add(-5 * time.Minute)
	if !snapshotStale(old, cfg, now) {
		t.Error("a 5m-old snapshot is stale at a 30s threshold")
	}
}

// The statusline fires on a timer whether or not the session did anything, so
// "when did we last sample" and "when did the session last move" are different
// questions. Cache staleness depends on the second one.
func TestUpdateSnapshotHoldsLastChangeWhenNothingMoved(t *testing.T) {
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	moved := now.Add(-40 * time.Minute)
	prev := SessionSnapshot{SessionID: "s1", CtxTokens: 200000, OpenedAtTokens: 40000, LastChangeAt: moved}

	var p statuslinePayload
	p.SessionID = "s1"
	p.ContextWindow.CurrentUsage.CacheReadTokens = 200000 // unchanged

	got := updateSnapshot(prev, p, "hive-x", now)
	if !got.LastChangeAt.Equal(moved) {
		t.Errorf("LastChangeAt = %v, want it held at %v", got.LastChangeAt, moved)
	}
	if !got.CapturedAt.Equal(now) {
		t.Errorf("CapturedAt = %v, want %v — sampling still happened", got.CapturedAt, now)
	}
}

func TestUpdateSnapshotBumpsLastChangeWhenTokensMove(t *testing.T) {
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	prev := SessionSnapshot{SessionID: "s1", CtxTokens: 200000, OpenedAtTokens: 40000,
		LastChangeAt: now.Add(-40 * time.Minute)}

	var p statuslinePayload
	p.SessionID = "s1"
	p.ContextWindow.CurrentUsage.CacheReadTokens = 250000

	got := updateSnapshot(prev, p, "hive-x", now)
	if !got.LastChangeAt.Equal(now) {
		t.Errorf("LastChangeAt = %v, want %v", got.LastChangeAt, now)
	}
}

func TestUpdateSnapshotKeepsTheOpeningBaseline(t *testing.T) {
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	prev := SessionSnapshot{SessionID: "s1", CtxTokens: 200000, OpenedAtTokens: 40000}

	var p statuslinePayload
	p.SessionID = "s1"
	p.ContextWindow.CurrentUsage.CacheReadTokens = 600000

	got := updateSnapshot(prev, p, "hive-x", now)
	if got.OpenedAtTokens != 40000 {
		t.Errorf("OpenedAtTokens = %d, want the original 40000", got.OpenedAtTokens)
	}
	if g := got.growth(); g < 14.9 || g > 15.1 {
		t.Errorf("growth = %.1f, want 15", g)
	}
}

func TestUpdateSnapshotFirstSightSetsTheBaseline(t *testing.T) {
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	var p statuslinePayload
	p.SessionID = "s1"
	p.ContextWindow.CurrentUsage.CacheReadTokens = 90000

	got := updateSnapshot(SessionSnapshot{}, p, "hive-x", now)
	if got.OpenedAtTokens != 90000 {
		t.Errorf("OpenedAtTokens = %d, want the current size on first sight", got.OpenedAtTokens)
	}
	if got.growth() != 1 {
		t.Errorf("growth = %v, want 1 on first sight", got.growth())
	}
}

// The verdict is carried by colour, not by a word. Spelling it out as well
// just takes width from a line that has to share space with the task subject.
func TestRenderTelemetrySuffixHasNoVerdictLabel(t *testing.T) {
	s := SessionSnapshot{CtxPct: 22.8, CostUSD: 25.07, Verdict: VerdictKeepGoing,
		Reason: "context 23% — turns ≈4.6× a fresh session"}
	got := renderTelemetrySuffix(s, false)
	for _, want := range []string{"23%", "$25.07"} {
		if !strings.Contains(got, want) {
			t.Errorf("render = %q, want it to contain %q", got, want)
		}
	}
	for _, unwanted := range []string{"keep going", "wrap up", "hand off"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("render = %q, should not spell out the verdict", got)
		}
	}
	if strings.Contains(got, "\x1b[") {
		t.Errorf("colour disabled but got escapes: %q", got)
	}
}

func TestRenderTelemetrySuffixColoured(t *testing.T) {
	s := SessionSnapshot{CtxPct: 74, Verdict: VerdictHandOff, Reason: "context 74% — compaction likely soon"}
	got := renderTelemetrySuffix(s, true)
	if !strings.Contains(got, "\x1b[") {
		t.Errorf("colour enabled but no escapes: %q", got)
	}
	if !strings.HasSuffix(got, "\x1b[0m") {
		t.Errorf("render must reset at the end or it bleeds into the rest of the line: %q", got)
	}
}

func TestRenderTelemetrySuffixStaleSaysSo(t *testing.T) {
	s := SessionSnapshot{CtxPct: 74, Verdict: VerdictHandOff, Stale: true}
	got := renderTelemetrySuffix(s, false)
	if !strings.Contains(got, "stale") {
		t.Errorf("a stale snapshot must say so rather than assert a verdict: %q", got)
	}
}

// The join key from a payload to a hive pane. Deliberately derived from fields
// the payload already carries — the statusline is on a hot path and must not
// shell out to git to work out where it is.
// The join key, against pairs observed live. hive names a worktree session
// <parent>-wt-<branch> (worktree.go:241), not after the worktree directory —
// so deriving it from the directory basename missed EVERY worktree session,
// which is every split. Tabs still matched for main checkouts by coincidence,
// because both sides derived the same wrong string there.
func TestPayloadTmuxSessionMatchesRealSessionNames(t *testing.T) {
	cases := []struct{ projectDir, want string }{
		{"/home/steve/repos/workspace/.worktrees/split-2", "hive-workspace-wt-split-2"},
		{"/home/steve/repos/workspace/.worktrees/split-3", "hive-workspace-wt-split-3"},
		{"/home/steve/repos/he-events/.worktrees/split-2", "hive-he-events-wt-split-2"},
		{"/home/steve/repos/stevenlawton.com/.worktrees/split-1", "hive-stevenlawton_com-wt-split-1"},
		{"/home/steve/repos/workspace", "hive-workspace"},
		{"/home/steve/repos/stevenlawton.com", "hive-stevenlawton_com"},
		// A sub-directory of a worktree still belongs to that worktree's session.
		{"/home/steve/repos/workspace/.worktrees/split-2/ui", "hive-workspace-wt-split-2"},
	}
	for _, c := range cases {
		var p statuslinePayload
		p.Workspace.ProjectDir = c.projectDir
		if got := payloadTmuxSession(p); got != c.want {
			t.Errorf("payloadTmuxSession(%q) = %q, want %q", c.projectDir, got, c.want)
		}
	}
}

func TestPayloadTmuxSession(t *testing.T) {
	var p statuslinePayload
	p.Workspace.ProjectDir = "/home/steve/repos/workspace"
	if got := payloadTmuxSession(p); got != "hive-workspace" {
		t.Errorf("got %q, want hive-workspace", got)
	}

	// A session running in a worktree belongs to that worktree's pane, not the
	// parent repo's — hive names those after the worktree directory.
	p.Worktree = &struct {
		Name   string `json:"name"`
		Branch string `json:"branch"`
	}{Name: "split-2"}
	if got := payloadTmuxSession(p); got != "hive-workspace" {
		t.Errorf("got %q, want the repo session — a worktree NAME alone is not the key", got)
	}

	if got := payloadTmuxSession(statuslinePayload{}); got != "" {
		t.Errorf("nothing to go on should give %q, got %q", "", got)
	}
}

func TestCollectTelemetryDisabledWritesNothing(t *testing.T) {
	cfg := testTelemetryConfig()
	cfg.Enabled = false
	var p statuslinePayload
	p.SessionID = "s1"
	if _, ok := collectTelemetry(p, cfg, time.Now()); ok {
		t.Error("disabled telemetry must not produce a snapshot")
	}
}

func TestCollectTelemetryNeedsASessionID(t *testing.T) {
	if _, ok := collectTelemetry(statuslinePayload{}, testTelemetryConfig(), time.Now()); ok {
		t.Error("a payload with no session id cannot be keyed and must be skipped")
	}
}

// On first sight the baseline IS the current size, so growth is trivially 1.0.
// Quoting that on a nearly-full session reads as "turns are as cheap as a fresh
// session", which is the opposite of the truth.
func TestGrowthClauseSuppressedWhenItSaysNothing(t *testing.T) {
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	s := healthySnapshot(now)
	s.CtxPct = 58
	s.OpenedAtTokens = 580000
	s.CtxTokens = 580000 // first sight: growth == 1.0
	_, reason := computeVerdict(s, testTelemetryConfig(), now)
	if strings.Contains(reason, "×") {
		t.Errorf("reason %q must not quote a growth of 1.0", reason)
	}
	if !strings.Contains(reason, "58%") {
		t.Errorf("reason %q should still report the context percentage", reason)
	}
}

func TestGrowthClauseKeptWhenMeaningful(t *testing.T) {
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	s := healthySnapshot(now) // 227793 / 49485 = 4.6x
	_, reason := computeVerdict(s, testTelemetryConfig(), now)
	if !strings.Contains(reason, "4.6×") {
		t.Errorf("reason %q should quote a real 4.6x growth", reason)
	}
}

// A config naming only some telemetry keys must keep the defaults for the rest.
// Silently zeroing unset thresholds would turn every session's verdict into
// hand_off, since ctx_pct >= 0 is always true.
func TestPartialTelemetryConfigKeepsDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("repos_dir: ~/repos\ntelemetry:\n  handoff_at_pct: 55\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Telemetry.HandoffAtPct != 55 {
		t.Errorf("HandoffAtPct = %v, want the configured 55", cfg.Telemetry.HandoffAtPct)
	}
	if cfg.Telemetry.WrapupAtPct != 50 {
		t.Errorf("WrapupAtPct = %v, want the default 50", cfg.Telemetry.WrapupAtPct)
	}
	if !cfg.Telemetry.Enabled {
		t.Error("Enabled should stay true when the key is not mentioned")
	}
	if cfg.Telemetry.CacheTTLMinutes != 60 {
		t.Errorf("CacheTTLMinutes = %v, want the default 60", cfg.Telemetry.CacheTTLMinutes)
	}
}

func TestReadSessionSnapshotsKeysByTmuxSession(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	cfg := testTelemetryConfig()

	fresh := SessionSnapshot{SessionID: "a", TmuxSession: "hive-workspace",
		CapturedAt: now.Add(-3 * time.Second), Verdict: VerdictHandOff}
	stale := SessionSnapshot{SessionID: "b", TmuxSession: "hive-other",
		CapturedAt: now.Add(-10 * time.Minute), Verdict: VerdictHandOff}
	if err := writeSnapshot(filepath.Join(dir, "a.json"), fresh); err != nil {
		t.Fatal(err)
	}
	if err := writeSnapshot(filepath.Join(dir, "b.json"), stale); err != nil {
		t.Fatal(err)
	}

	got := readSessionSnapshotsFrom(dir, cfg, now)
	if len(got) != 2 {
		t.Fatalf("got %d snapshots, want 2", len(got))
	}
	if got["hive-workspace"].Stale {
		t.Error("a 3s-old snapshot should not be marked stale")
	}
	if !got["hive-other"].Stale {
		t.Error("a 10m-old snapshot should be marked stale")
	}
}

// A snapshot with no tmux session cannot be attached to a pane. Keeping it
// under an empty key would let one unmanaged session claim every untagged tab.
func TestReadSessionSnapshotsSkipsUnplaceable(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	if err := writeSnapshot(filepath.Join(dir, "a.json"),
		SessionSnapshot{SessionID: "a", CapturedAt: now}); err != nil {
		t.Fatal(err)
	}
	if got := readSessionSnapshotsFrom(dir, testTelemetryConfig(), now); len(got) != 0 {
		t.Errorf("got %v, want nothing placeable", got)
	}
}

func TestReadSessionSnapshotsMissingDirIsEmpty(t *testing.T) {
	got := readSessionSnapshotsFrom(filepath.Join(t.TempDir(), "nope"), testTelemetryConfig(), time.Now())
	if len(got) != 0 {
		t.Errorf("a missing directory should read as empty, got %v", got)
	}
}

// Only a verdict that wants action earns colour. If keep_going tinted a tab,
// every tab would be tinted all the time and none of them would mean anything.
func TestTabToneForVerdict(t *testing.T) {
	cases := []struct {
		verdict string
		stale   bool
		want    ui.TabTone
	}{
		{VerdictKeepGoing, false, ui.ToneNone},
		{VerdictWrapUp, false, ui.ToneWarn},
		{VerdictHandOff, false, ui.ToneDanger},
		{VerdictPark, false, ui.ToneInfo},
		{VerdictHandOff, true, ui.ToneDanger}, // stale keeps its verdict
		{"", false, ui.ToneNone},
	}
	for _, c := range cases {
		if got := tabToneForVerdict(c.verdict, c.stale); got != c.want {
			t.Errorf("tabToneForVerdict(%q, stale=%v) = %v, want %v", c.verdict, c.stale, got, c.want)
		}
	}
}

// The join from a snapshot to a pane goes tab.ID -> TmuxSessionName -> snapshot.
// Getting it wrong would colour the wrong tab, or silently colour none, and
// neither failure is visible without running the TUI.
func TestApplySessionVerdictsColoursTheMatchingTab(t *testing.T) {
	rt := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", rt)
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)

	write := func(id, tmux, verdict string, at time.Time) {
		s := SessionSnapshot{SessionID: id, TmuxSession: tmux, Verdict: verdict, CapturedAt: at}
		if err := writeSnapshot(sessionSnapshotPath(id), s); err != nil {
			t.Fatal(err)
		}
	}
	write("a", TmuxSessionName("alpha", false), VerdictHandOff, now.Add(-2*time.Second))
	write("b", TmuxSessionName("beta", false), VerdictWrapUp, now.Add(-2*time.Second))
	write("c", TmuxSessionName("gamma", false), VerdictHandOff, now.Add(-30*time.Minute)) // stale

	wv := ui.NewWorkspaceView()
	wv.TabBar.Add("alpha", "alpha")
	wv.TabBar.Add("beta", "beta")
	wv.TabBar.Add("gamma", "gamma")
	wv.TabBar.Add("delta", "delta") // never reported

	m := model{cfg: &Config{Telemetry: testTelemetryConfig()}, workspace: wv}
	m.applySessionVerdicts(now)

	want := map[string]ui.TabTone{
		"alpha": ui.ToneDanger,
		"beta":  ui.ToneWarn,
		// gamma's snapshot is stale, and it keeps its tone. Idle is the normal
		// state of a session waiting on a human, and its context has not shrunk
		// while it waited. See TestStaleDoesNotSuppressTheTone.
		"gamma": ui.ToneDanger,
		"delta": ui.ToneNone, // no snapshot at all
	}
	for _, tab := range wv.TabBar.Tabs {
		if w, ok := want[tab.ID]; ok && tab.Tone != w {
			t.Errorf("tab %q tone = %v, want %v", tab.ID, tab.Tone, w)
		}
	}
}

func TestApplySessionVerdictsDisabledLeavesTabsAlone(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	wv := ui.NewWorkspaceView()
	wv.TabBar.Add("alpha", "alpha")
	wv.TabBar.Tabs[len(wv.TabBar.Tabs)-1].Tone = ui.ToneDanger

	cfg := testTelemetryConfig()
	cfg.Enabled = false
	m := model{cfg: &Config{Telemetry: cfg}, workspace: wv}
	m.applySessionVerdicts(time.Now())

	if wv.TabBar.Tabs[len(wv.TabBar.Tabs)-1].Tone != ui.ToneDanger {
		t.Error("disabled telemetry must not touch tab tones at all")
	}
}

// A split can show a session from a different repo than its tab, so panes key
// on the tmux session name directly rather than inheriting the tab's verdict.
func TestApplySessionVerdictsTintsPanesBySession(t *testing.T) {
	rt := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", rt)
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)

	s := SessionSnapshot{SessionID: "a", TmuxSession: "hive-beta",
		Verdict: VerdictHandOff, CapturedAt: now.Add(-2 * time.Second)}
	if err := writeSnapshot(sessionSnapshotPath("a"), s); err != nil {
		t.Fatal(err)
	}

	wv := ui.NewWorkspaceView()
	wv.TabBar.Add("alpha", "alpha")
	wt := &ui.WorkspaceTab{ID: "alpha", SplitPane: ui.NewSplitPane()}
	wt.SplitPane.AddSplit("beta", "hive-beta")
	wt.SplitPane.AddSplit("gamma", "hive-gamma")
	wv.Tabs["alpha"] = wt

	m := model{cfg: &Config{Telemetry: testTelemetryConfig()}, workspace: wv}
	m.applySessionVerdicts(now)

	if got := wt.SplitPane.Splits[0].Terminal.Tone; got != ui.ToneDanger {
		t.Errorf("pane on hive-beta tone = %v, want ToneDanger", got)
	}
	if got := wt.SplitPane.Splits[1].Terminal.Tone; got != ui.ToneNone {
		t.Errorf("pane with no snapshot tone = %v, want ToneNone", got)
	}
	// The tab itself never reported, so it stays untinted even though a split
	// inside it is in trouble.
	for _, tab := range wv.TabBar.Tabs {
		if tab.ID == "alpha" && tab.Tone != ui.ToneNone {
			t.Errorf("tab alpha tone = %v, want ToneNone", tab.Tone)
		}
	}
}

func TestTmuxWindowStyleArgs(t *testing.T) {
	got := tmuxWindowStyleArgs("hive-x", "bg=#2a1416")
	want := []string{"set-option", "-t", "=hive-x", "window-style", "bg=#2a1416"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
	// Clearing must go through the same verb: a tint left behind outlives the
	// reason for it, and there is no other exit path that would remove it.
	clear := tmuxWindowStyleArgs("hive-x", "default")
	if clear[len(clear)-1] != "default" {
		t.Errorf("clear args = %v, want them to end in default", clear)
	}
}

func TestVerdictWindowStyle(t *testing.T) {
	if s := verdictWindowStyle(VerdictKeepGoing, false); s != "default" {
		t.Errorf("keep_going = %q, want default (no wash for the common case)", s)
	}
	if s := verdictWindowStyle(VerdictHandOff, false); s == "default" {
		t.Error("hand_off should tint")
	}
	if s := verdictWindowStyle(VerdictHandOff, true); s != "default" {
		t.Errorf("a stale verdict must not tint: %q", s)
	}
}

// The 5-hour window is account-wide: every session reports the same figure and
// they all drain one bucket. So it renders once, not once per pane.
func TestFleetRateLimitStatus(t *testing.T) {
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	cfg := testTelemetryConfig()
	cfg.RateLimitFloorPct = 60

	quiet := map[string]SessionSnapshot{
		"a": {HasFiveHour: true, FiveHourPct: 22},
		"b": {HasFiveHour: true, FiveHourPct: 22},
	}
	if got := fleetRateLimitStatus(quiet, cfg, now); got != "" {
		t.Errorf("below the floor should say nothing, got %q", got)
	}

	busy := map[string]SessionSnapshot{
		"a": {HasFiveHour: true, FiveHourPct: 22, CapturedAt: now.Add(-9 * time.Second)},
		"b": {HasFiveHour: true, FiveHourPct: 94, CapturedAt: now.Add(-2 * time.Second),
			FiveHourResetsAt: now.Add(80 * time.Minute).Unix()},
	}
	got := fleetRateLimitStatus(busy, cfg, now)
	if !strings.Contains(got, "94%") {
		t.Errorf("got %q, want the freshest reading (94%%), not the stalest", got)
	}
	if !strings.Contains(got, "resets") {
		t.Errorf("got %q, want the reset time — it is a real deadline", got)
	}

	if got := fleetRateLimitStatus(map[string]SessionSnapshot{}, cfg, now); got != "" {
		t.Errorf("no sessions should say nothing, got %q", got)
	}

	// Absent means unknown, never zero.
	none := map[string]SessionSnapshot{"a": {HasFiveHour: false}}
	if got := fleetRateLimitStatus(none, cfg, now); got != "" {
		t.Errorf("a missing quota figure must not render as a reading, got %q", got)
	}
}

// The telemetry half must not depend on the todo half. A repo with an empty
// backlog used to return before anything rendered, which would have hidden the
// verdict exactly where there is no task text to look at instead.
func TestStatuslineJoin(t *testing.T) {
	cases := []struct {
		name, todo, tel, want string
	}{
		{"both", "2/9", "███░░ 38% · $29.14", "2/9 · ███░░ 38% · $29.14"},
		{"no backlog", "", "███░░ 38% · $29.14", "███░░ 38% · $29.14"},
		{"telemetry off", "2/9", "", "2/9"},
		{"neither", "", "", ""},
	}
	for _, c := range cases {
		if got := joinStatusline(c.todo, c.tel); got != c.want {
			t.Errorf("%s: joinStatusline(%q, %q) = %q, want %q", c.name, c.todo, c.tel, got, c.want)
		}
	}
}

// Two Claude sessions in one directory produce the same tmux session name, so
// snapshots collide. Last-writer-wins let an empty session mask a heavy one:
// observed live, a 59%/$102 session hidden behind a 0%/$0 one in the same dir.
// The worst verdict must win, or the signal can be silently swallowed.
func TestReadSessionSnapshotsWorstVerdictWinsACollision(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	cfg := testTelemetryConfig()

	heavy := SessionSnapshot{SessionID: "heavy", TmuxSession: "hive-workspace",
		CtxPct: 59, CostUSD: 102.23, Verdict: VerdictWrapUp, CapturedAt: now.Add(-519 * time.Second)}
	empty := SessionSnapshot{SessionID: "empty", TmuxSession: "hive-workspace",
		CtxPct: 0, Verdict: VerdictKeepGoing, CapturedAt: now.Add(-504 * time.Second)} // fresher
	if err := writeSnapshot(filepath.Join(dir, "heavy.json"), heavy); err != nil {
		t.Fatal(err)
	}
	if err := writeSnapshot(filepath.Join(dir, "empty.json"), empty); err != nil {
		t.Fatal(err)
	}

	got := readSessionSnapshotsFrom(dir, cfg, now)
	if got["hive-workspace"].Verdict != VerdictWrapUp {
		t.Errorf("collision resolved to %q (%.0f%%), want the wrap_up session",
			got["hive-workspace"].Verdict, got["hive-workspace"].CtxPct)
	}
}

func TestVerdictSeverityOrdering(t *testing.T) {
	order := []string{VerdictKeepGoing, VerdictPark, VerdictWrapUp, VerdictHandOff}
	for i := 1; i < len(order); i++ {
		if verdictSeverity(order[i]) <= verdictSeverity(order[i-1]) {
			t.Errorf("%s should outrank %s", order[i], order[i-1])
		}
	}
	if verdictSeverity("") != 0 {
		t.Error("an unknown verdict must rank lowest, not highest")
	}
}

// Context and cost do not decay while a session sits idle, so a stale
// snapshot's verdict is still TRUE — the session is merely quiet. Statuslines
// refresh on activity, not on a timer, so going quiet is normal and must not
// strip the colour off the sessions most worth flagging.
func TestStaleDoesNotSuppressTheTone(t *testing.T) {
	if got := tabToneForVerdict(VerdictHandOff, true); got != ui.ToneDanger {
		t.Errorf("stale hand_off tone = %v, want ToneDanger — idle+big is the strongest candidate", got)
	}
	if got := tabToneForVerdict(VerdictWrapUp, true); got != ui.ToneWarn {
		t.Errorf("stale wrap_up tone = %v, want ToneWarn", got)
	}
	if got := tabToneForVerdict(VerdictKeepGoing, true); got != ui.ToneNone {
		t.Errorf("stale keep_going tone = %v, want ToneNone", got)
	}
}

func sampleAt(min int, cost float64, base time.Time) costSample {
	return costSample{At: base.Add(time.Duration(min) * time.Minute), CostUSD: cost}
}

func TestBurnRate(t *testing.T) {
	base := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	s := SessionSnapshot{CostSamples: []costSample{
		sampleAt(0, 10.00, base), sampleAt(30, 13.00, base), sampleAt(60, 16.00, base),
	}}
	got, ok := burnRateUSDPerHour(s)
	if !ok {
		t.Fatal("three samples over an hour should give a rate")
	}
	if got < 5.9 || got > 6.1 {
		t.Errorf("burn rate = %.2f, want ~6.00/h", got)
	}
}

// The statusline fires on activity, so two samples can land milliseconds apart.
// Dividing by that span would report an absurd rate.
func TestBurnRateNeedsASpan(t *testing.T) {
	base := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	s := SessionSnapshot{CostSamples: []costSample{
		{At: base, CostUSD: 10}, {At: base.Add(2 * time.Second), CostUSD: 10.5},
	}}
	if r, ok := burnRateUSDPerHour(s); ok {
		t.Errorf("a 2s span should not yield a rate, got %.2f/h", r)
	}
	if _, ok := burnRateUSDPerHour(SessionSnapshot{}); ok {
		t.Error("no samples should not yield a rate")
	}
}

// Cost only rises. A drop means the session restarted and its counter reset, so
// the old samples describe a different run and would give a negative rate.
func TestBurnRateIgnoresACounterReset(t *testing.T) {
	base := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	s := SessionSnapshot{CostSamples: []costSample{
		sampleAt(0, 40.00, base), sampleAt(30, 2.00, base),
	}}
	if r, ok := burnRateUSDPerHour(s); ok {
		t.Errorf("a cost decrease should not yield a rate, got %.2f/h", r)
	}
}

func TestAppendCostSampleKeepsAWindow(t *testing.T) {
	base := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	var got []costSample
	for i := 0; i < costSampleWindow+5; i++ {
		got = appendCostSample(got, float64(i), base.Add(time.Duration(i)*time.Minute))
	}
	if len(got) != costSampleWindow {
		t.Fatalf("kept %d samples, want the window of %d", len(got), costSampleWindow)
	}
	if got[len(got)-1].CostUSD != float64(costSampleWindow+4) {
		t.Errorf("newest sample is %v, want the last one appended", got[len(got)-1].CostUSD)
	}
}

func TestFleetBurnRateSumsLiveSessions(t *testing.T) {
	base := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	mk := func(a, b float64) SessionSnapshot {
		return SessionSnapshot{CostSamples: []costSample{sampleAt(0, a, base), sampleAt(60, b, base)}}
	}
	snaps := map[string]SessionSnapshot{
		"one":  mk(10, 16),                                        // 6/h
		"two":  mk(5, 7),                                          // 2/h
		"idle": {CostSamples: []costSample{sampleAt(0, 3, base)}}, // no span, contributes nothing
	}
	got, n := fleetBurnRateUSDPerHour(snaps)
	if got < 7.9 || got > 8.1 {
		t.Errorf("fleet burn = %.2f/h, want ~8.00", got)
	}
	if n != 2 {
		t.Errorf("counted %d contributing sessions, want 2", n)
	}
}

func TestFleetStatusShowsBurnAndQuota(t *testing.T) {
	base := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	cfg := testTelemetryConfig()
	cfg.RateLimitFloorPct = 60
	snaps := map[string]SessionSnapshot{
		"a": {CostSamples: []costSample{sampleAt(0, 10, base), sampleAt(60, 16, base)},
			HasFiveHour: true, FiveHourPct: 94, FiveHourResetsAt: base.Add(80 * time.Minute).Unix(),
			CapturedAt: base},
		"b": {CostSamples: []costSample{sampleAt(0, 5, base), sampleAt(60, 7, base)}, CapturedAt: base},
	}
	got := fleetStatus(snaps, cfg, base.Add(60*time.Minute))
	if !strings.Contains(got, "8.00/h") {
		t.Errorf("got %q, want the summed burn rate", got)
	}
	if !strings.Contains(got, "5h 94%") {
		t.Errorf("got %q, want the quota", got)
	}

	// Burn alone, when the quota is quiet or absent.
	quiet := map[string]SessionSnapshot{
		"a": {CostSamples: []costSample{sampleAt(0, 10, base), sampleAt(60, 16, base)}, CapturedAt: base},
	}
	g2 := fleetStatus(quiet, cfg, base.Add(60*time.Minute))
	if !strings.Contains(g2, "6.00/h") {
		t.Errorf("got %q, want a burn rate with no quota reading", g2)
	}
	if strings.Contains(g2, "5h") {
		t.Errorf("got %q, should not invent a quota figure", g2)
	}

	if got := fleetStatus(map[string]SessionSnapshot{}, cfg, base); got != "" {
		t.Errorf("nothing to report should be empty, got %q", got)
	}
}

func TestStatuslineShowsSessionBurnRate(t *testing.T) {
	base := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	s := SessionSnapshot{CtxPct: 38, CostUSD: 29.14, Verdict: VerdictKeepGoing,
		CostSamples: []costSample{sampleAt(0, 26.14, base), sampleAt(60, 29.14, base)}}
	got := renderTelemetrySuffix(s, false)
	if !strings.Contains(got, "3.00/h") {
		t.Errorf("render = %q, want this session's burn rate", got)
	}
}

// Measured on this box: a session sat at 46% context — under every threshold —
// while being the most expensive session running, at $217. Context percentage
// describes the window as it stands. It says nothing about how many times that
// window has been re-read, and that is where the money goes.
func TestVerdictHandsOffOnCostAloneAtModestContext(t *testing.T) {
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	cfg := testTelemetryConfig()
	cfg.WrapupAtCostUSD, cfg.HandoffAtCostUSD = 50, 120

	s := healthySnapshot(now)
	s.CtxPct, s.CtxTokens = 46, 460000
	s.CostUSD = 216.72

	v, reason := computeVerdict(s, cfg, now)
	if v != VerdictHandOff {
		t.Fatalf("$217 at 46%% context: got %q (%q), want hand_off", v, reason)
	}
	if !strings.Contains(reason, "217") {
		t.Errorf("reason %q should name the spend that triggered it", reason)
	}
}

func TestVerdictWrapsUpOnCostBeforeContextBites(t *testing.T) {
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	cfg := testTelemetryConfig()
	cfg.WrapupAtCostUSD, cfg.HandoffAtCostUSD = 50, 120

	s := healthySnapshot(now)
	s.CtxPct, s.CtxTokens = 43, 430000
	s.CostUSD = 74.41

	if v, reason := computeVerdict(s, cfg, now); v != VerdictWrapUp {
		t.Fatalf("$74 at 43%% context: got %q (%q), want wrap_up", v, reason)
	}
}

// A delegating session spends real money with an almost-empty window — the
// shape ticket svl describes. Cost sees it where context cannot.
func TestVerdictSeesADelegatingSessionsSpend(t *testing.T) {
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	cfg := testTelemetryConfig()
	cfg.WrapupAtCostUSD, cfg.HandoffAtCostUSD = 50, 120

	s := healthySnapshot(now)
	s.CtxPct, s.CtxTokens = 11, 110000
	s.CostUSD = 136.89

	if v, reason := computeVerdict(s, cfg, now); v != VerdictHandOff {
		t.Fatalf("$137 at 11%% context: got %q (%q), want hand_off", v, reason)
	}
}

// Unset thresholds leave every existing verdict exactly as it was, so a config
// written before these keys existed is never silently reclassified.
func TestVerdictIgnoresCostWhenThresholdsUnset(t *testing.T) {
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	s := healthySnapshot(now)
	s.CostUSD = 5000

	if v, reason := computeVerdict(s, testTelemetryConfig(), now); v != VerdictKeepGoing {
		t.Fatalf("cost thresholds unset: got %q (%q), want keep_going", v, reason)
	}
}
