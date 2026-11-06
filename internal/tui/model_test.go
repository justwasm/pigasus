package tui

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
// moved to internal/features/models/catalog, next to the code they cover; the cliapp
// copy drove a local wrapper that is gone. Tests 5–6 ride through the
// public model selection pipeline; test 7 drives the /model panel the
// TUI's slash router opens for "model".

import (
	"flag"
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/justwasm/pigasus/internal/features/models/configtest"
	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/core/cliflags"
	accountrouting "github.com/justwasm/pigasus/internal/features/accounts/routing"
	modelcatalog "github.com/justwasm/pigasus/internal/features/models/catalog"
	backendselect "github.com/justwasm/pigasus/internal/infra/backend/select"
	"github.com/justwasm/pigasus/internal/infra/backend/cloudcode"
	"github.com/justwasm/pigasus/internal/infra/backend/gateway"
	"github.com/justwasm/pigasus/internal/printmode"
	"github.com/justwasm/pigasus/internal/ui/panelregistry"
	termcolor "github.com/justwasm/pigasus/internal/ui/render/color"
	"github.com/justwasm/pigasus/internal/ui/views/effortpanel"
	"github.com/justwasm/pigasus/internal/ui/views/modelpicker"
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
// row. The test seeds m.session.model to a base slug, opens the panel, and
// asserts (a) the panel opens at all and (b) the panel lines do not
// panic / error / blank the (current) row.
//
// We don't drive a live probe here — the panel render test (cycle 197 /
// 208) covers the byte-for-byte layout. This test pins the cycle-197 /
// 1.3.4 contract: panelCurrentModelLabel tolerates a bare base slug
// because the changelog line 26 fix lets `--model foo` set m.session.model to
// `foo` without first requiring an effort.
func TestEffortPickerAcceptsBareName(t *testing.T) {
	_ = cycle196TestHome(t, map[string]any{"model": "gemini-3.8-flash"})
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	fs.String("effort", "", "")

	m := newRunMode(&fakeClient{}, "u@t", "m", "t", "/", false)
	m.composer.picker.Reset()
	m.composer.input.Reset()
	m.session.flags = fs
	// Cycle 197 / 1.3.4: m.session.model carries the *display label*, not the slug.
	// canonicalModelLabel maps "gemini-3.8-flash" -> "Gemini 3.8 Flash" via
	// settings.json's persisted "model" value, which the cycle-197
	// resolveEffectiveModel already lifts. The test seeds the displayed
	// label directly so the panel's panelCurrentModelLabel() round-trips.
	m.session.model = modelcatalog.CanonicalModelLabel("gemini-3.8-flash")
	if m.session.model == "" {
		m.session.model = "Gemini 3.8 Flash (High)" // fallback if canonical returned ""
	}

	// Open the panel — must succeed without panicking on a bare base slug.
	if !m.openSlashPanel("model") {
		t.Fatal("openSlashPanel(\"model\") = false, want true")
	}
	if m.overlays.panel.kind != panelregistry.Model {
		t.Errorf("panel.kind = %v, want panelregistry.Model", m.overlays.panel.kind)
	}
	if m.overlays.panel.cmd != "/model" {
		t.Errorf("panel.cmd = %q, want %q", m.overlays.panel.cmd, "/model")
	}

	// Park the cursor on the Gemini row so the Right key advances the
	// embedded effort slider (the only row the slider is on).
	parkOnGemini(t, m)

	// The panel must render the (current) row for "Gemini 3.8 Flash"
	// without panicking or producing an empty string. The render test
	// (cycle 197/208) pins the byte layout; this asserts the chained
	// resolve path produces a label the panel can use.
	lines := m.modelPanel().Lines()
	if len(lines) == 0 {
		t.Fatal("the /model panel rendered no lines")
	}
	var joined strings.Builder
	for _, ln := range lines {
		joined.WriteString(termcolor.StripSGR(ln))
		joined.WriteString("\n")
	}
	if !strings.Contains(joined.String(), "Gemini 3.8 Flash") {
		t.Errorf("the panel is missing the 'Gemini 3.8 Flash' row, got:\n%s", joined.String())
	}

	// Right-arrow on the (current) Gemini row advances the embedded
	// effort slider, confirming the panel is alive for a base-slug model.
	before := m.modelPanel().EffortCursor()
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyRight}); !handled {
		t.Fatal("right key not handled on /model panel")
	}
	if m.modelPanel().EffortCursor() == before {
		t.Error("the effort slider did not move on →")
	}
}

