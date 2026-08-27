package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/stevenlawton/hive/ui"
)

// Session telemetry answers one question per session: hand the work off, or
// keep going. Everything here feeds that verdict — see
// docs/superpowers/specs/2026-08-27-session-telemetry-design.md.
//
// The rules are ratios rather than money wherever they can be, so nothing goes
// stale when prices move. The one exception is the resume estimate, which has
// to be a real figure because it is what decides the action.

const (
	VerdictKeepGoing = "keep_going"
	VerdictWrapUp    = "wrap_up"
	VerdictHandOff   = "hand_off"
	VerdictPark      = "park"
)

type TelemetryConfig struct {
	Enabled           bool    `yaml:"enabled"`
	WrapupAtPct       float64 `yaml:"wrapup_at_pct"`
	HandoffAtPct      float64 `yaml:"handoff_at_pct"`
	WrapupGrowth      float64 `yaml:"wrapup_growth"`
	ColdGrowth        float64 `yaml:"cold_growth"`
	ParkAtPct         float64 `yaml:"park_at_pct"`
	CacheTTLMinutes   int     `yaml:"cache_ttl_minutes"`
	RateLimitFloorPct float64 `yaml:"rate_limit_floor_pct"`
	StaleAfterSeconds int     `yaml:"stale_after_seconds"`
	PruneAfterHours   int     `yaml:"prune_after_hours"`
}

func defaultTelemetryConfig() TelemetryConfig {
	return TelemetryConfig{
		Enabled:           true,
		WrapupAtPct:       50,
		HandoffAtPct:      70,
		WrapupGrowth:      6,
		ColdGrowth:        5,
		ParkAtPct:         90,
		RateLimitFloorPct: 60,
		CacheTTLMinutes:   60,
		StaleAfterSeconds: 30,
		PruneAfterHours:   24,
	}
}

// SessionSnapshot is the latest known state of one Claude session. Verdict and
// Reason are stored rather than recomputed per frame so the statusline and the
// TUI cannot disagree.
type SessionSnapshot struct {
	SessionID   string    `json:"session_id"`
	CapturedAt  time.Time `json:"captured_at"`
	TmuxSession string    `json:"tmux_session,omitempty"`
	ProjectDir  string    `json:"project_dir,omitempty"`
	Model       string    `json:"model,omitempty"`

	CtxPct         float64 `json:"ctx_pct"`
	CtxTokens      int     `json:"ctx_tokens"`
	OpenedAtTokens int     `json:"opened_at_tokens"`
	Growth         float64 `json:"growth,omitempty"`
	CostUSD        float64 `json:"cost_usd"`

	HasFiveHour      bool    `json:"has_five_hour"`
	FiveHourPct      float64 `json:"five_hour_pct,omitempty"`
	FiveHourResetsAt int64   `json:"five_hour_resets_at,omitempty"`

	// LastChangeAt is when the session was last observed to *do* something,
	// not when it was last sampled. The statusline fires on a timer whether or
	// not anything happened, so CapturedAt cannot tell you if a cache has gone
	// cold; a change in token count can.
	LastChangeAt time.Time `json:"last_change_at"`

	Verdict string `json:"verdict"`
	Reason  string `json:"reason"`

	// Stale is set by the reader, not the writer: it depends on when you look.
	Stale bool `json:"-"`
}

// growth is how many times a fresh session's per-turn input cost this session
// now pays. Per-turn cost scales with context size, so the ratio of context
// sizes is the ratio of costs. Zero means no baseline is known yet.
func (s SessionSnapshot) growth() float64 {
	if s.OpenedAtTokens <= 0 || s.CtxTokens <= 0 {
		return 0
	}
	return float64(s.CtxTokens) / float64(s.OpenedAtTokens)
}

// ---------------------------------------------------------------- the verdict

func computeVerdict(s SessionSnapshot, cfg TelemetryConfig, now time.Time) (string, string) {
	verdict, reason := perSessionVerdict(s, cfg, now)
	if v, r, ok := quotaOverride(s, cfg, now); ok {
		return v, r
	}
	return verdict, reason
}

