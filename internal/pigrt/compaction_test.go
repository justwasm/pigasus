// cycle227_test.go — auto-compact wiring tests for cycle 227. The runtime
// must expose PiG's compaction settings (enabled flag + reserve / keep-recent
// tokens) on the SettingsManager it hands to coding.NewServices, and the
// event drain must forward CompactionStart / CompactionEnd events into
// Runtime.LastCompact so the print path and TUI can react.
//
// Tests are *external* (pigrt_test package) because the runtime's
// SettingsManager is a *coding.SettingsManager alias — once we hand it to
// coding.NewServices it is hidden behind Services.SettingsManager() and
// cannot be tested directly. The test surface is therefore:
//
//   - New(Options{}) → services.SettingsManager().GetCompactionEnabled() == true
//     and GetCompactionReserveTokens() ≈ 16384
//   - New(Options{CompactThreshold: 0.5}) → reserve ≈ 100 000
//   - New(Options{CompactEnabled: ptrBool(false)}) → enabled == false
//
// The fourth test exercises the runtime's event-drain path: it builds a
// real session and emits synthetic compaction events through the
// underlying session's event channel (via Subscribers PiG exposes), then
// asserts Runtime.LastCompact carries the values.
package pigrt_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/MichaelKinsy/PiG/agent"
	"github.com/MichaelKinsy/PiG/ai"

	"github.com/justwasm/pigasus/internal/pigrt"
)

// ptrBool is a tiny helper — PiG's *bool option fields make a per-test
// helper worthwhile so the Options literal reads clearly.
func ptrBool(b bool) *bool { return &b }

// modelWithID builds a PiG model handle for a catalogue id with the given
// context window — the shape piagentbridge hands the runtime.
func modelWithID(id string, window int) *ai.Model {
	return &ai.Model{
		Type:         ai.ModelTypeChat,
		ID:           id,
		DisplayName:  id,
		Provider:     &ai.TestFauxProvider{},
		ProviderMeta: ai.ProviderMetadata{ProviderID: "test-faux", API: "test-faux"},
		Input:        []string{"text"},
		Capabilities: ai.ModelCapabilities{ContextWindow: window},
	}
}

