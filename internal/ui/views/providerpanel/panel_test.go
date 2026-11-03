// panel_test.go — the /provider panel's interactive half.
//
// The panel is tested here, not in the host: it owns the rows, the form,
// the keys and the writes, and a test that drives those things belongs
// with the code that answers them.

package providerpanel

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/justwasm/pigasus/internal/features/models"
	"github.com/justwasm/pigasus/internal/features/models/configtest"
	"github.com/justwasm/pigasus/internal/features/oauthflow"
)

// newTestPanel writes a configuration and opens the panel on it.
//
// The configuration is isolated: the panel reads the file the user has on
// disk, and a test must not read — or write — the user's own providers.
func newTestPanel(t *testing.T, body string) *Panel {
	t.Helper()
	return newTestPanelWith(t, body, Seams{})
}

// newTestPanelWith is newTestPanel with the host's seams replaced.
func newTestPanelWith(t *testing.T, body string, seams Seams) *Panel {
	t.Helper()
	configtest.Isolate(t)
	if body != "" {
		configtest.WriteProviders(t, body)
	}
	p := New(seams)
	p.Reload()
	return p
}

// providerTypeField types a value into the field the cursor is on and
// moves to the next one.
func providerTypeField(p *Panel, value string) {
	p.input.SetValue(value)
	p.formMove(1)
}

// stripANSI removes the escape sequences from a rendered line.
func stripANSI(s string) string {
	return ansi.Strip(s)
}

func TestPanel_RowsListProvidersModelsAndBuiltins(t *testing.T) {
	p := newTestPanel(t, `{"providers": {"gateway": {"id": "gateway",
	  "base_url": "https://gw.example/v1", "models": [{"id": "minimax-m3", "model": "MiniMax-M3"}]}}}`)
	rows := p.rows
	if len(rows) < 3 {
		t.Fatalf("rows = %d, want the provider, its model and the built-in group", len(rows))
	}
	if rows[0].Kind != RowProvider || rows[0].Provider != "gateway" {
		t.Fatalf("row 0 = %+v, want the gateway provider", rows[0])
	}
	if rows[1].Kind != RowModel || rows[1].Model != "minimax-m3" {
		t.Fatalf("row 1 = %+v, want the minimax-m3 model", rows[1])
	}
	var builtin bool
	for _, r := range rows {
		if r.Kind == RowBuiltin && r.Model == "gemini-3.8-flash-high" {
			builtin = true
		}
	}
	if !builtin {
		t.Fatalf("the built-in catalogue is missing from the rows: %+v", rows)
	}
}

func TestPanel_AddProviderWritesTheFile(t *testing.T) {
	p := newTestPanel(t, "")
	p.HandleKey(tea.Key{Code: 'a'})
	if p.form == nil {
		t.Fatal("`a` did not open the form")
	}
	providerTypeField(p, "gateway")
	providerTypeField(p, "Gateway")
	p.formMove(1)
	providerTypeField(p, "https://gw.example/v1")
	p.HandleKey(tea.Key{Code: tea.KeyEnter})

	body, err := os.ReadFile(models.ProvidersPath())
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	for _, want := range []string{`"gateway"`, `"https://gw.example/v1"`, `"name": "Gateway"`} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("the file is missing %s:\n%s", want, body)
		}
	}
	if p.form != nil {
		t.Fatal("the form stayed open after the save")
	}
}

func TestPanel_EditKeepsTheCredential(t *testing.T) {
	p := newTestPanel(t, `{"providers": {"gateway": {"id": "gateway",
	  "base_url": "https://gw.example/v1", "api_key": "sk-literal-secret"}}}`)
	p.cursor = 0
	p.HandleKey(tea.Key{Code: tea.KeyEnter})
	if p.form == nil {
		t.Fatal("enter did not open the form")
	}
	if got := p.form.Value(FieldAPIKey); got != "" {
		t.Fatalf("the credential is in the form: %q", got)
	}
	p.HandleKey(tea.Key{Code: tea.KeyEnter})
	body, err := os.ReadFile(models.ProvidersPath())
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(body), `"sk-literal-secret"`) {
		t.Fatalf("the credential was lost:\n%s", body)
	}
}

func TestPanel_ShowsAnEnvironmentReference(t *testing.T) {
	p := newTestPanel(t, `{"providers": {"gateway": {"id": "gateway",
	  "base_url": "https://gw.example/v1", "api_key": "$GW_KEY"}}}`)
	p.cursor = 0
	p.HandleKey(tea.Key{Code: tea.KeyEnter})
	if got := p.form.Value(FieldAPIKey); got != "$GW_KEY" {
		t.Fatalf("the credential field = %q, want the environment reference", got)
	}
}

func TestPanel_DeleteNeedsTwoPresses(t *testing.T) {
	p := newTestPanel(t, `{"providers": {"gateway": {"id": "gateway",
	  "base_url": "https://gw.example/v1", "models": [{"id": "minimax-m3"}]}}}`)
	p.cursor = 0
	p.HandleKey(tea.Key{Code: 'd'})
	body, _ := os.ReadFile(models.ProvidersPath())
	if !strings.Contains(string(body), "gateway") {
		t.Fatalf("the provider was deleted on the first press:\n%s", body)
	}
	if !strings.Contains(p.notice, "again") {
		t.Fatalf("notice = %q, want the confirmation", p.notice)
	}
	p.HandleKey(tea.Key{Code: 'd'})
	body, _ = os.ReadFile(models.ProvidersPath())
	if strings.Contains(string(body), "gateway") {
		t.Fatalf("the provider survived the second press:\n%s", body)
	}
}

func TestPanel_DeleteAModelKeepsTheProvider(t *testing.T) {
	p := newTestPanel(t, `{"providers": {"gateway": {"id": "gateway",
	  "base_url": "https://gw.example/v1", "models": [{"id": "minimax-m3"}]}}}`)
	p.cursor = 1
	p.HandleKey(tea.Key{Code: 'd'})
	p.HandleKey(tea.Key{Code: 'd'})
	body, _ := os.ReadFile(models.ProvidersPath())
	if !strings.Contains(string(body), "gateway") {
		t.Fatalf("the provider was removed too:\n%s", body)
	}
	if strings.Contains(string(body), "minimax-m3") {
		t.Fatalf("the model survived:\n%s", body)
	}
}

