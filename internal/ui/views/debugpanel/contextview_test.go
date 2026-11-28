// contextview_test.go — the Context view: measured occupancy, and the
// provenance of every number in the report.
//
// The tests are grouped by the property they pin rather than by function:
// a table whose values are right but whose sources lie is worse than no
// table, so the honesty tests come first.
package debugpanel

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/justwasm/pigasus/internal/core/wirelog"
)

// ctxFixture is a session mid-conversation: the provider has reported a
// turn, the model has a known window, and the recorder holds the request
// that turn sent.
func ctxFixture() ContextState {
	return ContextState{
		Model:                   "gemini-3-pro",
		WindowTokens:            1_048_576,
		AutoCompactTokens:       800_000,
		LastPromptTokens:        41_000,
		LastCompletionTokens:    1_200,
		LastCachedTokens:        30_000,
		SessionPromptTokens:     120_000,
		SessionCompletionTokens: 4_000,
		Messages:                18,
		Wire: &wirelog.Record{
			Seq:           7,
			Method:        "POST",
			Path:          "/v1internal:streamGenerateContent",
			Status:        200,
			RequestBytes:  123_000,
			ResponseBytes: 4_096,
		},
		Width: 120,
	}
}

// factFor returns the fact with the given key.
func factFor(t *testing.T, st ContextState, key string) contextFact {
	t.Helper()
	for _, f := range contextFacts(st) {
		if f.Key == key {
			return f
		}
	}
	t.Fatalf("no fact with key %q", key)
	return contextFact{}
}

// TestTheOccupancyIsTheReportedPrompt — the headline number is the last
// turn's reported prompt tokens, not the session total.
//
// This is the confusion the view exists to prevent: the session total grows
// without bound and would read 120k here against a 41k prompt, making the
// window look three times fuller than it is.
func TestTheOccupancyIsTheReportedPrompt(t *testing.T) {
	st := ctxFixture()
	f := factFor(t, st, "used")
	plain := ansi.Strip(f.Value)
	if !strings.Contains(plain, "41 K") {
		t.Errorf("the context row reads %q, want the reported 41 K", plain)
	}
	if strings.Contains(plain, "120 K") {
		t.Errorf("the context row reads %q, which is the session total, not the prompt", plain)
	}
	if f.Source != sourceReported {
		t.Errorf("the context row's source is %q, want %q", f.Source, sourceReported)
	}
}

// TestNoReportMeansNoNumber — a session with no completed turn says so
// rather than printing a zero, a percentage or an estimate.
func TestNoReportMeansNoNumber(t *testing.T) {
	st := ctxFixture()
	st.LastPromptTokens, st.LastCompletionTokens, st.LastCachedTokens = 0, 0, 0

	f := factFor(t, st, "used")
	plain := ansi.Strip(f.Value)
	if strings.Contains(plain, "%") {
		t.Errorf("the context row shows a percentage with no report: %q", plain)
	}
	if !strings.Contains(plain, "—") {
		t.Errorf("the context row reads %q, want a dash for the missing measure", plain)
	}
	if f.Source != sourceNone {
		t.Errorf("the context row claims source %q with no report; want %q", f.Source, sourceNone)
	}
	// And no bar: drawing one would invent a fill for a number nobody has.
	if bar := ansi.Strip(strings.Join(ContextHeaderLines(st), "\n")); strings.Contains(bar, "█") {
		t.Errorf("the header drew a filled bar with no report: %q", bar)
	}
}

