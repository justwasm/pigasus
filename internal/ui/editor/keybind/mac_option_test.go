package keybind

import (
	"os"
	"runtime"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestMacOptionAsAlt(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("skipping macOS Option key test on non-darwin OS")
	}

	orig := os.Getenv("AGY_CLI_MAC_OPTION_AS_ALT")
	defer os.Setenv("AGY_CLI_MAC_OPTION_AS_ALT", orig)

	// Default: enabled
	os.Unsetenv("AGY_CLI_MAC_OPTION_AS_ALT")
	if !MacOptionAsAltEnabled() {
		t.Fatal("expected MacOptionAsAltEnabled to be true by default on darwin")
	}

	k := tea.Key{Code: 'µ', Text: "µ"}
	translated, ok := TranslateMacOptionToAlt(k)
	if !ok {
		t.Fatal("expected 'µ' to be translated to alt+m")
	}
	if translated.Code != 'm' || !translated.Mod.Contains(tea.ModAlt) {
		t.Fatalf("translated = %+v, want Code: 'm', Mod: ModAlt", translated)
	}

	// Option+s -> 'ß' -> alt+s
	kS := tea.Key{Code: 'ß', Text: "ß"}
	translatedS, okS := TranslateMacOptionToAlt(kS)
	if !okS || translatedS.Code != 's' || !translatedS.Mod.Contains(tea.ModAlt) {
		t.Fatalf("translated = %+v, want Code: 's', Mod: ModAlt", translatedS)
	}

	// Disable with AGY_CLI_MAC_OPTION_AS_ALT=0
	os.Setenv("AGY_CLI_MAC_OPTION_AS_ALT", "0")
	if MacOptionAsAltEnabled() {
		t.Fatal("expected MacOptionAsAltEnabled to be false when set to 0")
	}
	_, okDisabled := TranslateMacOptionToAlt(k)
	if okDisabled {
		t.Fatal("expected translation to be skipped when disabled")
	}
}
