// tokens_test.go — the prompt's own token arithmetic.

package agyprompt

import "testing"

func TestSectionTokenCount_MeasuresTheWholeSection(t *testing.T) {
	// The section is the body plus the opening and the closing tag,
	// and the reference counts three bytes per token.
	got := SectionTokenCount("skills", "12345")
	want := (5 + 2*len("skills") + 2 + 1) / 3
	if got != want {
		t.Errorf("SectionTokenCount = %d, want %d", got, want)
	}
}

func TestSectionTokenCount_AnEmptyBodyCountsNothing(t *testing.T) {
	if got := SectionTokenCount("skills", ""); got != 0 {
		t.Errorf("SectionTokenCount = %d, want 0", got)
	}
}
