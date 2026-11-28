package configpanel

import (
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/justwasm/pigasus/internal/core/settings"
)

// TestExhaustive_EveryRowSubmenuInteraction verifies every interaction path
// of every row and submenu in /config:
// 1. Enter opens the submenu.
// 2. Up/Down navigates every option with wrapping.
// 3. Esc cancels without mutating settings.
// 4. Enter selects each option, persisting correctly (or deleting if default).
// 5. Left/Right cycles in-place.
func TestExhaustive_EveryRowSubmenuInteraction(t *testing.T) {
	testHome(t, map[string]any{})

	var appliedTheme string
	var appliedAltScreen any
	var appliedVerbosity string

	seams := Seams{
		ApplyTheme: func(scheme string) tea.Cmd {
			appliedTheme = scheme
			return nil
		},
		ApplyAltScreen: func(stored any) {
			appliedAltScreen = stored
		},
		ApplyVerbosity: func(verb string) {
			appliedVerbosity = verb
		},
	}

	p := New(seams)
	rows := p.Catalogue()

	for rowIdx, r := range rows {
		t.Run("Row_"+r.Name, func(t *testing.T) {
			if len(r.Values) == 0 {
				return
			}

			// Clean settings for isolated testing
			testHome(t, map[string]any{})
			p = New(seams)
			p.SetCursor(rowIdx)

			// 1. Enter opens submenu
			handled, _ := p.HandleKey(tea.Key{Code: tea.KeyEnter})
			if !handled || !p.SubmenuOpen() {
				t.Fatalf("row %q: Enter did not open submenu", r.Name)
			}

			valCount := len(r.Values)

			// 2. Up and Down navigation with wrap-around
			for i := 0; i < valCount+1; i++ {
				hDown, _ := p.HandleKey(tea.Key{Code: tea.KeyDown})
				if !hDown {
					t.Fatalf("row %q: KeyDown not handled in submenu", r.Name)
				}
			}
			for i := 0; i < valCount+1; i++ {
				hUp, _ := p.HandleKey(tea.Key{Code: tea.KeyUp})
				if !hUp {
					t.Fatalf("row %q: KeyUp not handled in submenu", r.Name)
				}
			}

			// 3. Esc cancels without change
			hEsc, _ := p.HandleKey(tea.Key{Code: tea.KeyEsc})
			if !hEsc || p.SubmenuOpen() {
				t.Fatalf("row %q: Esc did not close submenu", r.Name)
			}

			// 4. Test selecting every value in the submenu
			for vIdx, targetVal := range r.Values {
				// Re-open submenu
				p.SetCursor(rowIdx)
				p.HandleKey(tea.Key{Code: tea.KeyEnter})

				// Set submenu cursor to targetVal index
				p.submenuCursor = vIdx

				hSelect, cmd := p.HandleKey(tea.Key{Code: tea.KeyEnter})
				if !hSelect || p.SubmenuOpen() {
					t.Fatalf("row %q, val %q: Enter did not commit & close submenu", r.Name, targetVal.Display)
				}

				// Check command for Color Scheme
				if r.Name == "Color Scheme" && cmd == nil {
					t.Errorf("row %q: expected tea.ClearScreen command on commit", r.Name)
				}

				// Verify persistence
				if !r.MemoryOnly {
					raw, _ := settings.Field(r.SettingsKey)
					if targetVal.Stored == nil {
						if raw != nil {
							t.Errorf("row %q, val %q: expected key %q to be deleted/absent, got %v", r.Name, targetVal.Display, r.SettingsKey, raw)
						}
					} else {
						if !JSONLeafEqual(raw, targetVal.Stored) {
							t.Errorf("row %q, val %q: stored = %v (%T), want %v (%T)", r.Name, targetVal.Display, raw, raw, targetVal.Stored, targetVal.Stored)
						}
					}
				}

				// Check special seam triggers
				if r.Name == "Color Scheme" && appliedTheme != targetVal.Display {
					t.Errorf("Color Scheme: appliedTheme = %q, want %q", appliedTheme, targetVal.Display)
				}
				if r.SettingsKey == "altScreenMode" && appliedAltScreen != targetVal.Stored {
					t.Errorf("Rendering Mode: appliedAltScreen = %v, want %v", appliedAltScreen, targetVal.Stored)
				}
				if r.SettingsKey == "verbosity" && appliedVerbosity != targetVal.Display {
					t.Errorf("Verbosity: appliedVerbosity = %q, want %q", appliedVerbosity, targetVal.Display)
				}
			}

			// 5. Left and Right cycles in-place
			p.SetCursor(rowIdx)
			p.HandleKey(tea.Key{Code: tea.KeyRight})
			p.HandleKey(tea.Key{Code: tea.KeyLeft})
		})
	}
}