// TestTheSourcesNameTheKindOfNumber — every row says where its value came
// from, and no row claims a source for a value it does not have.
//
// The rule: a row whose measure is missing reports sourceNone. A row that
// printed "reported" next to a dash would be telling the reader the
// provider reported nothing, which is a different claim from "no provider
// has spoken yet".
func TestTheSourcesNameTheKindOfNumber(t *testing.T) {
	st := ctxFixture()
	want := map[string]contextSource{
		"used":         sourceReported,
		"last":         sourceReported,
		"wire":         sourceMeasured,
		"estimate":     sourceDerived,
		"window":       sourceConfigured,
		"session":      sourceReported,
		"conversation": sourceMeasured,
		"compactions":  sourceMeasured,
	}
	facts := contextFacts(st)
	if len(facts) != len(want) {
		t.Fatalf("the report has %d rows, want %d", len(facts), len(want))
	}
	for _, f := range facts {
		expected, ok := want[f.Key]
		if !ok {
			t.Errorf("unexpected row %q", f.Key)
			continue
		}
		if f.Source != expected {
			t.Errorf("row %q: source = %q, want %q", f.Key, f.Source, expected)
		}
		if f.Detail == "" {
			t.Errorf("row %q has no provenance paragraph", f.Key)
		}
	}

	// With nothing measured at all, no row may claim a source — except the
	// two rows whose zero is itself a measurement, because the process
	// counted it rather than failed to: zero messages is a conversation
	// that has not started, and zero compactions is a history the recorder
	// watched and found empty.
	//
	// The distinction that matters is the wording: the compactions row says
	// "none recorded", not "none happened". A reader must not be told the
	// conversation was never compacted by a recorder that only exists in
	// this process.
	empty := contextFacts(ContextState{})
	for _, f := range empty {
		switch f.Key {
		case "conversation", "compactions":
			if f.Source != sourceMeasured {
				t.Errorf("empty report: row %q claims %q, want %q", f.Key, f.Source, sourceMeasured)
			}
		default:
			if f.Source != sourceNone {
				t.Errorf("empty report: row %q claims source %q", f.Key, f.Source)
			}
		}
	}
	// And the empty compactions row must not claim the conversation was
	// never compacted — only that this session recorded no compaction.
	for _, f := range empty {
		if f.Key == "compactions" && strings.Contains(f.Value, "none happened") {
			t.Errorf("the empty compactions row overclaims: %q", f.Value)
		}
	}
}

// TestThePercentageIsPromptOverWindow — the percentage is the arithmetic
// the status line uses, and it is styled by how close it is to the ceiling.
func TestThePercentageIsPromptOverWindow(t *testing.T) {
	cases := []struct {
		prompt, window int
		wantPct        string
		wantColour     string
	}{
		{41_000, 1_048_576, "3.9%", logSuccessOpen},
		{800_000, 1_048_576, "76.3%", logRetryOpen},
		{1_000_000, 1_048_576, "95.4%", logErrorOpen},
	}
	for _, c := range cases {
		st := ContextState{LastPromptTokens: c.prompt, WindowTokens: c.window}
		f := factFor(t, st, "used")
		if !strings.Contains(f.Value, c.wantPct) {
			t.Errorf("%d/%d: the row reads %q, want %s", c.prompt, c.window, ansi.Strip(f.Value), c.wantPct)
		}
		if !strings.Contains(f.Value, c.wantColour+c.wantPct) {
			t.Errorf("%d/%d: %s is not painted %q", c.prompt, c.window, c.wantPct, c.wantColour)
		}
	}
}

// TestTheBarMatchesTheNumber — the bar's fill is the percentage the row
// prints, so the two cannot disagree.
func TestTheBarMatchesTheNumber(t *testing.T) {
	// Half a window with 16 cells is 8 filled.
	st := ContextState{LastPromptTokens: 500, WindowTokens: 1000}
	bar := ansi.Strip(contextBar(st, 16))
	if got := strings.Count(bar, "█"); got != 8 {
		t.Errorf("the bar has %d filled cells, want 8: %q", got, bar)
	}
	if got := strings.Count(bar, "░"); got != 8 {
		t.Errorf("the bar has %d empty cells, want 8: %q", got, bar)
	}

	// Full is all filled, and must not overfill on rounding.
	full := ContextState{LastPromptTokens: 1000, WindowTokens: 1000}
	if got := strings.Count(ansi.Strip(contextBar(full, 16)), "█"); got != 16 {
		t.Errorf("a full window filled %d of 16 cells", got)
	}
	// An unknown window draws no bar at all rather than a wrong one.
	if bar := ansi.Strip(contextBar(ContextState{}, 16)); strings.Contains(bar, "█") {
		t.Errorf("an unknown window drew a filled bar: %q", bar)
	}
}