func perSessionVerdict(s SessionSnapshot, cfg TelemetryConfig, now time.Time) (string, string) {
	g := s.growth()

	if s.CtxPct >= cfg.HandoffAtPct {
		return VerdictHandOff, fmt.Sprintf("context %.0f%% — compaction likely soon", s.CtxPct)
	}
	// A session left past the cache TTL has had its window evicted: the next
	// turn re-writes the lot at the 2x price instead of reading it at 0.1x.
	// Only worth acting on when the window is big enough for that to hurt.
	if cacheCold(s, cfg, now) && g >= cfg.ColdGrowth {
		return VerdictHandOff, fmt.Sprintf("idle %s — cache likely cold, next turn ≈20× normal",
			roundDuration(now.Sub(s.LastChangeAt)))
	}
	if s.CtxPct >= cfg.WrapupAtPct {
		return VerdictWrapUp, fmt.Sprintf("context %.0f%%%s", s.CtxPct, growthClause(g))
	}
	if g >= cfg.WrapupGrowth {
		return VerdictWrapUp, fmt.Sprintf("turns ≈%.1f× a fresh session", g)
	}
	return VerdictKeepGoing, fmt.Sprintf("context %.0f%%%s", s.CtxPct, growthClause(g))
}

// quotaOverride applies the five-hour window, which is account-wide rather than
// per-session and so outranks whatever the session's own numbers said.
//
// It is conditional. Waiting for a reset normally outlasts the cache TTL, so
// resuming means re-writing the whole window — and that toll is paid out of the
// freshly reset quota. A handoff also costs tokens to rebuild context
// elsewhere, so a big session that waits until the quota is gone can afford
// neither move. Hand off while there is still quota to do it with.
func quotaOverride(s SessionSnapshot, cfg TelemetryConfig, now time.Time) (string, string, bool) {
	if !s.HasFiveHour || s.FiveHourPct < cfg.ParkAtPct {
		return "", "", false
	}
	head := fmt.Sprintf("5h quota %.0f%%", s.FiveHourPct)
	clock := ""
	resetsIn := time.Duration(0)
	if s.FiveHourResetsAt > 0 {
		reset := time.Unix(s.FiveHourResetsAt, 0)
		resetsIn = reset.Sub(now)
		clock = " — resets " + reset.Local().Format("15:04")
	}

	// The window rolls, so a reset can land inside the cache TTL. When it does,
	// waiting costs nothing at all and even a big session should just wait.
	if resetsIn > 0 && resetsIn < time.Duration(cfg.CacheTTLMinutes)*time.Minute {
		return VerdictPark, head + clock + ", cache holds", true
	}

	cost, priced := resumeCostUSD(s.Model, s.CtxTokens)
	if s.CtxPct < cfg.HandoffAtPct {
		r := head + clock
		if priced {
			r += fmt.Sprintf(", ~$%.2f to resume", cost)
		}
		return VerdictPark, r, true
	}
	r := head + " — hand off NOW"
	if priced {
		r += fmt.Sprintf(", $%.2f to resume later", cost)
	}
	return VerdictHandOff, r, true
}

func cacheCold(s SessionSnapshot, cfg TelemetryConfig, now time.Time) bool {
	if s.LastChangeAt.IsZero() {
		return false
	}
	return now.Sub(s.LastChangeAt) > time.Duration(cfg.CacheTTLMinutes)*time.Minute
}

// growthMeaningful is the point below which the multiple is not worth saying.
// On first sight the baseline is the current size, making growth exactly 1.0,
// and a session hive meets mid-life has a baseline that is simply too high — so
// small multiples are noise at best and misleading at worst.
const growthMeaningful = 1.2

// growthClause is empty when no baseline exists or the multiple says nothing.
// Quoting "turns ≈1.0× a fresh session" beside a nearly-full context reads as
// reassurance, which is the opposite of the truth.
func growthClause(g float64) string {
	if g < growthMeaningful {
		return ""
	}
	return fmt.Sprintf(" — turns ≈%.1f× a fresh session", g)
}

func roundDuration(d time.Duration) string {
	switch {
	case d >= time.Hour:
		return fmt.Sprintf("%.0fh", d.Hours())
	case d >= time.Minute:
		return fmt.Sprintf("%.0fm", d.Minutes())
	default:
		return fmt.Sprintf("%.0fs", d.Seconds())
	}
}

// ------------------------------------------------------------- resume pricing