func TestPanel_ChoiceFieldCycles(t *testing.T) {
	p := newTestPanel(t, `{"providers": {"gateway": {"id": "gateway",
	  "base_url": "https://gw.example/v1"}}}`)
	p.cursor = 0
	p.HandleKey(tea.Key{Code: tea.KeyEnter})
	p.formMove(1)
	if got := p.form.Value(FieldType); got != "openai" {
		t.Fatalf("type = %q, want openai", got)
	}
	p.HandleKey(tea.Key{Code: tea.KeyRight})
	if got := p.form.Value(FieldType); got != "anthropic" {
		t.Fatalf("type = %q, want anthropic", got)
	}
	p.HandleKey(tea.Key{Code: tea.KeyLeft})
	if got := p.form.Value(FieldType); got != "openai" {
		t.Fatalf("type = %q, want openai", got)
	}
}

func TestPanel_BuiltinRowsAreNotEditable(t *testing.T) {
	p := newTestPanel(t, "")
	for i, r := range p.rows {
		if r.Kind == RowBuiltin && r.Model != "" {
			p.cursor = i
			p.HandleKey(tea.Key{Code: tea.KeyEnter})
			if p.form != nil {
				t.Fatal("a built-in model opened the edit form")
			}
			if !strings.Contains(p.notice, "/model") {
				t.Fatalf("notice = %q, want the pointer to /model", p.notice)
			}
			return
		}
	}
	t.Fatal("the built-in group is empty")
}

func TestPanel_EscCancelsTheForm(t *testing.T) {
	p := newTestPanel(t, "")
	p.HandleKey(tea.Key{Code: 'a'})
	p.HandleKey(tea.Key{Code: tea.KeyEsc})
	if p.form != nil {
		t.Fatal("esc did not cancel the form")
	}
	if _, err := os.Stat(models.ProvidersPath()); !os.IsNotExist(err) {
		t.Fatalf("the cancelled form wrote the file: %v", err)
	}
}

func TestPanel_VerifyKeyStartsAProbe(t *testing.T) {
	p := newTestPanel(t, `{"providers": {"gateway": {"id": "gateway",
	  "base_url": "https://gw.example/v1", "models": [{"id": "minimax-m3"}]}}}`)
	p.cursor = 0
	handled, cmd := p.HandleKey(tea.Key{Code: 'v'})
	if !handled {
		t.Fatal("`v` was not handled by the panel")
	}
	if cmd == nil {
		t.Fatal("`v` did not start a probe")
	}
	if !p.verifying {
		t.Fatal("the panel is not showing the probe as in flight")
	}
	if p.verifyLabel != "gateway" {
		t.Fatalf("verifyLabel = %q, want the provider's id", p.verifyLabel)
	}
}

func TestPanel_VerifyResultIsRendered(t *testing.T) {
	p := newTestPanel(t, `{"providers": {"gateway": {"id": "gateway",
	  "base_url": "https://gw.example/v1"}}}`)
	p.HandleKey(tea.Key{Code: 'v'})
	p.ApplyVerifyMsg(VerifyMsg{
		Result: models.VerifyResult{OK: true, Detail: "the model answered"},
	})
	if p.verifying {
		t.Fatal("the panel still shows the probe as in flight")
	}
	if p.verify == nil || !p.verify.OK {
		t.Fatalf("verify = %+v, want the successful outcome", p.verify)
	}
	line := p.VerifyLine()
	if !strings.Contains(line, "the model answered") {
		t.Fatalf("the verification line = %q, want the outcome", line)
	}
	if !strings.Contains(line, "gateway") {
		t.Fatalf("the verification line = %q, want it to name the provider", line)
	}
}

func TestPanel_AFailedProbeIsRenderedAsAFailure(t *testing.T) {
	p := newTestPanel(t, `{"providers": {"gateway": {"id": "gateway",
	  "base_url": "https://gw.example/v1"}}}`)
	p.HandleKey(tea.Key{Code: 'v'})
	p.ApplyVerifyMsg(VerifyMsg{
		Result: models.VerifyResult{Status: 401, Detail: "the credential was rejected (HTTP 401)"},
	})
	line := p.VerifyLine()
	if !strings.Contains(line, "✗") {
		t.Fatalf("the verification line = %q, want a ✗", line)
	}
	if !strings.Contains(line, "rejected") {
		t.Fatalf("the verification line = %q, want the reason", line)
	}
}

