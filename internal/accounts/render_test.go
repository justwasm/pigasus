package accounts

import (
	"strings"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/auth"
)

func TestRenderQuotaCell(t *testing.T) {
	// Account with no cached quota shows dashes for both columns.
	empty := Account{Email: "a@example.com"}
	if got := QuotaCell(empty); got != "wk      -  5h      -" {
		t.Errorf("QuotaCell(empty) = %q, want %q", got, "wk      -  5h      -")
	}

	// Account with cached weekly and five-hour remaining.
	acc := quotaAccount(0.5, 0.9)
	if got := QuotaCell(acc); got != "wk  50.0%  5h  90.0%" {
		t.Errorf("QuotaCell(acc) = %q, want %q", got, "wk  50.0%  5h  90.0%")
	}
}

func TestQuotaSnapshotAge(t *testing.T) {
	if got := QuotaSnapshotAge(nil); got != "-" {
		t.Errorf("QuotaSnapshotAge(nil) = %q, want -", got)
	}

	zero := &auth.QuotaSnapshot{}
	if got := QuotaSnapshotAge(zero); got != "-" {
		t.Errorf("QuotaSnapshotAge(zero) = %q, want -", got)
	}

	nowSnap := &auth.QuotaSnapshot{FetchedAt: time.Now().Add(-30 * time.Second)}
	if got := QuotaSnapshotAge(nowSnap); got != "now" {
		t.Errorf("QuotaSnapshotAge(30s ago) = %q, want now", got)
	}

	minSnap := &auth.QuotaSnapshot{FetchedAt: time.Now().Add(-15 * time.Minute)}
	if got := QuotaSnapshotAge(minSnap); got != "15m" {
		t.Errorf("QuotaSnapshotAge(15m ago) = %q, want 15m", got)
	}

	hourSnap := &auth.QuotaSnapshot{FetchedAt: time.Now().Add(-4 * time.Hour)}
	if got := QuotaSnapshotAge(hourSnap); got != "4h" {
		t.Errorf("QuotaSnapshotAge(4h ago) = %q, want 4h", got)
	}

	daySnap := &auth.QuotaSnapshot{FetchedAt: time.Now().Add(-72 * time.Hour)}
	if got := QuotaSnapshotAge(daySnap); got != "3d" {
		t.Errorf("QuotaSnapshotAge(72h ago) = %q, want 3d", got)
	}
}

func TestRowLineCursorAndActive(t *testing.T) {
	acc := Account{
		Email:    "user@example.com",
		PlanTier: "Google AI Pro",
	}

	// Inactive row with no cursor.
	plain := RowLine(acc, false, "other@example.com", 30, "<blue>", "</blue>")
	if !strings.HasPrefix(plain, "  user@example.com") {
		t.Errorf("plain row missing prefix: %q", plain)
	}
	if strings.Contains(plain, "(active)") {
		t.Errorf("inactive row marked active: %q", plain)
	}
	if !strings.Contains(plain, "Google AI Pro") {
		t.Errorf("plain row missing tier: %q", plain)
	}

	// Active row under cursor.
	selected := RowLine(acc, true, "USER@example.com", 30, "<blue>", "</blue>")
	if !strings.HasPrefix(selected, "<blue>> user@example.com</blue>") {
		t.Errorf("cursor row missing blue prefix: %q", selected)
	}
	if !strings.HasSuffix(selected, "(active)") {
		t.Errorf("active row missing active marker: %q", selected)
	}
}

func TestRowLineDefaultsAndPadding(t *testing.T) {
	// Empty email falls back to (unknown).
	unknown := Account{}
	line := RowLine(unknown, false, "", 30, "", "")
	if !strings.HasPrefix(line, "  (unknown)") {
		t.Errorf("empty email row missing (unknown): %q", line)
	}
	if !strings.Contains(line, "-") {
		t.Errorf("empty tier missing fallback dash: %q", line)
	}

	// Email longer than padding gets at least 1 space.
	longAcc := Account{Email: "very-long-account-email@example.com"}
	tight := RowLine(longAcc, false, "", 5, "", "")
	wantPrefix := "  very-long-account-email@example.com -"
	if !strings.HasPrefix(tight, wantPrefix) {
		t.Errorf("tight row = %q, want prefix %q", tight, wantPrefix)
	}
}