// inputPricePerMTok is the only place money enters. Cache writes cost 2x input
// at the 1-hour TTL, and a resume after the cache has gone means the entire
// window is re-written at that rate with nothing read back.
//
// Prefix-matched so a dated or suffixed model id still prices. An unknown model
// reports not-ok rather than guessing — a wrong figure here would drive the
// wrong action.
var inputPricePerMTok = []struct {
	prefix string
	usd    float64
}{
	{"claude-fable-5", 10.0},
	{"claude-mythos-5", 10.0},
	{"claude-opus-5", 5.0},
	{"claude-opus-4", 5.0},
	{"claude-sonnet-5", 2.0},
	{"claude-sonnet-4-6", 3.0},
	{"claude-haiku-4-5", 1.0},
}

const cacheWrite1hMultiple = 2.0

func resumeCostUSD(model string, tokens int) (float64, bool) {
	if tokens <= 0 {
		return 0, false
	}
	for _, p := range inputPricePerMTok {
		if strings.HasPrefix(model, p.prefix) {
			return float64(tokens) * p.usd * cacheWrite1hMultiple / 1e6, true
		}
	}
	return 0, false
}

// ----------------------------------------------------------------- rendering

func renderBar(pct float64, width int) string {
	if width <= 0 {
		return ""
	}
	switch {
	case pct < 0:
		pct = 0
	case pct > 100:
		pct = 100
	}
	filled := int(pct/100*float64(width) + 0.5)
	return strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
}

// ------------------------------------------------------------------- payload

// statuslinePayload is what Claude Code pipes to the statusLine command. Every
// field is optional by design: a future build that renames or drops one must
// degrade, never break the line.
type statuslinePayload struct {
	SessionID      string `json:"session_id"`
	Cwd            string `json:"cwd"`
	TranscriptPath string `json:"transcript_path"`
	SessionName    string `json:"session_name"`
	Model          struct {
		ID          string `json:"id"`
		DisplayName string `json:"display_name"`
	} `json:"model"`
	Workspace struct {
		CurrentDir string `json:"current_dir"`
		ProjectDir string `json:"project_dir"`
	} `json:"workspace"`
	Cost struct {
		TotalCostUSD     float64 `json:"total_cost_usd"`
		TotalDurationMs  int64   `json:"total_duration_ms"`
		TotalAPIDuration int64   `json:"total_api_duration_ms"`
	} `json:"cost"`
	ContextWindow struct {
		UsedPercentage    float64 `json:"used_percentage"`
		ContextWindowSize int     `json:"context_window_size"`
		CurrentUsage      struct {
			InputTokens         int `json:"input_tokens"`
			CacheCreationTokens int `json:"cache_creation_input_tokens"`
			CacheReadTokens     int `json:"cache_read_input_tokens"`
			OutputTokens        int `json:"output_tokens"`
		} `json:"current_usage"`
	} `json:"context_window"`
	RateLimits struct {
		// Pointers, not values: absent means unknown, never zero. A missing
		// quota figure read as 0% would say "plenty left" when nothing is known.
		FiveHour *rateLimitWindow `json:"five_hour"`
		SevenDay *rateLimitWindow `json:"seven_day"`
	} `json:"rate_limits"`
	Worktree *struct {
		Name   string `json:"name"`
		Branch string `json:"branch"`
	} `json:"worktree"`
}

type rateLimitWindow struct {
	UsedPercentage float64 `json:"used_percentage"`
	ResetsAt       int64   `json:"resets_at"`
}

// contextTokens is what the window actually holds right now: everything the
// last request read or wrote, cache included.
func (p statuslinePayload) contextTokens() int {
	u := p.ContextWindow.CurrentUsage
	return u.InputTokens + u.CacheCreationTokens + u.CacheReadTokens
}

func decodeStatuslinePayload(b []byte) (statuslinePayload, bool) {
	var p statuslinePayload
	if err := json.Unmarshal(b, &p); err != nil {
		return statuslinePayload{}, false
	}
	return p, true
}

// ------------------------------------------------------------------- storage

// sessionSnapshotDir is runtime state, not data: a snapshot of how full a
// session's context is has no meaning after a reboot, and putting it here means
// the OS clears it for us.
func sessionSnapshotDir() string {
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "hive", "sessions")
}

func sessionSnapshotPath(sessionID string) string {
	return filepath.Join(sessionSnapshotDir(), slugify(sessionID)+".json")
}

