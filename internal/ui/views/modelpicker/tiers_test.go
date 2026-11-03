package modelpicker

import "testing"

func TestHasEffortTiers(t *testing.T) {
	// Empty name defaults to true.
	if !HasEffortTiers("") {
		t.Errorf("HasEffortTiers(\"\") = false, want true")
	}
	if !HasEffortTiers("   ") {
		t.Errorf("HasEffortTiers(\"   \") = false, want true")
	}

	// Reference models carry tiers.
	if !HasEffortTiers("Gemini 3.8 Flash (High)") {
		t.Errorf("HasEffortTiers(Gemini 3.8 Flash (High)) = false, want true")
	}
	if !HasEffortTiers("gemini-3.8-flash-high") {
		t.Errorf("HasEffortTiers(gemini-3.8-flash-high) = false, want true")
	}

	// Unknown model not in catalogue keeps tiers.
	if !HasEffortTiers("some-completely-unknown-model") {
		t.Errorf("HasEffortTiers(unknown) = false, want true")
	}
}
