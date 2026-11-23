package personas

import (
	"testing"
)

func TestPersonas_All(t *testing.T) {
	all := All()
	if len(all) != 5 {
		t.Fatalf("expected 5 personas, got %d", len(all))
	}

	expectedIDs := []string{
		PersonaEngineering,
		PersonaKnowledgeWork,
		PersonaData,
		PersonaScience,
		PersonaDesign,
	}

	for i, id := range expectedIDs {
		if all[i].ID != id {
			t.Errorf("persona[%d].ID = %q, want %q", i, all[i].ID, id)
		}
		if all[i].DisplayName == "" {
			t.Errorf("persona %q has empty displayName", id)
		}
		if all[i].Description == "" {
			t.Errorf("persona %q has empty description", id)
		}
		if all[i].Skin != ProductSkinDeveloper && all[i].Skin != ProductSkinNonTechnical {
			t.Errorf("persona %q has invalid skin %d", id, all[i].Skin)
		}
		if len(all[i].Suggestions) == 0 {
			t.Errorf("persona %q has 0 suggestions", id)
		}
	}
}

func TestPersonas_Resolve(t *testing.T) {
	cases := []struct {
		input  string
		wantID string
		wantOk bool
	}{
		{"engineering", PersonaEngineering, true},
		{"swe", PersonaEngineering, true},
		{"dev", PersonaEngineering, true},
		{"software-engineering", PersonaEngineering, true},
		{"knowledge-work", PersonaKnowledgeWork, true},
		{"kw", PersonaKnowledgeWork, true},
		{"non-technical", PersonaKnowledgeWork, true},
		{"data", PersonaData, true},
		{"analytics", PersonaData, true},
		{"science", PersonaScience, true},
		{"bio", PersonaScience, true},
		{"design", PersonaDesign, true},
		{"unknown-role", "", false},
	}

	for _, tc := range cases {
		p, ok := Resolve(tc.input)
		if ok != tc.wantOk {
			t.Errorf("Resolve(%q) ok = %v, want %v", tc.input, ok, tc.wantOk)
			continue
		}
		if ok && p.ID != tc.wantID {
			t.Errorf("Resolve(%q).ID = %q, want %q", tc.input, p.ID, tc.wantID)
		}
	}
}

func TestPersonas_ResolveSkin(t *testing.T) {
	cases := []struct {
		input    string
		wantSkin int
		wantOk   bool
	}{
		{"developer", ProductSkinDeveloper, true},
		{"dev", ProductSkinDeveloper, true},
		{"swe", ProductSkinDeveloper, true},
		{"1", ProductSkinDeveloper, true},
		{"non-technical", ProductSkinNonTechnical, true},
		{"kw", ProductSkinNonTechnical, true},
		{"knowledge-work", ProductSkinNonTechnical, true},
		{"2", ProductSkinNonTechnical, true},
		{"unspecified", ProductSkinUnspecified, true},
		{"0", ProductSkinUnspecified, true},
		{"invalid", ProductSkinUnspecified, false},
	}

	for _, tc := range cases {
		skin, ok := ResolveSkin(tc.input)
		if ok != tc.wantOk {
			t.Errorf("ResolveSkin(%q) ok = %v, want %v", tc.input, ok, tc.wantOk)
		}
		if ok && skin != tc.wantSkin {
			t.Errorf("ResolveSkin(%q) = %d, want %d", tc.input, skin, tc.wantSkin)
		}
	}
}

func TestPersonas_WireConversion(t *testing.T) {
	if got := SkinWire(ProductSkinDeveloper); got != WireSkinDeveloper {
		t.Errorf("SkinWire(1) = %q, want %q", got, WireSkinDeveloper)
	}
	if got := SkinWire(ProductSkinNonTechnical); got != WireSkinNonTechnical {
		t.Errorf("SkinWire(2) = %q, want %q", got, WireSkinNonTechnical)
	}

	if val, ok := SkinFromWire("PRODUCT_SKIN_DEVELOPER"); !ok || val != ProductSkinDeveloper {
		t.Errorf("SkinFromWire(DEVELOPER) = (%d, %v), want (1, true)", val, ok)
	}
	if val, ok := SkinFromWire(float64(2)); !ok || val != ProductSkinNonTechnical {
		t.Errorf("SkinFromWire(float64(2)) = (%d, %v), want (2, true)", val, ok)
	}
}

func TestGetOnboardingPersonasResponse(t *testing.T) {
	resp := GetOnboardingPersonasResponse()
	personas, ok := resp["personas"].([]any)
	if !ok || len(personas) != 5 {
		t.Fatalf("resp[personas] not a 5-element array: %v", resp["personas"])
	}

	custom, ok := resp["customPersona"].(map[string]any)
	if !ok {
		t.Fatalf("resp[customPersona] missing or invalid: %v", resp["customPersona"])
	}
	if custom["skin"] != ProductSkinNonTechnical {
		t.Errorf("customPersona.skin = %v, want %d", custom["skin"], ProductSkinNonTechnical)
	}

	customizations, ok := resp["customizations"].([]any)
	if !ok || len(customizations) == 0 {
		t.Fatalf("resp[customizations] missing or empty: %v", resp["customizations"])
	}
}
