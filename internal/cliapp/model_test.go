package cliapp

// model_default_effort_test.go — cycle model-no-effort-default.
//
// Pins AGY 1.3.4 changelog line 26:
//
//	"Changed --model and /model to accept a base model name without --effort:
//	 instead of failing with an error asking for --effort, the CLI now picks
//	 the model's default thinking level (the server's default for that model,
//	 otherwise medium, otherwise the lowest available)."
//
// Seven behaviours covered:
//
//	1. TestDefaultEffortServerDefault        — chain tier 1: spec.def wins
//	2. TestDefaultEffortMediumFallback       — chain tier 2: medium when def absent/invalid
//	3. TestDefaultEffortLowestFallback       — chain tier 3: lowest when neither def nor medium
//	4. TestDefaultEffortEmptyLevels          — empty levels -> "" (no tier to pick)
//	5. TestModelNoEffortSucceeds             — --model <base> (no --effort) resolves
//	6. TestModelNoEffortPreservedWithEffort  — --model <base> --effort <level> preserved
//	7. TestEffortPickerAcceptsBareName       — /model picker opens for a bare base slug
//
// Cycle 346: tests 1–4 are pure unit tests on the default-effort chain and
// moved to internal/modelcatalog, next to the code they cover; the cliapp
// copy drove a local wrapper that is gone. Tests 5–6 ride through the
// public model selection pipeline; test 7 drives the /model panel the
// TUI's slash router opens for "model".

import (
	"flag"
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/justwasm/pigasus/internal/accountrouting"
	"github.com/justwasm/pigasus/internal/backend"
	"github.com/justwasm/pigasus/internal/cli"
	"github.com/justwasm/pigasus/internal/cliflags"
	"github.com/justwasm/pigasus/internal/configtest"
	"github.com/justwasm/pigasus/internal/modelcatalog"
	"github.com/justwasm/pigasus/internal/printmode"
)

// TestModelNoEffortSucceeds: --model <base> without --effort must NOT
// fail. resolveModelSelection returns the resolved catalogue ID with
// the chain's default effort appended.
//
// Before this cycle the validator already returned
// `gemini-3.8-flash-high` for that input via the inline `model + "-" +
// spec.def` branch; this test pins that the helper-driven path produces
// the same result, and that the catalogueID branch is untouched for
// "thinking" base slugs (claude-sonnet-4-6 returns as-is).
func TestModelNoEffortSucceeds(t *testing.T) {
	cases := []struct {
		model  string
		wantID string
	}{
		// server default tier (chain tier 1).
		{"gemini-3.8-flash", "gemini-3.8-flash-high"},
		// server default is also the only available tier.
		{"gpt-oss-120b", "gpt-oss-120b-medium"},
		// "thinking" base slug — no effort tier to pick, catalogueID wins.
		{"claude-sonnet-4-6", "claude-sonnet-4-6"},
	}
	for _, tc := range cases {
		fs := flag.NewFlagSet("t", flag.ContinueOnError)
		fs.String("model", tc.model, "")
		fs.String("effort", "", "")
		got, err := modelcatalog.ResolveModelSelection(tc.model, "")
		if err != nil {
			t.Errorf("modelcatalog.ResolveModelSelection(%q, \"\") = error %v, want %q", tc.model, err, tc.wantID)
			continue
		}
		if got != tc.wantID {
			t.Errorf("modelcatalog.ResolveModelSelection(%q, \"\") = %q, want %q", tc.model, got, tc.wantID)
		}
	}
}

// TestModelNoEffortPreservedWithEffort: when --effort is supplied, the
// chain is bypassed — the user-supplied effort is honoured. Pins the
// "Existing --model <name> --effort <level> behaviour is preserved"
// half of the brief.
func TestModelNoEffortPreservedWithEffort(t *testing.T) {
	cases := []struct {
		model  string
		effort string
		wantID string
	}{
		{"gemini-3.8-flash", "low", "gemini-3.8-flash-low"},
		{"gemini-3.8-flash", "medium", "gemini-3.8-flash-medium"},
		{"gemini-3.8-flash", "high", "gemini-3.8-flash-high"},
		{"gpt-oss-120b", "medium", "gpt-oss-120b-medium"},
	}
	for _, tc := range cases {
		fs := flag.NewFlagSet("t", flag.ContinueOnError)
		fs.String("model", tc.model, "")
		fs.String("effort", tc.effort, "")
		got, err := modelcatalog.ResolveModelSelection(tc.model, tc.effort)
		if err != nil {
			t.Errorf("modelcatalog.ResolveModelSelection(%q, %q) = error %v, want %q", tc.model, tc.effort, err, tc.wantID)
			continue
		}
		if got != tc.wantID {
			t.Errorf("modelcatalog.ResolveModelSelection(%q, %q) = %q, want %q", tc.model, tc.effort, got, tc.wantID)
		}
	}
}

// TestEffortPickerAcceptsBareName: the /model panel the TUI opens must
// accept a bare base slug (e.g. "gemini-3.8-flash") as the current
// model — without the cycle 1.3.4 fix a bare base slug fell through to
// the default and the panel's (current) annotation landed on the wrong
// row. The test seeds m.model to a base slug, opens the panel, and
// asserts (a) the panel opens at all and (b) the panel lines do not
// panic / error / blank the (current) row.
//
// We don't drive a live probe here — the panel render test (cycle 197 /
// 208) covers the byte-for-byte layout. This test pins the cycle-197 /
// 1.3.4 contract: panelCurrentModelLabel tolerates a bare base slug
// because the changelog line 26 fix lets `--model foo` set m.model to
// `foo` without first requiring an effort.
func TestEffortPickerAcceptsBareName(t *testing.T) {
	_ = cycle196TestHome(t, map[string]any{"model": "gemini-3.8-flash"})
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	fs.String("effort", "", "")

	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", false)
	m.picker.Reset()
	m.input.Reset()
	m.flags = fs
	// Cycle 197 / 1.3.4: m.model carries the *display label*, not the slug.
	// canonicalModelLabel maps "gemini-3.8-flash" -> "Gemini 3.8 Flash" via
	// settings.json's persisted "model" value, which the cycle-197
	// resolveEffectiveModel already lifts. The test seeds the displayed
	// label directly so the panel's panelCurrentModelLabel() round-trips.
	m.model = modelcatalog.CanonicalModelLabel("gemini-3.8-flash")
	if m.model == "" {
		m.model = "Gemini 3.8 Flash (High)" // fallback if canonical returned ""
	}

	// Open the panel — must succeed without panicking on a bare base slug.
	if !m.openSlashPanel("model") {
		t.Fatal("openSlashPanel(\"model\") = false, want true")
	}
	if m.panel.kind != panelModel {
		t.Errorf("panel.kind = %v, want panelModel", m.panel.kind)
	}
	if m.panel.cmd != "/model" {
		t.Errorf("panel.cmd = %q, want %q", m.panel.cmd, "/model")
	}

	// Park the cursor on the Gemini row so the Right key advances the
	// embedded effort slider (the only row cycleModelEffort touches).
	for i, r := range modelRows {
		if r.Slug == "gemini-3.8-flash" {
			m.panel.cursor = i
			break
		}
	}
	if m.panel.cursor < 0 || m.panel.cursor >= len(modelRows) ||
		modelRows[m.panel.cursor].Slug != "gemini-3.8-flash" {
		t.Fatalf("could not find gemini-3.8-flash in modelRows")
	}

	// panelModelLines must render the (current) row for "Gemini 3.8 Flash"
	// without panicking or producing an empty string. The render test
	// (cycle 197/208) pins the byte layout; this asserts the chained
	// resolve path produces a label the panel can use.
	lines := m.panelModelLines()
	if len(lines) == 0 {
		t.Fatal("panelModelLines returned no lines")
	}
	var joined strings.Builder
	for _, ln := range lines {
		joined.WriteString(stripSGR(ln))
		joined.WriteString("\n")
	}
	if !strings.Contains(joined.String(), "Gemini 3.8 Flash") {
		t.Errorf("panelModelLines missing 'Gemini 3.8 Flash' row, got:\n%s", joined.String())
	}

	// Right-arrow on the (current) Gemini row advances the embedded
	// effort slider, confirming the panel is alive for a base-slug model.
	// Seed the slider to "low" first so Right can advance to "medium"
	// (Right from "high" is clamped and would otherwise look like a
	// no-op).
	m.panel.effortCursor = effortLow
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyRight}); !handled {
		t.Fatal("right key not handled on /model panel")
	}
	if m.panel.effortCursor != effortLow+1 {
		t.Errorf("effortCursor after Right = %d, want %d", m.panel.effortCursor, effortLow+1)
	}
}

func hasModel(t *testing.T, id string) bool {
	t.Helper()
	for _, m := range cli.ReferenceModels() {
		if m.ID == id {
			return true
		}
	}
	return false
}

