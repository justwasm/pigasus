// Package announce owns the in-TUI announcement cards AGY 1.2.17 ships
// (changelog line 96: "Added announcement cards above the prompt for
// model launches, deprecations, and other service notices.").
//
// The cards are one-at-a-time notifications the user sees between the
// top separator and the prompt input. Esc on an empty prompt dismisses
// the current card permanently (persisted to settings.json's
// `dismissedAnnouncements` subkey); sending any prompt hides the
// current card for the rest of the session without persisting.
//
// For this cycle the catalog is a fixed test set shipped from the
// binary — wiring the catalog to a remote feed is a future cycle and
// is not the responsibility of this package. Anything in this file
// stays stdlib-only so a future refactor can swap the source without
// breaking the persistence contract.
package announce

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/justwasm/pigasus/internal/auth"
)

// Kind classifies a card so future renderers can switch icon + colour
// per category. Cycle-announcement-cards does not style on Kind yet —
// every card renders the same "ⓘ" prefix and dim-gray body — but the
// field is on the type so a future cycle can branch on it without
// touching call sites.
type Kind string

const (
	KindModelLaunch   Kind = "model_launch"
	KindDeprecation   Kind = "deprecation"
	KindServiceNotice Kind = "service_notice"
)

// Card is one announcement card the TUI renders above the prompt.
//
// Fields are immutable: TUI code reads them, never writes them. The
// dismiss / hide state lives on the runMode (m.sessionHiddenCardID),
// not on the Card itself — that way the same Card can be re-shown in
// a fresh session without a per-Card dismiss log.
type Card struct {
	ID       string    `json:"id"`
	Kind     Kind      `json:"kind"`
	Title    string    `json:"title"`
	Body     string    `json:"body"`
	Hint     string    `json:"hint,omitempty"`
	PostedAt time.Time `json:"postedAt"`
}

// IsZero reports whether c carries no user-visible content. A zero
// card never reaches the renderer — VisibleCards drops them so the
// row above the prompt simply does not exist.
func (c Card) IsZero() bool {
	return strings.TrimSpace(c.ID) == "" && strings.TrimSpace(c.Title) == ""
}

// Catalog returns the fixed test catalog shipped from the binary.
//
// Order is *display order*: the first entry of the returned slice is
// the card the TUI shows first (highest priority, freshest first).
// The test for "newest first" ordering is in catalog_test.go and
// pins that semantic — a future cycle that re-orders by PostedAt
// must keep the head-of-slice = highest-priority contract.
//
// The IDs are stable across cycles so a card the user already
// dismissed keeps its dismissedAnnouncements entry valid even if the
// wording shifts.
func Catalog() []Card {
	return []Card{
		{
			ID:       "model-claude-opus-5-5",
			Kind:     KindModelLaunch,
			Title:    "Claude Opus 5.5 available",
			Body:     "Try `/model claude-opus-5-5` to use it.",
			Hint:     "Esc to dismiss · Send a message to hide",
			PostedAt: time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC),
		},
		{
			ID:       "deprecate-gemini-2-0-flash",
			Kind:     KindDeprecation,
			Title:    "Gemini 2.0 Flash retiring 2026-12-01",
			Body:     "Switch to Gemini 3.x with `/model` before that date.",
			Hint:     "Esc to dismiss · Send a message to hide",
			PostedAt: time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC),
		},
		{
			ID:       "service-quota-rollover",
			Kind:     KindServiceNotice,
			Title:    "Daily quota resets at 00:00 UTC",
			Body:     "Antigravity Starter Quota and Pro tiers reset together.",
			Hint:     "Esc to dismiss · Send a message to hide",
			PostedAt: time.Date(2026, 10, 10, 9, 0, 0, 0, time.UTC),
		},
	}
}

// VisibleCards returns the subset of the catalog that is *not* in
// dismissed. The returned slice is a fresh copy so callers can sort or
// paginate without mutating the catalog; the order matches Catalog()'s
// "newest first, head-of-slice = highest priority" contract.
//
// dismissed may be nil (first-launch path) — VisibleCards returns a
// copy of Catalog in that case.
//
// The return value omits zero-valued cards (no ID, no Title) so a
// future catalog source that fails to materialise an entry cannot
// poison the rendered row.
func VisibleCards(dismissed map[string]bool) []Card {
	out := make([]Card, 0, len(Catalog()))
	for _, c := range Catalog() {
		if c.IsZero() {
			continue
		}
		if dismissed[c.ID] {
			continue
		}
		out = append(out, c)
	}
	return out
}

// dismissedKey is the settings.json subkey under which the persistent
// dismiss list lives. Exported so tests can assert on it without
// re-typing the literal string.
const dismissedKey = "dismissedAnnouncements"

// LoadDismissed reads ~/.gemini/antigravity-cli/settings.json's
// dismissedAnnouncements list. A missing file, a missing key, or any
// decode error is *not* a hard failure: the TUI treats the first-launch
// state the same way it treats an empty list, so a corrupt settings
// file does not block startup. The returned map is non-nil even on the
// error path so callers can mark IDs against it without a nil check.
//
// This function reads the file directly (rather than going through
// auth.updateSettings) because the dismissed list is a slice of
// strings that the reference binary stores verbatim — the cycle
// shipped here adds the subkey to the existing settings.json schema
// without rewriting the whole file.
func LoadDismissed() (map[string]bool, error) {
	path := filepath.Join(auth.AntigravityConfigHome(), "settings.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]bool{}, nil
		}
		return map[string]bool{}, err
	}
	var m map[string]any
	if uerr := json.Unmarshal(data, &m); uerr != nil {
		return map[string]bool{}, nil
	}
	raw, ok := m[dismissedKey].([]any)
	if !ok {
		return map[string]bool{}, nil
	}
	out := make(map[string]bool, len(raw))
	for _, v := range raw {
		if s, ok := v.(string); ok && s != "" {
			out[s] = true
		}
	}
	return out, nil
}

// SaveDismissed appends id to settings.json's dismissedAnnouncements
// list, preserving every other field. The list is kept alphabetised
// and de-duplicated so re-dismissing the same card is idempotent —
// the settings.json file content stays byte-stable across repeated
// dismissals of the same card.
//
// Errors are surfaced so the TUI can warn the user, but they are not
// fatal: a transient disk-full condition still lets the in-memory
// dismiss take effect for the rest of the session (the TUI tracks
// the in-memory dismissed map independently).
func SaveDismissed(id string) error {
	if id == "" {
		return fmt.Errorf("announce: empty card id")
	}
	path := filepath.Join(auth.AntigravityConfigHome(), "settings.json")
	if err := os.MkdirAll(auth.AntigravityConfigHome(), 0700); err != nil {
		return err
	}
	m := map[string]any{}
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		_ = json.Unmarshal(data, &m) // corrupt → start from empty; Save still writes a valid file
	case os.IsNotExist(err):
		// fresh install — start with an empty map
	default:
		return err
	}
	list := []string{}
	if existing, ok := m[dismissedKey].([]any); ok {
		for _, v := range existing {
			if s, ok := v.(string); ok {
				list = append(list, s)
			}
		}
	}
	seen := make(map[string]bool, len(list))
	for _, s := range list {
		seen[s] = true
	}
	if !seen[id] {
		list = append(list, id)
		seen[id] = true
	}
	sort.Strings(list)
	m[dismissedKey] = list
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, out, 0600)
}