func TestPanel_ASaveStartsAProbe(t *testing.T) {
	p := newTestPanel(t, `{"providers": {"gateway": {"id": "gateway",
	  "base_url": "https://gw.example/v1"}}}`)
	p.cursor = 0
	p.HandleKey(tea.Key{Code: tea.KeyEnter})
	_, cmd := p.HandleKey(tea.Key{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("the save did not start a probe")
	}
	if !p.verifying {
		t.Fatal("the panel is not showing the probe as in flight")
	}
}

func TestPanel_VerifyProbesTheTypedCredential(t *testing.T) {
	var seen string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get("Authorization")
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()
	p := newTestPanel(t, fmt.Sprintf(`{"providers": {"gateway": {"id": "gateway",
	  "base_url": %q, "api_key": "sk-stored"}}}`, srv.URL+"/v1"))
	p.cursor = 0
	p.HandleKey(tea.Key{Code: tea.KeyEnter})
	form := p.form
	idx := -1
	for i, f := range form.Fields {
		if f.Label == FieldAPIKey {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatal("the form has no credential field")
	}
	form.Cursor = idx
	p.input.SetValue("sk-typed")
	_, cmd := p.HandleKey(tea.Key{Code: 'v'})
	if cmd == nil {
		t.Fatal("`v` did not start a probe")
	}
	if _, ok := cmd().(VerifyMsg); !ok {
		t.Fatal("the command did not produce a VerifyMsg")
	}
	if seen != "Bearer sk-typed" {
		t.Fatalf("the probe carried %q, want the typed credential", seen)
	}
}

func TestPanel_VerifyReachesTheEndpointWithTheStoredCredential(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()
	p := newTestPanel(t, fmt.Sprintf(`{"providers": {"gateway": {"id": "gateway",
	  "base_url": %q, "api_key": "sk-stored"}}}`, srv.URL+"/v1"))
	p.cursor = 0
	_, cmd := p.HandleKey(tea.Key{Code: 'v'})
	if cmd == nil {
		t.Fatal("`v` did not start a probe")
	}
	msg, ok := cmd().(VerifyMsg)
	if !ok {
		t.Fatal("the command did not produce a VerifyMsg")
	}
	if !msg.Result.OK {
		t.Fatalf("the probe failed: %s", msg.Result.Detail)
	}
}

func TestPanel_PolishedUIFeatures(t *testing.T) {
	// 1. Current model badge and delete confirmation naming target
	p := newTestPanelWith(t, `{"providers": {"gateway": {"id": "gateway",
	  "base_url": "https://gw.example/v1", "models": [{"id": "minimax-m3", "model": "MiniMax-M3"}]}}}`,
		Seams{CurrentModel: func() string { return "minimax-m3" }})

	lines := p.Lines(0)
	joined := stripANSI(strings.Join(lines, "\n"))
	if !strings.Contains(joined, "minimax-m3") || !strings.Contains(joined, "(current)") {
		t.Fatalf("expected (current) badge for active model minimax-m3, got:\n%s", joined)
	}

	// 2. Delete notice names the targeted provider and model
	p.cursor = 0 // provider row
	p.HandleKey(tea.Key{Code: 'd'})
	if !strings.Contains(p.notice, "delete provider gateway") {
		t.Fatalf("provider delete notice = %q, want target provider name", p.notice)
	}
	p.cursor = 1 // model row
	p.confirm = -1
	p.HandleKey(tea.Key{Code: 'd'})
	if !strings.Contains(p.notice, "delete model minimax-m3 (from gateway)") {
		t.Fatalf("model delete notice = %q, want target model name", p.notice)
	}

	// 3. Styled notice with symbols
	styledLines := p.Lines(0)
	styledJoined := strings.Join(styledLines, "\n")
	if !strings.Contains(styledJoined, "⚠") {
		t.Fatalf("expected ⚠ in delete warning notice, got:\n%s", styledJoined)
	}

	p.notice = "saved provider gateway"
	styledLines = p.Lines(0)
	styledJoined = strings.Join(styledLines, "\n")
	if !strings.Contains(styledJoined, "✓") {
		t.Fatalf("expected ✓ in save success notice, got:\n%s", styledJoined)
	}

	// 4. Form UX: Provider form shows Verify; Model form does not
	p.cursor = 0
	p.openEditForm()
	formLines := p.formLines()
	formJoined := stripANSI(strings.Join(formLines, "\n"))
	if !strings.Contains(formJoined, "v Verify") {
		t.Fatalf("provider form expected 'v Verify' hint, got:\n%s", formJoined)
	}
	// Focus on Choice field (FieldType is at index 1 for existing provider)
	p.formMove(1)
	activeChoiceLine := stripANSI(p.formFieldLine(p.form.Field(), true))
	if !strings.Contains(activeChoiceLine, "‹ openai ›") || !strings.Contains(activeChoiceLine, "←/→") {
		t.Fatalf("active choice field expected '‹ openai › (←/→)', got: %q", activeChoiceLine)
	}
	inactiveChoiceLine := stripANSI(p.formFieldLine(p.form.Field(), false))
	if !strings.Contains(inactiveChoiceLine, "[ openai ]") {
		t.Fatalf("inactive choice field expected '[ openai ]', got: %q", inactiveChoiceLine)
	}

	// Close provider form and open model form
	p.form = nil
	p.cursor = 1
	p.openEditForm()
	modelFormLines := p.formLines()
	modelFormJoined := stripANSI(strings.Join(modelFormLines, "\n"))
	if strings.Contains(modelFormJoined, "v Verify") {
		t.Fatalf("model form should NOT show 'v Verify' hint, got:\n%s", modelFormJoined)
	}

	// 5. Empty custom providers onboarding hint
	emptyP := newTestPanel(t, "")
	emptyLines := emptyP.Lines(0)
	emptyJoined := stripANSI(strings.Join(emptyLines, "\n"))
	if !strings.Contains(emptyJoined, "No custom providers configured") {
		t.Fatalf("expected onboarding hint when no custom providers, got:\n%s", emptyJoined)
	}
}

func TestPanel_PresetProvidersListedAndConfigured(t *testing.T) {
	p := newTestPanel(t, "")
	rows := p.rows

	// Find the deepseek preset row
	deepseekIdx := -1
	for i, r := range rows {
		if r.Kind == RowPreset && r.Provider == "deepseek" {
			deepseekIdx = i
			break
		}
	}
	if deepseekIdx < 0 {
		t.Fatalf("expected deepseek preset row in provider panel, got rows: %+v", rows)
	}

	// Move cursor to deepseek preset and press enter
	p.cursor = deepseekIdx
	p.HandleKey(tea.Key{Code: tea.KeyEnter})

	form := p.form
	if form == nil {
		t.Fatal("expected form to open for deepseek preset, got nil")
	}
	if form.Provider != "deepseek" {
		t.Errorf("form.Provider = %q, want deepseek", form.Provider)
	}
	if form.Value(FieldName) != "DeepSeek" {
		t.Errorf("form Name = %q, want DeepSeek", form.Value(FieldName))
	}
	if !strings.Contains(form.Value(FieldBaseURL), "deepseek.com") {
		t.Errorf("form BaseURL = %q, want deepseek.com", form.Value(FieldBaseURL))
	}
	if form.Field().Label != FieldAPIKey {
		t.Errorf("expected cursor directly on API Key field, got on %q", form.Field().Label)
	}

	// Type an API key and submit
	p.input.SetValue("sk-preset-test-key")
	p.formStore()
	p.HandleKey(tea.Key{Code: tea.KeyEnter})

	if p.form != nil {
		t.Fatal("expected form to close after save")
	}
	if !strings.Contains(p.notice, "saved provider deepseek") {
		t.Fatalf("notice = %q, want saved provider deepseek", p.notice)
	}

	// Check saved provider in editor
	ed := models.ProviderEditor()
	list, err := ed.List()
	if err != nil {
		t.Fatalf("ed.List: %v", err)
	}
	saved, ok := list["deepseek"]
	if !ok {
		t.Fatal("saved provider deepseek not found in editor")
	}
	if saved.APIKey != "sk-preset-test-key" {
		t.Errorf("saved.APIKey = %q, want sk-preset-test-key", saved.APIKey)
	}
	if len(saved.Models) == 0 {
		t.Fatal("expected preset models to be preloaded, got 0")
	}

	// Now deepseek should be in configured provider rows, not duplicated in presets
	foundConfigured := false
	foundDuplicatePreset := false
	for _, r := range p.rows {
		if r.Kind == RowProvider && r.Provider == "deepseek" {
			foundConfigured = true
		}
		if r.Kind == RowPreset && r.Provider == "deepseek" {
			foundDuplicatePreset = true
		}
	}
	if !foundConfigured {
		t.Error("expected deepseek to be listed as a configured provider row")
	}
	if foundDuplicatePreset {
		t.Error("deepseek should no longer appear in preset list once configured")
	}
}

func TestPanel_SearchFiltering(t *testing.T) {
	p := newTestPanel(t, `{"providers": {"gateway": {"id": "gateway",
	  "base_url": "https://gw.example/v1", "models": [{"id": "minimax-m3", "model": "MiniMax-M3"}]}}}`)

	// 1. Initially on Configured tab with empty query
	lines := p.Lines(0)
	joined := stripANSI(strings.Join(lines, "\n"))
	if !strings.Contains(joined, "Configured") || !strings.Contains(joined, "gateway") {
		t.Fatalf("expected Configured tab with gateway, got:\n%s", joined)
	}

	// 2. Activate search via '/' and type "mini"
	p.HandleKey(tea.Key{Code: '/'})
	if !p.searchActive {
		t.Fatal("expected search to be active after '/'")
	}
	for _, ch := range "mini" {
		p.HandleKey(tea.Key{Code: rune(ch), Text: string(ch)})
	}
	if p.query != "mini" {
		t.Fatalf("query = %q, want 'mini'", p.query)
	}

	filteredLines := p.Lines(0)
	filteredJoined := stripANSI(strings.Join(filteredLines, "\n"))
	if !strings.Contains(filteredJoined, "Search: mini") {
		t.Fatalf("expected 'Search: mini' in header, got:\n%s", filteredJoined)
	}
	if !strings.Contains(filteredJoined, "minimax-m3") {
		t.Fatalf("expected minimax-m3 in search results, got:\n%s", filteredJoined)
	}

	// 3. Backspace removes character
	p.HandleKey(tea.Key{Code: tea.KeyBackspace})
	if p.query != "min" {
		t.Fatalf("after backspace query = %q, want 'min'", p.query)
	}

	// 4. Esc clears query
	p.HandleKey(tea.Key{Code: tea.KeyEsc})
	if p.query != "" {
		t.Fatalf("after esc query = %q, want empty", p.query)
	}

	// 5. Query matching nothing shows empty notice
	for _, ch := range "nonexistent_query_xyz" {
		p.HandleKey(tea.Key{Code: rune(ch), Text: string(ch)})
	}
	emptyLines := p.Lines(0)
	emptyJoined := stripANSI(strings.Join(emptyLines, "\n"))
	if !strings.Contains(emptyJoined, "No matching providers or models") {
		t.Fatalf("expected empty search notice, got:\n%s", emptyJoined)
	}
}

func TestPanel_TabSwitching(t *testing.T) {
	p := newTestPanel(t, `{"providers": {"gateway": {"id": "gateway",
	  "base_url": "https://gw.example/v1", "models": [{"id": "m1"}]}}}`)

	// Initial: Tab 0 (Configured)
	if p.tab != int(TabConfigured) {
		t.Fatalf("initial tab = %d, want TabConfigured (0)", p.tab)
	}
	cfgLines := stripANSI(strings.Join(p.Lines(0), "\n"))
	if !strings.Contains(cfgLines, "gateway") {
		t.Fatalf("Configured tab should contain gateway, got:\n%s", cfgLines)
	}

	// Tab: moves to Tab 1 (Presets)
	p.HandleKey(tea.Key{Code: tea.KeyTab})
	if p.tab != int(TabPresets) {
		t.Fatalf("after Tab, tab = %d, want TabPresets (1)", p.tab)
	}
	presetLines := stripANSI(strings.Join(p.Lines(0), "\n"))
	if !strings.Contains(presetLines, "deepseek") && !strings.Contains(presetLines, "DeepSeek") {
		t.Fatalf("Presets tab should contain deepseek, got:\n%s", presetLines)
	}

	// Tab again: moves to Tab 2 (All)
	p.HandleKey(tea.Key{Code: tea.KeyTab})
	if p.tab != int(TabAll) {
		t.Fatalf("after 2nd Tab, tab = %d, want TabAll (2)", p.tab)
	}
	allLines := stripANSI(strings.Join(p.Lines(0), "\n"))
	if !strings.Contains(allLines, "gateway") || (!strings.Contains(allLines, "deepseek") && !strings.Contains(allLines, "DeepSeek")) {
		t.Fatalf("All tab should contain both gateway and deepseek, got:\n%s", allLines)
	}

	// Tab 3rd time: wraps back to Tab 0 (Configured)
	p.HandleKey(tea.Key{Code: tea.KeyTab})
	if p.tab != int(TabConfigured) {
		t.Fatalf("after 3rd Tab, tab = %d, want TabConfigured (0)", p.tab)
	}

	// Shift+Tab: moves backwards to Tab 2 (All)
	p.HandleKey(tea.Key{Code: tea.KeyTab, Mod: tea.ModShift})
	if p.tab != int(TabAll) {
		t.Fatalf("after Shift+Tab, tab = %d, want TabAll (2)", p.tab)
	}
}

func TestPanel_SearchAndConfigurePreset(t *testing.T) {
	p := newTestPanel(t, "")
	// Switch to Presets tab
	p.HandleKey(tea.Key{Code: tea.KeyTab})

	// Activate search and search "deepseek"
	p.HandleKey(tea.Key{Code: '/'})
	for _, ch := range "deepseek" {
		p.HandleKey(tea.Key{Code: rune(ch), Text: string(ch)})
	}
	if len(p.Rows()) == 0 {
		t.Fatal("expected deepseek preset row in search results")
	}

	// Press Enter to configure the filtered preset
	p.cursor = 0
	p.HandleKey(tea.Key{Code: tea.KeyEnter})

	form := p.form
	if form == nil {
		t.Fatal("expected form to open for deepseek preset from search")
	}
	if form.Provider != "deepseek" {
		t.Errorf("form.Provider = %q, want deepseek", form.Provider)
	}
	// Cursor should focus on API key
	if form.Field().Label != FieldAPIKey {
		t.Errorf("expected cursor on APIKey field, got %q", form.Field().Label)
	}
}

func TestPanel_FormPasteCmdV(t *testing.T) {
	clip := "sk-pasted-secret-key"
	p := newTestPanelWith(t, "", Seams{
		ClipboardRead: func() string { return clip },
	})
	// Switch to Presets and configure deepseek
	p.HandleKey(tea.Key{Code: tea.KeyTab})
	p.cursor = 0
	p.HandleKey(tea.Key{Code: tea.KeyEnter})

	if p.form == nil {
		t.Fatal("expected form to open")
	}

	// Press Cmd+V (ModSuper)
	p.HandleKey(tea.Key{Code: 'v', Mod: tea.ModSuper})
	if got := p.input.Value(); got != "sk-pasted-secret-key" {
		t.Fatalf("after Cmd+V, input.Value = %q, want sk-pasted-secret-key", got)
	}
	if got := p.form.Field().Value; got != "sk-pasted-secret-key" {
		t.Fatalf("after Cmd+V, form.Field.Value = %q, want sk-pasted-secret-key", got)
	}

	// Test Ctrl+V (ModCtrl)
	clip = "sk-ctrl-v-secret"
	p.input.SetValue("")
	p.HandleKey(tea.Key{Code: 'v', Mod: tea.ModCtrl})
	if got := p.input.Value(); got != "sk-ctrl-v-secret" {
		t.Fatalf("after Ctrl+V, input.Value = %q, want sk-ctrl-v-secret", got)
	}
}

func TestPanel_FormBracketedPaste(t *testing.T) {
	p := newTestPanel(t, "")
	p.HandleKey(tea.Key{Code: tea.KeyTab})
	p.cursor = 0
	p.HandleKey(tea.Key{Code: tea.KeyEnter})

	if p.form == nil {
		t.Fatal("expected form to open")
	}

	// Deliver a bracketed paste.
	p.HandlePaste("  sk-bracketed-paste-token  \n")
	if got := p.input.Value(); got != "sk-bracketed-paste-token" {
		t.Fatalf("after bracketed paste, input.Value = %q, want sk-bracketed-paste-token", got)
	}
	if got := p.form.Field().Value; got != "sk-bracketed-paste-token" {
		t.Fatalf("after bracketed paste, form.Field.Value = %q, want sk-bracketed-paste-token", got)
	}
}

func TestPanel_FormTypeLiteralV(t *testing.T) {
	p := newTestPanel(t, "")
	p.HandleKey(tea.Key{Code: tea.KeyTab})
	p.cursor = 0
	p.HandleKey(tea.Key{Code: tea.KeyEnter})

	p.input.SetValue("model-")
	// Type 'v' with Text: "v"
	p.HandleKey(tea.Key{Code: 'v', Text: "v"})
	if got := p.input.Value(); got != "model-v" {
		t.Fatalf("after typing 'v', input.Value = %q, want model-v", got)
	}
}

func TestPanel_ListPasteCmdV(t *testing.T) {
	p := newTestPanelWith(t, "", Seams{
		ClipboardRead: func() string { return "deepseek" },
	})

	// In list mode, press Cmd+V
	p.HandleKey(tea.Key{Code: 'v', Mod: tea.ModSuper})
	if p.query != "deepseek" {
		t.Fatalf("after Cmd+V in list mode, query = %q, want deepseek", p.query)
	}
	if !p.searchActive {
		t.Fatal("expected searchActive to be true after paste")
	}
}

func TestPanel_CollapsibleTree(t *testing.T) {
	p := newTestPanel(t, `{"providers": {"gateway": {"id": "gateway",
	  "base_url": "https://gw.example/v1", "models": [{"id": "minimax-m3"}]}}}`)

	// 1. Initial state: gateway is expanded by default with '▾'
	lines := p.Lines(0)
	joined := stripANSI(strings.Join(lines, "\n"))
	if !strings.Contains(joined, "▾ gateway") || !strings.Contains(joined, "minimax-m3") {
		t.Fatalf("expected expanded gateway with '▾ gateway' and minimax-m3, got:\n%s", joined)
	}

	// 2. Press Space on gateway: collapses it
	p.cursor = 0
	p.HandleKey(tea.Key{Code: ' '})
	lines = p.Lines(0)
	joined = stripANSI(strings.Join(lines, "\n"))
	if !strings.Contains(joined, "▸ gateway") {
		t.Fatalf("expected collapsed '▸ gateway', got:\n%s", joined)
	}
	if strings.Contains(joined, "minimax-m3") {
		t.Fatalf("expected child model minimax-m3 to be hidden when collapsed, got:\n%s", joined)
	}

	// 3. Press Enter on collapsed gateway: expands it back
	p.HandleKey(tea.Key{Code: tea.KeyEnter})
	lines = p.Lines(0)
	joined = stripANSI(strings.Join(lines, "\n"))
	if !strings.Contains(joined, "▾ gateway") || !strings.Contains(joined, "minimax-m3") {
		t.Fatalf("expected Enter on collapsed gateway to expand it, got:\n%s", joined)
	}

	// 4. Move cursor to child model and press Ctrl+E: collapses parent provider and focuses gateway
	p.cursor = 1
	p.HandleKey(tea.Key{Code: 'e', Mod: tea.ModCtrl})
	if p.cursor != 0 {
		t.Fatalf("after Ctrl+E on child model, cursor = %d, want 0 (parent provider)", p.cursor)
	}
	lines = p.Lines(0)
	joined = stripANSI(strings.Join(lines, "\n"))
	if !strings.Contains(joined, "▸ gateway") || strings.Contains(joined, "minimax-m3") {
		t.Fatalf("expected parent provider to collapse, got:\n%s", joined)
	}

	// 5. While collapsed, typing search query "mini" automatically expands matching items
	for _, ch := range "mini" {
		p.HandleKey(tea.Key{Code: rune(ch), Text: string(ch)})
	}
	searchLines := p.Lines(0)
	searchJoined := stripANSI(strings.Join(searchLines, "\n"))
	if !strings.Contains(searchJoined, "minimax-m3") {
		t.Fatalf("expected search query 'mini' to auto-expand and show minimax-m3, got:\n%s", searchJoined)
	}

	// 6. Esc clears search and restores collapsed state
	p.HandleKey(tea.Key{Code: tea.KeyEsc})
	restoredLines := p.Lines(0)
	restoredJoined := stripANSI(strings.Join(restoredLines, "\n"))
	if !strings.Contains(restoredJoined, "▸ gateway") || strings.Contains(restoredJoined, "minimax-m3") {
		t.Fatalf("expected cleared search to restore collapsed state, got:\n%s", restoredJoined)
	}
}

func TestPanel_PreventShortcutSearchCollision(t *testing.T) {
	p := newTestPanel(t, `{"providers": {"gateway": {"id": "gateway",
	  "base_url": "https://gw.example/v1", "models": [{"id": "m1"}]}}}`)

	// Real typing with Text: "a" must NOT trigger the add form
	p.HandleKey(tea.Key{Code: 'a', Text: "a"})
	if p.form != nil {
		t.Fatal("typing 'a' opened the add form; expected it to append to search query")
	}
	if p.query != "a" {
		t.Fatalf("query = %q, want 'a'", p.query)
	}

	// Clear query
	p.HandleKey(tea.Key{Code: tea.KeyEsc})

	// Real typing with Text: "d" must NOT trigger the delete notice
	p.HandleKey(tea.Key{Code: 'd', Text: "d"})
	if p.confirm != -1 || strings.Contains(p.notice, "delete") {
		t.Fatal("typing 'd' triggered delete confirmation; expected search query")
	}
	if p.query != "d" {
		t.Fatalf("query = %q, want 'd'", p.query)
	}

	// Clear query
	p.HandleKey(tea.Key{Code: tea.KeyEsc})

	// Real typing with Text: "v" must NOT trigger the verify probe
	_, cmd := p.HandleKey(tea.Key{Code: 'v', Text: "v"})
	if cmd != nil {
		t.Fatal("typing 'v' started verify probe; expected search query")
	}
	if p.query != "v" {
		t.Fatalf("query = %q, want 'v'", p.query)
	}

	// Clear query
	p.HandleKey(tea.Key{Code: tea.KeyEsc})

	// Dedicated modifier hotkeys WORK:
	// Ctrl+A opens the add provider form
	p.HandleKey(tea.Key{Code: 'a', Mod: tea.ModCtrl})
	if p.form == nil || p.form.Provider != "" {
		t.Fatal("Ctrl+A did not open Add Provider form")
	}
	p.HandleKey(tea.Key{Code: tea.KeyEsc}) // cancel form

	// F2 opens the add provider form
	p.HandleKey(tea.Key{Code: tea.KeyF2})
	if p.form == nil {
		t.Fatal("F2 did not open Add Provider form")
	}
	p.HandleKey(tea.Key{Code: tea.KeyEsc})

	// Ctrl+M opens the add model form
	p.HandleKey(tea.Key{Code: 'm', Mod: tea.ModCtrl})
	if p.form == nil || p.form.Kind != FormModel {
		t.Fatal("Ctrl+M did not open Add Model form")
	}
	p.HandleKey(tea.Key{Code: tea.KeyEsc})

	// Ctrl+D triggers delete
	p.HandleKey(tea.Key{Code: 'd', Mod: tea.ModCtrl})
	if !strings.Contains(p.notice, "delete") {
		t.Fatalf("Ctrl+D notice = %q, want delete notice", p.notice)
	}
}

func TestPanel_VimKeysDoNotInterceptSearch(t *testing.T) {
	p := newTestPanel(t, `{"providers": {"gateway": {"id": "gateway",
	  "base_url": "https://gw.example/v1", "models": [{"id": "m1"}]}}}`)

	// Vim keys: g, G, j, k, h, l, q must NOT be intercepted by list navigation.
	// They must all append to the query text.
	keys := []rune{'g', 'G', 'j', 'k', 'h', 'l', 'q'}
	for _, r := range keys {
		p.query = ""
		p.cursor = 0
		p.HandleKey(tea.Key{Code: r, Text: string(r)})
		if p.query != string(r) {
			t.Fatalf("typing %q produced query %q, want %q", string(r), p.query, string(r))
		}
		if p.form != nil {
			t.Fatalf("typing %q opened the form", string(r))
		}
	}

	// Verify typing "google" builds the full query "google" without 'g' getting swallowed
	p.query = ""
	for _, r := range "google" {
		p.HandleKey(tea.Key{Code: r, Text: string(r)})
	}
	if p.query != "google" {
		t.Fatalf("typing 'google' produced query %q, want 'google'", p.query)
	}
}

func TestPanel_BrowserOAuth(t *testing.T) {
	var copiedText string
	p := newTestPanelWith(t, "{}", Seams{
		ClipboardWrite: func(s string) error {
			copiedText = s
			return nil
		},
	})

	// Set up a synthetic OAuth state for Hyper
	p.oauth = &OAuthState{
		Provider: "hyper",
		Name:     "Hyper",
		DeviceAuth: &oauthflow.DeviceAuth{
			Provider:        "hyper",
			VerificationURL: "https://hyper.charm.land/device/auth",
			UserCode:        "ABCD-1234",
		},
		Spinning: true,
	}

	// Verify rendering contains prompt, URL, code and key hints
	lines := strings.Join(p.Lines(0), "\n")
	if !strings.Contains(lines, "Authenticate with Hyper") {
		t.Fatalf("expected title with Hyper, got:\n%s", lines)
	}
	if !strings.Contains(lines, "https://hyper.charm.land/device/auth") {
		t.Fatalf("expected verification URL, got:\n%s", lines)
	}
	if !strings.Contains(lines, "ABCD-1234") {
		t.Fatalf("expected user code, got:\n%s", lines)
	}
	if !strings.Contains(lines, "Copy Code") || !strings.Contains(lines, "Copy URL") {
		t.Fatalf("expected keyboard hints, got:\n%s", lines)
	}

	// Press 'c': copies user code to clipboard
	copiedText = ""
	p.HandleKey(tea.Key{Code: 'c', Text: "c"})
	if copiedText != "ABCD-1234" {
		t.Fatalf("pressing 'c' copied %q, want 'ABCD-1234'", copiedText)
	}
	if !strings.Contains(p.oauth.Notice, "Code copied") {
		t.Fatalf("expected notice about code copied, got %q", p.oauth.Notice)
	}

	// Press 'y': copies verification URL to clipboard
	copiedText = ""
	p.HandleKey(tea.Key{Code: 'y', Text: "y"})
	if copiedText != "https://hyper.charm.land/device/auth" {
		t.Fatalf("pressing 'y' copied %q, want verification URL", copiedText)
	}
	if !strings.Contains(p.oauth.Notice, "URL copied") {
		t.Fatalf("expected notice about URL copied, got %q", p.oauth.Notice)
	}

	// Tick increments spinner frame
	initSpin := p.oauth.SpinFrame
	p.ApplyOAuthTick()
	if p.oauth.SpinFrame != initSpin+1 {
		t.Fatalf("tick did not advance SpinFrame: %d -> %d", initSpin, p.oauth.SpinFrame)
	}

	// Result message completes authentication and saves provider
	p.ApplyOAuthResultMsg(OAuthResultMsg{
		Provider: "hyper",
		Token:    "hp_token_live_12345",
	})
	if p.oauth != nil {
		t.Fatal("expected oauth state to clear after successful result")
	}

	list, err := models.ProviderEditor().List()
	if err != nil {
		t.Fatalf("List() error: %v", err)
	}
	saved, ok := list["hyper"]
	if !ok {
		t.Fatal("expected hyper provider to be saved in providers.json")
	}
	if saved.APIKey != "hp_token_live_12345" {
		t.Fatalf("saved.APIKey = %q, want 'hp_token_live_12345'", saved.APIKey)
	}
	if len(saved.Models) != 23 {
		t.Fatalf("saved.Models count = %d, want 23 preloaded models", len(saved.Models))
	}
	if !strings.Contains(p.notice, "Successfully authenticated") {
		t.Fatalf("notice = %q, want success notice", p.notice)
	}

	// Esc cancels in-progress OAuth dialog
	p.oauth = &OAuthState{
		Provider: "hyper",
		Name:     "Hyper",
		DeviceAuth: &oauthflow.DeviceAuth{
			Provider: "hyper",
		},
	}
	p.HandleKey(tea.Key{Code: tea.KeyEsc})
	if p.oauth != nil {
		t.Fatal("pressing Esc did not cancel oauth state")
	}
}

func TestPanel_GoogleAntigravityOAuth(t *testing.T) {
	var copiedText string
	p := newTestPanelWith(t, "{}", Seams{
		ClipboardWrite: func(s string) error {
			copiedText = s
			return nil
		},
	})

	testAuthURL := "https://accounts.google.com/o/oauth2/auth?access_type=offline&client_id=1071006060591-tmhssin2h21lcre235vtolojh4g403ep.apps.googleusercontent.com&code_challenge=testchallenge&code_challenge_method=S256&prompt=consent&redirect_uri=https%3A%2F%2Fantigravity.google%2Foauth-callback&response_type=code&scope=https%3A%2F%2Fwww.googleapis.com%2Fauth%2Fcloud-platform&state=teststate"

	// Set up a synthetic OAuth state for Google Antigravity with a local session detected
	p.oauth = &OAuthState{
		Provider: "google-antigravity",
		Name:     "Google Antigravity",
		DeviceAuth: &oauthflow.DeviceAuth{
			Provider:        "google-antigravity",
			VerificationURL: testAuthURL,
			DeviceCode:      "test-verifier-12345",
			UserCode:        "(paste authorization code with Cmd+V / Ctrl+V)",
		},
		LocalEmail: "navigaid@gmail.com",
		LocalToken: "ya29.test-local-token",
		Spinning:   true,
	}

	// 1. Verify rendering
	lines := strings.Join(p.Lines(0), "\n")
	if !strings.Contains(lines, "Authenticate with Google Antigravity") {
		t.Fatalf("expected title with Google Antigravity, got:\n%s", lines)
	}
	if !strings.Contains(lines, "https://accounts.google.com/o/oauth2/auth") {
		t.Fatalf("expected Google OAuth verification URL, got:\n%s", lines)
	}
	if !strings.Contains(lines, "Paste authorization code below") {
		t.Fatalf("expected prompt to paste code, got:\n%s", lines)
	}
	if !strings.Contains(lines, "Local session detected") || !strings.Contains(lines, "navigaid@gmail.com") {
		t.Fatalf("expected local session notice with email, got:\n%s", lines)
	}

	// 2. Press 'y': copies verification URL to clipboard
	copiedText = ""
	p.HandleKey(tea.Key{Code: 'y', Text: "y"})
	if copiedText != testAuthURL {
		t.Fatalf("pressing 'y' copied %q, want testAuthURL", copiedText)
	}

	// 3. Typing characters updates CodeInput
	p.HandleKey(tea.Key{Code: '4', Text: "4"})
	p.HandleKey(tea.Key{Code: '/', Text: "/"})
	p.HandleKey(tea.Key{Code: '0', Text: "0"})
	if p.oauth.CodeInput != "4/0" {
		t.Fatalf("CodeInput = %q, want '4/0'", p.oauth.CodeInput)
	}

	// Backspace deletes characters
	p.HandleKey(tea.Key{Code: tea.KeyBackspace})
	if p.oauth.CodeInput != "4/" {
		t.Fatalf("CodeInput after backspace = %q, want '4/'", p.oauth.CodeInput)
	}

	// 4. A pasted code
	cmd := p.HandlePaste("4/0AeanS-pasted-auth-code")
	if cmd == nil {
		t.Fatal("HandlePaste returned nil cmd during oauth code paste")
	}
	if p.oauth.CodeInput != "4/0AeanS-pasted-auth-code" {
		t.Fatalf("CodeInput after paste = %q, want '4/0AeanS-pasted-auth-code'", p.oauth.CodeInput)
	}

	// 5. Test activating via local session ('a' key)
	p.oauth.CodeInput = ""
	handled, saveCmd := p.HandleKey(tea.Key{Code: 'a', Text: "a"})
	if !handled {
		t.Fatal("expected 'a' key to be handled for local session binding")
	}
	_ = saveCmd
	if p.oauth != nil {
		t.Fatal("expected oauth state to be nil after activating local session")
	}

	list, err := models.ProviderEditor().List()
	if err != nil {
		t.Fatalf("List() error: %v", err)
	}
	saved, ok := list["google-antigravity"]
	if !ok {
		t.Fatal("expected google-antigravity provider to be saved in providers.json")
	}
	if saved.APIKey != "ya29.test-local-token" {
		t.Fatalf("saved.APIKey = %q, want 'ya29.test-local-token'", saved.APIKey)
	}
	if !strings.Contains(p.notice, "Authenticated google-antigravity from local session") {
		t.Fatalf("notice = %q, want local session success notice", p.notice)
	}
}

// The tick's message type carries a time so a stale tick can be told from
// a fresh one; the panel only advances the spinner.
func TestPanel_OAuthTickTypeCarriesATime(t *testing.T) {
	var msg OAuthTickMsg = OAuthTickMsg(time.Now())
	if time.Time(msg).IsZero() {
		t.Fatal("the tick message carries no time")
	}
}

func TestPanel_ResponsiveHeight(t *testing.T) {
	for _, tc := range []struct {
		height int
		total  int
		want   int
	}{
		{15, 20, 4},
		{20, 20, 5},
		{24, 20, 8},
		{28, 20, 10},
		{32, 20, 10},
		{40, 30, 10},
		{60, 50, 10},
		{40, 10, 10},
		{0, 20, 20},
		{-1, 20, 20},
	} {
		if got := visibleRowsFor(tc.height, tc.total); got != tc.want {
			t.Errorf("visibleRowsFor(%d, %d) = %d, want %d", tc.height, tc.total, got, tc.want)
		}
	}
}

func TestPanel_ResponsiveWidth(t *testing.T) {
	p := newTestPanel(t, `{
		"my-custom-provider": {
			"wire": "openai",
			"endpoint": "https://api.openai-very-long-domain-name.example.com/v1/chat/completions",
			"models": {
				"very-long-model-identifier-that-exceeds-standard-columns": {
					"wire": "upstream-custom-model-long-identifier",
					"context": 128000
				}
			}
		}
	}`)

	// 1. Render at narrow width (80 columns)
	narrowLines := p.Lines(30, 80)
	for i, line := range narrowLines {
		w := ansi.StringWidth(line)
		if w > 80 {
			t.Errorf("line %d exceeds 80 cols (width=%d): %q", i, w, line)
		}
	}

	// Verify compact legend on narrow terminal (< 90 cols)
	narrowLegend := narrowLines[len(narrowLines)-1]
	if !strings.Contains(narrowLegend, "Del") || !strings.Contains(narrowLegend, "Edit") {
		t.Errorf("expected compact keyboard legend on 80 cols, got: %q", narrowLegend)
	}

	// Mid-width (100 cols) includes Test
	midLines := p.Lines(30, 100)
	midLegend := midLines[len(midLines)-1]
	if !strings.Contains(midLegend, "Test") {
		t.Errorf("expected mid-width legend to contain Test, got: %q", midLegend)
	}

	// 2. Render at wide width (140 columns)
	wideLines := p.Lines(30, 140)
	wideLegend := wideLines[len(wideLines)-1]
	if !strings.Contains(wideLegend, "Delete") || !strings.Contains(wideLegend, "Verify") {
		t.Errorf("expected full keyboard legend on 140 cols, got: %q", wideLegend)
	}
}

func TestRowText_Truncation(t *testing.T) {
	text := RowText("super-long-provider-name-that-is-very-long", "https://api.example.com/very/long/path/to/completion", false, false, "", "", 40)
	w := ansi.StringWidth(text)
	if w > 40 {
		t.Errorf("RowText width = %d, want <= 40", w)
	}
	if !strings.HasSuffix(text, "…") {
		t.Errorf("RowText expected truncation suffix '…', got %q", text)
	}
}
