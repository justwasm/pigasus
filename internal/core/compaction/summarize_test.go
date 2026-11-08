package compaction

import "testing"

// TestNormalizeSummary_DropsReasoning pins the live 3.8 shape: CloudCode can
// prepend a <think> block to the summary stream, but the reference's stored
// checkpoint starts at <summary> and ends at </summary>.
func TestNormalizeSummary_DropsReasoning(t *testing.T) {
	in := "<think>The goal is to summarize.\n\nsecond thought</think><summary>\n### 1. Task Overview\n- ok\n</summary>"
	want := "<summary>\n### 1. Task Overview\n- ok\n</summary>"
	if got := normalizeSummary(in); got != want {
		t.Errorf("normalizeSummary = %q, want %q", got, want)
	}
}

func TestNormalizeSummary_KeepsUnwrappedText(t *testing.T) {
	in := "  plain summary  "
	if got := normalizeSummary(in); got != "plain summary" {
		t.Errorf("normalizeSummary = %q", got)
	}
}

func TestNormalizeSummary_DropsTrailingProse(t *testing.T) {
	in := "<summary>body</summary>\nDone."
	if got := normalizeSummary(in); got != "<summary>body</summary>" {
		t.Errorf("normalizeSummary = %q", got)
	}
}
