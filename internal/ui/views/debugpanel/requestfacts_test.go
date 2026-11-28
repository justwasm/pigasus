// requestfacts_test.go — the ratio line, which is the one figure in the
// Requests view that is a judgement rather than a measurement.
package debugpanel

import (
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/core/wirelog"
)

// TestTheRatioNeverContradictsItsOwnLabel — the line states a number and
// then a band, and the number must be inside the band it names.
//
// The first version printed "1.3 B/prompt token (normal ~3.0-4.0)": a
// self-contradiction on one line, in the column a reader is supposed to use
// to spot an anomalous request. A reader who catches that stops believing
// the rest of the table, which is the only thing the table has.
func TestTheRatioNeverContradictsItsOwnLabel(t *testing.T) {
	// Every band the label can name, probed at its edges.
	cases := []struct {
		bytes   int64
		tokens  int
		wantAny []string
	}{
		{500, 1000, []string{"fewer bytes than tokens"}},    // 0.5
		{1500, 1000, []string{"byte-lean"}},                 // 1.5
		{3200, 1000, []string{"usual 2–6 band"}},            // 3.2
		{5800, 1000, []string{"usual 2–6 band"}},            // 5.8
		{8000, 1000, []string{"byte-heavy"}},                // 8.0
		{20000, 1000, []string{"far above the usual band"}}, // 20.0
	}
	for _, c := range cases {
		r := wirelog.Record{RequestBytes: c.bytes, PromptTokens: c.tokens}
		facts := requestFacts(r)
		if !strings.Contains(facts, "B/prompt token") {
			t.Errorf("%d B / %d tokens: no ratio line:\n%s", c.bytes, c.tokens, facts)
			continue
		}
		matched := false
		for _, want := range c.wantAny {
			if strings.Contains(facts, want) {
				matched = true
			}
		}
		if !matched {
			t.Errorf("%d B / %d tokens (%.1f B/token): the label does not name its band %v:\n%s",
				c.bytes, c.tokens, float64(c.bytes)/float64(c.tokens), c.wantAny, facts)
		}
		// And no band may claim "normal" while naming a range: that is the
		// exact sentence the old version printed.
		if strings.Contains(facts, "normal ~") {
			t.Errorf("the ratio line still names a band it does not check:\n%s", facts)
		}
	}
}

// TestTheRatioIsAbsentWithoutBothHalves — a ratio needs bytes and tokens,
// and half of one is not a ratio.
func TestTheRatioIsAbsentWithoutBothHalves(t *testing.T) {
	for _, r := range []wirelog.Record{
		{RequestBytes: 3200},               // no tokens reported
		{PromptTokens: 1000},               // no bytes measured
		{RequestBytes: 0, PromptTokens: 0}, // nothing
	} {
		if facts := requestFacts(r); strings.Contains(facts, "B/prompt token") {
			t.Errorf("a ratio was invented from %+v:\n%s", r, facts)
		}
	}
}

// TestTheTokensLineIsOnlyThereWhenReported — the usage line comes from the
// provider, so it must not appear on an exchange that did not report usage.
func TestTheTokensLineIsOnlyThereWhenReported(t *testing.T) {
	if facts := requestFacts(wirelog.Record{RequestBytes: 100}); strings.Contains(facts, "prompt ·") {
		t.Errorf("an exchange with no usage report shows a token count:\n%s", facts)
	}
	with := requestFacts(wirelog.Record{RequestBytes: 3200, PromptTokens: 1000, CompletionTokens: 50, TotalTokens: 1050})
	for _, want := range []string{"1000 prompt", "50 completion", "1050 total"} {
		if !strings.Contains(with, want) {
			t.Errorf("the tokens line lost %q:\n%s", want, with)
		}
	}
}