// TestTheEstimateRowMeasuresItsOwnError — the estimator appears only to be
// checked against the provider's count, and says so.
//
// The row is the reason the estimator is in this view at all: printed
// alone, "123000 bytes / 3 = 41 K tokens" would read as a measurement of
// the prompt. Printed beside the reported count and its drift, it is a
// calibration.
func TestTheEstimateRowMeasuresItsOwnError(t *testing.T) {
	st := ctxFixture()
	f := factFor(t, st, "estimate")
	plain := ansi.Strip(f.Value)
	if !strings.Contains(plain, "41 K") {
		t.Errorf("the estimate row reads %q, want the 41 K the divisor yields", plain)
	}
	if !strings.Contains(plain, "vs 41 K reported") {
		t.Errorf("the estimate row reads %q, want it compared with the report", plain)
	}
	if !strings.Contains(plain, "%") {
		t.Errorf("the estimate row reads %q, want the drift percentage", plain)
	}
	if f.Source != sourceDerived {
		t.Errorf("the estimate row's source is %q, want %q", f.Source, sourceDerived)
	}

	// Without one of the two halves there is nothing to compare, so the row
	// carries no number rather than half a comparison.
	st.Wire = nil
	if got := ansi.Strip(factFor(t, st, "estimate").Value); strings.Contains(got, "÷") {
		t.Errorf("the estimate row computed with no recorded request: %q", got)
	}
}

// TestTheDriftIsDirectional — the drift says which way the estimator is
// wrong, because "10% off" and "-10% off" want different fixes.
func TestTheDriftIsDirectional(t *testing.T) {
	// The estimate is low: the provider counted more than the divisor gives.
	under := ansi.Strip(contextDriftSuffix(100, 150))
	if !strings.Contains(under, "+50.0%") {
		t.Errorf("under-estimate drift = %q, want +50.0%%", under)
	}
	over := ansi.Strip(contextDriftSuffix(100, 50))
	if !strings.Contains(over, "-50.0%") {
		t.Errorf("over-estimate drift = %q, want -50.0%%", over)
	}
	// A large drift is flagged; a small one is not.
	if strings.Contains(contextDriftSuffix(100, 103), logRetryOpen) {
		t.Error("a 3% drift was flagged as a problem")
	}
	if !strings.Contains(contextDriftSuffix(100, 150), logRetryOpen) {
		t.Error("a 50% drift was not flagged")
	}
}

// TestTheWireRowIsTheMeasuredSize — the wire row prints the recorder's real
// byte counts, which are the only measured sizes in the report.
func TestTheWireRowIsTheMeasuredSize(t *testing.T) {
	st := ctxFixture()
	f := factFor(t, st, "wire")
	plain := ansi.Strip(f.Value)
	if !strings.Contains(plain, "120.1 KB") {
		t.Errorf("the wire row reads %q, want the 120.1 KB sent", plain)
	}
	if !strings.Contains(plain, "4 KB") {
		t.Errorf("the wire row reads %q, want the 4 KB received", plain)
	}
	if f.Source != sourceMeasured {
		t.Errorf("the wire row's source is %q, want %q", f.Source, sourceMeasured)
	}

	st.Wire = nil
	gone := factFor(t, st, "wire")
	if !strings.Contains(ansi.Strip(gone.Value), "no exchange recorded") {
		t.Errorf("with no recorder the wire row reads %q", ansi.Strip(gone.Value))
	}
	if gone.Source != sourceNone {
		t.Errorf("with no recorder the wire row claims source %q", gone.Source)
	}
}