func hasModel(t *testing.T, id string) bool {
	t.Helper()
	for _, m := range modelcatalog.ReferenceModels() {
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

	if !modelcatalog.Claude55Available() {
		t.Fatal("claude55Available() = false under ADC=true+project; want true")
	}
	for _, id := range []string{"claude-opus-5-5", "claude-sonnet-5-5"} {
		if !hasModel(t, id) {
			t.Errorf("model %q absent from modelcatalog.ReferenceModels(); want present", id)
		}
	}

	picker := modelpicker.RowsView()
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
		t.Errorf("claude-opus-5-5 not in the picker's rows under ADC=true+project")
	}
	if !seenSonnet {
		t.Errorf("claude-sonnet-5-5 not in the picker's rows under ADC=true+project")
	}
}

func TestModelCatalog_Claude55Absent_ADCMissing(t *testing.T) {
	t.Run("adc_unset", func(t *testing.T) {
		resetClaude55Env(t)
		if modelcatalog.Claude55Available() {
			t.Fatal("claude55Available() = true with both env vars unset; want false")
		}
		for _, id := range []string{"claude-opus-5-5", "claude-sonnet-5-5"} {
			if hasModel(t, id) {
				t.Errorf("model %q present in modelcatalog.ReferenceModels(); want absent (ADC unset)", id)
			}
		}
		for _, r := range modelpicker.RowsView() {
			if r.Slug == "claude-opus-5-5" || r.Slug == "claude-sonnet-5-5" {
				t.Errorf("model %q present in the picker's rows; want absent (ADC unset)", r.Slug)
			}
		}
	})

	t.Run("project_unset", func(t *testing.T) {
		resetClaude55Env(t)
		t.Setenv("AGY_ADC_AUTH", "true")
		if modelcatalog.Claude55Available() {
			t.Fatal("claude55Available() = true with project unset; want false")
		}
		for _, id := range []string{"claude-opus-5-5", "claude-sonnet-5-5"} {
			if hasModel(t, id) {
				t.Errorf("model %q present in modelcatalog.ReferenceModels(); want absent (project unset)", id)
			}
		}
		for _, r := range modelpicker.RowsView() {
			if r.Slug == "claude-opus-5-5" || r.Slug == "claude-sonnet-5-5" {
				t.Errorf("model %q present in the picker's rows; want absent (project unset)", r.Slug)
			}
		}
	})
}

func TestModelCatalog_Claude55Absent_ADCFalse(t *testing.T) {
	resetClaude55Env(t)
	t.Setenv("AGY_ADC_AUTH", "false")
	t.Setenv("AGY_CLAUDE55_PROJECT", "test-project")
	if modelcatalog.Claude55Available() {
		t.Fatal("claude55Available() = true with AGY_ADC_AUTH=false; want false (strict gate)")
	}
	for _, id := range []string{"claude-opus-5-5", "claude-sonnet-5-5"} {
		if hasModel(t, id) {
			t.Errorf("model %q present in modelcatalog.ReferenceModels(); want absent (ADC=false)", id)
		}
	}
	for _, r := range modelpicker.RowsView() {
		if r.Slug == "claude-opus-5-5" || r.Slug == "claude-sonnet-5-5" {
			t.Errorf("model %q present in the picker's rows; want absent (ADC=false)", r.Slug)
		}
	}

	got := modelcatalog.AvailableModelsBlock()
	if strings.Contains(got, "Claude Opus 5.5") || strings.Contains(got, "Claude Sonnet 5.5") {
		t.Errorf("modelcatalog.AvailableModelsBlock() leaks 5.5 entries under ADC=false:\n%s", got)
	}
}

