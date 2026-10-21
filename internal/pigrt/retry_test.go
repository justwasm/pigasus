// retry_test.go — cycle retry-30s-cap wiring tests. The cycle's
// byte-for-byte guarantee is that the SettingsManager the runtime
// hands to coding.NewServices carries the AGY 1.3.4 retry policy:
//
//	MaxRetries     = 5
//	BaseDelayMs    = 1000  (= retry.InitialInterval)
//	MaxAgentDelayMs= 30000 (= retry.MaxInterval, the AGY 1.3.4 cap)
//
// PiG's defaults (MaxRetries=3, BaseDelayMs=2000, MaxAgentDelayMs=60000)
// would silently inherit if buildDefaultSettingsManager forgot the
// `retry` section. These tests pin the seam.
//
// GetRetrySettings() returns an icodingagent.RetryConfig (the type
// lives in PiG's internal package and isn't importable from outside
// the module). The fields are exported, so we can call the method,
// assign to a typed `:=` variable, and read MaxRetries / BaseDelayMs /
// MaxAgentDelayMs without importing icodingagent.
package pigrt_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/pigrt"
	"github.com/justwasm/pigasus/internal/retry"
)

// TestRuntimeRetryCapIs30s proves the agent-side retry cap is the
// AGY 1.3.4 number (30 s), not PiG's default 60 s and not the
// pre-change 4-minute number. If this test ever reports 60 000 the
// buildDefaultSettingsManager payload lost the `retry` section.
func TestRuntimeRetryCapIs30s(t *testing.T) {
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
	defer func() { _ = rt.Close() }()

	sm := rt.Session().Services().SettingsManager()
	cfg := sm.GetRetrySettings()
	if cfg.MaxAgentDelayMs != retry.MaxIntervalMs {
		t.Errorf("MaxAgentDelayMs = %d, want %d (retry.MaxIntervalMs)", cfg.MaxAgentDelayMs, retry.MaxIntervalMs)
	}
	if cfg.MaxAgentDelayMs == 60_000 {
		t.Errorf("MaxAgentDelayMs still on PiG's default (60 000 ms); the retry 30s cap wasn't applied")
	}
	if cfg.MaxAgentDelayMs == 240_000 {
		t.Errorf("MaxAgentDelayMs still on the legacy 4-minute cap; the retry 30s cap wasn't applied")
	}
}

// TestRuntimeRetryBudgetIsFive proves MaxRetries=5 (AGY's effective
// budget) is set in the SettingsManager the session consults on every
// Send. Default 3 would mean the 30 s cap + the budget together can't
// wall-clock past 1+2+4 = 7 s — far less than the 5-×1s doubling
// schedule the cycle's budget promises.
func TestRuntimeRetryBudgetIsFive(t *testing.T) {
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
	defer func() { _ = rt.Close() }()

	cfg := rt.Session().Services().SettingsManager().GetRetrySettings()
	if cfg.MaxRetries != retry.MaxAttempts {
		t.Errorf("MaxRetries = %d, want %d (retry.MaxAttempts)", cfg.MaxRetries, retry.MaxAttempts)
	}
}

// TestRuntimeRetryBaseDelayIs1s proves the first retry sleeps exactly
// 1 s. PiG's default of 2 s would silence the AGY 1.3.4 changelog's
// "Initial retry wait (1s) … unchanged" line.
func TestRuntimeRetryBaseDelayIs1s(t *testing.T) {
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
	defer func() { _ = rt.Close() }()

	cfg := rt.Session().Services().SettingsManager().GetRetrySettings()
	if cfg.BaseDelayMs != retry.InitialIntervalMs {
		t.Errorf("BaseDelayMs = %d, want %d (retry.InitialIntervalMs)", cfg.BaseDelayMs, retry.InitialIntervalMs)
	}
}

// TestRuntimeRetryEnabledStillTrue is the safety net for the existing
// print_pig_test.go:134 `{"retry":{"enabled":false}}` regression
// guard. The clone's in-memory SettingsManager does NOT load that
// file (PiG's NewInMemorySettingsManager is file-free, and the
// SettingsManager we hand to NewServices replaces whatever PiG would
// have read), so retry.enabled stays at its PiG default (true) on a
// fresh runtime. This test pins that with a single bool so the
// getRetrySettings branch on `Enabled` cannot silently flip.
func TestRuntimeRetryEnabledStillTrue(t *testing.T) {
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
	defer func() { _ = rt.Close() }()

	sm := rt.Session().Services().SettingsManager()
	if !sm.GetRetryEnabled() {
		t.Errorf("GetRetryEnabled = false; the clone's default SettingsManager must keep retries on")
	}
}
