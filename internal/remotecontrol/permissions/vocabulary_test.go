// Package permissions — vocabulary_test.go: the reference's own
// permission vocabulary, and the one place where it meets the
// clone's.
//
// The names are the reference's (【一手】, from the 1.4.8 descriptor);
// the pairing onto the clone's own three presets and three review
// modes is a judgement, and it is marked as one in vocabulary.go.
//
// The dispatcher's round-trip against this vocabulary lives in
// internal/remotecontrol/permissions_ls_test.go, because the
// dispatcher is the host's.
package permissions

import (
	"testing"
)

// TestLSPermissionVocabulary pins the translation table: every enum
// name the reference has must land on a preset the clone models, and
// the clone's own three presets must render to the reference's names.
func TestLSPermissionVocabulary(t *testing.T) {
	referencePresets := []string{
		ReferencePresetUnspecified,
		ReferencePresetDefault,
		ReferencePresetRequestReview,
		ReferencePresetTurbo,
		ReferencePresetVetted,
		ReferencePresetNone,
		ReferencePresetAuto,
	}
	for _, name := range referencePresets {
		preset, ok := PresetFromReferenceName(name)
		if !ok {
			t.Errorf("PresetFromReferenceName(%q) has no counterpart", name)
			continue
		}
		if !IsValidPreset(preset) {
			t.Errorf("PresetFromReferenceName(%q) = %q, which is not one of the clone's presets", name, preset)
		}
	}
	referenceReviews := []string{
		ReferenceReviewUnspecified,
		ReferenceReviewAlways,
		ReferenceReviewTurbo,
		ReferenceReviewAuto,
	}
	for _, name := range referenceReviews {
		mode, ok := ReviewFromReferenceName(name)
		if !ok {
			t.Errorf("ReviewFromReferenceName(%q) has no counterpart", name)
			continue
		}
		if !IsValidReviewMode(mode) {
			t.Errorf("ReviewFromReferenceName(%q) = %q, which is not one of the clone's modes", name, mode)
		}
	}
	// The three the clone models have an exact counterpart.
	for preset, want := range map[Preset]string{
		PresetDefault:    ReferencePresetDefault,
		PresetStrict:     ReferencePresetRequestReview,
		PresetPermissive: ReferencePresetNone,
	} {
		if got := ReferencePresetName(preset); got != want {
			t.Errorf("ReferencePresetName(%q) = %q, want %q", preset, got, want)
		}
	}
	for mode, want := range map[ReviewMode]string{
		ReviewModeAlways: ReferenceReviewAlways,
		ReviewModeNever:  ReferenceReviewTurbo,
		ReviewModeAuto:   ReferenceReviewAuto,
	} {
		if got := ReferenceReviewName(mode); got != want {
			t.Errorf("ReferenceReviewName(%q) = %q, want %q", mode, got, want)
		}
	}
	// A name the reference does not have is refused, not guessed.
	if _, ok := PresetFromReferenceName("AGENT_PERMISSION_PRESET_NOPE"); ok {
		t.Error("PresetFromReferenceName accepted a name the reference does not have")
	}
	if _, ok := ReviewFromReferenceName("ARTIFACT_REVIEW_MODE_NOPE"); ok {
		t.Error("ReviewFromReferenceName accepted a name the reference does not have")
	}
}