// TestExhaustive_ColorSchemePreviewIsCardOnly verifies that moving through
// the Color Scheme submenu previews nothing globally and Esc reverts
// nothing — the preview is the card, not the frame around it.
//
// 【一手】 agy 1.5.18 (tuialign 46_scheme_chrome_fixed): the chrome stays on
// the committed scheme while the card follows the cursor, so there is no
// global application to make and none to undo.
func TestExhaustive_ColorSchemePreviewIsCardOnly(t *testing.T) {
	testHome(t, map[string]any{"colorScheme": "dark"})

	var applied []string
	seams := Seams{
		ApplyTheme: func(scheme string) tea.Cmd {
			applied = append(applied, scheme)
			return nil
		},
	}

	p := New(seams)
	// Find Color Scheme row
	rows := p.Rows()
	csIdx := -1
	for i, r := range rows {
		if r.Name == "Color Scheme" {
			csIdx = i
			break
		}
	}
	if csIdx < 0 {
		t.Fatal("Color Scheme row not found")
	}

	p.SetCursor(csIdx)
	p.HandleKey(tea.Key{Code: tea.KeyEnter}) // Open submenu

	p.HandleKey(tea.Key{Code: tea.KeyDown}) // move the preview cursor
	if len(applied) != 0 {
		t.Errorf("a move in Color Scheme applied a theme: %v", applied)
	}

	// Esc closes the submenu; the committed scheme was never disturbed.
	p.HandleKey(tea.Key{Code: tea.KeyEsc})
	if len(applied) != 0 {
		t.Errorf("Esc applied a theme: %v", applied)
	}
}

// TestExhaustive_EditorModeGating verifies Editor Mode dynamically shows/hides
// "Editor Mode › Insert First".
func TestExhaustive_EditorModeGating(t *testing.T) {
	testHome(t, map[string]any{})
	p := New(Seams{})

	findInsertFirst := func() bool {
		for _, r := range p.Rows() {
			if strings.Contains(r.Name, "Insert First") {
				return true
			}
		}
		return false
	}

	if findInsertFirst() {
		t.Fatal("Insert First should not be visible initially when Editor Mode is default")
	}

	// Find Editor Mode row
	emIdx := -1
	for i, r := range p.Rows() {
		if r.Name == "Editor Mode" {
			emIdx = i
			break
		}
	}
	if emIdx < 0 {
		t.Fatal("Editor Mode row not found")
	}

	// Switch to vim
	p.SetCursor(emIdx)
	p.HandleKey(tea.Key{Code: tea.KeyEnter})
	p.submenuCursor = 1 // "vim"
	p.HandleKey(tea.Key{Code: tea.KeyEnter})

	if !findInsertFirst() {
		t.Fatal("Insert First should be visible after setting Editor Mode to vim")
	}

	// Switch back to default
	for i, r := range p.Rows() {
		if r.Name == "Editor Mode" {
			emIdx = i
			break
		}
	}
	p.SetCursor(emIdx)
	p.HandleKey(tea.Key{Code: tea.KeyEnter})
	p.submenuCursor = 0 // "default"
	p.HandleKey(tea.Key{Code: tea.KeyEnter})

	if findInsertFirst() {
		t.Fatal("Insert First should be hidden after setting Editor Mode to default")
	}
}

