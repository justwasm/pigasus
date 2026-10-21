package announce_test

// catalog_test.go — cycle-announcement-cards: tests for the catalog
// reader, the visible-card filter, and the settings.json persistence
// pair.
//
// The tests run with $ANTIGRAVITY_HOME pointed at a tempdir so the
// persistent side never touches ~/.gemini/antigravity-cli/settings.json
// on the host — the same pattern cmd/wagyu/slash_actions_test.go uses.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/announce"
)

// TestCatalog_Order verifies that Catalog() returns cards in the
// "newest first, head-of-slice = highest priority" order the spec
// promises. The first card is a model launch (the highest-priority
// category), the third is a service notice. We pin IDs because the
// persistence layer relies on stable IDs.
func TestCatalog_Order(t *testing.T) {
	cards := announce.Catalog()
	if len(cards) < 3 {
		t.Fatalf("Catalog() returned %d cards, want >= 3", len(cards))
	}
	wantOrder := []string{
		"model-claude-opus-5-5",
		"deprecate-gemini-2-0-flash",
		"service-quota-rollover",
	}
	for i, want := range wantOrder {
		if i >= len(cards) {
			t.Fatalf("Catalog() too short at index %d", i)
		}
		if cards[i].ID != want {
			t.Errorf("Catalog()[%d].ID = %q, want %q", i, cards[i].ID, want)
		}
	}
	// PostedAt should be strictly non-increasing (newest first).
	for i := 1; i < len(cards); i++ {
		if cards[i].PostedAt.After(cards[i-1].PostedAt) {
			t.Errorf("Catalog() not in PostedAt-descending order at index %d: %v after %v",
				i, cards[i].PostedAt, cards[i-1].PostedAt)
		}
	}
}

// TestCatalog_FieldsPopulated checks every card carries a non-empty
// title and body — an empty Title would render as a blank card.
func TestCatalog_FieldsPopulated(t *testing.T) {
	for _, c := range announce.Catalog() {
		if c.ID == "" {
			t.Error("card with empty ID")
		}
		if strings.TrimSpace(c.Title) == "" {
			t.Errorf("card %q has empty Title", c.ID)
		}
		if strings.TrimSpace(c.Body) == "" {
			t.Errorf("card %q has empty Body", c.ID)
		}
		if c.Kind == "" {
			t.Errorf("card %q has empty Kind", c.ID)
		}
	}
}

// TestVisibleCards_NoDismissed checks the first-launch path: with an
// empty dismiss map, VisibleCards returns a copy of Catalog in the
// same order. VisibleCards must not return the catalog's underlying
// slice — a caller must be free to sort it.
func TestVisibleCards_NoDismissed(t *testing.T) {
	got := announce.VisibleCards(nil)
	want := announce.Catalog()
	if len(got) != len(want) {
		t.Fatalf("len(VisibleCards(nil)) = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].ID != want[i].ID {
			t.Errorf("VisibleCards(nil)[%d].ID = %q, want %q", i, got[i].ID, want[i].ID)
		}
	}
}

// TestVisibleCards_FiltersDismissed drops a card whose ID is in the
// dismissed set; the rest stay in catalog order.
func TestVisibleCards_FiltersDismissed(t *testing.T) {
	dismissed := map[string]bool{
		"model-claude-opus-5-5": true,
	}
	got := announce.VisibleCards(dismissed)
	for _, c := range got {
		if c.ID == "model-claude-opus-5-5" {
			t.Errorf("VisibleCards returned dismissed card %q", c.ID)
		}
	}
	if len(got) != len(announce.Catalog())-1 {
		t.Errorf("len(VisibleCards) = %d, want %d", len(got), len(announce.Catalog())-1)
	}
}

// TestVisibleCards_AllDismissed returns an empty slice — a fully
// dismissed catalog renders zero rows.
func TestVisibleCards_AllDismissed(t *testing.T) {
	dismissed := map[string]bool{}
	for _, c := range announce.Catalog() {
		dismissed[c.ID] = true
	}
	got := announce.VisibleCards(dismissed)
	if len(got) != 0 {
		t.Errorf("VisibleCards(all dismissed) = %d cards, want 0", len(got))
	}
}

// TestLoadDismissed_MissingFile is the first-launch path: no
// settings.json means no dismissed list. The function returns an
// empty (non-nil) map.
func TestLoadDismissed_MissingFile(t *testing.T) {
	t.Setenv("ANTIGRAVITY_HOME", t.TempDir())
	got, err := announce.LoadDismissed()
	if err != nil {
		t.Fatalf("LoadDismissed: %v", err)
	}
	if got == nil {
		t.Fatal("LoadDismissed returned nil map; want empty map")
	}
	if len(got) != 0 {
		t.Errorf("empty settings.json: len = %d, want 0", len(got))
	}
}

