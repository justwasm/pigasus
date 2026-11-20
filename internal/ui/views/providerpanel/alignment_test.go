package providerpanel

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/palette"
)

func TestProviderPanel_AlignmentWithProjects(t *testing.T) {
	// Setup test panel with a custom provider and model
	p := newTestPanelWith(t, `{"providers": {"openrouter": {"id": "openrouter",
	  "type": "openai", "base_url": "https://openrouter.ai/api/v1",
	  "models": [{"id": "claude-3.5-sonnet", "model": "anthropic/claude-3.5-sonnet", "context_window": 204800}]}}}`,
		Seams{CurrentModel: func() string { return "claude-3.5-sonnet" }})

	// 1. Verify visual header and layout structure (2-space left indentation, no hardcoded underlines)
	lines := p.Lines(30, 100)
	joined := strings.Join(lines, "\n")

	if !strings.Contains(joined, "  \x1b[1mProviders\x1b[0m") {
		t.Fatalf("expected 2-space indented bold header '  Providers', got:\n%s", joined)
	}
	if strings.Contains(joined, "────") {
		t.Fatalf("found legacy hardcoded underline in output:\n%s", joined)
	}
	if !strings.Contains(joined, "Type to search providers and models...") {
		t.Fatalf("expected clean placeholder search line, got:\n%s", joined)
	}

	// 2. Action row: "+ Add a provider..." is rendered at top
	if !strings.Contains(joined, "+ Add a provider...") {
		t.Fatalf("expected '+ Add a provider...' action row, got:\n%s", joined)
	}

	// 3. Tab bar matches /projects styling (2-space indent, bold active tab, dim inactive tabs)
	tabBar := p.TabBar()
	if !strings.HasPrefix(tabBar, "  \x1b[1;4m") || !strings.Contains(tabBar, "Configured") {
		t.Fatalf("expected /projects-aligned tab bar, got %q", tabBar)
	}

	// 4. Bullet and current badge alignment
	// Active provider (openrouter serves claude-3.5-sonnet) has green bullet and current badge
	if !strings.Contains(joined, palette.AgentFgOpen+"●"+palette.AgentFgClose) {
		t.Fatalf("expected active green bullet for provider serving current model, got:\n%s", joined)
	}
	if !strings.Contains(joined, "(current)") {
		t.Fatalf("expected (current) badge on active model, got:\n%s", joined)
	}

	// 5. Navigate Up to row -1 ("+ Add a provider...")
	p.cursor = 0
	p.HandleKey(tea.Key{Code: tea.KeyUp})
	if p.cursor != -1 {
		t.Fatalf("expected cursor = -1 after KeyUp from row 0, got %d", p.cursor)
	}
	navLines := p.Lines(30, 100)
	navJoined := strings.Join(navLines, "\n")
	if !strings.Contains(navJoined, palette.BlueOpen+"> "+ansiReset+ansiBold+"+ Add a provider...") {
		t.Fatalf("expected selected '+ Add a provider...' row with '> ' cursor, got:\n%s", navJoined)
	}

	// 6. Enter on row -1 opens Add Provider form
	p.HandleKey(tea.Key{Code: tea.KeyEnter})
	if p.form == nil || p.form.Kind != FormProvider || p.form.Provider != "" {
		t.Fatalf("expected Enter on row -1 to open new Add Provider form, got %+v", p.form)
	}

	// 7. Form view visual alignment: breadcrumb header and tree branches "└─ "
	formLines := p.formLines(100)
	formJoined := strings.Join(formLines, "\n")
	if !strings.Contains(formJoined, "Providers\x1b[0m\x1b[38;5;238m › \x1b[1mAdd provider") {
		t.Fatalf("expected breadcrumb title 'Providers › Add provider', got:\n%s", formJoined)
	}
	if !strings.Contains(formJoined, "└─") {
		t.Fatalf("expected tree branch '└─ ' in form inputs, got:\n%s", formJoined)
	}

	// Close form
	p.HandleKey(tea.Key{Code: tea.KeyEsc})
	if p.form != nil {
		t.Fatal("expected Esc to close form")
	}

	// 8. Delete confirmation with 'y' key (aligned with /projects [Delete provider? (y/n)])
	p.cursor = 0
	p.HandleKey(tea.Key{Code: 'd'})
	if p.confirm != 0 {
		t.Fatalf("expected delete to be armed on row 0, confirm = %d", p.confirm)
	}
	confirmLines := p.Lines(30, 100)
	confirmJoined := strings.Join(confirmLines, "\n")
	if !strings.Contains(confirmJoined, "[Delete provider openrouter? (y/n)]") {
		t.Fatalf("expected '[Delete provider openrouter? (y/n)]' prompt, got:\n%s", confirmJoined)
	}

	// Cancel with 'n'
	p.HandleKey(tea.Key{Code: 'n'})
	if p.confirm != -1 {
		t.Fatalf("expected 'n' to cancel deletion, confirm = %d", p.confirm)
	}

	// Arm delete again and confirm with 'y'
	p.HandleKey(tea.Key{Code: 'd'})
	p.HandleKey(tea.Key{Code: 'y'})
	if p.confirm != -1 {
		t.Fatalf("expected confirm = -1 after delete, got %d", p.confirm)
	}
	if _, ok := p.providers["openrouter"]; ok {
		t.Fatal("expected openrouter provider to be deleted after 'y'")
	}
}

func TestProviderPanel_CtrlSSavesForm(t *testing.T) {
	p := newTestPanel(t, "")
	p.openAddForm()
	providerTypeField(p, "test-prov")
	providerTypeField(p, "Test Provider")
	p.formMove(1)
	providerTypeField(p, "https://api.test.com/v1")

	// Save via Ctrl+S (matching /projects shortcut)
	p.HandleKey(tea.Key{Code: 's', Mod: tea.ModCtrl})
	if p.form != nil {
		t.Fatal("expected Ctrl+S to save and close form")
	}
	if _, ok := p.providers["test-prov"]; !ok {
		t.Fatal("expected test-prov to be saved in configuration")
	}
}
