package main

import (
	"strings"
	"testing"
)

func TestCompareWire_SelfIdentical(t *testing.T) {
	for _, scName := range []string{"standard_turn", "compaction_request", "compaction_turn"} {
		t.Run(scName, func(t *testing.T) {
			sc, err := GetScenario(scName)
			if err != nil {
				t.Fatalf("scenario %s: %v", scName, err)
			}
			p := BuildReferenceWire(*sc)

			opts := DefaultCompareOptions()
			diff := CompareWire(p, p, opts)

			if !diff.Passed {
				t.Errorf("self comparison failed for %s: %s", scName, RenderTerminal(diff))
			}
			if diff.FailedChecks > 0 {
				t.Errorf("%s failed checks = %d, want 0", scName, diff.FailedChecks)
			}
			if len(diff.Diagnostics) > 0 {
				t.Errorf("%s diagnostics = %v, want none", scName, diff.Diagnostics)
			}
		})
	}
}

func TestCompareWire_CatchesDriftDiagnostics(t *testing.T) {
	sc, err := GetScenario("standard_turn")
	if err != nil {
		t.Fatalf("scenario standard_turn: %v", err)
	}
	ref := BuildReferenceWire(*sc)
	tgt := BuildReferenceWire(*sc)

	// Inject 4 classic drifts into target:
	// 1. Terminal sandbox in unsandboxed session
	tgt.SystemInstruction = "<terminal_sandbox>sandbox warning</terminal_sandbox>\n" + tgt.SystemInstruction
	// 2. Identity changed to non-generic coding assistant
	tgt.SystemInstruction = strings.Replace(tgt.SystemInstruction, "agentic AI assistant designed by the Google Deepmind team.", "agentic AI coding assistant designed by the Google Deepmind team working on Advanced Agentic Coding.", 1)
	// 3. User info changed to Code relating to...
	tgt.SystemInstruction = strings.Replace(tgt.SystemInstruction, "Files relating to the user's requests", "Code relating to the user's requests", 1)
	// 4. invoke_subagent has appended catalog
	for i := range tgt.Tools {
		if tgt.Tools[i].Name == "invoke_subagent" {
			tgt.Tools[i].Description += "\n\nBuilt-in subagents:\n- self"
		}
	}

	opts := DefaultCompareOptions()
	diff := CompareWire(ref, tgt, opts)

	if diff.Passed {
		t.Fatalf("expected diff to fail, but it passed")
	}

	diagCodes := make(map[string]bool)
	for _, d := range diff.Diagnostics {
		diagCodes[d.Code] = true
	}

	expectedCodes := []string{
		"DIAG_SECTION_ORDER",
		"DIAG_SANDBOX_MISMATCH",
		"DIAG_IDENTITY_DRIFT",
		"DIAG_USER_INFO_WORDING",
		"DIAG_INVOKE_SUBAGENT_DESCRIPTION",
	}

	for _, code := range expectedCodes {
		if !diagCodes[code] {
			t.Errorf("expected diagnostic code %s was not triggered! Triggered: %v", code, diagCodes)
		}
	}
}
