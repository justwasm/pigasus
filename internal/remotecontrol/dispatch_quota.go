// dispatch_quota.go — the quota summary the Web UI's usage panel asks for.
//
// # The RPC
//
// The model picker's "View Usage" entry opens a panel that asks the
// language server for the account's quota:
//
//	/exa.language_server_pb.LanguageServerService/RetrieveUserQuotaSummary
//
// The Web UI renders the answer with its own component — one row per
// bucket (the label, the reset countdown, the remaining percentage)
// and a ring gauge per bucket. Without a route the transport answers
// `{}`, the panel finds no groups and renders "No quota information
// available.", so the Web UI cannot tell which account it is on: the
// one thing the account switcher has to prove.
//
// # The shape
//
// The answer is proto-JSON of
// exa.language_server_pb.RetrieveUserQuotaSummaryResponse, whose
// `response` field is
// google.internal.cloud.code.v1internal.RetrieveUserQuotaSummaryResponse:
//
//	{"response": {"description": "...",
//	  "groups": [{"displayName": "GEMINI MODELS", "description": "...",
//	    "buckets": [{"bucketId": "weekly", "displayName": "...",
//	                 "description": "...", "window": "weekly",
//	                 "remainingFraction": 0.45,
//	                 "resetTime": "2026-10-29T12:00:00Z",
//	                 "disabled": false}]}]}}
//
// Two details the Web UI depends on:
//
//   - `remaining_fraction` is a member of the `remaining` oneof, so
//     proto-JSON writes it at the bucket's top level under its JSON
//     name (`remainingFraction`) — never nested under "remaining".
//   - the "Resets in 3d 4h" line is only computed when the bucket
//     carries a non-empty `description`, so the description is never
//     empty.
//
// 【一手】 the descriptor closure the Web UI parses
// (lsproto/descriptor/language_server.protoset,
// google/internal/cloud/code/v1internal/quota_summary.proto): the
// oneof `remaining` holds `remaining_fraction` (float, 4) and
// `remaining_amount` (int64, 5), and the bucket's `description` is
// field 7.
package remotecontrol

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/justwasm/pigasus/internal/accounts"
	"github.com/justwasm/pigasus/internal/auth"
)

// quotaFreshness is how long a cached summary is served before the
// route asks the server again. The Web UI's own cache is five
// seconds; the clone's is longer, because a quota summary changes
// slowly and every read costs a round trip to the server.
const quotaFreshness = 2 * time.Minute

// quotaFetchTimeout bounds the live read. The Web UI shows its own
// "Refreshing…" row while the answer is on its way, so a slow answer
// is expected — but not an unbounded one.
const quotaFetchTimeout = 10 * time.Second

// quotaSnapshotSource returns the active account's email and its
// cached quota summary (nil when there is none). It is a variable so
// a test can install a snapshot without a registry on disk.
var quotaSnapshotSource = activeQuotaSnapshot

// quotaSummaryFetcher reads the quota of the active credential from
// the server. It is a variable so a test can answer without a
// network.
var quotaSummaryFetcher = fetchActiveQuotaSummary

// routeQuotaMethods serves the quota RPCs the Web UI's usage panel
// needs. Returns ok=true when the method was handled.
func (d *Dispatcher) routeQuotaMethods(ctx context.Context, method string, params json.RawMessage) (any, *rpcErrorBody, bool) {
	switch method {
	case "RetrieveUserQuotaSummary":
		return d.handleLSRetrieveUserQuotaSummary(ctx, params)
	}
	return nil, nil, false
}

// handleLSRetrieveUserQuotaSummary answers the Web UI's quota call.
//
// The cached summary of the *active* account is what the answer
// carries: the quota belongs to the credential, so switching accounts
// switches the numbers the panel shows. A request that asks for a
// refresh (the Web UI's panel always does) reads the server; a failed
// read falls back to the cached summary rather than to an error, so
// the panel still shows the last known numbers.
func (d *Dispatcher) handleLSRetrieveUserQuotaSummary(ctx context.Context, params json.RawMessage) (any, *rpcErrorBody, bool) {
	var req struct {
		ForceRefresh bool `json:"forceRefresh"`
	}
	if len(params) > 0 {
		// A malformed request is answered from the cache: the quota
		// panel is a read, not a command.
		_ = json.Unmarshal(params, &req)
	}
	email, snap := quotaSnapshotSource()
	if snap == nil || req.ForceRefresh || quotaExpired(snap) {
		if live := quotaSummaryFetcher(ctx); live != nil {
			snap = live.Snapshot()
			if email != "" {
				_ = accounts.SetQuota(email, snap)
			}
		}
	}
	return map[string]any{"response": quotaResponseBody(snap)}, nil, true
}