func writeSnapshot(path string, s SessionSnapshot) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func readSnapshot(path string) (SessionSnapshot, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return SessionSnapshot{}, false
	}
	var s SessionSnapshot
	if err := json.Unmarshal(b, &s); err != nil {
		return SessionSnapshot{}, false
	}
	return s, true
}

// snapshotStale reports whether a session has stopped reporting. A stale
// verdict is worse than none: "hand off" on a session that finished hours ago
// sends you somewhere pointless.
func snapshotStale(s SessionSnapshot, cfg TelemetryConfig, now time.Time) bool {
	if s.CapturedAt.IsZero() {
		return true
	}
	return now.Sub(s.CapturedAt) > time.Duration(cfg.StaleAfterSeconds)*time.Second
}

// ----------------------------------------------------------------- collecting

// updateSnapshot folds a fresh payload into what was already known.
//
// Two things can only be known across samples rather than within one. The
// opening context size is the baseline for growth, so it is carried forward and
// set only on first sight. And LastChangeAt tracks when the session last *moved*
// — the statusline fires on a timer whether or not anything happened, so a
// sample is not evidence of activity, but a change in token count is.
func updateSnapshot(prev SessionSnapshot, p statuslinePayload, tmuxSession string, now time.Time) SessionSnapshot {
	s := SessionSnapshot{
		SessionID:   p.SessionID,
		CapturedAt:  now,
		TmuxSession: tmuxSession,
		ProjectDir:  p.Workspace.ProjectDir,
		Model:       p.Model.ID,
		CtxPct:      p.ContextWindow.UsedPercentage,
		CtxTokens:   p.contextTokens(),
		CostUSD:     p.Cost.TotalCostUSD,
	}
	if s.SessionID == "" {
		s.SessionID = prev.SessionID
	}
	if s.ProjectDir == "" {
		s.ProjectDir = p.Cwd
	}

	if prev.OpenedAtTokens > 0 {
		s.OpenedAtTokens = prev.OpenedAtTokens
	} else {
		s.OpenedAtTokens = s.CtxTokens
	}

	if prev.CtxTokens == s.CtxTokens && !prev.LastChangeAt.IsZero() {
		s.LastChangeAt = prev.LastChangeAt
	} else {
		s.LastChangeAt = now
	}

	if fh := p.RateLimits.FiveHour; fh != nil {
		s.HasFiveHour = true
		s.FiveHourPct = fh.UsedPercentage
		s.FiveHourResetsAt = fh.ResetsAt
	}

	s.Growth = s.growth()
	return s
}

// ------------------------------------------------------------------ rendering

const (
	colourKeepGoing = "[38;5;108m"
	colourWrapUp    = "[38;5;179m"
	colourHandOff   = "[38;5;167m"
	colourPark      = "[38;5;110m"
	colourStale     = "[38;5;244m"
	colourReset     = "[0m"
)

func verdictLabel(v string) string {
	switch v {
	case VerdictWrapUp:
		return "wrap up"
	case VerdictHandOff:
		return "hand off"
	case VerdictPark:
		return "park"
	default:
		return "keep going"
	}
}

func verdictColour(v string) string {
	switch v {
	case VerdictWrapUp:
		return colourWrapUp
	case VerdictHandOff:
		return colourHandOff
	case VerdictPark:
		return colourPark
	default:
		return colourKeepGoing
	}
}

// reasonTail drops a leading "context NN% — " from a reason, because the bar
// and the percentage beside it have already said that much.
func reasonTail(reason string) string {
	if !strings.HasPrefix(reason, "context ") {
		return reason
	}
	if i := strings.Index(reason, "— "); i >= 0 {
		return reason[i+len("— "):]
	}
	return ""
}

// renderTelemetrySuffix is the part appended to the statusline. A stale
// snapshot reports staleness instead of a verdict: an assertion about a session
// that stopped reporting an hour ago is worse than no assertion.
func renderTelemetrySuffix(s SessionSnapshot, colour bool) string {
	if s.Stale {
		out := "· ○ stale"
		if colour {
			return colourStale + out + colourReset
		}
		return out
	}

	out := "· ● " + verdictLabel(s.Verdict) +
		" · " + renderBar(s.CtxPct, 10) +
		fmt.Sprintf(" %.0f%%", s.CtxPct)

	if s.Verdict == VerdictKeepGoing {
		if s.CostUSD > 0 {
			out += fmt.Sprintf(" · $%.2f", s.CostUSD)
		}
	} else if tail := reasonTail(s.Reason); tail != "" {
		out += " · " + tail
	}

	if colour {
		return verdictColour(s.Verdict) + out + colourReset
	}
	return out
}

