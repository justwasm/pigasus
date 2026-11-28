// summaryview_test.go — the landing view: what it puts on one screen, and
// what it refuses to overclaim.
package debugpanel

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/core/compaction"
	"github.com/justwasm/pigasus/internal/core/wirelog"
)

// summaryFixture is a session with something in every section, so a missing
// section is a missing section rather than an empty input.
func summaryFixture() (ContextState, RequestState) {
	ctx := ctxFixture()
	ctx.Compactions = []compaction.Event{
		{
			Seq: 1, Timestamp: time.Now(), Reason: "threshold",
			TokensBefore: 200_000, TokensAfter: 40_000, StepsSummarized: 5,
			Duration: 3 * time.Second, Summary: "summarised turns 1-5",
		},
	}
	req := RequestState{
		Records: []wirelog.Record{
			{Seq: 1, Method: "POST", Host: "daily-cloudcode-pa.googleapis.com", Path: "/v1internal:streamGenerateContent", Status: 200, Duration: 900 * time.Millisecond, TTFB: 200 * time.Millisecond, RequestBytes: 32_000, ResponseBytes: 8_000},
			{Seq: 2, Method: "POST", Host: "daily-cloudcode-pa.googleapis.com", Path: "/v1internal:streamGenerateContent", Status: 429, Duration: 1200 * time.Millisecond, TTFB: 100 * time.Millisecond, RequestBytes: 32_000, ResponseBytes: 400},
		},
		Width: 120,
	}
	return ctx, req
}

// TestTheSummaryAnswersTheQuestionThatOpensThePanel — model, occupancy,
// endpoint, exchange health and the last error are all on the landing screen.
//
// The view exists so a user does not page through five tabs to find out
// whether anything is wrong. If a section is not here, that argument dies.
func TestTheSummaryAnswersTheQuestionThatOpensThePanel(t *testing.T) {
	ctx, req := summaryFixture()
	rows := SummaryRows(ctx, req, 120)
	if len(rows) == 0 {
		t.Fatal("the summary drew no rows")
	}

	var text strings.Builder
	for _, r := range rows {
		text.WriteString(r.Text)
		text.WriteString("\n")
	}
	got := text.String()

	for _, want := range []string{
		"gemini-3-pro",                      // the model
		"daily-cloudcode-pa.googleapis.com", // the endpoint it talked to
		"occupancy",                         // how full the window is
		"exchanges",                         // how many requests
		"429",                               // the failure, named
		"compaction",                        // the history section
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the summary does not mention %q:\n%s", want, got)
		}
	}
}

// TestTheSummarySaysWhenItHasNothing — an empty session says so, rather than
// drawing a confident zero.
//
// "0 failed exchanges" on a session that has not made one is true and
// useless in the same breath: the reader needs to know the panel is empty,
// not that the network is healthy.
func TestTheSummarySaysWhenItHasNothing(t *testing.T) {
	rows := SummaryRows(ContextState{Width: 120}, RequestState{Width: 120}, 120)
	var text strings.Builder
	for _, r := range rows {
		text.WriteString(r.Text)
		text.WriteString("\n")
	}
	got := text.String()
	if strings.Contains(got, "0 failed") {
		t.Errorf("an empty session claims a failure count:\n%s", got)
	}
	if !strings.Contains(got, "—") && !strings.Contains(got, "no turns") && !strings.Contains(got, "none") {
		t.Errorf("an empty session does not say it is empty:\n%s", got)
	}
}

// TestTheSummaryRowsAreJumpable — every row that names another view can
// reach it, which is the view's own promise in its header text.
func TestTheSummaryRowsAreJumpable(t *testing.T) {
	ctx, req := summaryFixture()
	p := New(Seams{
		Context:  func() ContextState { return ctx },
		Requests: func() RequestState { return req },
		Width:    func() int { return 120 },
		Height:   func() int { return 40 },
	})
	if p.View() != ViewSummary {
		t.Fatalf("the panel opened on %v, want Summary", p.View())
	}
	p.Lines()

	want := map[string]View{
		"jump/context":  ViewContext,
		"jump/requests": ViewRequests,
		"jump/logs":     ViewLogs,
	}
	for id, target := range want {
		// Put the cursor on a row with this ID, if the fixture has one.
		rows := p.summary.flattened()
		found := false
		for i, row := range rows {
			if row.ID != id {
				continue
			}
			found = true
			p.summary.cursor = i
			if _, _ = p.Act(); p.View() != target {
				t.Errorf("enter on %q landed on %v, want %v", id, p.View(), target)
			}
			p.SetView(ViewSummary)
			break
		}
		if !found {
			t.Logf("the fixture has no %q row to jump from", id)
		}
	}
}

// TestTheSummaryKeyboardReachesEveryView — the legend says "1-6 switch view",
// and every number must land somewhere.
func TestTheSummaryKeyboardReachesEveryView(t *testing.T) {
	ctx, req := summaryFixture()
	for i := 0; i < ViewCount(); i++ {
		p := New(Seams{
			Context:  func() ContextState { return ctx },
			Requests: func() RequestState { return req },
			Width:    func() int { return 120 },
			Height:   func() int { return 40 },
		})
		p.SetView(ViewSummary)
		key := string(rune('1' + i))
		p.HandleKey(tea.Key{Code: rune(key[0]), Text: key})
		if p.View() != View(i) {
			t.Errorf("key %s landed on %v, want %v", key, p.View(), View(i))
		}
	}
}

// TestTheSummaryLegendNamesTheKeysThePanelHandles — a hint that names a key
// the panel ignores is worse than no hint.
func TestTheSummaryLegendNamesTheKeysThePanelHandles(t *testing.T) {
	legend := SummaryLegend()
	for _, key := range []string{"enter", "esc", "tab"} {
		if !strings.Contains(legend, key) {
			t.Errorf("the legend does not name %q: %s", key, legend)
		}
	}
}

// TestTheSummaryFitsTheHeight — the landing view is drawn in the same frame
// arithmetic as every other, so it cannot be the one that overflows.
func TestTheSummaryFitsTheHeight(t *testing.T) {
	ctx, req := summaryFixture()
	for _, height := range []int{12, 20, 24, 40, 60} {
		p := New(Seams{
			Context:  func() ContextState { return ctx },
			Requests: func() RequestState { return req },
			Width:    func() int { return 120 },
			Height:   func() int { return height },
		})
		if got := len(p.Lines()); got != height {
			t.Errorf("height %d: the summary drew %d rows", height, got)
		}
	}
}