// quotaExpired reports whether a cached summary is older than the
// freshness window.
func quotaExpired(snap *auth.QuotaSnapshot) bool {
	if snap == nil || snap.FetchedAt.IsZero() {
		return true
	}
	return time.Since(snap.FetchedAt) > quotaFreshness
}

// activeQuotaSnapshot returns the active account's email and its
// cached quota summary.
//
// The active account is the one behind the shared credential — the
// same credential the session's turns use — so the numbers the panel
// shows are the numbers the account actually has.
func activeQuotaSnapshot() (string, *auth.QuotaSnapshot) {
	_, email := accounts.Current()
	email = strings.TrimSpace(email)
	if email == "" {
		return "", nil
	}
	acc, ok, err := accounts.Get(email)
	if err != nil || !ok {
		return email, nil
	}
	return email, acc.Quota
}

// fetchActiveQuotaSummary reads the quota of the active credential
// from the server. It returns nil when the read fails — an offline
// machine still gets the cached numbers.
func fetchActiveQuotaSummary(ctx context.Context) *auth.QuotaSummary {
	ctx, cancel := context.WithTimeout(ctx, quotaFetchTimeout)
	defer cancel()
	summary, err := auth.FetchQuotaSummary(ctx)
	if err != nil || summary == nil {
		return nil
	}
	return summary
}

// quotaResponseBody renders a summary in the shape the Web UI's usage
// panel decodes.
//
// The groups are the model families the account has ("Gemini Models",
// "Claude and GPT models"); each group holds the buckets (the weekly
// limit and the five-hour limit). A bucket without a display name is
// dropped: the Web UI skips it anyway, and an unnamed row would show
// an empty label.
func quotaResponseBody(snap *auth.QuotaSnapshot) map[string]any {
	body := map[string]any{
		"description": "",
		"groups":      []any{},
	}
	if snap == nil {
		return body
	}
	groups := make([]any, 0, len(snap.Groups))
	for _, g := range snap.Groups {
		buckets := make([]any, 0, len(g.Buckets))
		for _, b := range g.Buckets {
			name := strings.TrimSpace(b.DisplayName)
			if name == "" {
				continue
			}
			bucket := map[string]any{
				"bucketId":          quotaBucketID(b),
				"displayName":       name,
				"description":       quotaBucketDescription(b, g.Description),
				"window":            b.Window,
				"remainingFraction": b.RemainingFraction,
				"disabled":          false,
			}
			if !b.ResetTime.IsZero() {
				bucket["resetTime"] = b.ResetTime.UTC().Format(time.RFC3339)
			}
			buckets = append(buckets, bucket)
		}
		if len(buckets) == 0 {
			continue
		}
		groups = append(groups, map[string]any{
			"displayName": g.Name,
			"description": g.Description,
			"buckets":     buckets,
		})
	}
	body["groups"] = groups
	body["description"] = snap.Description
	return body
}

// quotaBucketID names a bucket. The Web UI uses it as the row's key
// and as the tooltip's anchor, so it has to be unique inside a group:
// the window ("weekly", "5h") is.
func quotaBucketID(b auth.QuotaBucketSnapshot) string {
	if window := auth.QuotaWindow(b.DisplayName, b.Window); window != "" {
		return window
	}
	if b.Window != "" {
		return b.Window
	}
	return b.DisplayName
}

// quotaBucketDescription writes the bucket's tooltip.
//
// The Web UI only computes the "Resets in 3d 4h" countdown when the
// description is non-empty, so it is never empty: the group's own
// description when there is one, a sentence about the window
// otherwise.
func quotaBucketDescription(b auth.QuotaBucketSnapshot, groupDescription string) string {
	if d := strings.TrimSpace(groupDescription); d != "" {
		return d
	}
	switch auth.QuotaWindow(b.DisplayName, b.Window) {
	case auth.QuotaWindowWeekly:
		return "Remaining quota for the weekly limit."
	case auth.QuotaWindowFiveHour:
		return "Remaining quota for the five-hour limit."
	}
	return "Remaining quota."
}
