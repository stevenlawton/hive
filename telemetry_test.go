package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
	if !strings.Contains(reason, "quota") {
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

func TestResumeCostMatchesObservation(t *testing.T) {
	// Measured: a 102,441-token resume cost $1.05. Predicted at 2x input.
	got, ok := resumeCostUSD("claude-opus-5", 102441)
	if !ok {
		t.Fatal("opus 5 should be priced")
	}
	if got < 0.95 || got > 1.15 {
		t.Errorf("resumeCostUSD = %.2f, want ~1.02 (observed 1.05)", got)
	}
	if _, ok := resumeCostUSD("some-future-model", 100000); ok {
		t.Error("an unpriced model must report not-ok rather than guess")
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

func TestRenderTelemetrySuffix(t *testing.T) {
	s := SessionSnapshot{CtxPct: 22.8, CostUSD: 25.07, Verdict: VerdictKeepGoing,
		Reason: "context 23% — turns ≈4.6× a fresh session"}
	got := renderTelemetrySuffix(s, false)
	for _, want := range []string{"keep going", "23%", "$25.07"} {
		if !strings.Contains(got, want) {
			t.Errorf("render = %q, want it to contain %q", got, want)
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
	if got := payloadTmuxSession(p); got != "hive-split-2" {
		t.Errorf("got %q, want hive-split-2", got)
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
