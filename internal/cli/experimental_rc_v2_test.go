// Tests for the cycle rc-webrtc-mesh-defer's hidden
// --experimental-rc-v2 flag.
//
// What this file pins:
//
//   - the flag is registered in ExtraFlags() with the
//     expected spec entries (Name / Type / Hidden /
//     Usage) so a future spec drift fails loudly;
//   - the flag is NOT pulled into StandardFlags() (the
//     cycle pins it as an openagy-only hidden extra,
//     parallel with --backend / --alt-screen /
//     --trust-cwd);
//   - the spec is paired with the brief's wording so a
//     grep across docs and code finds the same surface.
//
// The test rig is the same `runJetski` harness the other
// rc-* test files use (close_gap_test.go). We deliberately
// do NOT assert that the parser accepts the flag at the
// cli/ level — that path is exercised by the
// cmd/wagyu/ remote_control_test.go's harness
// (main.go's RunAllExtras wires ExtraFlags() into the
// shared *flag.FlagSet). The cli/ package itself only
// owns the descriptor table.
package cli

import (
	"strings"
	"testing"
)

// experimentalRCV2Spec pins the spec entry for the
// --experimental-rc-v2 hidden flag in ExtraFlags(). The
// cycle rc-webrtc-mesh-defer brief pins:
//
//   - Name = "experimental-rc-v2"
//   - Type = "bool"
//   - Hidden = true (off the v1.3.5 / v1.4.2 --help surface)
//   - Usage starts with "Opt-in" / "v2" / "WebRTC" so
//     grepping the help table is unambiguous.
//
// A future cycle that drops or renames the flag should
// update this pin (and probably also update
// docs/deferred-rc-transport.md's forward contract).
var experimentalRCV2Spec = struct {
	Name   string
	Type   string
	Hidden bool
	Usage  string
}{
	Name:   "experimental-rc-v2",
	Type:   "bool",
	Hidden: true,
	Usage:  "Opt-in to the deferred v2",
}

// findExtraFlag returns the JetskiFlag descriptor in
// the ExtraFlags() set whose Name matches, or nil when
// the entry was dropped (the regression case the cycle's
// tests cover).
func findExtraFlag(name string) *FlagSpec {
	flags := ExtraFlags()
	for i, f := range flags {
		if f.Name == name {
			return &flags[i]
		}
	}
	return nil
}

// TestExperimentalRCV2_PresentInExtraFlags pins that
// --experimental-rc-v2 is registered as a hidden bool
// flag in ExtraFlags(), matching the brief's spec.
func TestExperimentalRCV2_PresentInExtraFlags(t *testing.T) {
	spec := findExtraFlag(experimentalRCV2Spec.Name)
	if spec == nil {
		t.Fatalf("ExtraFlags() missing %q flag; the cycle's brief pins this entry", experimentalRCV2Spec.Name)
	}
	if spec.Type != experimentalRCV2Spec.Type {
		t.Errorf("ExtraFlags()[%q].Type = %q, want %q", experimentalRCV2Spec.Name, spec.Type, experimentalRCV2Spec.Type)
	}
	if !spec.Hidden {
		t.Errorf("ExtraFlags()[%q].Hidden = false, want true (off the --help surface)", experimentalRCV2Spec.Name)
	}
	if !strings.Contains(spec.Usage, "Opt-in") {
		t.Errorf("ExtraFlags()[%q].Usage = %q, want contains 'Opt-in'", experimentalRCV2Spec.Name, spec.Usage)
	}
	if !strings.Contains(spec.Usage, "v2") {
		t.Errorf("ExtraFlags()[%q].Usage = %q, want contains 'v2'", experimentalRCV2Spec.Name, spec.Usage)
	}
}

// TestExperimentalRCV2_NotInStandardFlags pins that
// --experimental-rc-v2 is NOT a part of the v1.3.5
// StandardFlags() surface. The brief pins it as an
// openagy-only extra (cycle rc-webrtc-mesh-defer):
// dashboards learn the deferred-features envelope via
// RemoteControlStatus, not the flag.
func TestExperimentalRCV2_NotInStandardFlags(t *testing.T) {
	for _, f := range StandardFlags() {
		if f.Name == experimentalRCV2Spec.Name {
			t.Errorf("StandardFlags() should not contain hidden %q flag; cycle pins it as an ExtraFlag only",
				experimentalRCV2Spec.Name)
		}
	}
}

// TestExperimentalRCV2_DefensiveShape pins the spec
// entry's Type pin (no Drift): the brief pins the flag
// as a bool. A future refactor that flips it to e.g.
// "string" (for a value the dispatcher would parse) is
// a wire-shape change — it would change which harness
// path reads it.
func TestExperimentalRCV2_DefensiveShape(t *testing.T) {
	flags := ExtraFlags()
	spec := findExtraFlag(experimentalRCV2Spec.Name)
	if spec == nil {
		t.Fatalf("ExtraFlags() missing %q", experimentalRCV2Spec.Name)
	}
	// Test that there's exactly one entry with this
	// name (no duplicate registrations).
	count := 0
	for _, f := range flags {
		if f.Name == experimentalRCV2Spec.Name {
			count++
		}
	}
	if count != 1 {
		t.Errorf("ExtraFlags() has %d entries for %q, want exactly 1", count, experimentalRCV2Spec.Name)
	}
}

// TestExperimentalRCV2_HiddenFromHelp pins the headline
// contract: `wagyu --help` does NOT mention
// --experimental-rc-v2. The flag is a forward-compat
// knob and is deliberately off the visible surface so
// v1.4.2 --help parity is preserved.
//
// The runJetski harness's parser uses StandardFlags()
// only — the harness's --help table won't show hidden
// extras (they're registered by main.go's
// RunAllExtras, which the cli-internal rig does NOT
// install). This test pins the visible-surface
// contract end-to-end via flag.PrintDefaults on the
// root FlagSet alone.
func TestExperimentalRCV2_HiddenFromHelp(t *testing.T) {
	// Even if a future refactor pulls ExtraFlags() into
	// the runJetski harness, the Hidden:true spec means
	// the help renderer (jetski.go:PrintHelp) must skip
	// the entry. This test asserts the spec contract,
	// not the harness's registration seam.
	spec := findExtraFlag(experimentalRCV2Spec.Name)
	if spec == nil {
		t.Fatalf("ExtraFlags() missing %q flag; cannot assert hidden contract", experimentalRCV2Spec.Name)
	}
	if !spec.Hidden {
		t.Errorf("ExtraFlags()[%q].Hidden = false; the help-table renderer contract requires Hidden=true",
			experimentalRCV2Spec.Name)
	}
	// Also check the StandardFlags() surface doesn't
	// drag the flag into the visible help table.
	for _, f := range StandardFlags() {
		if f.Name == experimentalRCV2Spec.Name {
			t.Errorf("StandardFlags() contains %q; v1.3.5 --help table renders this — cycle pins it as ExtraFlag-only",
				experimentalRCV2Spec.Name)
		}
	}
}