// payloadTmuxSession names the hive pane this session is running in, so the TUI
// never has to work it out backwards. Derived from fields the payload already
// carries: the statusline runs on every refresh and must not shell out to git.
//
// A session inside a worktree belongs to that worktree's pane — hive names
// those after the worktree directory, not the parent repo.
func payloadTmuxSession(p statuslinePayload) string {
	name := ""
	switch {
	case p.Worktree != nil && p.Worktree.Name != "":
		name = p.Worktree.Name
	case p.Workspace.ProjectDir != "":
		name = filepath.Base(p.Workspace.ProjectDir)
	case p.Cwd != "":
		name = filepath.Base(p.Cwd)
	}
	if name == "" || name == "." || name == string(filepath.Separator) {
		return ""
	}
	return TmuxSessionName(name, false)
}

// collectTelemetry folds the payload into this session's snapshot and persists
// it. Every failure is the caller's to ignore: the statusline must render
// whether or not telemetry worked.
func collectTelemetry(p statuslinePayload, cfg TelemetryConfig, now time.Time) (SessionSnapshot, bool) {
	if !cfg.Enabled || p.SessionID == "" {
		return SessionSnapshot{}, false
	}
	path := sessionSnapshotPath(p.SessionID)
	prev, _ := readSnapshot(path)

	s := updateSnapshot(prev, p, payloadTmuxSession(p), now)
	s.Verdict, s.Reason = computeVerdict(s, cfg, now)

	// A snapshot that could not be written still renders — the reader just
	// misses this tick, and the next one will carry the same story.
	_ = writeSnapshot(path, s)
	return s, true
}

// loadTelemetryConfig reads just the telemetry block. The statusline runs
// outside the TUI and has no config loaded, and a missing or broken config must
// leave telemetry on its defaults rather than off — silently disabling a
// feature because a yaml key was fat-fingered is the kind of failure nobody
// notices for weeks.
func loadTelemetryConfig() TelemetryConfig {
	home, err := os.UserHomeDir()
	if err != nil {
		return defaultTelemetryConfig()
	}
	cfg, err := LoadConfig(filepath.Join(home, ".config", "hive", "config.yaml"))
	if err != nil || cfg == nil {
		return defaultTelemetryConfig()
	}
	return cfg.Telemetry
}

// ------------------------------------------------------------------- the fleet

// readSessionSnapshots gives the TUI every session it can place, keyed by the
// tmux session name so a tab can find its own verdict without a reverse lookup.
func readSessionSnapshots(cfg TelemetryConfig, now time.Time) map[string]SessionSnapshot {
	return readSessionSnapshotsFrom(sessionSnapshotDir(), cfg, now)
}

func readSessionSnapshotsFrom(dir string, cfg TelemetryConfig, now time.Time) map[string]SessionSnapshot {
	out := map[string]SessionSnapshot{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		s, ok := readSnapshot(path)
		if !ok {
			continue
		}
		if cfg.PruneAfterHours > 0 && !s.CapturedAt.IsZero() &&
			now.Sub(s.CapturedAt) > time.Duration(cfg.PruneAfterHours)*time.Hour {
			_ = os.Remove(path)
			continue
		}
		// A snapshot with no tmux session cannot be attached to a pane. Filing
		// it under the empty key would let one unmanaged session claim every
		// tab that has not reported.
		if s.TmuxSession == "" {
			continue
		}
		s.Stale = snapshotStale(s, cfg, now)
		out[s.TmuxSession] = s
	}
	return out
}

// tabToneForVerdict decides what earns colour in the tab bar. Only a verdict
// asking for action does: if keep_going tinted a tab then every tab would be
// tinted all the time, and none of them would mean anything.
//
// A stale snapshot earns nothing either. Its verdict was true when the session
// was still reporting, and colouring a tab red for a session that stopped an
// hour ago sends you somewhere pointless.
func tabToneForVerdict(verdict string, stale bool) ui.TabTone {
	if stale {
		return ui.ToneNone
	}
	switch verdict {
	case VerdictWrapUp:
		return ui.ToneWarn
	case VerdictHandOff:
		return ui.ToneDanger
	case VerdictPark:
		return ui.ToneInfo
	default:
		return ui.ToneNone
	}
}