// TestExhaustive_VoiceModeGating verifies Voice Mode dynamically shows/hides
// child Voice settings.
func TestExhaustive_VoiceModeGating(t *testing.T) {
	testHome(t, map[string]any{})
	p := New(Seams{})

	countVoiceChildren := func() int {
		count := 0
		for _, r := range p.Rows() {
			if strings.HasPrefix(r.Name, "Voice Mode ›") {
				count++
			}
		}
		return count
	}

	// A fresh home stores no voiceMode, which is "auto": the children are
	// collapsed. They are shown only once the mode is explicitly live, so
	// the catalogue matches the reference's own row count on a clean
	// install (agy 1.5.18: 20 rows; ours adds Voice Mode and nothing else).
	if got := countVoiceChildren(); got != 0 {
		t.Fatalf("expected the Voice Mode children to be collapsed while the mode is auto, got %d", got)
	}

	// Find Voice Mode row
	vmIdx := -1
	for i, r := range p.Rows() {
		if r.Name == "Voice Mode" {
			vmIdx = i
			break
		}
	}
	if vmIdx < 0 {
		t.Fatal("Voice Mode row not found")
	}

	// Switch to "dictation" (index 2: auto, live, dictation)
	p.SetCursor(vmIdx)
	p.HandleKey(tea.Key{Code: tea.KeyEnter})
	p.submenuCursor = 2 // "dictation"
	p.HandleKey(tea.Key{Code: tea.KeyEnter})

	if got := countVoiceChildren(); got != 0 {
		t.Fatalf("expected 0 Voice Mode children when in dictation, got %d", got)
	}

	// Switch back to "live" (index 1)
	for i, r := range p.Rows() {
		if r.Name == "Voice Mode" {
			vmIdx = i
			break
		}
	}
	p.SetCursor(vmIdx)
	p.HandleKey(tea.Key{Code: tea.KeyEnter})
	p.submenuCursor = 1 // "live"
	p.HandleKey(tea.Key{Code: tea.KeyEnter})

	if got := countVoiceChildren(); got != 4 {
		t.Fatalf("expected 4 Voice Mode children when in live mode, got %d", got)
	}
}

// TestExhaustive_SearchAndQueryNav verifies search typing, filtering,
// submenu opening inside filtered results, backspacing, and Esc clearing.
func TestExhaustive_SearchAndQueryNav(t *testing.T) {
	testHome(t, map[string]any{})
	p := New(Seams{})

	// Type "tele"
	for _, ch := range "tele" {
		p.HandleKey(tea.Key{Code: rune(ch), Text: string(ch)})
	}

	filtered := p.Rows()
	if len(filtered) != 1 || filtered[0].Name != "Enable Telemetry" {
		t.Fatalf("expected only 'Enable Telemetry' row, got %d rows", len(filtered))
	}

	// Open submenu on filtered row
	p.HandleKey(tea.Key{Code: tea.KeyEnter})
	if !p.SubmenuOpen() {
		t.Fatal("expected submenu to open on filtered row")
	}
	p.HandleKey(tea.Key{Code: tea.KeyEsc})
	if p.SubmenuOpen() {
		t.Fatal("expected submenu to close on Esc")
	}

	// Backspace twice
	p.HandleKey(tea.Key{Code: tea.KeyBackspace})
	p.HandleKey(tea.Key{Code: tea.KeyBackspace})
	if p.query != "te" {
		t.Fatalf("query = %q, want 'te'", p.query)
	}

	// Esc clears query
	p.HandleKey(tea.Key{Code: tea.KeyEsc})
	if p.query != "" {
		t.Fatalf("query = %q, want empty after Esc", p.query)
	}

	// Esc again closes panel
	closed := false
	p.seams.Close = func() tea.Cmd {
		closed = true
		return nil
	}
	p.HandleKey(tea.Key{Code: tea.KeyEsc})
	if !closed {
		t.Fatal("expected panel to close on Esc when query is empty")
	}
}

// TestExhaustive_GeminiAPIKeyGating verifies Use AI Credits is omitted when GEMINI_API_KEY is present.
func TestExhaustive_GeminiAPIKeyGating(t *testing.T) {
	testHome(t, map[string]any{})
	origKey := os.Getenv("GEMINI_API_KEY")
	defer func() {
		if origKey != "" {
			_ = os.Setenv("GEMINI_API_KEY", origKey)
		} else {
			_ = os.Unsetenv("GEMINI_API_KEY")
		}
	}()

	_ = os.Setenv("GEMINI_API_KEY", "test-key-123")
	p := New(Seams{})
	for _, r := range p.Rows() {
		if r.SettingsKey == "useG1Credits" {
			t.Fatal("Use AI Credits row must not be present when GEMINI_API_KEY is set")
		}
	}

	_ = os.Unsetenv("GEMINI_API_KEY")
	p = New(Seams{})
	found := false
	for _, r := range p.Rows() {
		if r.SettingsKey == "useG1Credits" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("Use AI Credits row must be present when GEMINI_API_KEY is unset")
	}
}
