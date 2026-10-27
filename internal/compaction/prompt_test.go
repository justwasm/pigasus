// prompt_test.go — the reference's continuation-summary instruction.
//
// The prompt is byte-exact evidence: it was captured from a live agy
// 1.4.5 compaction request (the CloudCode request body the proxy
// (internal/agyproxy) recorded). The tests pin the byte count, the split
// between
// the shared body and the on-demand tail, and the structural markers
// the reference's own post-compaction context depends on.

package compaction

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// cataloguePath is the committed live capture of the server's model
// catalogue. Every entry carries the model's own checkpoint experiment
// payload, and for the SAME_MODEL family that payload's
// `session_summary_prompt_override` is the reference's *automatic*
// compaction instruction.
//
// 【一手】 agy 1.4.4, live probe of fetchAvailableModels.
const cataloguePath = "../../docs/cycles/model-catalogue-argon-gate/fetchAvailableModels_1.4.4.json"

// catalogueOverride is the `session_summary_prompt_override` of the
// named catalogue entry.
func catalogueOverride(t *testing.T, modelID string) (string, bool) {
	t.Helper()
	raw, err := os.ReadFile(cataloguePath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false
		}
		t.Fatalf("read catalogue: %v", err)
	}
	var catalogue struct {
		Models map[string]struct {
			ModelExperiments struct {
				Experiments map[string]struct {
					StringValue string `json:"stringValue"`
				} `json:"experiments"`
			} `json:"modelExperiments"`
		} `json:"models"`
	}
	if err := json.Unmarshal(raw, &catalogue); err != nil {
		t.Fatalf("parse catalogue: %v", err)
	}
	entry, ok := catalogue.Models[modelID]
	if !ok {
		t.Fatalf("catalogue has no model %q", modelID)
	}
	exp, ok := entry.ModelExperiments.Experiments["CASCADE_USE_EXPERIMENT_CHECKPOINTER"]
	if !ok {
		t.Fatalf("catalogue entry %q has no checkpoint experiment", modelID)
	}
	var payload struct {
		SessionSummaryPromptOverride string `json:"session_summary_prompt_override"`
	}
	if err := json.Unmarshal([]byte(exp.StringValue), &payload); err != nil {
		t.Fatalf("parse checkpoint payload: %v", err)
	}
	return payload.SessionSummaryPromptOverride, true
}

// TestSummarizePrompt_MatchesCapturedBytes pins the byte count of the
// instruction the reference carries.
//
// 【一手】 agy 1.4.7 holds the instruction as a string literal at file
// offset 0x3785081: 3 937 bytes (3 931 characters — the body carries three
// em-dashes). The on-demand tail adds 120 bytes, so the user turn the
// reference sends is 4 057 bytes. A drift in either number means the
// prompt is no longer the one the reference ships.
func TestSummarizePrompt_MatchesCapturedBytes(t *testing.T) {
	if got := len(SummarizePrompt); got != 3937 {
		t.Errorf("len(SummarizePrompt) = %d, want 3937", got)
	}
	if got := len(SummarizePrompt + OnDemandCompactionSuffix); got != 4057 {
		t.Errorf("len(SummarizePrompt+OnDemandCompactionSuffix) = %d, want 4057", got)
	}
}

// TestSummarizePrompt_Structure pins the parts the reference's
// post-compaction context and its instruction agree on: the seven
// sections the summary must carry, and the <summary> wrapper the
// reference embeds verbatim in the resuming block.
//
// 【一手】 agy 1.4.7's instruction asks for seven sections: it splits
// "Errors & Fixes" out of "Key Findings" and renumbers the rest. The
// 1.4.4 revision the clone used to carry had six.
func TestSummarizePrompt_Structure(t *testing.T) {
	for _, want := range []string{
		"1. **Task Overview**",
		"2. **Progress**",
		"3. **Key Findings**",
		"4. **Errors & Fixes**",
		"5. **Active Context**",
		"6. **Next Steps**",
		"7. **Commitments & Constraints**",
		"Wrap your response in <summary></summary> tags.",
		"Do not call any tools",
	} {
		if !strings.Contains(SummarizePrompt, want) {
			t.Errorf("SummarizePrompt missing %q", want)
		}
	}
	if !strings.HasSuffix(SummarizePrompt, "tags.\n") {
		t.Errorf("SummarizePrompt should end with the wrapper sentence; got %q", SummarizePrompt[len(SummarizePrompt)-20:])
	}
}