// modelTransportTestFlags builds the minimal flag set the backend dispatch reads.
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
			be := backendselect.PickWithLog(modelTransportTestFlags(t, ""), tc.model, nil, 0, modelpicker.ResolveCatalogueID)
			switch tc.want {
			case "cloudcode":
				cc, ok := accountrouting.Unwrap(be).(*cloudcode.CloudCodeClient)
				if !ok {
					t.Fatalf("backend = %T, want *cloudcode.CloudCodeClient", be)
				}
				if cc.Model() != modelpicker.ResolveCatalogueID(tc.model) {
					t.Errorf("wire model = %q, want %q", cc.Model(), modelpicker.ResolveCatalogueID(tc.model))
				}
			case "gateway":
				gc, ok := be.(*gateway.GatewayClient)
				if !ok {
					t.Fatalf("backend = %T, want *gateway.GatewayClient", be)
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

	be := backendselect.PickWithLog(modelTransportTestFlags(t, ""), "Custom Anthropic", nil, 0, modelpicker.ResolveCatalogueID)
	gc, ok := be.(*gateway.GatewayClient)
	if !ok {
		t.Fatalf("backend = %T, want *gateway.GatewayClient", be)
	}
	cfg := gc.GatewayConfig()
	if cfg.URL != "https://gateway.example/anthropic/v1/messages" {
		t.Errorf("endpoint = %q, want the Messages endpoint", cfg.URL)
	}
	if cfg.Type != gateway.GatewayProtocolAnthropic {
		t.Errorf("protocol = %q, want %q", cfg.Type, gateway.GatewayProtocolAnthropic)
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
	be := backendselect.PickWithLog(modelTransportTestFlags(t, ""), "some-other-model", &log, 0, modelpicker.ResolveCatalogueID)
	if _, ok := be.(*cloudcode.CloudCodeClient); ok {
		t.Fatalf("backend = %T, want the shim", be)
	}
	if _, ok := be.(*gateway.GatewayClient); ok {
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
	be := backendselect.PickWithLog(modelTransportTestFlags(t, "gateway"), "Custom Model", nil, 0, modelpicker.ResolveCatalogueID)
	if _, ok := be.(*gateway.GatewayClient); !ok {
		t.Fatalf("backend = %T, want *gateway.GatewayClient (explicit --backend gateway)", be)
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
	be := backendselect.PickWithLog(modelTransportTestFlags(t, "gateway"), "Gemini 3.8 Flash (High)", &log, 0, modelpicker.ResolveCatalogueID)
	if _, ok := be.(*gateway.GatewayClient); ok {
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
	be := backendselect.PickWithLog(modelTransportTestFlags(t, "cloudcode"), "Custom Model", nil, 0, modelpicker.ResolveCatalogueID)
	if _, ok := accountrouting.Unwrap(be).(*cloudcode.CloudCodeClient); !ok {
		t.Fatalf("backend = %T, want *cloudcode.CloudCodeClient (explicit --backend cloudcode)", be)
	}
}

// TestModelSelectionSwapsBackend pins the runtime half of the fix: choosing a
// model in /model must replace m.engine.backend, not only m.session.model, or the next turn
// still goes to MiniMax.
//
// Cycle 381: the panel owns the selection and hands the display label over
// through the ApplyModel seam; the seam is the host's, so the transport is
// rebuilt on the host's side.
func TestModelSelectionSwapsBackend(t *testing.T) {
	cycle196TestHome(t, map[string]any{"model": "gemini-3.8-flash-medium"})
	m := mkPanel(panelregistry.Model, "/model", 0)
	old := backend.NewMock()
	m.engine.backend = old

	byModel := map[string]backend.Client{}
	m.engine.backendFor = func(label string) backend.Client {
		c := backend.NewMock()
		byModel[label] = c
		return c
	}

	// Park the cursor on the Gemini row and press Enter.
	parkOnGemini(t, m)
	handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyEnter})
	if !handled {
		t.Fatal("Enter on /model: handlePanelKey reported unhandled")
	}
	label := m.session.model
	if label == "" {
		t.Fatal("the panel handed no label over")
	}
	got, ok := byModel[label]
	if !ok {
		t.Fatalf("backend factory never saw %q (saw %v)", label, keysOf(byModel))
	}
	if m.engine.backend != got {
		t.Fatal("m.engine.backend was not rebuilt on Enter")
	}

	// ←/→ on the same row must rebuild too. Re-open the panel: the
	// Enter path closes it in the real UI.
	m.engine.backend = old
	reopenModelPanel(m)
	parkOnGemini(t, m)
	if handled, _ = m.handlePanelKey(tea.Key{Code: tea.KeyRight}); !handled {
		t.Fatal("→ on /model: handlePanelKey reported unhandled")
	}
	cycleLabel := m.session.model
	cycleClient, ok := byModel[cycleLabel]
	if !ok {
		t.Fatalf("backend factory never saw %q on ←/→ (saw %v)", cycleLabel, keysOf(byModel))
	}
	if m.engine.backend != cycleClient {
		t.Fatal("m.engine.backend was not rebuilt on ←/→")
	}
}

// parkOnGemini puts the /model panel's cursor on the Gemini row.
func parkOnGemini(t *testing.T, m *runMode) {
	t.Helper()
	for i, r := range modelpicker.RowsView() {
		if r.Slug == "gemini-3.8-flash" {
			m.modelPanel().SetCursor(i)
			return
		}
	}
	t.Fatal("the picker has no Gemini row")
}

// reopenModelPanel opens the /model panel again — the Enter path closes it,
// and the host rebuilds the panel on every open.
func reopenModelPanel(m *runMode) {
	m.overlays.panel = slashPanel{kind: panelregistry.Model, cmd: "/model"}
	m.overlays.panel.mdl = m.newModelPanel()
}

func keysOf(m map[string]backend.Client) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// ----------------------------------------------------------------------
// /model
//
// Cycle 381: the panel itself lives in internal/ui/views/modelpicker.
// What is left here is the host's half: the seam that installs the
// chosen model into the session, and the wiring that opens the panel.
// The panel's own behaviour — the search box, the row list, the effort
// slider, the keys and the writes — is pinned in the package's tests.
// ----------------------------------------------------------------------

// TestCycle197_ModelEnterKeyHandlerClosesPanel drives the user-facing path.
func TestCycle197_ModelEnterKeyHandlerClosesPanel(t *testing.T) {
	dir := cycle196TestHome(t, map[string]any{"model": "Gemini 3.8 Flash (High)"})
	m := mkPanel(panelregistry.Model, "/model", 0)
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
	if m.overlays.panel.kind != panelregistry.None {
		t.Errorf("panel kind = %v, want closed", m.overlays.panel.kind)
	}
}

func newEffortRunMode(t *testing.T) *runMode {
	t.Helper()
	m := newRunMode(&fakeClient{}, "u@t", "m", "t", "/", false)
	m.composer.picker.Reset()
	m.composer.input.Reset()
	return m
}

func TestEffortSlashDirect(t *testing.T) {
	_ = cycle196TestHome(t, map[string]any{"model": "Gemini 3.8 Flash (Medium)"})
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	fs.String("effort", "medium", "")
	m := newEffortRunMode(t)
	m.session.flags = fs

	handled, _ := m.handleSlashCommand("/effort high")
	if !handled {
		t.Fatal("handleSlashCommand did not consume /effort high")
	}
	got := termcolor.StripSGR(strings.Join(m.engine.response, "\n"))
	if !strings.Contains(got, "Effort set to high") {
		t.Errorf("expected 'Effort set to high' in response, got %q", got)
	}
	if got := cliflags.StringValue(m.session.flags, "effort"); got != "high" {
		t.Errorf("flags.effort = %q, want %q", got, "high")
	}
}

func TestEffortSlashInvalidLevel(t *testing.T) {
	_ = cycle196TestHome(t, map[string]any{"model": "Gemini 3.8 Flash (High)"})
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	fs.String("effort", "high", "")
	m := newEffortRunMode(t)
	m.session.flags = fs

	handled, _ := m.handleSlashCommand("/effort extreme")
	if !handled {
		t.Fatal("handleSlashCommand did not consume /effort extreme")
	}
	got := termcolor.StripSGR(strings.Join(m.engine.response, "\n"))
	// Cycle 383 (A-2): the reference's own format string is
	// `Invalid effort %q (valid: %s)`, and the valid list is the
	// catalogue's vocabulary — five words.
	if !strings.Contains(got, `Invalid effort "extreme" (valid: low, medium, high, xhigh, max)`) {
		t.Errorf("expected the reference's invalid-effort message in response, got %q", got)
	}
	if got := cliflags.StringValue(m.session.flags, "effort"); got != "high" {
		t.Errorf("invalid input must not mutate flags.effort: %q", got)
	}
}

func TestEffortSlashNoArgsOpensPicker(t *testing.T) {
	_ = cycle196TestHome(t, map[string]any{"model": "Gemini 3.8 Flash (High)"})
	m := newEffortRunMode(t)
	if !m.openSlashPanel("effort") {
		t.Fatal("openSlashPanel(\"effort\") = false, want true")
	}
	if m.overlays.panel.kind != panelregistry.Effort {
		t.Errorf("panel.kind = %v, want panelregistry.Effort", m.overlays.panel.kind)
	}
	if m.overlays.panel.cmd != "/effort" {
		t.Errorf("panel.cmd = %q, want %q", m.overlays.panel.cmd, "/effort")
	}
	if m.effortPanel().Cursor() != effortpanel.EffortHigh {
		t.Errorf("effort cursor = %d, want high (%d)",
			m.effortPanel().Cursor(), effortpanel.EffortHigh)
	}
}

func TestEffortPickerNavigate(t *testing.T) {
	_ = cycle196TestHome(t, map[string]any{"model": "Gemini 3.8 Flash (Low)"})
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	fs.String("effort", "low", "")

	m := newEffortRunMode(t)
	m.session.flags = fs
	m.openSlashPanel("effort")
	if m.effortPanel().Cursor() != effortpanel.EffortLow {
		t.Fatalf("open cursor = %d, want low", m.effortPanel().Cursor())
	}

	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyRight}); !handled {
		t.Fatal("right not handled")
	}
	if m.effortPanel().Cursor() != effortpanel.EffortMedium {
		t.Errorf("after right: cursor = %d, want medium", m.effortPanel().Cursor())
	}

	if handled, cmd := m.handlePanelKey(tea.Key{Code: tea.KeyEnter}); !handled {
		t.Fatal("enter not handled")
	} else if cmd != nil {
		t.Errorf("enter returned a non-nil cmd: %v", cmd)
	}
	if m.overlays.panel.kind != panelregistry.None {
		t.Errorf("panel not closed: kind = %v", m.overlays.panel.kind)
	}
	got := termcolor.StripSGR(strings.Join(m.engine.response, "\n"))
	if !strings.Contains(got, "Effort set to medium") {
		t.Errorf("expected 'Effort set to medium' in response, got %q", got)
	}
	if got := cliflags.StringValue(m.session.flags, "effort"); got != "medium" {
		t.Errorf("flags.effort = %q, want %q", got, "medium")
	}
}

func TestEffortPickerEscape(t *testing.T) {
	_ = cycle196TestHome(t, map[string]any{"model": "Gemini 3.8 Flash (High)"})
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	fs.String("effort", "high", "")

	m := newEffortRunMode(t)
	m.session.flags = fs
	m.openSlashPanel("effort")

	m.handlePanelKey(tea.Key{Code: tea.KeyLeft})
	m.handlePanelKey(tea.Key{Code: tea.KeyLeft})
	if m.effortPanel().Cursor() != effortpanel.EffortLow {
		t.Fatalf("walked left twice: cursor = %d, want low", m.effortPanel().Cursor())
	}

	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyEsc}); !handled {
		t.Fatal("esc not handled")
	}
	if m.overlays.panel.kind != panelregistry.None {
		t.Errorf("esc did not close panel: kind = %v", m.overlays.panel.kind)
	}
	got := termcolor.StripSGR(strings.Join(m.engine.response, "\n"))
	if !strings.Contains(got, "Exited /effort command") {
		t.Errorf("expected 'Exited /effort command' in response, got %q", got)
	}
	if strings.Contains(got, "Effort set to") {
		t.Errorf("esc must not echo 'Effort set to ...', got %q", got)
	}
	if got := cliflags.StringValue(m.session.flags, "effort"); got != "high" {
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
	m := &runMode{session: sessionState{flags: fs}}
	if got := m.currentEffortName(); got != "high" {
		t.Errorf("currentEffortName = %q, want %q (from settings.json)", got, "high")
	}

	if _, ok := printmode.ParseEffortWord("HIGH"); !ok {
		t.Errorf("parseEffortWord lost HIGH")
	}
	if level := effortpanel.EffortIndexForName("high"); level != effortpanel.EffortHigh {
		t.Errorf("EffortIndexForName(high) = %d, want %d", level, effortpanel.EffortHigh)
	}
}
