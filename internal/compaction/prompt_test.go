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
// captured instruction.
//
// The captured user turn was 2409 bytes: a 2289-byte body followed by
// the on-demand tail (the body carries three em-dashes, so it is 2283
// characters). A drift in either number means the prompt is no
// longer the one the reference sent.
func TestSummarizePrompt_MatchesCapturedBytes(t *testing.T) {
	if got := len(SummarizePrompt); got != 2289 {
		t.Errorf("len(SummarizePrompt) = %d, want 2289", got)
	}
	if got := len(SummarizePrompt + OnDemandCompactionSuffix); got != 2409 {
		t.Errorf("len(SummarizePrompt+OnDemandCompactionSuffix) = %d, want 2409", got)
	}
}

// TestSummarizePrompt_Structure pins the parts the reference's
// post-compaction context and its instruction agree on: the six
// sections the summary must carry, and the <summary> wrapper the
// reference embeds verbatim in the resuming block.
func TestSummarizePrompt_Structure(t *testing.T) {
	for _, want := range []string{
		"1. **Task Overview**",
		"2. **Progress**",
		"3. **Key Findings**",
		"4. **Active Context**",
		"5. **Next Steps**",
		"6. **Commitments & Constraints**",
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
// 【一手】 The server's catalogue carries, per model, the checkpoint
// experiment payload — and for the SAME_MODEL family its
// `session_summary_prompt_override` is 2289 bytes, byte-identical to
// the instruction the clone sends for the manual `/compact`. So the
// reference asks for the same six-section <summary> block in both
// paths, and the clone needs no second prompt.
//
// The fixture is the payload's own bytes, extracted from the committed
// live capture; TestSummarizePrompt_MatchesLiveCatalogue re-derives it
// from that capture.
func TestSummarizePrompt_MatchesCheckpointOverride(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "session_summary_prompt_override_1.4.4.txt"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if got := string(raw); got != SummarizePrompt {
		t.Errorf("SummarizePrompt differs from the catalogue's session_summary_prompt_override:\n got %d bytes\nwant %d bytes",
			len(got), len(SummarizePrompt))
	}
}

// TestSummarizePrompt_MatchesLiveCatalogue closes the loop to the live
// capture: the fixture in testdata/ must be the catalogue's own bytes,
// not a hand-copy of them.
func TestSummarizePrompt_MatchesLiveCatalogue(t *testing.T) {
	override, ok := catalogueOverride(t, "gemini-3.8-flash-medium")
	if !ok {
		t.Skipf("catalogue capture %s is not present", cataloguePath)
	}
	if override != SummarizePrompt {
		t.Errorf("the catalogue's session_summary_prompt_override differs from SummarizePrompt:\n got %d bytes\nwant %d bytes",
			len(override), len(SummarizePrompt))
	}
}