// TestLoadDismissed_NoKey covers the case where settings.json exists
// but does not carry a dismissedAnnouncements key (e.g. an existing
// install from before this cycle). The function returns an empty
// (non-nil) map.
func TestLoadDismissed_NoKey(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "settings.json"),
		[]byte(`{"colorScheme":"light"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := announce.LoadDismissed()
	if err != nil {
		t.Fatalf("LoadDismissed: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("no-key settings.json: len = %d, want 0", len(got))
	}
}

// TestSaveDismissed_PreservesOtherFields is the spec requirement:
// dismissing a card must NOT clobber existing settings.json keys
// (colorScheme, trustedWorkspaces, etc.). We pre-write a settings.json
// with two unrelated keys, dismiss a card, then read the file back
// and assert the keys still appear.
func TestSaveDismissed_PreservesOtherFields(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "settings.json"),
		[]byte(`{"colorScheme":"dark","trustedWorkspaces":["/keep/me"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := announce.SaveDismissed("model-claude-opus-5-5"); err != nil {
		t.Fatalf("SaveDismissed: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(home, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	for _, want := range []string{`"colorScheme"`, `"dark"`, `"trustedWorkspaces"`, `"/keep/me"`} {
		if !strings.Contains(body, want) {
			t.Errorf("settings.json missing %q after SaveDismissed:\n%s", want, body)
		}
	}
	if !strings.Contains(body, `"dismissedAnnouncements"`) {
		t.Errorf("settings.json missing dismissedAnnouncements key:\n%s", body)
	}
}

// TestSaveDismissed_Idempotent verifies the spec's "persist is a
// no-op if the card is already dismissed" rule: dismissing the same
// card twice produces the same list with a single entry, never a
// duplicate.
func TestSaveDismissed_Idempotent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)
	for i := 0; i < 3; i++ {
		if err := announce.SaveDismissed("service-quota-rollover"); err != nil {
			t.Fatalf("SaveDismissed #%d: %v", i, err)
		}
	}
	data, err := os.ReadFile(filepath.Join(home, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	raw, ok := m["dismissedAnnouncements"].([]any)
	if !ok {
		t.Fatalf("dismissedAnnouncements not a list: %v", m["dismissedAnnouncements"])
	}
	if len(raw) != 1 {
		t.Errorf("dismissedAnnouncements = %d entries, want 1", len(raw))
	}
	if raw[0] != "service-quota-rollover" {
		t.Errorf("dismissedAnnouncements[0] = %v, want service-quota-rollover", raw[0])
	}
}

// TestSaveDismissed_Alphabetised pins the "alphabetised on save"
// contract — it lets a regression that re-sorts the list on every
// dismiss be detected. We dismiss two IDs out of order and assert
// the file lists them in alphabetic order.
func TestSaveDismissed_Alphabetised(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)
	if err := announce.SaveDismissed("zzz-card"); err != nil {
		t.Fatal(err)
	}
	if err := announce.SaveDismissed("aaa-card"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(home, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	raw, ok := m["dismissedAnnouncements"].([]any)
	if !ok || len(raw) != 2 {
		t.Fatalf("dismissedAnnouncements = %v, want 2 sorted entries", raw)
	}
	got := []string{raw[0].(string), raw[1].(string)}
	sortedCopy := []string{got[0], got[1]}
	sort.Strings(sortedCopy)
	for i := range got {
		if got[i] != sortedCopy[i] {
			t.Errorf("dismissedAnnouncements = %v, want %v", got, sortedCopy)
			break
		}
	}
}

// TestSaveDismissed_FreshInstall covers the cycle-125 sibling path:
// settings.json does not yet exist on disk, SaveDismissed creates
// the parent dir and writes the file from scratch. We assert the
// file lands in the right place with the right shape.
func TestSaveDismissed_FreshInstall(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)
	if err := announce.SaveDismissed("model-claude-opus-5-5"); err != nil {
		t.Fatalf("SaveDismissed: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(home, "settings.json"))
	if err != nil {
		t.Fatalf("settings.json not created: %v", err)
	}
	if !strings.Contains(string(data), "model-claude-opus-5-5") {
		t.Errorf("settings.json missing dismissed id:\n%s", data)
	}
}

// TestSaveDismissed_EmptyIDRejects pins the boundary: the persistence
// helper rejects an empty ID rather than writing an empty string into
// the list. A zero-id entry would be silently honoured on read and
// would block every card from rendering.
func TestSaveDismissed_EmptyIDRejects(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)
	if err := announce.SaveDismissed(""); err == nil {
		t.Error("SaveDismissed(\"\") = nil, want non-nil")
	}
}

// TestLoadDismissed_RoundTrip is the integration test: SaveDismissed
// followed by LoadDismissed returns the saved ID. This is what the
// TUI's session-restart path relies on — the in-memory map must
// survive a fresh NewRunMode call because it was persisted.
func TestLoadDismissed_RoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)
	if err := announce.SaveDismissed("model-claude-opus-5-5"); err != nil {
		t.Fatal(err)
	}
	if err := announce.SaveDismissed("service-quota-rollover"); err != nil {
		t.Fatal(err)
	}
	got, err := announce.LoadDismissed()
	if err != nil {
		t.Fatal(err)
	}
	if !got["model-claude-opus-5-5"] {
		t.Error("LoadDismissed missing model-claude-opus-5-5")
	}
	if !got["service-quota-rollover"] {
		t.Error("LoadDismissed missing service-quota-rollover")
	}
	if got["deprecate-gemini-2-0-flash"] {
		t.Error("LoadDismissed unexpectedly contains deprecate-gemini-2-0-flash")
	}
}
