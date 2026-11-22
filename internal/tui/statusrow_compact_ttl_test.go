package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/ui/views/statusrow"
)

func TestStatusRow_CompactLifecycleAndTTL(t *testing.T) {
	be := &slowFakeClient{delay: 50 * time.Millisecond}
	m := newRootModel(be, "u@t", "m", "t", "/", true)

	// 1. Initially idle: status row shows "? for shortcuts"
	st := m.statusRowState()
	rendered := statusrow.Render(st)
	if !strings.Contains(rendered, "? for shortcuts") {
		t.Fatalf("expected '? for shortcuts' initially, got %q", rendered)
	}

	// 2. Compaction starts: status row displays "Compacting context…"
	start := time.Now()
	m.Update(compactStartMsg{
		StartedAt: start,
		Reason:    "threshold",
	})
	st = m.statusRowState()
	if st.Compact == nil {
		t.Fatalf("expected st.Compact != nil while compacting")
	}
	rendered = statusrow.Render(st)
	if !strings.Contains(rendered, "Compacting context…") {
		t.Fatalf("expected 'Compacting context…' in status row while running, got %q", rendered)
	}

	// 3. Compaction finishes: status row displays "Compacted 50.3 K → 25.1 K (35s)"
	end := start.Add(35 * time.Second)
	_, expireCmd := m.Update(compactEndMsg{
		Reason:               "threshold",
		EndedAt:              end,
		Summary:              "Summary text",
		TokensBefore:         50300,
		EstimatedTokensAfter: 25100,
		Steps:                5,
	})
	if expireCmd == nil {
		t.Fatalf("expected non-nil expireCmd returned from compactEndMsg")
	}

	st = m.statusRowState()
	if st.Compact == nil {
		t.Fatalf("expected st.Compact != nil immediately after compaction finished")
	}
	rendered = statusrow.Render(st)
	if !strings.Contains(rendered, "Compacted 50.3 K → 25.1 K (35s)") {
		t.Fatalf("expected 'Compacted 50.3 K → 25.1 K (35s)' in status row, got %q", rendered)
	}

	// 4. User types in buffer: finished compaction notice yields so composer is quiet
	m.prompt.input.SetValue("hello")
	st = m.statusRowState()
	rendered = statusrow.Render(st)
	if strings.Contains(rendered, "Compacted") {
		t.Fatalf("expected finished compaction notice to hide when user types in buffer, got %q", rendered)
	}
	m.prompt.input.SetValue("")

	// 5. Expired by TTL: simulate time elapsed past compactNoticeTTL (6s)
	m.session.store.CompactEnd = time.Now().Add(-7 * time.Second)
	st = m.statusRowState()
	if st.Compact != nil {
		t.Fatalf("expected st.Compact to be nil after TTL passed, got %+v", st.Compact)
	}
	rendered = statusrow.Render(st)
	if strings.Contains(rendered, "Compacted") {
		t.Fatalf("expected 'Compacted' to be gone from status row after TTL, got %q", rendered)
	}
	if !strings.Contains(rendered, "? for shortcuts") {
		t.Fatalf("expected status row to revert to '? for shortcuts', got %q", rendered)
	}

	// 6. compactExpiredMsg cleans up store telemetry cleanly
	m.session.store.CompactStart = time.Now().Add(-10 * time.Second)
	m.session.store.CompactEnd = time.Now().Add(-4 * time.Second)
	m.Update(compactExpiredMsg{})

	if !m.session.store.CompactStart.IsZero() || !m.session.store.CompactEnd.IsZero() {
		t.Fatalf("expected store CompactStart/End to be zeroed by compactExpiredMsg")
	}
}

func TestStatusRow_RunningCompactionDoesNotExpire(t *testing.T) {
	be := &slowFakeClient{delay: 50 * time.Millisecond}
	m := newRootModel(be, "u@t", "m", "t", "/", true)

	// Running compaction started 60s ago
	m.session.store.CompactStart = time.Now().Add(-60 * time.Second)
	m.session.store.CompactEnd = time.Time{}

	st := m.statusRowState()
	if st.Compact == nil {
		t.Fatalf("expected running compaction to NOT expire while running")
	}
	rendered := statusrow.Render(st)
	if !strings.Contains(rendered, "Compacting context…") {
		t.Fatalf("expected 'Compacting context…' in status row for long-running compaction, got %q", rendered)
	}
}
