// quota_snapshot.go — the serializable form of a quota summary.
//
// # Why it exists
//
// A QuotaSummary carries its groups and buckets under `json:"-"`: the
// live response is decoded once and rendered, never stored. The
// multi-account registry is different: it *caches* the quota of every
// account it knows about, so the /accounts panel can show each
// account's usage without a second query — and so the /usage panel can
// reuse a snapshot the /accounts panel already fetched.
//
// A snapshot is that cache entry: the same numbers, in a shape that
// survives a round trip through the registry file.
//
// # The numbers
//
// The response has one group per model family ("GEMINI MODELS",
// "CLAUDE AND GPT MODELS") and one bucket per window inside a group
// (the weekly limit and the five-hour limit). The panel tracks the
// Gemini models quota; Claude and GPT groups are ignored.
package auth

import (
	"strings"
	"time"
)

// QuotaSnapshot is the cached form of one account's quota summary.
type QuotaSnapshot struct {
	// FetchedAt is when the summary was read from the server.
	FetchedAt time.Time `json:"fetched_at"`
	// Description is the response's own note (the reference's group
	// note). It is optional: the panel does not render it.
	Description string `json:"description,omitempty"`
	// Groups are the model groups the account has, with their buckets.
	Groups []QuotaGroupSnapshot `json:"groups,omitempty"`
}

// QuotaGroupSnapshot is one model group's cached quota.
type QuotaGroupSnapshot struct {
	Name        string                `json:"name"`
	Description string                `json:"description,omitempty"`
	Buckets     []QuotaBucketSnapshot `json:"buckets,omitempty"`
}

// QuotaBucketSnapshot is one quota window's cached state.
//
// ID is the bucket's own id on the wire ("3p-5h", "gemini-weekly"). The
// status line keys its quota map by it, so the cached form has to carry
// it: a snapshot that dropped it could not be rendered back into the
// document the live summary produces.
type QuotaBucketSnapshot struct {
	ID                string    `json:"id,omitempty"`
	DisplayName       string    `json:"display_name,omitempty"`
	Window            string    `json:"window,omitempty"`
	RemainingFraction float64   `json:"remaining_fraction"`
	ResetTime         time.Time `json:"reset_time,omitempty"`
}

// Snapshot converts a live summary into its cached form.
func (s *QuotaSummary) Snapshot() *QuotaSnapshot {
	if s == nil {
		return nil
	}
	out := &QuotaSnapshot{
		FetchedAt:   time.Now(),
		Description: s.Description,
	}
	for _, g := range s.Groups {
		group := QuotaGroupSnapshot{Name: g.Name, Description: g.Description}
		for _, b := range g.Buckets {
			group.Buckets = append(group.Buckets, QuotaBucketSnapshot{
				ID:                b.ID,
				DisplayName:       b.DisplayName,
				Window:            b.Window,
				RemainingFraction: b.RemainingFraction,
				ResetTime:         b.ResetTime,
			})
		}
		out.Groups = append(out.Groups, group)
	}
	return out
}

// Summary converts a cached snapshot back into the live form, so the
// /usage panel can render a snapshot with the same code that renders a
// fresh response.
func (s *QuotaSnapshot) Summary() *QuotaSummary {
	if s == nil {
		return nil
	}
	out := &QuotaSummary{Description: s.Description}
	for _, g := range s.Groups {
		group := QuotaGroup{Name: g.Name, Description: g.Description}
		for _, b := range g.Buckets {
			group.Buckets = append(group.Buckets, QuotaBucket{
				ID:                b.ID,
				DisplayName:       b.DisplayName,
				Window:            b.Window,
				RemainingFraction: b.RemainingFraction,
				ResetTime:         b.ResetTime,
			})
		}
		out.Groups = append(out.Groups, group)
	}
	return out
}

// The two windows the quota is measured in.
const (
	QuotaWindowWeekly   = "weekly"
	QuotaWindowFiveHour = "5h"
)

// QuotaWindow classifies a bucket into one of the two windows.
//
// The bucket's display name is the reference's own wording ("Weekly
// Limit Remaining", "Five Hour Limit Remaining"), so it is the
// primary signal; the wire's window field is the fallback.
func QuotaWindow(displayName, window string) string {
	name := strings.ToLower(displayName)
	switch {
	case strings.Contains(name, "weekly"):
		return QuotaWindowWeekly
	case strings.Contains(name, "five hour"), strings.Contains(name, "5 hour"),
		strings.Contains(name, "5h"):
		return QuotaWindowFiveHour
	}
	w := strings.ToLower(window)
	switch {
	case strings.Contains(w, "week"):
		return QuotaWindowWeekly
	case strings.Contains(w, "five"), strings.Contains(w, "5h"), strings.Contains(w, "5_hour"):
		return QuotaWindowFiveHour
	}
	return ""
}

// WeeklyRemaining returns the weekly remaining fraction for Gemini
// models. It reports false when the snapshot holds no weekly bucket — a
// quota response from a plan that has none.
func (s *QuotaSnapshot) WeeklyRemaining() (float64, bool) {
	return s.remainingIn(QuotaWindowWeekly)
}

// FiveHourRemaining returns the five-hour remaining fraction for Gemini
// models. It reports false when the snapshot holds no five-hour bucket.
func (s *QuotaSnapshot) FiveHourRemaining() (float64, bool) {
	return s.remainingIn(QuotaWindowFiveHour)
}

// remainingIn returns the lowest remaining fraction of the buckets in
// the given window for Gemini models. Non-Gemini groups (such as Claude/GPT)
// are ignored. If no explicit Gemini group is matched, it falls back to
// searching groups that are not Claude/GPT (e.g. for snapshots without
// explicit group names).
func (s *QuotaSnapshot) remainingIn(window string) (float64, bool) {
	if s == nil {
		return 0, false
	}
	found := false
	lowest := 0.0

	// Check Gemini groups first.
	for _, g := range s.Groups {
		if strings.Contains(strings.ToLower(g.Name), "gemini") {
			for _, b := range g.Buckets {
				if QuotaWindow(b.DisplayName, b.Window) != window {
					continue
				}
				if !found || b.RemainingFraction < lowest {
					lowest = b.RemainingFraction
					found = true
				}
			}
		}
	}
	if found {
		return lowest, true
	}

	// Fallback for snapshots where group names do not explicitly mention "gemini",
	// skipping groups explicitly named claude or gpt.
	for _, g := range s.Groups {
		name := strings.ToLower(g.Name)
		if strings.Contains(name, "claude") || strings.Contains(name, "gpt") {
			continue
		}
		for _, b := range g.Buckets {
			if QuotaWindow(b.DisplayName, b.Window) != window {
				continue
			}
			if !found || b.RemainingFraction < lowest {
				lowest = b.RemainingFraction
				found = true
			}
		}
	}
	return lowest, found
}