// applySessionVerdicts colours each tab by its session's verdict, so "which of
// these nine needs handing off" is answerable without opening any of them.
//
// A tab with no snapshot loses its tone rather than keeping the last one: a
// session that stopped reporting has no verdict, and holding the old colour
// would assert something nobody is still checking.
func (m model) applySessionVerdicts(now time.Time) {
	if m.cfg == nil || !m.cfg.Telemetry.Enabled || m.workspace == nil || m.workspace.TabBar == nil {
		return
	}
	snaps := readSessionSnapshots(m.cfg.Telemetry, now)
	for i := range m.workspace.TabBar.Tabs {
		tab := &m.workspace.TabBar.Tabs[i]
		s, ok := snaps[TmuxSessionName(tab.ID, false)]
		if !ok {
			tab.Tone = ui.ToneNone
			continue
		}
		tab.Tone = tabToneForVerdict(s.Verdict, s.Stale)
	}

	m.workspace.TabBar.RightStatus = fleetRateLimitStatus(snaps, m.cfg.Telemetry, now)

	// Splits key on the tmux session name directly, so a split showing another
	// repo's session gets that session's verdict rather than its tab's.
	for _, wt := range m.workspace.Tabs {
		if wt == nil || wt.SplitPane == nil {
			continue
		}
		for i := range wt.SplitPane.Splits {
			term := wt.SplitPane.Splits[i].Terminal
			if term == nil {
				continue
			}
			s, ok := snaps[term.SessionName]
			if !ok {
				term.Tone = ui.ToneNone
				continue
			}
			term.Tone = tabToneForVerdict(s.Verdict, s.Stale)
		}
	}
}

// verdictWindowStyle is the tmux window-style for a verdict, or "default" to
// clear. Deliberately dark: Claude Code paints foreground colours chosen
// against the terminal's own background, so a strong wash costs readability.
func verdictWindowStyle(verdict string, stale bool) string {
	if stale {
		return "default"
	}
	switch verdict {
	case VerdictHandOff:
		return "bg=#2a1416"
	case VerdictWrapUp:
		return "bg=#2a2410"
	case VerdictPark:
		return "bg=#141a2a"
	default:
		return "default"
	}
}

// attachWindowStyle tints a session's tmux window for the duration of a
// full-screen attach, returning a function that clears it again. The clear must
// run on every exit path — a tint left behind outlives the reason for it, and
// nothing else would ever remove it.
func (m model) attachWindowStyle(sessionName string) func() {
	if m.cfg == nil || !m.cfg.Telemetry.Enabled || sessionName == "" {
		return func() {}
	}
	snaps := readSessionSnapshots(m.cfg.Telemetry, time.Now())
	s, ok := snaps[sessionName]
	if !ok {
		return func() {}
	}
	style := verdictWindowStyle(s.Verdict, s.Stale)
	if style == "default" {
		return func() {}
	}
	if err := TmuxSetWindowStyle(sessionName, style); err != nil {
		return func() {}
	}
	return func() { _ = TmuxSetWindowStyle(sessionName, "default") }
}

// fleetRateLimitStatus renders the shared five-hour window once, for the whole
// machine. Every session reports the same account-wide figure, so this takes
// the freshest reading rather than summing or averaging nine copies of it.
//
// It stays silent below the floor: a number that is always on screen stops
// being read long before it starts mattering.
func fleetRateLimitStatus(snaps map[string]SessionSnapshot, cfg TelemetryConfig, now time.Time) string {
	var best SessionSnapshot
	found := false
	for _, s := range snaps {
		if !s.HasFiveHour {
			continue
		}
		if !found || s.CapturedAt.After(best.CapturedAt) {
			best, found = s, true
		}
	}
	if !found || best.FiveHourPct < cfg.RateLimitFloorPct {
		return ""
	}
	out := fmt.Sprintf(" 5h %.0f%%", best.FiveHourPct)
	if best.FiveHourResetsAt > 0 {
		out += " · resets " + time.Unix(best.FiveHourResetsAt, 0).Local().Format("15:04")
	}
	return out + " "
}