// TestOnDemandCompactionSuffix pins the tail the reference appends when
// the *user* triggered the compaction.
func TestOnDemandCompactionSuffix(t *testing.T) {
	if !strings.HasPrefix(OnDemandCompactionSuffix, "\n\nON-DEMAND COMPACTION:\n") {
		t.Errorf("OnDemandCompactionSuffix should open with the blank line + header; got %q", OnDemandCompactionSuffix)
	}
	if !strings.HasSuffix(OnDemandCompactionSuffix,
		"The user triggered this compaction. Follow any instructions in their latest message, if present.") {
		t.Errorf("OnDemandCompactionSuffix tail changed; got %q", OnDemandCompactionSuffix)
	}
}

// TestSummarizePrompt_MatchesCheckpointOverride pins the instruction the
// clone sends against the reference's *configured* automatic-compaction
// instruction.
//
// 【一手】 agy 1.4.7 carries the instruction twice: as a string literal in
// its rodata and as the `session_summary_prompt_override` of the two
// profiles that override it (MODEL_PLACEHOLDER_M124,
// MODEL_PLACEHOLDER_M408). Both are the same 3 937 bytes, and that is
// what the clone sends — so the reference asks for the same seven-section
// <summary> block in the automatic and the manual path.
//
// The fixture is the override's own bytes, taken from the binary's
// embedded model_profiles.json (the document committed at
// internal/modelctx/testdata/model_profiles_1.4.7.json).
func TestSummarizePrompt_MatchesCheckpointOverride(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "session_summary_prompt_1.4.7.txt"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if got := string(raw); got != SummarizePrompt {
		t.Errorf("SummarizePrompt differs from the catalogue's session_summary_prompt_override:\n got %d bytes\nwant %d bytes",
			len(got), len(SummarizePrompt))
	}
}

// TestSummarizePrompt_SupersedesThe144Revision pins that the clone's
// instruction is the *newer* revision and that the older one is kept as
// evidence.
//
// The 1.4.4 catalogue the clone used to read carries the six-section
// revision (2 289 bytes). The fixture next to it must still be those
// bytes — otherwise the historical capture was edited — and it must no
// longer be what the clone sends.
func TestSummarizePrompt_SupersedesThe144Revision(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "session_summary_prompt_override_1.4.4.txt"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	old := string(raw)
	if len(old) != 2289 {
		t.Fatalf("the 1.4.4 fixture is %d bytes, want 2289", len(old))
	}
	if old == SummarizePrompt {
		t.Fatal("SummarizePrompt is still the 1.4.4 revision")
	}
	if !strings.Contains(old, "6. **Commitments & Constraints**") {
		t.Error("the 1.4.4 fixture should carry the six-section revision")
	}
	if strings.Contains(old, "Errors & Fixes") {
		t.Error("the 1.4.4 fixture should not carry the 1.4.7 section")
	}
}

// TestSummarizePrompt_MatchesLiveCatalogue closes the loop to the live
// catalogue: the 1.4.4 capture's own override must still be the bytes the
// 1.4.4 fixture holds — the fixture is evidence, not a hand-copy of the
// clone's current prompt.
//
// The capture is a *1.4.4* probe, so it cannot show the 1.4.7 revision;
// the live 1.4.7 text is the binary's own string literal, committed as
// testdata/session_summary_prompt_1.4.7.txt.
func TestSummarizePrompt_MatchesLiveCatalogue(t *testing.T) {
	override, ok := catalogueOverride(t, "gemini-3.8-flash-medium")
	if !ok {
		t.Skipf("catalogue capture %s is not present", cataloguePath)
	}
	raw, err := os.ReadFile(filepath.Join("testdata", "session_summary_prompt_override_1.4.4.txt"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if override != string(raw) {
		t.Errorf("the catalogue's session_summary_prompt_override differs from the 1.4.4 fixture:\n got %d bytes\nwant %d bytes",
			len(override), len(raw))
	}
}