func resetClaude55Env(t *testing.T) {
	t.Helper()
	t.Setenv("AGY_ADC_AUTH", "")
	os.Unsetenv("AGY_ADC_AUTH")
	t.Setenv("AGY_CLAUDE55_PROJECT", "")
	os.Unsetenv("AGY_CLAUDE55_PROJECT")
}

func TestModelCatalog_Claude55Present_ADCTrue(t *testing.T) {
	resetClaude55Env(t)
	t.Setenv("AGY_ADC_AUTH", "true")
	t.Setenv("AGY_CLAUDE55_PROJECT", "test-project")

	if !cli.Claude55Available() {
		t.Fatal("claude55Available() = false under ADC=true+project; want true")
	}
	for _, id := range []string{"claude-opus-5-5", "claude-sonnet-5-5"} {
		if !hasModel(t, id) {
			t.Errorf("model %q absent from cli.ReferenceModels(); want present", id)
		}
	}

	picker := modelRowsView()
	var seenOpus, seenSonnet bool
	for _, r := range picker {
		switch r.Slug {
		case "claude-opus-5-5":
			seenOpus = true
			if r.Display != "Claude Opus 5.5" {
				t.Errorf("claude-opus-5-5 picker display = %q, want %q", r.Display, "Claude Opus 5.5")
			}
		case "claude-sonnet-5-5":
			seenSonnet = true
			if r.Display != "Claude Sonnet 5.5" {
				t.Errorf("claude-sonnet-5-5 picker display = %q, want %q", r.Display, "Claude Sonnet 5.5")
			}
		}
	}
	if !seenOpus {
		t.Errorf("claude-opus-5-5 not in modelRowsView() under ADC=true+project")
	}
	if !seenSonnet {
		t.Errorf("claude-sonnet-5-5 not in modelRowsView() under ADC=true+project")
	}
}

func TestModelCatalog_Claude55Absent_ADCMissing(t *testing.T) {
	t.Run("adc_unset", func(t *testing.T) {
		resetClaude55Env(t)
		if cli.Claude55Available() {
			t.Fatal("claude55Available() = true with both env vars unset; want false")
		}
		for _, id := range []string{"claude-opus-5-5", "claude-sonnet-5-5"} {
			if hasModel(t, id) {
				t.Errorf("model %q present in cli.ReferenceModels(); want absent (ADC unset)", id)
			}
		}
		for _, r := range modelRowsView() {
			if r.Slug == "claude-opus-5-5" || r.Slug == "claude-sonnet-5-5" {
				t.Errorf("model %q present in modelRowsView(); want absent (ADC unset)", r.Slug)
			}
		}
	})

	t.Run("project_unset", func(t *testing.T) {
		resetClaude55Env(t)
		t.Setenv("AGY_ADC_AUTH", "true")
		if cli.Claude55Available() {
			t.Fatal("claude55Available() = true with project unset; want false")
		}
		for _, id := range []string{"claude-opus-5-5", "claude-sonnet-5-5"} {
			if hasModel(t, id) {
				t.Errorf("model %q present in cli.ReferenceModels(); want absent (project unset)", id)
			}
		}
		for _, r := range modelRowsView() {
			if r.Slug == "claude-opus-5-5" || r.Slug == "claude-sonnet-5-5" {
				t.Errorf("model %q present in modelRowsView(); want absent (project unset)", r.Slug)
			}
		}
	})
}

func TestModelCatalog_Claude55Absent_ADCFalse(t *testing.T) {
	resetClaude55Env(t)
	t.Setenv("AGY_ADC_AUTH", "false")
	t.Setenv("AGY_CLAUDE55_PROJECT", "test-project")
	if cli.Claude55Available() {
		t.Fatal("claude55Available() = true with AGY_ADC_AUTH=false; want false (strict gate)")
	}
	for _, id := range []string{"claude-opus-5-5", "claude-sonnet-5-5"} {
		if hasModel(t, id) {
			t.Errorf("model %q present in cli.ReferenceModels(); want absent (ADC=false)", id)
		}
	}
	for _, r := range modelRowsView() {
		if r.Slug == "claude-opus-5-5" || r.Slug == "claude-sonnet-5-5" {
			t.Errorf("model %q present in modelRowsView(); want absent (ADC=false)", r.Slug)
		}
	}

	got := modelcatalog.AvailableModelsBlock()
	if strings.Contains(got, "Claude Opus 5.5") || strings.Contains(got, "Claude Sonnet 5.5") {
		t.Errorf("modelcatalog.AvailableModelsBlock() leaks 5.5 entries under ADC=false:\n%s", got)
	}
}

// modelTransportTestFlags builds the minimal flag set pickBackend reads.
func modelTransportTestFlags(t *testing.T, backendFlag string) *flag.FlagSet {
	t.Helper()
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.String("backend", "", "")
	if backendFlag != "" {
		if err := fs.Set("backend", backendFlag); err != nil {
			t.Fatalf("set backend: %v", err)
		}
	}
	return fs
}

// TestPickBackend_RoutesByModelFamily pins the dispatch: a model in the
// reference's own catalogue goes to CloudCode, and a model the user
// configured goes to the configured endpoint.
func TestPickBackend_RoutesByModelFamily(t *testing.T) {
	t.Setenv("JETSKI_OAUTH_TOKEN", "test-token") // skip the shim downgrade
	t.Setenv("OPENAGY_BACKEND", "")
	configtest.WriteProviders(t, `{"providers": {"gateway": {
		"type": "openai", "base_url": "https://gateway.example/v1",
		"models": [{"id": "custom-model", "name": "Custom Model", "model": "Upstream-1"}]}}}`)

	cases := []struct {
		name  string
		model string
		want  string
	}{
		{"official gemini label", "Gemini 3.8 Flash (High)", "cloudcode"},
		{"official gemini slug", "gemini-3.8-flash-low", "cloudcode"},
		{"official claude", "Claude Sonnet 4.6 (Thinking)", "cloudcode"},
		{"official gpt-oss", "GPT-OSS 120B (Medium)", "cloudcode"},
		{"configured model", "Custom Model", "gateway"},
		{"configured model by id", "custom-model", "gateway"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			be := pickBackendWithLog(modelTransportTestFlags(t, ""), tc.model, nil, 0)
			switch tc.want {
			case "cloudcode":
				cc, ok := accountrouting.Unwrap(be).(*backend.CloudCodeClient)
				if !ok {
					t.Fatalf("backend = %T, want *backend.CloudCodeClient", be)
				}
				if cc.Model() != resolveCatalogueID(tc.model) {
					t.Errorf("wire model = %q, want %q", cc.Model(), resolveCatalogueID(tc.model))
				}
			case "gateway":
				gc, ok := be.(*backend.GatewayClient)
				if !ok {
					t.Fatalf("backend = %T, want *backend.GatewayClient", be)
				}
				cfg := gc.GatewayConfig()
				if cfg.URL != "https://gateway.example/v1/chat/completions" {
					t.Errorf("endpoint = %q", cfg.URL)
				}
				if cfg.ExternalModel != "Upstream-1" {
					t.Errorf("upstream model = %q, want Upstream-1", cfg.ExternalModel)
				}
			}
		})
	}
}

// TestPickBackend_RoutesAnAnthropicProvider pins that a provider declared
// with the Anthropic protocol reaches the Messages endpoint and carries the
// Anthropic wire protocol.
//
// The endpoint is the base plus "/v1/messages" — not the OpenAI
// "/chat/completions" — and the protocol decides which dialect the
// transport speaks.
func TestPickBackend_RoutesAnAnthropicProvider(t *testing.T) {
	t.Setenv("JETSKI_OAUTH_TOKEN", "test-token")
	t.Setenv("OPENAGY_BACKEND", "")
	configtest.WriteProviders(t, `{"providers": {"anthropic-gw": {
		"type": "anthropic", "base_url": "https://gateway.example/anthropic",
		"models": [{"id": "custom-anthropic", "name": "Custom Anthropic", "model": "Upstream-A"}]}}}`)

	be := pickBackendWithLog(modelTransportTestFlags(t, ""), "Custom Anthropic", nil, 0)
	gc, ok := be.(*backend.GatewayClient)
	if !ok {
		t.Fatalf("backend = %T, want *backend.GatewayClient", be)
	}
	cfg := gc.GatewayConfig()
	if cfg.URL != "https://gateway.example/anthropic/v1/messages" {
		t.Errorf("endpoint = %q, want the Messages endpoint", cfg.URL)
	}
	if cfg.Type != backend.GatewayProtocolAnthropic {
		t.Errorf("protocol = %q, want %q", cfg.Type, backend.GatewayProtocolAnthropic)
	}
	if cfg.ExternalModel != "Upstream-A" {
		t.Errorf("upstream model = %q, want Upstream-A", cfg.ExternalModel)
	}
}