// TestTheWindowRowNamesTheThresholdAndItsSource — the window row says where
// the limit came from and where compaction fires, and admits when the
// catalogue has no limit.
func TestTheWindowRowNamesTheThresholdAndItsSource(t *testing.T) {
	st := ctxFixture()
	plain := ansi.Strip(factFor(t, st, "window").Value)
	if !strings.Contains(plain, "1 M") {
		t.Errorf("the window row reads %q, want the 1 M window", plain)
	}
	if !strings.Contains(plain, "auto-compact at 800 K") {
		t.Errorf("the window row reads %q, want the threshold", plain)
	}

	st.AutoCompactSpec = "AGY_CLI_AUTO_COMPACT_OVERRIDE=800k"
	if got := ansi.Strip(factFor(t, st, "window").Value); !strings.Contains(got, "AGY_CLI_AUTO_COMPACT_OVERRIDE") {
		t.Errorf("the window row hid the override: %q", got)
	}

	// No threshold configured means the server decides, and the row says so
	// rather than printing zero.
	st.AutoCompactTokens, st.AutoCompactSpec = 0, ""
	if got := ansi.Strip(factFor(t, st, "window").Value); !strings.Contains(got, "server's default") {
		t.Errorf("the window row reads %q, want the server default", got)
	}

	// An unknown window is named as unknown, not defaulted to 1M.
	unknown := ContextState{}
	if got := ansi.Strip(factFor(t, unknown, "window").Value); !strings.Contains(got, "unknown") {
		t.Errorf("an unknown window reads %q", got)
	}
	if src := factFor(t, unknown, "window").Source; src != sourceNone {
		t.Errorf("an unknown window claims source %q", src)
	}
}

// TestTheSessionRowSaysItIsBilling — the cumulative total is labelled as
// billing, because it is the number most easily mistaken for occupancy.
func TestTheSessionRowSaysItIsBilling(t *testing.T) {
	plain := ansi.Strip(factFor(t, ctxFixture(), "session").Value)
	if !strings.Contains(plain, "billed across every turn") {
		t.Errorf("the session row reads %q, want it labelled as billing", plain)
	}
}

// TestTheReaderExplainsTheProvenance — a row's reader body names the
// mechanism rather than restating the number.
func TestTheReaderExplainsTheProvenance(t *testing.T) {
	st := ctxFixture()
	for _, f := range contextFacts(st) {
		body := ContextBody("ctx/"+f.Key, st)
		if len(body.Parts) != 2 {
			t.Errorf("row %q: the body has %d parts, want 2", f.Key, len(body.Parts))
			continue
		}
		if body.Parts[0].Text != f.Detail {
			t.Errorf("row %q: the reader's provenance is not the row's", f.Key)
		}
		// The second part is the whole report, so the reader can check the
		// arithmetic against the other rows.
		if !strings.Contains(body.Parts[1].Text, "conversation") {
			t.Errorf("row %q: the body does not carry the whole report", f.Key)
		}
		if body.Title == "" {
			t.Errorf("row %q: the body has no title", f.Key)
		}
	}
}

// TestTheReaderFallsBackForAnAgedOutRow — an ID the state no longer holds
// opens a note rather than an empty body.
func TestTheReaderFallsBackForAnAgedOutRow(t *testing.T) {
	body := ContextBody("ctx/nonexistent", ctxFixture())
	if len(body.Parts) == 0 || !strings.Contains(body.Parts[0].Text, "not in the current report") {
		t.Errorf("an unknown row opened %+v", body.Parts)
	}
}

// TestTheRowsAreFlatAndKeyed — the rows are a table, and each carries the
// stable ID the reader is asked for.
func TestTheRowsAreFlatAndKeyed(t *testing.T) {
	rows := ContextRows(ctxFixture())
	if len(rows) == 0 {
		t.Fatal("the view rendered no rows")
	}
	seen := map[string]bool{}
	for _, r := range rows {
		if !strings.HasPrefix(r.ID, "ctx/") {
			t.Errorf("row %q has ID %q, want a ctx/ prefix", r.Text, r.ID)
		}
		if seen[r.ID] {
			t.Errorf("row ID %q appears twice", r.ID)
		}
		seen[r.ID] = true
		if !r.Flat {
			t.Errorf("row %q is not flat; the table would break its columns", r.ID)
		}
		if r.Text == "" {
			t.Errorf("row %q has no text", r.ID)
		}
	}
}

