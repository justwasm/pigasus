package announce

// notice_queue_bytes_test.go — the byte parity pin for the eligibility
// notice row and the announcement queue's visible rows.
//
// Cycle 381, tranche 53: the notice row moved here from
// internal/features/auth/eligibility and the queue moved here from
// internal/cliapp. A move must not change a rendered byte, so the
// golden below was captured from the pre-move tree (the host's own
// announcementRow / eligibilityNoticeRow paths) and is compared
// byte for byte.
//
// The colour scheme is pinned, because the bytes carry it.

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/palette"
	"github.com/justwasm/pigasus/render"
)

// goldenPath is the captured pre-move byte dump.
func goldenPath() string {
	return filepath.Join("testdata", "notice_queue.golden")
}

// TestNoticeAndQueueBytes compares the rendered bytes against the
// golden captured before the move.
func TestNoticeAndQueueBytes(t *testing.T) {
	restore := palette.PushScheme("solarized light")
	defer restore()

	want, err := os.ReadFile(goldenPath())
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}

	var b strings.Builder
	add := func(label, s string) {
		b.WriteString("### " + label + "\n")
		b.WriteString(s)
		b.WriteString("<<END>>\n")
	}

	// 1. the eligibility notice row
	add("notice/empty", RenderNoticeRow("", "", render.HyperlinkCaps{OSC8: true}))
	add("notice/reason-only", RenderNoticeRow("your account is not eligible for this request", "", render.HyperlinkCaps{OSC8: true}))
	add("notice/reason-url-osc8", RenderNoticeRow("phone verification required", "https://example.com/verify", render.HyperlinkCaps{OSC8: true}))
	add("notice/reason-url-plain", RenderNoticeRow("phone verification required", "https://example.com/verify", render.HyperlinkCaps{}))

	// 2. the announcement queue's visible rows
	var q Queue
	add("queue/zero-cards", q.Row())

	q.Cards = VisibleCards(nil)[:1]
	add("queue/one-card", q.Row())

	q.Cards = VisibleCards(nil)
	add("queue/all-cards", q.Row())

	// 3. the dismissed-set behaviour
	dismissed := map[string]bool{}
	for _, c := range Catalog() {
		dismissed[c.ID] = true
	}
	q.Cards = VisibleCards(dismissed)
	add("queue/all-dismissed", q.Row())

	q.Cards = VisibleCards(map[string]bool{"model-claude-opus-5-5": true})
	for _, c := range q.Cards {
		card := c
		add("queue/visible/"+c.ID, RenderCard(&card))
	}
	add("queue/visible-ids", func() string {
		var ids []string
		for _, c := range q.Cards {
			ids = append(ids, c.ID)
		}
		return strings.Join(ids, ",")
	}())

	// 4. the session-hide path
	q.Cards = VisibleCards(nil)
	q.HideForSession()
	add("queue/hidden-for-session/row", q.Row())
	add("queue/hidden-for-session/id", q.SessionHiddenID)

	// 5. the persistence failure path
	q.SaveError = errors.New("disk full")
	add("queue/save-error", q.SaveErrorMessage())

	if got := b.String(); got != string(want) {
		t.Errorf("rendered bytes changed.\ngot:\n%q\nwant:\n%q", got, string(want))
	}
}

// TestRenderNoticeRow_EmptyReason pins the boundary the host relies
// on: a notice without a reason renders nothing at all, so the row
// slot between the separator and the composer stays empty.
func TestRenderNoticeRow_EmptyReason(t *testing.T) {
	if got := RenderNoticeRow("", "", render.HyperlinkCaps{}); got != "" {
		t.Errorf("empty reason = %q, want \"\"", got)
	}
	if got := RenderNoticeRow("", "https://example.com/verify", render.HyperlinkCaps{OSC8: true}); got != "" {
		t.Errorf("empty reason with a URL = %q, want \"\"", got)
	}
}