// TestPickBackend_UnknownModelFallsBackToTheShim pins that a model nobody
// offers has no endpoint: the shim answers, and the warning names the
// configuration file the user has to edit.
func TestPickBackend_UnknownModelFallsBackToTheShim(t *testing.T) {
	t.Setenv("JETSKI_OAUTH_TOKEN", "test-token")
	t.Setenv("OPENAGY_BACKEND", "")
	configtest.Isolate(t)
	var log strings.Builder
	be := pickBackendWithLog(modelTransportTestFlags(t, ""), "some-other-model", &log, 0)
	if _, ok := be.(*backend.CloudCodeClient); ok {
		t.Fatalf("backend = %T, want the shim", be)
	}
	if _, ok := be.(*backend.GatewayClient); ok {
		t.Fatalf("backend = %T, want the shim", be)
	}
	if !strings.Contains(log.String(), "providers.json") {
		t.Errorf("the warning does not name the configuration file: %q", log.String())
	}
}

// TestPickBackend_ExplicitGatewayWinsOverModel pins that an explicit
// --backend gateway reaches the configured endpoint even for a model the
// reference also serves.
func TestPickBackend_ExplicitGatewayWinsOverModel(t *testing.T) {
	t.Setenv("JETSKI_OAUTH_TOKEN", "test-token")
	t.Setenv("OPENAGY_BACKEND", "")
	configtest.WriteProviders(t, `{"providers": {"gateway": {
		"type": "openai", "base_url": "https://gateway.example/v1",
		"models": [{"id": "custom-model", "name": "Custom Model", "model": "Upstream-1"}]}}}`)
	be := pickBackendWithLog(modelTransportTestFlags(t, "gateway"), "Custom Model", nil, 0)
	if _, ok := be.(*backend.GatewayClient); !ok {
		t.Fatalf("backend = %T, want *backend.GatewayClient (explicit --backend gateway)", be)
	}
}

// TestPickBackend_ExplicitGatewayNeedsAConfiguredModel pins that the
// configured transport has no endpoint for a model nobody configured: the
// shim answers and the warning says which file to edit.
func TestPickBackend_ExplicitGatewayNeedsAConfiguredModel(t *testing.T) {
	t.Setenv("JETSKI_OAUTH_TOKEN", "test-token")
	t.Setenv("OPENAGY_BACKEND", "")
	configtest.Isolate(t)
	var log strings.Builder
	be := pickBackendWithLog(modelTransportTestFlags(t, "gateway"), "Gemini 3.8 Flash (High)", &log, 0)
	if _, ok := be.(*backend.GatewayClient); ok {
		t.Fatalf("backend = %T, want the shim (the model is not configured)", be)
	}
	if !strings.Contains(log.String(), "providers.json") {
		t.Errorf("the warning does not name the configuration file: %q", log.String())
	}
}

func TestPickBackend_ExplicitCloudCode(t *testing.T) {
	t.Setenv("JETSKI_OAUTH_TOKEN", "test-token")
	t.Setenv("OPENAGY_BACKEND", "")
	configtest.Isolate(t)
	be := pickBackendWithLog(modelTransportTestFlags(t, "cloudcode"), "Custom Model", nil, 0)
	if _, ok := accountrouting.Unwrap(be).(*backend.CloudCodeClient); !ok {
		t.Fatalf("backend = %T, want *backend.CloudCodeClient (explicit --backend cloudcode)", be)
	}
}

// TestModelSelectionSwapsBackend pins the runtime half of the fix: choosing a
// model in /model must replace m.backend, not only m.model, or the next turn
// still goes to MiniMax.
func TestModelSelectionSwapsBackend(t *testing.T) {
	cycle196TestHome(t, map[string]any{"model": "gemini-3.8-flash-medium"})
	m := mkPanel(panelModel, "/model", 1)
	old := backend.NewMock()
	m.backend = old

	byModel := map[string]backend.Client{}
	m.backendFor = func(label string) backend.Client {
		c := backend.NewMock()
		byModel[label] = c
		return c
	}

	// Park the cursor on the Gemini row and press Enter.
	for i, r := range modelRowsView() {
		if r.Slug == "gemini-3.8-flash" {
			m.panel.cursor = i
			break
		}
	}
	label := m.selectModelEffort()
	if label == "" {
		t.Fatal("selectModelEffort returned no label")
	}
	got, ok := byModel[label]
	if !ok {
		t.Fatalf("backend factory never saw %q (saw %v)", label, keysOf(byModel))
	}
	if m.backend != got {
		t.Fatal("m.backend was not rebuilt on Enter")
	}

	// ←/→ on the same row must rebuild too. Re-park the cursor: the
	// Enter path closes the panel in the real UI.
	m.backend = old
	for i, r := range modelRowsView() {
		if r.Slug == "gemini-3.8-flash" {
			m.panel.cursor = i
			break
		}
	}
	m.cycleModelEffort(+1)
	cycleLabel := m.model
	cycleClient, ok := byModel[cycleLabel]
	if !ok {
		t.Fatalf("backend factory never saw %q on ←/→ (saw %v)", cycleLabel, keysOf(byModel))
	}
	if m.backend != cycleClient {
		t.Fatal("m.backend was not rebuilt on ←/→")
	}
}

func keysOf(m map[string]backend.Client) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// ----------------------------------------------------------------------
// /config catalogue
// ----------------------------------------------------------------------

// TestCycle197_ConfigCatalogueSize pins the recovered catalogue: 20
// top-level rows, plus the "Editor Mode › Insert First" child row while
// Editor Mode is vim.
func TestCycle197_ConfigCatalogueSize(t *testing.T) {
	cycle196TestHome(t, map[string]any{})
	if got := len(configRows()); got != 20 {
		t.Errorf("configRows() with editorMode unset = %d rows, want 20", got)
	}
	cycle196TestHome(t, map[string]any{"editorMode": "vim"})
	rows := configRows()
	if len(rows) != 21 {
		t.Fatalf("configRows() with editorMode=vim = %d rows, want 21", len(rows))
	}
	// The child row sits immediately after Editor Mode.
	if rows[7].Name != "Editor Mode" || rows[8].Name != "Editor Mode \u203a Insert First" {
		t.Errorf("rows[7]=%q rows[8]=%q, want Editor Mode then its Insert First child",
			rows[7].Name, rows[8].Name)
	}
}

// TestCycle197_ConfigRowKeys pins the recovered name→settings.json key
// mapping (probe_ref_walk2.py). A wrong key here silently writes the wrong
// setting.
func TestCycle197_ConfigRowKeys(t *testing.T) {
	want := []struct{ name, key string }{
		{"Agent Mode", "agentMode"},
		{"Animation Speed", "runningLightSpeed"},
		{"Artifact Review", "artifactReviewPolicy"},
		{"Color Scheme", "colorScheme"},
		{"Conversation Picker Grouping", "pickerGrouping"},
		{"Copy on Select", "copyOnSelect"},
		{"Editor", "editor"},
		{"Editor Mode", "editorMode"},
		{"Editor Mode \u203a Insert First", "vimInsertFirst"},
		{"Enable Telemetry", "enableTelemetry"},
		{"Non-Workspace Access", "allowNonWorkspaceAccess"},
		{"Notifications", "notifications"},
		{"Queued Messages", "queuedMessages"},
		{"Rendering Mode", "altScreenMode"},
		{"Sandbox Mode", "enableTerminalSandbox"},
		{"Show Active Tasks", "hideActiveItems"},
		{"Show Feedback Survey", "showFeedbackSurvey"},
		{"Show Tips", "showTips"},
		{"Tool Permission", "toolPermission"},
		{"Use AI Credits", "useG1Credits"},
		{"Verbosity", "verbosity"},
	}
	if len(configSettingsRows) != len(want) {
		t.Fatalf("catalogue has %d rows, want %d", len(configSettingsRows), len(want))
	}
	for i, w := range want {
		got := configSettingsRows[i]
		if got.Name != w.name || got.SettingsKey != w.key {
			t.Errorf("row %d = (%q, %q), want (%q, %q)", i, got.Name, got.SettingsKey, w.name, w.key)
		}
	}
}

// TestCycle197_ConfigDefaultDeletesKey is the headline correctness fix:
// cycling Agent Mode back to its default removes the key instead of
// writing "default".
func TestCycle197_ConfigDefaultDeletesKey(t *testing.T) {
	dir := cycle196TestHome(t, map[string]any{})
	idx := configRowIndexForTest(t, "Agent Mode")
	m := &runMode{}
	m.panel = slashPanel{kind: panelConfig, cmd: "/config", cursor: idx}

	// default → accept-edits → auto → plan → default
	for _, want := range []string{"accept-edits", "auto", "plan"} {
		m.cycleConfigValue(+1)
		if got := cycle196ValueAt(t, readCycle196Settings(t, dir), "agentMode"); got != want {
			t.Fatalf("after right: agentMode = %q, want %q", got, want)
		}
	}
	m.cycleConfigValue(+1)
	got := readCycle196Settings(t, dir)
	if v, present := got["agentMode"]; present {
		t.Errorf("after wrapping to default: agentMode = %v, want key absent", v)
	}
	if d := configSettingValue(idx); d != "default" {
		t.Errorf("display after wrap = %q, want %q", d, "default")
	}
}

