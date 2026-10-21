// slash_cycle225_test.go — cycle 225 regression tests for the /usage quota
// state machine. Cycle 193/201/207 had a static placeholder doc; cycle 225
// adds the REF 1.4.0 "Loading quota summary…" spinner state, a 1.5 s
// transition to quotaLoaded, and a live refresh-countdown string.
//
// All tests here exercise the height-aware path (m.height > 0) so the
// new usageDocLive / usageLoadingDoc / quotaStubRefills functions are
// reached. The height-0 (cycle-193 byte-exact) path is covered by
// TestPanelUsageLayout in slash_panels_test.go and stays unchanged.
package main

import (
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------- /usage

// The loading doc renders one dim-gray row with a Braille glyph and the
// literal text REF shows in
// /private/var/folders/.../agy225/ref/usage_seq_0.raw.txt row 15:
// `  ⣾  Loading quota summary...`. The cycle-225 row is identical
// (modulo the glyph frame number).
func TestCycle225UsageLoadingDoc(t *testing.T) {
	for _, n := range []int{0, 1, 2, 3, 4, 5, 6, 7} {
		m := mkPanel(panelUsage, "/usage", 0)
		m.width, m.height = 220, 50
		m.quotaFrame = n
		m.panel.quota.state = quotaLoading
		doc := usageDocLive(m)
		if len(doc) != 1 {
			t.Fatalf("frame=%d: doc has %d rows, want 1", n, len(doc))
		}
		// The first frame uses ⣾, the second ⣽, etc. — the 8-frame
		// sp.Dot set.
		wantFrame := []string{"⣾", "⣽", "⣻", "⢿", "⡿", "⣟", "⣯", "⣷"}[n]
		// Row shape: "  <glyph>  <dim-gray>Loading quota summary...<reset>".
		want := "  " + wantFrame + "  " + cDimGrayOpen + "Loading quota summary..." + cDimGrayClose
		if doc[0] != want {
			t.Errorf("frame=%d:\n got %q\nwant %q", n, doc[0], want)
		}
	}
}

// The loaded doc has the same 20-row layout as the cycle-207 helper
// usageDoc(width), but the two refresh rows are computed live from
// m.panel.quota.refreshG / refreshO.
//
// Layout (same as TestCycle207UsageDocGeometry uses):
//
//	0: GEMINI MODELS
//	1: Models within this group
//	2: blank
//	3: Weekly Limit Remaining
//	4: bar
//	5: refresh row (Gemini, green)
//	6-7: blank, blank
//	8: CLAUDE AND GPT MODELS
//	9: Models within this group
//	10: blank
//	11: Weekly Limit Remaining
//	12: bar
//	13: refresh row (Claude/GPT, red)
//	14-15: blank, blank
//	16-19: note (4 rows wrapped)
func TestCycle225UsageLoadedDoc(t *testing.T) {
	m := mkPanel(panelUsage, "/usage", 0)
	m.width, m.height = 220, 50
	// Pin the two refill times to deterministic values 168h / 48h out.
	m.panel.quota.refreshG = time.Now().Add(168 * time.Hour)
	m.panel.quota.refreshO = time.Now().Add(48 * time.Hour)
	doc := usageDocLive(m)
	if len(doc) != 20 {
		t.Fatalf("loaded doc = %d rows, want 20", len(doc))
	}
	// Row 5 is the Gemini refresh row. We don't pin the exact value
	// because it's live; just confirm the marker shape.
	if !strings.Contains(doc[5], "\x1b[32mRefreshes in ") {
		t.Errorf("gemini refresh row = %q", doc[5])
	}
	// Row 13 is the Claude/GPT refresh row (red).
	if !strings.Contains(doc[13], "\x1b[31mRefreshes in ") {
		t.Errorf("claude refresh row = %q", doc[13])
	}
	// The note rows are unchanged from cycle 207.
	if !strings.HasPrefix(doc[16], "  \x1b[38;2;238;232;213m│\x1b[38;2;101;123;131mWithin each group,") {
		t.Errorf("note row[0] = %q", doc[16])
	}
}

// The countdown string has three forms (agy 1.4.3):
//   - "Nh Nm" at or above 1 h, even past 24 h ("163h 40m")
//   - "Nm"    at or above 1 min ("40m")
//   - "Ns"    under 1 min
//
// Negative durations (stub buckets that have rolled over) clamp to 0.
// Each input adds one second so it lands cleanly inside its bucket.
func TestCycle225QuotaCountdown(t *testing.T) {
	for _, tc := range []struct {
		name string
		d    time.Duration
		want string
	}{
		{"1 week + 1s", 168*time.Hour + 1*time.Second, "168h 0m"},
		{"167h 59m + 1s", 167*time.Hour + 59*time.Minute + 1*time.Second, "167h 59m"},
		{"2 days 5 hours + 1s", 53*time.Hour + 1*time.Second, "53h 0m"},
		{"1 hour + 1s", 1*time.Hour + 1*time.Second, "1h 0m"},
		{"40 minutes + 1s", 40*time.Minute + 1*time.Second, "40m"},
		{"59 minutes + 999ms (one-second-truncation check)", 59*time.Minute + 999*time.Millisecond, "59m"},
		{"42 seconds + 1ms", 42*time.Second + 1*time.Millisecond, "42s"},
		{"1 second + 1ms", 1*time.Second + 1*time.Millisecond, "1s"},
		{"0 (clamp)", 0, "0s"},
		{"negative (clamp)", -10 * time.Minute, "0s"},
	} {
		got := quotaCountdown(time.Now().Add(tc.d))
		if got != tc.want {
			t.Errorf("%s: quotaCountdown(%v) = %q, want %q", tc.name, tc.d, got, tc.want)
		}
	}
}

// quotaStubRefills stamps refreshG / refreshO with 168h / 48h from now
// and flips state to quotaLoaded. After it runs, the loaded doc must
// render and the two refresh rows must be in the future.
func TestCycle225QuotaStubRefills(t *testing.T) {
	m := mkPanel(panelUsage, "/usage", 0)
	m.panel.quota.state = quotaLoading
	before := time.Now()
	quotaStubRefills(m)
	after := time.Now()
	if m.panel.quota.state != quotaLoaded {
		t.Errorf("state = %d, want quotaLoaded (%d)", m.panel.quota.state, quotaLoaded)
	}
	// refreshG should be ~168h from now (between before+168h and after+168h).
	if !m.panel.quota.refreshG.After(before.Add(168*time.Hour)) ||
		!m.panel.quota.refreshG.Before(after.Add(168*time.Hour)) {
		t.Errorf("refreshG = %v, want in [%v, %v]",
			m.panel.quota.refreshG, before.Add(168*time.Hour), after.Add(168*time.Hour))
	}
	if !m.panel.quota.refreshO.After(before.Add(48*time.Hour)) ||
		!m.panel.quota.refreshO.Before(after.Add(48*time.Hour)) {
		t.Errorf("refreshO = %v, want in [%v, %v]",
			m.panel.quota.refreshO, before.Add(48*time.Hour), after.Add(48*time.Hour))
	}
}

// The tick message handler is the heart of the cycle-225 cadence: it
// advances the spinner frame, transitions loading → loaded after 15
// ticks, and re-arms itself only while the panel is open.
func TestCycle225UsageQuotaTickHandler(t *testing.T) {
	// 1. The panel is closed — the tick is a no-op (no cmd returned).
	m := &runMode{}
	m.panel = slashPanel{kind: panelNone}
	if _, cmd := m.Update(usageQuotaTickMsg{}); cmd != nil {
		t.Errorf("closed panel returned a cmd: %v", cmd)
	}
	if m.quotaFrame != 0 {
		t.Errorf("quotaFrame = %d, want 0", m.quotaFrame)
	}

	// 2. Panel open, loading: 15 ticks flip to quotaLoaded and arm
	//    another tick.
	m = mkPanel(panelUsage, "/usage", 0)
	m.panel.quota.state = quotaLoading
	for i := 0; i < usageQuotaLoadingTicks; i++ {
		_, cmd := m.Update(usageQuotaTickMsg{})
		if cmd == nil {
			t.Fatalf("tick %d returned nil cmd", i)
		}
	}
	if m.panel.quota.state != quotaLoaded {
		t.Errorf("state = %d, want quotaLoaded", m.panel.quota.state)
	}
	if m.quotaFrame != usageQuotaLoadingTicks {
		t.Errorf("quotaFrame = %d, want %d", m.quotaFrame, usageQuotaLoadingTicks)
	}

	// 3. After the flip, additional ticks still increment the frame
	//    and re-arm the next tick (the spinner stays ready).
	_, cmd := m.Update(usageQuotaTickMsg{})
	if cmd == nil {
		t.Error("post-flip tick returned nil cmd")
	}
	if m.quotaFrame != usageQuotaLoadingTicks+1 {
		t.Errorf("quotaFrame = %d, want %d", m.quotaFrame, usageQuotaLoadingTicks+1)
	}
}

// The height-aware /usage panel renders the loading doc while state ==
// quotaLoading and the 20-line doc after the flip. m.height is set so
// the height-aware dispatch fires; this is the same path cycle 207
// tests against, except cycle 225 has the loading doc in front.
//
// Note: panelLines() at h=50 with a 20-row doc and a 37-row page
// renders the entire doc (no counter row because page >= total), so
// the loaded case produces the full 26-row shape (title + 3 chrome +
// 20 doc + blank + legend). The loading case produces a smaller shape:
// title + 3 chrome + 1 loading row + blank + legend = 7 rows.
func TestCycle225UsagePanelHeightAware(t *testing.T) {
	m := mkPanel(panelUsage, "/usage", 0)
	m.width, m.height = 220, 50
	m.panel.quota.state = quotaLoading
	m.quotaFrame = 0
	lines := m.panelLines()
	if len(lines) != 7 {
		t.Fatalf("loading-state panel = %d rows, want 7", len(lines))
	}
	if !strings.Contains(stripSGR(lines[4]), "Loading quota summary") {
		t.Errorf("row 4 should be the loading row: %q", stripSGR(lines[4]))
	}

	// Flip to loaded and re-render: at h=50 the doc fits in one page
	// (no counter row), so the shape is title + 3 chrome + 20 doc +
	// blank + legend = 26 rows.
	quotaStubRefills(m)
	m.panel.window = 0
	lines = m.panelLines()
	if len(lines) != 26 {
		t.Errorf("loaded-state panel = %d rows, want 26", len(lines))
	}
	if !strings.Contains(stripSGR(lines[8]), "100.00%") {
		t.Errorf("row 8 should be the Gemini 100%% bar: %q", stripSGR(lines[8]))
	}
}

// Cycle 225 RETRACTION GUARD: the cycle-193 / cycle-201 byte-exact
// tests in slash_panels_test.go must keep passing byte-for-byte. The
// height-0 path (unit tests) renders usageViewerLinesStatic, which
// has the same 20 rows and the same SGR sequences it always did.
// Cycle 225 adds the "(m *runMode) usageViewerLines()" method but
// returns the static doc when m.height == 0.
func TestCycle225UsageViewerStaticRegression(t *testing.T) {
	m := mkPanel(panelUsage, "/usage", 0)
	if m.height != 0 {
		t.Fatalf("mkPanel did not seed height=0 (got %d)", m.height)
	}
	doc := m.usageViewerLines()
	if len(doc) != 20 {
		t.Fatalf("doc = %d rows, want 20", len(doc))
	}
	// Same row-7 byte shape as the cycle-193 doc.
	if !strings.Contains(doc[7], "\x1b[1mWeekly Limit Remaining\x1b[0m") {
		t.Errorf("doc[7] = %q", doc[7])
	}
	// The cycle-193 placeholder "Quota available" is preserved (it is
	// cycle-193 wording, not the cycle-225 live wording).
	if !strings.Contains(doc[9], "Quota available") {
		t.Errorf("doc[9] should preserve the cycle-193 'Quota available' line: %q", doc[9])
	}
	// viewerSpecFor's height-0 branch also delivers the same 20 rows
	// (it builds the spec from usageViewerLinesStatic).
	spec, ok := m.viewerSpecFor()
	if !ok {
		t.Fatal("viewerSpecFor returned ok=false for panelUsage")
	}
	if spec.total != 20 {
		t.Errorf("spec.total = %d, want 20", spec.total)
	}
}

// The openSlashPanel path (cmd, height>0) initialises the loading
// state. Cycle 193's mkPanel does not run that path, so this test
// drives openSlashPanel directly.
func TestCycle225OpenUsagePanelArmsLoading(t *testing.T) {
	m := &runMode{}
	m.width, m.height = 220, 50
	if !m.openSlashPanel("usage") {
		t.Fatal("openSlashPanel(\"usage\") = false")
	}
	if m.panel.kind != panelUsage {
		t.Errorf("panel kind = %v, want panelUsage", m.panel.kind)
	}
	if m.panel.quota.state != quotaLoading {
		t.Errorf("quota.state = %d, want quotaLoading", m.panel.quota.state)
	}
}