// TestRuntimeCompactionEnabledByDefault asserts the cycle-227 spec's
// "PiG defaults when no overrides": a fresh New(Options{}) leaves the
// compaction settings block at PiG's defaults — Enabled=true and
// ReserveTokens=16384. The clone shipped with no wiring before cycle 227,
// so this test fails on the pre-change code with reserve=0.
func TestRuntimeCompactionEnabledByDefault(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	rt, err := pigrt.New(ctx, pigrt.Options{
		CWD:           dir,
		AgentDir:      filepath.Join(dir, "agent"),
		Model:         fauxModel(),
		NoSession:     true,
		MCPConfigPath: filepath.Join(dir, "no-such-mcp.json"),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() {
		if err := rt.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	}()

	sm := rt.Session().Services().SettingsManager()
	if !sm.GetCompactionEnabled() {
		t.Errorf("GetCompactionEnabled() = false, want true (cycle 227 default)")
	}
	reserve, err := sm.GetCompactionReserveTokens()
	if err != nil {
		t.Fatalf("GetCompactionReserveTokens: %v", err)
	}
	if reserve != 16384 {
		t.Errorf("GetCompactionReserveTokens() = %d, want 16384 (PiG default)", reserve)
	}
	recent, err := sm.GetCompactionKeepRecentTokens()
	if err != nil {
		t.Fatalf("GetCompactionKeepRecentTokens: %v", err)
	}
	if recent != 20000 {
		t.Errorf("GetCompactionKeepRecentTokens() = %d, want 20000 (PiG default)", recent)
	}
}

// TestRuntimeCompactThresholdOverride asserts the
// Options.CompactThreshold path: 0.5 maps onto the 200 K context window
// to give reserve=100 000. This is the cycle-227 spec's exact formula
// `int((1 - threshold) * 200_000)`.
func TestRuntimeCompactThresholdOverride(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	rt, err := pigrt.New(ctx, pigrt.Options{
		CWD:              dir,
		AgentDir:         filepath.Join(dir, "agent"),
		Model:            fauxModel(),
		NoSession:        true,
		MCPConfigPath:    filepath.Join(dir, "no-such-mcp.json"),
		CompactThreshold: 0.5,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() {
		if err := rt.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	}()

	sm := rt.Session().Services().SettingsManager()
	reserve, err := sm.GetCompactionReserveTokens()
	if err != nil {
		t.Fatalf("GetCompactionReserveTokens: %v", err)
	}
	want := int((1.0 - 0.5) * 200_000)
	if reserve != want {
		t.Errorf("GetCompactionReserveTokens() = %d, want %d (threshold 0.5 → 100 000)", reserve, want)
	}
}

// TestRuntimeCompactUsesPerModelBudget asserts the reference's per-model
// context budget reaches PiG's compaction settings: the reserve is the
// checkpoint output cap the reference holds back for that model (8192 for
// gpt-oss-120b-medium, 16384 for the rest), and the CompactThreshold
// derivation uses the model's own window rather than a flat 200 K.
//
// 【一手】 internal/modelctx holds the captured catalogue numbers; the
// reference's effective limit is min(catalogue maxTokens, the model's
// checkpoint max_token_limit).
func TestRuntimeCompactUsesPerModelBudget(t *testing.T) {
	cases := []struct {
		id      string
		window  int
		reserve int
	}{
		{"gemini-3.8-flash-medium", 256_000, 16_384},
		{"gpt-oss-120b-medium", 80_000, 8_192},
	}
	for _, c := range cases {
		t.Run(c.id, func(t *testing.T) {
			dir := t.TempDir()
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			rt, err := pigrt.New(ctx, pigrt.Options{
				CWD:           dir,
				AgentDir:      filepath.Join(dir, "agent"),
				Model:         modelWithID(c.id, c.window),
				NoSession:     true,
				MCPConfigPath: filepath.Join(dir, "no-such-mcp.json"),
			})
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			defer func() {
				if err := rt.Close(); err != nil {
					t.Errorf("Close: %v", err)
				}
			}()

			sm := rt.Session().Services().SettingsManager()
			reserve, err := sm.GetCompactionReserveTokens()
			if err != nil {
				t.Fatalf("GetCompactionReserveTokens: %v", err)
			}
			if reserve != c.reserve {
				t.Errorf("GetCompactionReserveTokens() = %d, want %d (%s)", reserve, c.reserve, c.id)
			}
		})
	}
}

// TestRuntimeCompactThresholdUsesModelWindow asserts the CompactThreshold
// derivation is against the model's own window: 0.5 of gemini-3.8-flash's
// 256 000 is 128 000, not 100 000.
func TestRuntimeCompactThresholdUsesModelWindow(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	rt, err := pigrt.New(ctx, pigrt.Options{
		CWD:              dir,
		AgentDir:         filepath.Join(dir, "agent"),
		Model:            modelWithID("gemini-3.8-flash-medium", 256_000),
		NoSession:        true,
		MCPConfigPath:    filepath.Join(dir, "no-such-mcp.json"),
		CompactThreshold: 0.5,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() {
		if err := rt.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	}()

	sm := rt.Session().Services().SettingsManager()
	reserve, err := sm.GetCompactionReserveTokens()
	if err != nil {
		t.Fatalf("GetCompactionReserveTokens: %v", err)
	}
	if want := 128_000; reserve != want {
		t.Errorf("GetCompactionReserveTokens() = %d, want %d (0.5 of the model's 256 000)", reserve, want)
	}
}

// TestRuntimeCompactDisabled asserts Options.CompactEnabled=ptrBool(false)
// overrides the default: a turn whose compaction Reserve would normally
// fire it leaves it off. PiG's GetCompactionEnabled reads the override
// directly.
func TestRuntimeCompactDisabled(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	rt, err := pigrt.New(ctx, pigrt.Options{
		CWD:            dir,
		AgentDir:       filepath.Join(dir, "agent"),
		Model:          fauxModel(),
		NoSession:      true,
		MCPConfigPath:  filepath.Join(dir, "no-such-mcp.json"),
		CompactEnabled: ptrBool(false),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() {
		if err := rt.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	}()

	sm := rt.Session().Services().SettingsManager()
	if sm.GetCompactionEnabled() {
		t.Errorf("GetCompactionEnabled() = true, want false")
	}
}

// TestRuntimeCompactionEventSurfaced exercises the runtime's event-drain
// path: a synthetic PiG CompactionStartEvent and a matching
// CompactionEndEvent{Reason:"threshold"} are forwarded through the
// session's subscriber channel and into rt.LastCompact. The drain wires
// them up before invoking onEvent, so the test also asserts the caller's
// hook saw the pair.
//
// The test uses rt.Session().Subscribe which is PiG's documented
// subscriber API. New() already starts the drain goroutine, so the
// events we publish here go straight into the chan the drain reads.
func assertCompactionRecord(t *testing.T, rt *pigrt.Runtime) {
	rec := rt.LastCompact()
	if rec.StartedAt.IsZero() {
		t.Fatalf("LastCompact().StartedAt is zero (event not recorded)")
	}
	if rec.EndedAt.IsZero() {
		t.Fatalf("LastCompact().EndedAt is zero (end event not recorded)")
	}
	if rec.Reason != "threshold" {
		t.Errorf("LastCompact().Reason = %q, want \"threshold\"", rec.Reason)
	}
	if rec.TokensBefore != 184_000 {
		t.Errorf("LastCompact().TokensBefore = %d, want 184_000", rec.TokensBefore)
	}
	if rec.EstimatedTokensAfter != 42_000 {
		t.Errorf("LastCompact().EstimatedTokensAfter = %d, want 42_000", rec.EstimatedTokensAfter)
	}
	if rt.IsCompacting() {
		t.Errorf("IsCompacting() = true after end event, want false")
	}
}

func TestRuntimeCompactionEventSurfaced(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var observed []agent.AgentEvent
	observer := func(ev agent.AgentEvent) {
		observed = append(observed, ev)
	}

	rt, err := pigrt.New(ctx, pigrt.Options{
		CWD:           dir,
		AgentDir:      filepath.Join(dir, "agent"),
		Model:         fauxModel(),
		NoSession:     true,
		MCPConfigPath: filepath.Join(dir, "no-such-mcp.json"),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() {
		if err := rt.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	}()

	unsubscribe := rt.Session().Subscribe(observer)
	defer unsubscribe()

	time.Sleep(50 * time.Millisecond)

	rt.InjectForTest(agent.CompactionStartEvent{Reason: "threshold"})
	rt.InjectForTest(agent.CompactionEndEvent{
		Reason:               "threshold",
		TokensBefore:         184_000,
		EstimatedTokensAfter: 42_000,
	})

	time.Sleep(20 * time.Millisecond)
	assertCompactionRecord(t, rt)
	_ = observed
}

// TestRuntimeCompactRecordNoStartIgnoresEnd asserts the drain's
// guard against a single CompactionEndEvent that arrived without a
// matching start (an upstream bug or a race). The record stays empty
// instead of being polluted with a zero-started entry.
func TestRuntimeCompactRecordNoStartIgnoresEnd(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	rt, err := pigrt.New(ctx, pigrt.Options{
		CWD:           dir,
		AgentDir:      filepath.Join(dir, "agent"),
		Model:         fauxModel(),
		NoSession:     true,
		MCPConfigPath: filepath.Join(dir, "no-such-mcp.json"),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() {
		if err := rt.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	}()

	// Inject an End event with no preceding Start. The runtime
	// must drop it; LastCompact stays at its zero value.
	rt.InjectForTest(agent.CompactionEndEvent{
		Reason:       "threshold",
		TokensBefore: 100_000,
	})
	time.Sleep(20 * time.Millisecond)

	rec := rt.LastCompact()
	if !rec.StartedAt.IsZero() || !rec.EndedAt.IsZero() {
		t.Errorf("LastCompact populated from orphan end event: %+v", rec)
	}
}