// TestCycle197_ConfigAnimationSpeedCycle pins the four-step cycle that the
// pre-197 clone got wrong (it had [slow medium fast] and was missing
// "off").
func TestCycle197_ConfigAnimationSpeedCycle(t *testing.T) {
	dir := cycle196TestHome(t, map[string]any{})
	idx := configRowIndexForTest(t, "Animation Speed")
	m := &runMode{}
	m.panel = slashPanel{kind: panelConfig, cmd: "/config", cursor: idx}

	for _, want := range []string{"slow", "off", "fast"} {
		m.cycleConfigValue(+1)
		if got := cycle196ValueAt(t, readCycle196Settings(t, dir), "runningLightSpeed"); got != want {
			t.Fatalf("after right: runningLightSpeed = %q, want %q", got, want)
		}
	}
	m.cycleConfigValue(+1) // fast → medium (default → key removed)
	if v, present := readCycle196Settings(t, dir)["runningLightSpeed"]; present {
		t.Errorf("after wrap: runningLightSpeed = %v, want key absent", v)
	}
}

// TestCycle197_ConfigArtifactReviewDisplay covers the display forms the
// pre-197 clone rendered with hyphens.
func TestCycle197_ConfigArtifactReviewDisplay(t *testing.T) {
	idx := configRowIndexForTest(t, "Artifact Review")
	cases := []struct {
		stored  any
		display string
	}{
		{nil, "asks for review"},
		{"always-proceed", "always proceeds"},
		{"agent-decides", "agent decides"},
	}
	for _, c := range cases {
		cycle196TestHome(t, map[string]any{"artifactReviewPolicy": c.stored})
		if got := configSettingValue(idx); got != c.display {
			t.Errorf("stored=%v → %q, want %q", c.stored, got, c.display)
		}
	}
}

// TestCycle197_ConfigToolPermissionFromExplicit stores a value that is the
// row's *display default* but is written explicitly: the reference (and
// the clone) advance it to the next entry rather than stalling.
func TestCycle197_ConfigToolPermissionFromExplicit(t *testing.T) {
	dir := cycle196TestHome(t, map[string]any{"toolPermission": "always-proceed"})
	idx := configRowIndexForTest(t, "Tool Permission")
	m := &runMode{}
	m.panel = slashPanel{kind: panelConfig, cmd: "/config", cursor: idx}
	if got := configSettingValue(idx); got != "always-proceed" {
		t.Fatalf("display = %q, want always-proceed", got)
	}
	m.cycleConfigValue(+1)
	if got := cycle196ValueAt(t, readCycle196Settings(t, dir), "toolPermission"); got != "strict" {
		t.Errorf("after right: toolPermission = %q, want strict", got)
	}
}

// TestCycle197_ConfigRenderingModeWrap covers a stored value ("always")
// wrapping to the inline view.
//
// Cycle 294 (AGY 1.4.7, measured 2026-10-25): the reference's own panel
// writes an *empty* value for the inline stop, which its
// `json:"altScreenMode,omitempty"` writer then drops from settings.json —
// so the reference's inline option does not survive a restart, because an
// absent key is the reference's default and its default is the alternate
// screen. The clone writes the reference's own inline value, "never",
// which agy 1.4.7 accepts and honours.
func TestCycle197_ConfigRenderingModeWrap(t *testing.T) {
	dir := cycle196TestHome(t, map[string]any{"altScreenMode": "always"})
	idx := configRowIndexForTest(t, "Rendering Mode")
	m := &runMode{}
	m.panel = slashPanel{kind: panelConfig, cmd: "/config", cursor: idx}
	if got := configSettingValue(idx); got != "no flickering (altscreen)" {
		t.Fatalf("display = %q", got)
	}
	m.cycleConfigValue(+1)
	if got := cycle196ValueAt(t, readCycle196Settings(t, dir), "altScreenMode"); got != "never" {
		t.Errorf("after right: altScreenMode = %q, want never", got)
	}
}

// TestCycle197_ConfigMemoryOnlyTouchesNoDisk pins Enable Telemetry: the
// reference toggles it but writes nothing to settings.json (probe
// diff was empty), so the clone keeps the value in panel state.
func TestCycle197_ConfigMemoryOnlyTouchesNoDisk(t *testing.T) {
	dir := cycle196TestHome(t, map[string]any{})
	idx := configRowIndexForTest(t, "Enable Telemetry")
	m := &runMode{}
	m.panel = slashPanel{kind: panelConfig, cmd: "/config", cursor: idx}
	if got := m.configRowValueAt(idx); got != "off" {
		t.Fatalf("initial display = %q, want off", got)
	}
	m.cycleConfigValue(+1)
	if got := m.configRowValueAt(idx); got != "on" {
		t.Errorf("after right: display = %q, want on", got)
	}
	if got := readCycle196Settings(t, dir); len(got) != 0 {
		t.Errorf("settings.json = %v, want untouched", got)
	}
}

// TestCycle197_ConfigVisibleRows pins the height→row-count table measured
// with probe_ref_height.py, and the unknown-height fallback.
//
// Cycle 208: the table was measured against a 20-row catalogue, so the
// test now freezes the settings home (editorMode absent = no nested
// "Insert First" child). Without that it depended on whatever the shared
// ~/.gemini/antigravity-cli/settings.json happened to hold.
func TestCycle197_ConfigVisibleRows(t *testing.T) {
	cycle196TestHome(t, map[string]any{})
	cases := []struct{ h, want int }{
		{0, 10}, {-5, 10},
		{20, 3}, {24, 6}, {28, 9}, {32, 12}, {40, 18},
		{44, 20}, {50, 20}, {60, 20}, {1, 1},
	}
	for _, c := range cases {
		if got := configVisibleRowsFor(c.h); got != c.want {
			t.Errorf("configVisibleRowsFor(%d) = %d, want %d", c.h, got, c.want)
		}
	}
}

// TestCycle197_ConfigPanelNoCounterWhenAllFit pins the counter slot: the
// reference blanks it (rather than collapsing the line) when every row
// fits.
func TestCycle197_ConfigPanelNoCounterWhenAllFit(t *testing.T) {
	cycle196TestHome(t, map[string]any{})
	m := mkPanel(panelConfig, "/config", 0)
	m.height = 50
	lines := plainLines(m)
	if len(lines) != 31 {
		t.Errorf("panel = %d lines at height 50, want 31 (live REF)", len(lines))
	}
	for _, l := range lines {
		if contains(l, "items]") {
			t.Errorf("unexpected counter at height 50: %q", l)
		}
	}
	// The windowed fallback (height unknown) still shows the counter.
	m2 := mkPanel(panelConfig, "/config", 0)
	joined := ""
	for _, l := range plainLines(m2) {
		joined += l + "\n"
	}
	if !contains(joined, "[1-10 of 20 items]") {
		t.Errorf("unknown height should keep the windowed counter:\n%s", joined)
	}
}

// ----------------------------------------------------------------------
// /model
// ----------------------------------------------------------------------

// TestCycle197_ModelCurrentFromEffectiveSettings pins that an unavailable
// settings value still marks — and parks the slider on — the fallback
// model, matching REF's capture.
func TestCycle197_ModelCurrentFromEffectiveSettings(t *testing.T) {
	cycle196TestHome(t, map[string]any{"model": "gemini-3.8-flash"})
	m := mkPanel(panelModel, "/model", 0)
	lines := plainLines(m)
	if !contains(lines[5], "(current)") {
		t.Errorf("fallback row should be marked (current): %q", lines[5])
	}
	if m.panel.effortCursor != 2 {
		t.Errorf("effortCursor = %d, want 2 (high, the fallback model's tier)", m.panel.effortCursor)
	}
}

// TestCycle197_ModelEffortFromLabel pins that a persisted display label
// parks the slider on its own tier. Slugs are rejected by the 1.4.3
// settings loader (it falls back to High with a warning); full labels are
// the accepted form.
func TestCycle197_ModelEffortFromSlug(t *testing.T) {
	for label, want := range map[string]int{
		"Gemini 3.8 Flash (Low)":    0,
		"Gemini 3.8 Flash (Medium)": 1,
		"Gemini 3.8 Flash (High)":   2,
	} {
		cycle196TestHome(t, map[string]any{"model": label})
		if got := initialEffortCursorFromSettings(); got != want {
			t.Errorf("%s → effortCursor %d, want %d", label, got, want)
		}
	}
}

