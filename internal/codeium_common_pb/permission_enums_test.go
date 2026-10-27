// Package codeium_common_pb — permission_enums_test.go: the
// permission vocabulary, pinned against the reference's own
// descriptor.
//
// The 1.3.5 recovery of this package guessed the enum values from the
// binary's string table, and the guesses were wrong: the reference's
// AgentPermissionPreset has seven values (not four), ArtifactReviewMode
// has four (not three), and none of the old names is one of the
// reference's. The fixture is the descriptor itself, read out of the
// agy 1.4.8 binary — see testdata/permission_enums_1.4.8.txt.
package codeium_common_pb

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// permissionEnumValues parses the fixture into one map per enum: the
// enum's short name → its values' names and numbers.
func permissionEnumValues(t *testing.T) map[string]map[string]int {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "permission_enums_1.4.8.txt"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	out := map[string]map[string]int{}
	current := ""
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "enum ") {
			name := strings.TrimPrefix(line, "enum ")
			name = strings.TrimPrefix(name, "exa.codeium_common_pb.")
			current = name
			out[current] = map[string]int{}
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 || current == "" {
			t.Fatalf("fixture line %q is not a value", line)
		}
		name := strings.TrimSpace(parts[0])
		num, err := strconv.Atoi(strings.TrimSpace(parts[1]))
		if err != nil {
			t.Fatalf("fixture line %q has no number: %v", line, err)
		}
		out[current][name] = num
	}
	return out
}

// TestPermissionEnums_MatchTheReference pins every value of the two
// enums against the descriptor, in both directions: the value the
// reference has is the value the clone has, with the same number, and
// the clone has nothing the reference does not.
func TestPermissionEnums_MatchTheReference(t *testing.T) {
	reference := permissionEnumValues(t)

	presets := map[string]AgentPermissionPreset{
		"AGENT_PERMISSION_PRESET_UNSPECIFIED":    AgentPermissionPresetUnspecified,
		"AGENT_PERMISSION_PRESET_DEFAULT":        AgentPermissionPresetDefault,
		"AGENT_PERMISSION_PRESET_REQUEST_REVIEW": AgentPermissionPresetRequestReview,
		"AGENT_PERMISSION_PRESET_TURBO":          AgentPermissionPresetTurbo,
		"AGENT_PERMISSION_PRESET_VETTED":         AgentPermissionPresetVetted,
		"AGENT_PERMISSION_PRESET_NONE":           AgentPermissionPresetNone,
		"AGENT_PERMISSION_PRESET_AUTO":           AgentPermissionPresetAuto,
	}
	modes := map[string]ArtifactReviewMode{
		"ARTIFACT_REVIEW_MODE_UNSPECIFIED": ArtifactReviewModeUnspecified,
		"ARTIFACT_REVIEW_MODE_ALWAYS":      ArtifactReviewModeAlways,
		"ARTIFACT_REVIEW_MODE_TURBO":       ArtifactReviewModeTurbo,
		"ARTIFACT_REVIEW_MODE_AUTO":        ArtifactReviewModeAuto,
	}

	check := func(enum string, got map[string]int, want map[string]int) {
		t.Helper()
		for name, number := range want {
			gotNumber, ok := got[name]
			if !ok {
				t.Errorf("%s: the clone has no value named %s", enum, name)
				continue
			}
			if gotNumber != number {
				t.Errorf("%s: %s = %d in the clone, %d in the reference", enum, name, gotNumber, number)
			}
		}
		for name := range got {
			if _, ok := want[name]; !ok {
				t.Errorf("%s: the clone has a value named %s, which the reference does not have", enum, name)
			}
		}
	}

	check("AgentPermissionPreset", presetNumbers(presets), reference["AgentPermissionPreset"])
	check("ArtifactReviewMode", modeNumbers(modes), reference["ArtifactReviewMode"])
}

// presetNumbers renders the preset constants as the reference's
// numbers.
func presetNumbers(values map[string]AgentPermissionPreset) map[string]int {
	out := make(map[string]int, len(values))
	for name, value := range values {
		out[name] = int(value)
	}
	return out
}

// modeNumbers renders the review-mode constants as the reference's
// numbers.
func modeNumbers(values map[string]ArtifactReviewMode) map[string]int {
	out := make(map[string]int, len(values))
	for name, value := range values {
		out[name] = int(value)
	}
	return out
}

// TestPermissionEnumNames pins the wire spelling: proto-JSON spells an
// enum by its name, and the name is what the reference's own clients
// send and expect.
func TestPermissionEnumNames(t *testing.T) {
	cases := []struct {
		preset AgentPermissionPreset
		want   string
	}{
		{AgentPermissionPresetUnspecified, "AGENT_PERMISSION_PRESET_UNSPECIFIED"},
		{AgentPermissionPresetDefault, "AGENT_PERMISSION_PRESET_DEFAULT"},
		{AgentPermissionPresetRequestReview, "AGENT_PERMISSION_PRESET_REQUEST_REVIEW"},
		{AgentPermissionPresetTurbo, "AGENT_PERMISSION_PRESET_TURBO"},
		{AgentPermissionPresetVetted, "AGENT_PERMISSION_PRESET_VETTED"},
		{AgentPermissionPresetNone, "AGENT_PERMISSION_PRESET_NONE"},
		{AgentPermissionPresetAuto, "AGENT_PERMISSION_PRESET_AUTO"},
	}
	for _, tc := range cases {
		if got := AgentPermissionPresetName(tc.preset); got != tc.want {
			t.Errorf("AgentPermissionPresetName(%d) = %q, want %q", tc.preset, got, tc.want)
		}
	}
	modes := []struct {
		mode ArtifactReviewMode
		want string
	}{
		{ArtifactReviewModeUnspecified, "ARTIFACT_REVIEW_MODE_UNSPECIFIED"},
		{ArtifactReviewModeAlways, "ARTIFACT_REVIEW_MODE_ALWAYS"},
		{ArtifactReviewModeTurbo, "ARTIFACT_REVIEW_MODE_TURBO"},
		{ArtifactReviewModeAuto, "ARTIFACT_REVIEW_MODE_AUTO"},
	}
	for _, tc := range modes {
		if got := ArtifactReviewModeName(tc.mode); got != tc.want {
			t.Errorf("ArtifactReviewModeName(%d) = %q, want %q", tc.mode, got, tc.want)
		}
	}
}