// TestTheRowsClipToTheWidth — a narrow terminal cuts the value, not the
// row, and never overflows the frame.
func TestTheRowsClipToTheWidth(t *testing.T) {
	for _, width := range []int{40, 60, 80, 120, 200} {
		st := ctxFixture()
		st.Width = width
		for _, r := range ContextRows(st) {
			if got := ansi.StringWidth(r.Text); got > width {
				t.Errorf("width %d: row %q is %d cells wide", width, r.ID, got)
			}
		}
	}
}

// TestTheViewIsLive — the Context rows are re-read every frame.
//
// It is the regression the Requests view already paid for: the recorder adds
// records from a transport goroutine and the token store is written from the
// stream loop, and neither bumps the panel's generation. A generation-keyed
// memo would freeze the occupancy at whatever it was when something else
// last moved — the exact number the user opened the view to watch.
func TestTheViewIsLive(t *testing.T) {
	usage := int64(10_000)
	p := New(Seams{
		Context: func() ContextState {
			return ContextState{
				WindowTokens:     100_000,
				LastPromptTokens: int(usage),
			}
		},
		Width:  func() int { return 120 },
		Height: func() int { return 24 },
	})
	p.SetView(ViewContext)
	p.Lines()

	find := func() string {
		for _, r := range ContextRows(p.ctxState) {
			if r.ID == "ctx/used" {
				return ansi.Strip(r.Text)
			}
		}
		return ""
	}
	if got := find(); !strings.Contains(got, "10 K") {
		t.Fatalf("the first draw reads %q, want 10 K", got)
	}

	// The store moves without telling the panel: no key, no wheel, no
	// Invalidate. The next frame must still see the new value.
	usage = 55_000
	p.Lines()
	if got := find(); !strings.Contains(got, "55 K") {
		t.Errorf("after the store moved the row reads %q, want 55 K", got)
	}
}

// TestTheContextViewIsInTheTabCycle — the fifth tab is reachable by tab and
// by its number key.
func TestTheContextViewIsInTheTabCycle(t *testing.T) {
	names := make([]string, 0, ViewCount())
	for i := 0; i < ViewCount(); i++ {
		names = append(names, View(i).Name())
	}
	if got := names[len(names)-1]; got != "Context" {
		t.Fatalf("the bar's labels are %v, want Context last", names)
	}

	p := fixture()
	p.SetView(ViewLogs)
	// The number key is derived from the view's own position, so adding a
	// view ahead of Context does not leave this test pointing at the old
	// slot (which is how key 5 came to land on Requests).
	key := fmt.Sprintf("%d", int(ViewContext)+1)
	p.HandleKey(tea.Key{Code: rune(key[0]), Text: key})
	if p.View() != ViewContext {
		t.Errorf("key %s landed on %v, want Context", key, p.View())
	}
}

// TestEnterReadsOnTheFirstPress — a Context row is a leaf, so Enter opens
// the reader rather than unfolding an inline excerpt.
//
// The Requests view shipped the opposite for one commit: its legend said
// "enter read" while Enter unfolded a summary that repeated the columns
// beside it, and the reader needed a second key. A row whose Detail is set
// is foldable, and a foldable row eats the first Enter.
func TestEnterReadsOnTheFirstPress(t *testing.T) {
	for _, r := range ContextRows(ctxFixture()) {
		if r.Foldable() {
			t.Errorf("row %q is foldable; Enter would fold it instead of reading it", r.ID)
		}
	}

	p := New(Seams{
		Context: func() ContextState { return ctxFixture() },
		Width:   func() int { return 120 },
		Height:  func() int { return 24 },
	})
	p.SetView(ViewContext)
	p.Lines()

	handled, _ := p.HandleKey(tea.Key{Code: tea.KeyEnter})
	if !handled {
		t.Fatal("Enter was not handled")
	}
	if p.reader == nil {
		t.Fatal("Enter did not open the reader")
	}
	if !strings.HasPrefix(p.reader.body.Title, "context · ") {
		t.Errorf("the reader opened %q, want a context row's body", p.reader.body.Title)
	}
	if p.reader.body.Title == "context · context" {
		t.Error("the reader's title repeats the view's name")
	}
}