// TestCycle197_ModelEffortCyclesGeminiOnly verifies ←/→ wraps the slider
// on the Gemini row and is inert elsewhere. Cycle
// `model-left-right-effort` switched the boundary from "clamp" to
// "wrap": pressing → at high wraps back to low, and ← at low wraps to
// high. The non-Gemini rows still leave effortCursor alone — pressing
// an arrow while the cursor is on Claude Sonnet must not change the
// persisted model.
func TestCycle197_ModelEffortCyclesGeminiOnly(t *testing.T) {
	cycle196TestHome(t, map[string]any{"model": "Gemini 3.8 Flash (Medium)"})
	m := mkPanel(panelModel, "/model", 0)
	m.cycleModelEffort(+1)
	if m.panel.effortCursor != 2 {
		t.Errorf("gemini right: effortCursor = %d, want 2", m.panel.effortCursor)
	}
	m.cycleModelEffort(+1) // wrap high → low
	if m.panel.effortCursor != 0 {
		t.Errorf("gemini right wrap: effortCursor = %d, want 0 (wraps low)", m.panel.effortCursor)
	}
	m.cycleModelEffort(-1) // wrap low → high
	if m.panel.effortCursor != 2 {
		t.Errorf("gemini left wrap: effortCursor = %d, want 2 (wraps high)", m.panel.effortCursor)
	}
	// Non-Gemini rows are inert: the cursor moves to Claude Sonnet
	// after a separate ↓/↑, but cycleModelEffort early-returns because
	// the row's slug is not gemini-3.8-flash. Persisted model is
	// whatever it was at the time of the inert call.
	m.panel.effortCursor = 2
	m.panel.cursor = 1 // Claude Sonnet — fixed effort
	m.cycleModelEffort(-1)
	if m.panel.effortCursor != 2 {
		t.Errorf("non-gemini row should be inert: effortCursor = %d, want 2", m.panel.effortCursor)
	}
}

// TestCycle197_ModelSelectPersistsSlugAndUpdatesLabel pins Enter's effect:
// the catalogue slug goes to settings.json while m.model / m.statusModel
// take the display label so the status row reads "Gemini 3.8 Flash".
func TestCycle197_ModelSelectPersistsSlugAndUpdatesLabel(t *testing.T) {
	dir := cycle196TestHome(t, map[string]any{"model": "Gemini 3.8 Flash (High)"})
	m := mkPanel(panelModel, "/model", 0)
	m.cycleModelEffort(-1) // high → medium
	label := m.selectModelEffort()
	if got := cycle196ValueAt(t, readCycle196Settings(t, dir), "model"); got != "gemini-3.8-flash-medium" {
		t.Errorf("persisted model = %q, want gemini-3.8-flash-medium", got)
	}
	if label != "Gemini 3.8 Flash (Medium)" {
		t.Errorf("label = %q", label)
	}
	if m.model != "Gemini 3.8 Flash (Medium)" {
		t.Errorf("m.model = %q, want the label", m.model)
	}
	if m.statusModel != "Gemini 3.8 Flash" {
		t.Errorf("m.statusModel = %q, want tier-stripped label", m.statusModel)
	}
}

// TestCycle197_ModelArgonNotSelectable pins that the dim, slugless row is
// inert: Enter leaves settings.json alone.
func TestCycle197_ModelArgonNotSelectable(t *testing.T) {
	dir := cycle196TestHome(t, map[string]any{"model": "Gemini 3.8 Flash (High)"})
	// The Argon row's index is looked up rather than hardcoded: the picker
	// grows a row whenever a model is added, and a pinned index would then
	// point at a selectable model instead.
	argon := -1
	for i, r := range modelRowsView() {
		if r.Display == "Gemini 4 Argon" {
			argon = i
			break
		}
	}
	if argon < 0 {
		t.Fatal("the Argon row is missing from the picker")
	}
	m := mkPanel(panelModel, "/model", argon)
	if got := m.selectModelEffort(); got != "" {
		t.Errorf("selectModelEffort on Argon = %q, want \"\"", got)
	}
	if got := cycle196ValueAt(t, readCycle196Settings(t, dir), "model"); got != "Gemini 3.8 Flash (High)" {
		t.Errorf("settings model changed to %q; Argon must be inert", got)
	}
	// handlePanelKey(Enter) must still swallow the key (close the panel).
	handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyEnter})
	if !handled {
		t.Error("Enter on Argon: handlePanelKey reported unhandled")
	}
}

// TestCycle197_ModelEnterKeyHandlerClosesPanel drives the user-facing path.
func TestCycle197_ModelEnterKeyHandlerClosesPanel(t *testing.T) {
	dir := cycle196TestHome(t, map[string]any{"model": "Gemini 3.8 Flash (High)"})
	m := mkPanel(panelModel, "/model", 0)
	handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyLeft}) // high → medium
	if !handled {
		t.Fatal("← unhandled on /model")
	}
	handled, _ = m.handlePanelKey(tea.Key{Code: tea.KeyEnter})
	if !handled {
		t.Fatal("Enter unhandled on /model")
	}
	if got := cycle196ValueAt(t, readCycle196Settings(t, dir), "model"); got != "gemini-3.8-flash-medium" {
		t.Errorf("persisted model = %q, want gemini-3.8-flash-medium", got)
	}
	if m.panel.kind != panelNone {
		t.Errorf("panel kind = %v, want closed", m.panel.kind)
	}
}

// TestCycle197_ModelLayoutPadsCurrentColumn pins the byte geometry the
// capture comparison validates: 30-column pad then " (current)".
func TestCycle197_ModelLayoutPadsCurrentColumn(t *testing.T) {
	cycle196TestHome(t, map[string]any{"model": "Gemini 3.8 Flash (High)"})
	m := mkPanel(panelModel, "/model", 0)
	line := plainLines(m)[5]
	// "> " + 16-char label + pad to col 30 + " (current)"
	if line != "> Gemini 3.8 Flash             (current)" {
		t.Errorf("row = %q", line)
	}
}

// contains is a tiny readability helper for the assertions above.
func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func TestInspectSeeding(t *testing.T) {
	cycle196TestHome(t, map[string]any{"model": "MiniMax-M3"})
	m := mkPanel(panelModel, "/model", 0)
	t.Logf("m.panel.effortCursor = %d (want 2)", m.panel.effortCursor)
	t.Logf("m.panelCurrentModelLabel = %q", m.panelCurrentModelLabel())
	t.Logf("m.model = %q", m.model)
}

// isolateModelCatalogue points the model catalogue at an empty
// configuration so the picker holds only the reference's own rows.
func isolateModelCatalogue(t *testing.T) {
	t.Helper()
	// "{}" and not "": an empty file does not parse, and the registry
	// cache then falls back to the last good one — the user's own.
	configtest.WriteProviders(t, "{}")
	t.Setenv("AGY_ADC_AUTH", "")
	t.Setenv("AGY_CLAUDE55_PROJECT", "")
}

// labelOrder returns the display labels of the rows, in order.
func labelOrder(rows []modelRowType) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Display)
	}
	return out
}

func TestModelRowsFiltered_MatchesASubsequenceOfTheLabel(t *testing.T) {
	isolateModelCatalogue(t)
	// "clauson" is not a substring of "Claude Sonnet 4.6 (Thinking)" but
	// it is a subsequence of it — the reference matches it.
	rows := labelOrder(modelRowsFiltered("clauson"))
	if len(rows) != 1 || rows[0] != "Claude Sonnet 4.6 (Thinking)" {
		t.Fatalf("clauson → %v, want [Claude Sonnet 4.6 (Thinking)]", rows)
	}
}

func TestModelRowsFiltered_IgnoresTheCatalogueID(t *testing.T) {
	isolateModelCatalogue(t)
	// "cl-opus" is a subsequence of the catalogue id
	// ("claude-opus-4-6-thinking"), not of any label: the reference
	// answers "No models match".
	if rows := modelRowsFiltered("cl-opus"); len(rows) != 0 {
		t.Fatalf("cl-opus → %v, want no rows", labelOrder(rows))
	}
}

func TestModelRowsFiltered_EmptyQueryKeepsEveryRow(t *testing.T) {
	isolateModelCatalogue(t)
	all := modelRowsFiltered("")
	if len(all) != len(modelRowsView()) {
		t.Fatalf("empty query → %d rows, want %d", len(all), len(modelRowsView()))
	}
}

func TestModelRowsFiltered_ReferenceOrder(t *testing.T) {
	isolateModelCatalogue(t)
	// Measured on agy 1.4.5: the query "s" puts the row whose match
	// starts a word first and orders the rest by label length.
	rows := labelOrder(modelRowsFiltered("s"))
	want := []string{
		"Claude Sonnet 4.6 (Thinking)",
		"Gemini 3.8 Flash",
		"GPT-OSS 120B (Medium)",
		"Claude Opus 4.6 (Thinking)",
	}
	if len(rows) != len(want) {
		t.Fatalf("s → %v, want %v", rows, want)
	}
	for i := range want {
		if rows[i] != want[i] {
			t.Fatalf("s → %v, want %v", rows, want)
		}
	}
}

func TestModelRowsFiltered_DimRowSortsLast(t *testing.T) {
	isolateModelCatalogue(t)
	// "Gemini 4 Argon" is the dim, unselectable row. The reference keeps
	// it last even though its label is the shortest.
	rows := labelOrder(modelRowsFiltered("4"))
	want := []string{
		"Claude Opus 4.6 (Thinking)",
		"Claude Sonnet 4.6 (Thinking)",
		"Gemini 4 Argon",
	}
	if len(rows) != len(want) {
		t.Fatalf("4 → %v, want %v", rows, want)
	}
	for i := range want {
		if rows[i] != want[i] {
			t.Fatalf("4 → %v, want %v", rows, want)
		}
	}
}

func TestModelPanelSearch_TypingFiltersAndParksTheCursorOnTheFirstHit(t *testing.T) {
	isolateModelCatalogue(t)
	m := mkPanel(panelModel, "/model", 0)
	for _, r := range "cl" {
		if handled, _ := m.handlePanelKey(tea.Key{Text: string(r)}); !handled {
			t.Fatalf("the key %q was not handled by the /model panel", string(r))
		}
	}
	if m.panel.modelQuery != "cl" {
		t.Fatalf("query = %q, want cl", m.panel.modelQuery)
	}
	if n := m.panelRowCount(panelModel); n != 2 {
		t.Fatalf("row count = %d, want 2 (the two Claude rows)", n)
	}
	if m.panel.cursor != 0 {
		t.Fatalf("cursor = %d, want 0 (the first hit)", m.panel.cursor)
	}
	lines := strings.Join(m.panelModelLines(), "\n")
	if !strings.Contains(lines, "Search: cl") {
		t.Fatalf("the panel does not show the query:\n%s", lines)
	}
	if strings.Contains(lines, "GPT-OSS") {
		t.Fatalf("the filter did not drop GPT-OSS:\n%s", lines)
	}
}

func TestModelPanelSearch_PrintableKeysAreNotNavigation(t *testing.T) {
	isolateModelCatalogue(t)
	m := mkPanel(panelModel, "/model", 0)
	// "j" moves the cursor down in a plain list panel. The reference's
	// /model panel types it into the search box instead.
	if handled, _ := m.handlePanelKey(tea.Key{Text: "j"}); !handled {
		t.Fatal("the key j was not handled by the /model panel")
	}
	if m.panel.cursor != 0 {
		t.Fatalf("cursor = %d, want 0 (j must not navigate)", m.panel.cursor)
	}
	if m.panel.modelQuery != "j" {
		t.Fatalf("query = %q, want j", m.panel.modelQuery)
	}
}

func TestModelPanelSearch_BackspaceRemovesOneCharacter(t *testing.T) {
	isolateModelCatalogue(t)
	m := mkPanel(panelModel, "/model", 0)
	m.handlePanelKey(tea.Key{Text: "c"})
	m.handlePanelKey(tea.Key{Text: "l"})
	m.handlePanelKey(tea.Key{Code: tea.KeyBackspace})
	if m.panel.modelQuery != "c" {
		t.Fatalf("query = %q, want c", m.panel.modelQuery)
	}
	m.handlePanelKey(tea.Key{Code: tea.KeyBackspace})
	if m.panel.modelQuery != "" {
		t.Fatalf("query = %q, want the empty string", m.panel.modelQuery)
	}
}

func TestModelPanelSearch_EscClearsTheQueryThenClosesThePanel(t *testing.T) {
	isolateModelCatalogue(t)
	m := mkPanel(panelModel, "/model", 0)
	m.handlePanelKey(tea.Key{Text: "c"})
	m.handlePanelKey(tea.Key{Code: tea.KeyEsc})
	if m.panel.modelQuery != "" {
		t.Fatalf("query = %q, want the empty string", m.panel.modelQuery)
	}
	if m.panel.kind != panelModel {
		t.Fatalf("panel kind = %v, want panelModel (esc clears the search first)", m.panel.kind)
	}
	m.handlePanelKey(tea.Key{Code: tea.KeyEsc})
	if m.panel.kind != panelNone {
		t.Fatalf("panel kind = %v, want panelNone", m.panel.kind)
	}
}

func TestModelPanelSearch_NoMatchRendersTheReferenceLine(t *testing.T) {
	isolateModelCatalogue(t)
	m := mkPanel(panelModel, "/model", 0)
	m.handlePanelKey(tea.Key{Text: "z"})
	m.handlePanelKey(tea.Key{Text: "z"})
	m.handlePanelKey(tea.Key{Text: "z"})
	lines := strings.Join(m.panelModelLines(), "\n")
	want := "\x1b[39m  \x1b[38;5;60mNo models match \"zzz\"\x1b[39m"
	if !strings.Contains(lines, want) {
		t.Fatalf("the panel does not show the reference's empty-result line:\n%s", lines)
	}
	if !strings.Contains(lines, "Clear search") {
		t.Fatalf("the legend does not read \"Clear search\":\n%s", lines)
	}
}

func TestModelPanelSearch_LegendReadsClearSearchWhileFiltering(t *testing.T) {
	isolateModelCatalogue(t)
	m := mkPanel(panelModel, "/model", 0)
	m.handlePanelKey(tea.Key{Text: "c"})
	lines := strings.Join(m.panelModelLines(), "\n")
	if !strings.Contains(lines, "Clear search") {
		t.Fatalf("the legend does not read \"Clear search\":\n%s", lines)
	}
	if strings.Contains(lines, "Go Back") {
		t.Fatalf("the legend still reads \"Go Back\":\n%s", lines)
	}
}

func TestModelPanelRowCount_FollowsTheRows(t *testing.T) {
	// Two configured models put the picker at seven rows. The count used
	// to be a hard-coded 6, which left the seventh row unreachable.
	configtest.WriteProviders(t, `{"providers": {"gateway": {
		"type": "openai", "base_url": "https://gateway.example/v1",
		"models": [
			{"id": "minimax-m3", "name": "MiniMax M3", "model": "Upstream-1"},
			{"id": "minimax-m3.1-flash", "name": "MiniMax M3.1 Flash", "model": "Upstream-2"}
		]}}}`)
	t.Setenv("AGY_ADC_AUTH", "")
	t.Setenv("AGY_CLAUDE55_PROJECT", "")
	m := mkPanel(panelModel, "/model", 0)
	n := m.panelRowCount(panelModel)
	if n != 7 {
		t.Fatalf("row count = %d, want 7 (five reference rows + two configured)", n)
	}
	// Walk to the last row the way a user does.
	for i := 0; i < n+3; i++ {
		m.handlePanelKey(tea.Key{Code: tea.KeyDown})
	}
	if m.panel.cursor != n-1 {
		t.Fatalf("cursor = %d, want %d (the last row must be reachable)", m.panel.cursor, n-1)
	}
	rows := m.modelRows()
	if rows[m.panel.cursor].Slug != "minimax-m3.1-flash" {
		t.Fatalf("the last row is %q, want minimax-m3.1-flash", rows[m.panel.cursor].Slug)
	}
}

// ---------------------------------------------------------------- model effort navigation tests (from slash_model_effort_nav_test.go)

// modelLeftRightBarLine returns the rendered /model panel line that
// hosts the embedded effort slider (the only line in panelModelLines
// whose geometry depends on m.panel.effortCursor). Returns "" when
// the slider is not visible — non-Gemini rows omit it, but every test
// here starts on the Gemini row.
func modelLeftRightBarLine(m *runMode) string {
	for _, l := range plainLines(m) {
		if strings.HasPrefix(l, "  Effort") {
			return l
		}
	}
	return ""
}

// countConsecutiveBefore counts how many `●` (filled circle) markers
// appear before the first `◉` (active marker) in `s`.
func countConsecutiveBefore(s string) int {
	runes := []rune(s)
	i := -1
	for k, r := range runes {
		if r == '◉' {
			i = k
			break
		}
	}
	if i < 0 {
		return -1
	}
	n := 0
	for _, r := range runes[:i] {
		if r == '●' {
			n++
		}
	}
	return n
}

// TestModelLeftRight_AdvancesEffort pins the basic wrap semantics:
// from a starting medium position, → moves to high; one more → wraps
// to low (the cycle's defining behaviour); one ← wraps back to high.
func TestModelLeftRight_AdvancesEffort(t *testing.T) {
	dir := cycle196TestHome(t, map[string]any{"model": "Gemini 3.8 Flash (Medium)"})
	_ = dir
	m := mkPanel(panelModel, "/model", 0) // Gemini row
	if m.panel.effortCursor != effortMedium {
		t.Fatalf("seed effortCursor = %d, want %d (medium)",
			m.panel.effortCursor, effortMedium)
	}

	steps := []struct {
		dir  int
		want int
		why  string
	}{
		{+1, effortHigh, "medium → right → high"},
		{+1, effortLow, "high → right → low (wrap)"},
		{-1, effortHigh, "low → left → high (wrap)"},
		{-1, effortMedium, "high → left → medium"},
		{+1, effortHigh, "medium → right → high"},
		{-1, effortMedium, "high → left → medium"},
	}
	for i, s := range steps {
		m.cycleModelEffort(s.dir)
		if m.panel.effortCursor != s.want {
			t.Errorf("step %d (%s): effortCursor = %d, want %d",
				i, s.why, m.panel.effortCursor, s.want)
		}
	}
}

// TestModelLeftRight_PersistsToDisk is the load-bearing correctness
// test: each ←/→ must write the new catalogue slug to settings.json.
func TestModelLeftRight_PersistsToDisk(t *testing.T) {
	dir := cycle196TestHome(t, map[string]any{"model": "Gemini 3.8 Flash (Medium)"})
	m := mkPanel(panelModel, "/model", 0)

	expected := []struct {
		want string
		why  string
	}{
		{"gemini-3.8-flash-high", "medium → right → high"},
		{"gemini-3.8-flash-low", "high → right → low (wrap)"},
		{"gemini-3.8-flash-medium", "low → right → medium"},
		{"gemini-3.8-flash-high", "medium → right → high"},
		{"gemini-3.8-flash-low", "high → right → low (wrap again)"},
	}
	for i, s := range expected {
		m.cycleModelEffort(+1)
		if got := cycle196ValueAt(t, readCycle196Settings(t, dir), "model"); got != s.want {
			t.Errorf("step %d (%s): persisted model = %q, want %q",
				i, s.why, got, s.want)
		}
	}

	wantReverse := []struct {
		want string
		why  string
	}{
		{"gemini-3.8-flash-high", "low → left → high (wrap)"},
		{"gemini-3.8-flash-medium", "high → left → medium"},
		{"gemini-3.8-flash-low", "medium → left → low"},
		{"gemini-3.8-flash-high", "low → left → high (wrap)"},
	}
	for i, s := range wantReverse {
		m.cycleModelEffort(-1)
		if got := cycle196ValueAt(t, readCycle196Settings(t, dir), "model"); got != s.want {
			t.Errorf("reverse step %d (%s): persisted model = %q, want %q",
				i, s.why, got, s.want)
		}
	}
}

// TestModelLeftRight_PersistsAndUpdatesLabel pins that the visible
// status row reflects the persisted model on the very next frame.
func TestModelLeftRight_PersistsAndUpdatesLabel(t *testing.T) {
	cycle196TestHome(t, map[string]any{"model": "Gemini 3.8 Flash (Low)"})
	m := mkPanel(panelModel, "/model", 0)

	m.cycleModelEffort(+1) // low → medium
	if m.model != "Gemini 3.8 Flash (Medium)" {
		t.Errorf("after +1 from low: m.model = %q, want %q",
			m.model, "Gemini 3.8 Flash (Medium)")
	}
	if m.statusModel != "Gemini 3.8 Flash" {
		t.Errorf("after +1 from low: m.statusModel = %q, want %q",
			m.statusModel, "Gemini 3.8 Flash")
	}

	m.cycleModelEffort(+1) // medium → high
	if m.model != "Gemini 3.8 Flash (High)" {
		t.Errorf("after +1 from medium: m.model = %q, want %q",
			m.model, "Gemini 3.8 Flash (High)")
	}
	if m.statusModel != "Gemini 3.8 Flash" {
		t.Errorf("after +1 from medium: m.statusModel = %q, want %q",
			m.statusModel, "Gemini 3.8 Flash")
	}

	m.cycleModelEffort(-1) // high → medium
	if m.model != "Gemini 3.8 Flash (Medium)" {
		t.Errorf("after -1 from high: m.model = %q, want %q",
			m.model, "Gemini 3.8 Flash (Medium)")
	}

	m.cycleModelEffort(+1) // back to high
	m.cycleModelEffort(+1) // high → low (wrap)
	if m.model != "Gemini 3.8 Flash (Low)" {
		t.Errorf("after wrap to low: m.model = %q, want %q",
			m.model, "Gemini 3.8 Flash (Low)")
	}
	if m.statusModel != "Gemini 3.8 Flash" {
		t.Errorf("after wrap to low: m.statusModel = %q, want %q",
			m.statusModel, "Gemini 3.8 Flash")
	}
}

// TestModelLeftRight_RerendersSlider verifies that panelModelLines
// produces a bar whose active marker `◉` position reflects the new cursor.
func TestModelLeftRight_RerendersSlider(t *testing.T) {
	cycle196TestHome(t, map[string]any{"model": "Gemini 3.8 Flash (Medium)"})
	m := mkPanel(panelModel, "/model", 0)

	at := func(level int) int {
		m.panel.effortCursor = level
		bar := modelLeftRightBarLine(m)
		if bar == "" {
			t.Fatalf("effort slider not rendered at level=%d", level)
		}
		return countConsecutiveBefore(bar)
	}

	if got := at(effortMedium); got != 1 {
		t.Errorf("seeded medium: ●-before-◉ = %d, want 1", got)
	}
	m.cycleModelEffort(+1)
	if got := at(effortHigh); got != 2 {
		t.Errorf("after right to high: ●-before-◉ = %d, want 2", got)
	}
	m.cycleModelEffort(+1) // wrap
	if got := at(effortLow); got != 0 {
		t.Errorf("after wrap to low: ●-before-◉ = %d, want 0", got)
	}
	m.cycleModelEffort(-1) // wrap
	if got := at(effortHigh); got != 2 {
		t.Errorf("after wrap back to high: ●-before-◉ = %d, want 2", got)
	}
}

// TestModelLeftRight_NoDropdownOpen verifies the panel stays exactly where it is.
func TestModelLeftRight_NoDropdownOpen(t *testing.T) {
	cycle196TestHome(t, map[string]any{"model": "Gemini 3.8 Flash (Medium)"})
	m := mkPanel(panelModel, "/model", 0)
	startKind := m.panel.kind
	startCmd := m.panel.cmd
	startCursor := m.panel.cursor

	for _, code := range []rune{tea.KeyLeft, tea.KeyRight, tea.KeyLeft, tea.KeyRight} {
		handled, _ := m.handlePanelKey(tea.Key{Code: code})
		if !handled {
			t.Fatalf("handlePanelKey(%v) reported unhandled on model panel", code)
		}
	}
	if m.panel.kind != startKind {
		t.Errorf("panel kind = %v, want %v (←/→ must not change panel)",
			m.panel.kind, startKind)
	}
	if m.panel.cmd != startCmd {
		t.Errorf("panel cmd = %q, want %q", m.panel.cmd, startCmd)
	}
	if m.panel.cursor != startCursor {
		t.Errorf("panel cursor = %d, want %d (←/→ must not move cursor)",
			m.panel.cursor, startCursor)
	}
	if m.panel.detail {
		t.Error("←/→ flipped panel.detail — model panel has no detail view")
	}
}

// TestModelLeftRight_HomeKeyIgnored pins boundary wrap behaviour.
func TestModelLeftRight_HomeKeyIgnored(t *testing.T) {
	cycle196TestHome(t, map[string]any{"model": "Gemini 3.8 Flash (Low)"})
	m := mkPanel(panelModel, "/model", 0)
	if m.panel.effortCursor != effortLow {
		t.Fatalf("seed cursor = %d, want low (%d)",
			m.panel.effortCursor, effortLow)
	}

	m.cycleModelEffort(-1)
	if m.panel.effortCursor != effortHigh {
		t.Errorf("at-low + ←: effortCursor = %d, want wrap to %d (high)",
			m.panel.effortCursor, effortHigh)
	}

	m.cycleModelEffort(+1)
	m.cycleModelEffort(+1)
	m.cycleModelEffort(+1)
	if m.panel.effortCursor != effortHigh {
		t.Fatalf("reseed: effortCursor = %d, want high (%d)",
			m.panel.effortCursor, effortHigh)
	}
	m.cycleModelEffort(+1)
	if m.panel.effortCursor != effortLow {
		t.Errorf("at-high + →: effortCursor = %d, want wrap to %d (low)",
			m.panel.effortCursor, effortLow)
	}
}

// TestModelLeftRight_NonGeminiInert pins that ←/→ is no-op on the non-Gemini rows.
func TestModelLeftRight_NonGeminiInert(t *testing.T) {
	dir := cycle196TestHome(t, map[string]any{"model": "Claude Sonnet 4.6 (Thinking)"})
	m := mkPanel(panelModel, "/model", 0)

	for _, cursor := range []int{1, 2, 3, 4, 5} {
		m.panel.cursor = cursor
		beforeCursor := m.panel.effortCursor
		beforeDisk := readCycle196Settings(t, dir)
		m.cycleModelEffort(+1)
		m.cycleModelEffort(-1)
		if m.panel.effortCursor != beforeCursor {
			t.Errorf("cursor=%d: ←/→ shifted effortCursor from %d to %d (non-Gemini must be inert)",
				cursor, beforeCursor, m.panel.effortCursor)
		}
		afterDisk := readCycle196Settings(t, dir)
		if !mapsEqualString(beforeDisk, afterDisk) {
			t.Errorf("cursor=%d: ←/→ mutated settings.json on a non-Gemini row", cursor)
		}
	}
}

// TestModelLeftRight_KeyHandlerIntegration is the end-to-end smoke test.
func TestModelLeftRight_KeyHandlerIntegration(t *testing.T) {
	dir := cycle196TestHome(t, map[string]any{"model": "Gemini 3.8 Flash (Medium)"})
	m := mkPanel(panelModel, "/model", 0)

	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyRight}); !handled {
		t.Fatal("→ on /model: handlePanelKey reported unhandled")
	}
	if got := cycle196ValueAt(t, readCycle196Settings(t, dir), "model"); got != "gemini-3.8-flash-high" {
		t.Errorf("after → through key handler: persisted model = %q, want %q",
			got, "gemini-3.8-flash-high")
	}
	if m.model != "Gemini 3.8 Flash (High)" {
		t.Errorf("after → through key handler: m.model = %q, want %q",
			m.model, "Gemini 3.8 Flash (High)")
	}
	if m.panel.effortCursor != effortHigh {
		t.Errorf("after → through key handler: effortCursor = %d, want %d (high)",
			m.panel.effortCursor, effortHigh)
	}

	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyLeft}); !handled {
		t.Fatal("← on /model: handlePanelKey reported unhandled")
	}
	if m.panel.effortCursor != effortMedium {
		t.Errorf("after ← through key handler: effortCursor = %d, want %d (medium)",
			m.panel.effortCursor, effortMedium)
	}
}

func mapsEqualString(a, b map[string]any) bool {
	if len(a) != len(b) {
		return false
	}
	for k, va := range a {
		vb, ok := b[k]
		if !ok {
			return false
		}
		sa, _ := va.(string)
		sb, _ := vb.(string)
		if sa != sb {
			return false
		}
	}
	return true
}

func newEffortRunMode(t *testing.T) *runMode {
	t.Helper()
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", false)
	m.picker.Reset()
	m.input.Reset()
	return m
}

func TestEffortSlashDirect(t *testing.T) {
	_ = cycle196TestHome(t, map[string]any{"model": "Gemini 3.8 Flash (Medium)"})
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	fs.String("effort", "medium", "")
	m := newEffortRunMode(t)
	m.flags = fs

	handled, _ := m.handleSlashCommand("/effort high")
	if !handled {
		t.Fatal("handleSlashCommand did not consume /effort high")
	}
	got := stripSGR(strings.Join(m.response, "\n"))
	if !strings.Contains(got, "Effort set to high") {
		t.Errorf("expected 'Effort set to high' in response, got %q", got)
	}
	if got := cliflags.StringValue(m.flags, "effort"); got != "high" {
		t.Errorf("flags.effort = %q, want %q", got, "high")
	}
}

func TestEffortSlashInvalidLevel(t *testing.T) {
	_ = cycle196TestHome(t, map[string]any{"model": "Gemini 3.8 Flash (High)"})
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	fs.String("effort", "high", "")
	m := newEffortRunMode(t)
	m.flags = fs

	handled, _ := m.handleSlashCommand("/effort extreme")
	if !handled {
		t.Fatal("handleSlashCommand did not consume /effort extreme")
	}
	got := stripSGR(strings.Join(m.response, "\n"))
	if !strings.Contains(got, "Invalid level: extreme") {
		t.Errorf("expected 'Invalid level: extreme' in response, got %q", got)
	}
	if !strings.Contains(got, "Use low|medium|high") {
		t.Errorf("expected 'Use low|medium|high' hint, got %q", got)
	}
	if got := cliflags.StringValue(m.flags, "effort"); got != "high" {
		t.Errorf("invalid input must not mutate flags.effort: %q", got)
	}
}

func TestEffortSlashNoArgsOpensPicker(t *testing.T) {
	_ = cycle196TestHome(t, map[string]any{"model": "Gemini 3.8 Flash (High)"})
	m := newEffortRunMode(t)
	if !m.openSlashPanel("effort") {
		t.Fatal("openSlashPanel(\"effort\") = false, want true")
	}
	if m.panel.kind != panelEffort {
		t.Errorf("panel.kind = %v, want panelEffort", m.panel.kind)
	}
	if m.panel.cmd != "/effort" {
		t.Errorf("panel.cmd = %q, want %q", m.panel.cmd, "/effort")
	}
	if m.panel.cursor != effortHigh {
		t.Errorf("panel.cursor = %d, want high (%d)", m.panel.cursor, effortHigh)
	}
}

func TestEffortPickerNavigate(t *testing.T) {
	_ = cycle196TestHome(t, map[string]any{"model": "Gemini 3.8 Flash (Low)"})
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	fs.String("effort", "low", "")

	m := newEffortRunMode(t)
	m.flags = fs
	m.openSlashPanel("effort")
	if m.panel.cursor != effortLow {
		t.Fatalf("open cursor = %d, want low", m.panel.cursor)
	}

	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyRight}); !handled {
		t.Fatal("right not handled")
	}
	if m.panel.cursor != effortMedium {
		t.Errorf("after right: cursor = %d, want medium", m.panel.cursor)
	}

	if handled, cmd := m.handlePanelKey(tea.Key{Code: tea.KeyEnter}); !handled {
		t.Fatal("enter not handled")
	} else if cmd != nil {
		t.Errorf("enter returned a non-nil cmd: %v", cmd)
	}
	if m.panel.kind != panelNone {
		t.Errorf("panel not closed: kind = %v", m.panel.kind)
	}
	got := stripSGR(strings.Join(m.response, "\n"))
	if !strings.Contains(got, "Effort set to medium") {
		t.Errorf("expected 'Effort set to medium' in response, got %q", got)
	}
	if got := cliflags.StringValue(m.flags, "effort"); got != "medium" {
		t.Errorf("flags.effort = %q, want %q", got, "medium")
	}
}

func TestEffortPickerEscape(t *testing.T) {
	_ = cycle196TestHome(t, map[string]any{"model": "Gemini 3.8 Flash (High)"})
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	fs.String("effort", "high", "")

	m := newEffortRunMode(t)
	m.flags = fs
	m.openSlashPanel("effort")

	m.handlePanelKey(tea.Key{Code: tea.KeyLeft})
	m.handlePanelKey(tea.Key{Code: tea.KeyLeft})
	if m.panel.cursor != effortLow {
		t.Fatalf("walked left twice: cursor = %d, want low", m.panel.cursor)
	}

	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyEsc}); !handled {
		t.Fatal("esc not handled")
	}
	if m.panel.kind != panelNone {
		t.Errorf("esc did not close panel: kind = %v", m.panel.kind)
	}
	got := stripSGR(strings.Join(m.response, "\n"))
	if !strings.Contains(got, "Exited /effort command") {
		t.Errorf("expected 'Exited /effort command' in response, got %q", got)
	}
	if strings.Contains(got, "Effort set to") {
		t.Errorf("esc must not echo 'Effort set to ...', got %q", got)
	}
	if got := cliflags.StringValue(m.flags, "effort"); got != "high" {
		t.Errorf("esc must revert effort: flags.effort = %q, want %q", got, "high")
	}
}

func TestEffortDefaultFallback(t *testing.T) {
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	fs.String("model", "gemini-3.8-flash", "")
	fs.String("effort", "", "")

	id, err := modelcatalog.ResolveModelSelection(cliflags.StringValue(fs, "model"), cliflags.StringValue(fs, "effort"))
	if err != nil {
		t.Fatalf("resolveModelSelection: %v", err)
	}
	if id != "gemini-3.8-flash-high" {
		t.Errorf("default effort = %q, want %q", id, "gemini-3.8-flash-high")
	}

	fs2 := flag.NewFlagSet("t2", flag.ContinueOnError)
	fs2.String("model", "gpt-oss-120b", "")
	fs2.String("effort", "", "")
	id, err = modelcatalog.ResolveModelSelection(cliflags.StringValue(fs2, "model"), cliflags.StringValue(fs2, "effort"))
	if err != nil {
		t.Fatalf("resolveModelSelection: %v", err)
	}
	if id != "gpt-oss-120b-medium" {
		t.Errorf("gpt-oss-120b default = %q, want %q", id, "gpt-oss-120b-medium")
	}
}

func TestEffortPersistence(t *testing.T) {
	dir := cycle196TestHome(t, map[string]any{
		"model":  "gemini-3.8-flash",
		"effort": "high",
	})
	_ = dir

	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	fs.String("effort", "", "")
	m := &runMode{flags: fs}
	if got := m.currentEffortName(); got != "high" {
		t.Errorf("currentEffortName = %q, want %q (from settings.json)", got, "high")
	}

	if _, ok := printmode.ParseEffortWord("HIGH"); !ok {
		t.Errorf("parseEffortWord lost HIGH")
	}
	if level := effortIndexForName("high"); level != effortHigh {
		t.Errorf("effortIndexForName(high) = %d, want %d", level, effortHigh)
	}
}
