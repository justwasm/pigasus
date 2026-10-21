// Package remotecontrol — deep_link.go: pure parsing
// helpers for the `agy://...` deep-link surface the
// `remoteControlDeepLink` JSON-RPC method exposes.
//
// The dashboard's browser companion delivers agy://
// events as JSON-RPC params:
//
//	{"url": "agy://<verb>?<query>", "cascade_id": "..."}
//
// The dispatcher's handler hands the raw URL to
// ParseDeepLink and dispatches on the resolved
// DeepLinkAction. ParseDeepLink is a *pure* function
// — it does not look up cascades, does not allocate
// dispatch state, does not depend on time. The cycle
// brief pins this so the parse path is unit-testable
// without spinning a dispatcher.
//
// Cycle rc-deep-link adds this file. Stdlib-only.
package remotecontrol

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"
)

// DeepLinkAction is the resolved verb of an agy://
// deep-link. The closed set today is:
//
//	DeepLinkOpen         — record last_deep_link_open on the dispatcher
//	DeepLinkFocusCascade — set cascade.Focused = true on the named cascade
//	DeepLinkPasteAndSend — enqueue a SendRemoteAgentCascadeMessage on the named cascade
//
// Unknown verbs surface as -32602 (Invalid params) so
// dashboards that preview-future-verbs don't crash the
// daemon. A dashboard that wants to query "what verbs
// does the daemon support?" reads the schema docs;
// this surface is intentionally read-only.
//
// Cycle rc-deep-link adds this type.
type DeepLinkAction string

const (
	DeepLinkOpen         DeepLinkAction = "open"
	DeepLinkFocusCascade DeepLinkAction = "focus-cascade"
	DeepLinkPasteAndSend DeepLinkAction = "paste-and-send"
)

// DeepLink is the resolved shape ParseDeepLink returns.
// One struct, all four verbs: the verb-specific fields
// (CascadeID, Text) are zero-valued when the verb
// doesn't need them. Action is always populated for a
// successful parse; Host / Raw / Query are always
// populated (they describe the URL itself).
//
// JSON shape on the wire (echoed back to the dashboard
// by the handler):
//
//	{
//	  "host":  "open" | "focus-cascade" | "paste-and-send",
//	  "query": {"<key>": "<first-value>"},
//	  "raw":   "agy://...?..."
//	}
//
// Cycle rc-deep-link adds this type.
type DeepLink struct {
	// Action is the resolved verb. Always set on a
	// successful ParseDeepLink; nil-equivalent
	// (empty string) on error.
	Action DeepLinkAction
	// Host is the URL's host component — the verb
	// the URL addresses. Echoed back to the
	// dashboard.
	Host string
	// Raw is the original URL string the dashboard
	// supplied, trimmed of surrounding whitespace.
	// Echoed back so the dashboard's UI can render
	// the original (the host/path parsing above may
	// have normalised the scheme casing, etc.).
	Raw string
	// Query is the URL's query parameters, parsed by
	// net/url.Parse. Echoed back as a flat
	// map[string]string (first-value wins on
	// repeated keys) in the wire response.
	Query url.Values
	// CascadeID is the resolved `cascade_id` query
	// parameter. Populated for focus-cascade and
	// paste-and-send only. Empty when the verb
	// doesn't expect one.
	CascadeID string
	// Text is the resolved `text` query parameter.
	// Populated for paste-and-send only. Empty
	// otherwise. Already URL-decoded by net/url.
	Text string
}

// ErrDeepLinkBadScheme is returned when the URL's
// scheme is not "agy". The handler surfaces this as
// -32602 Invalid params.
var ErrDeepLinkBadScheme = errors.New("remotecontrol: deep-link scheme must be agy")

// ErrDeepLinkMissingVerb is returned when the URL
// has no host component (e.g. just "agy://" with no
// verb after the //). The handler surfaces this as
// -32602.
var ErrDeepLinkMissingVerb = errors.New("remotecontrol: deep-link is missing the verb (host)")

// ErrDeepLinkUnknownVerb is returned when the URL's
// host is not one of the recognised verbs
// (open / focus-cascade / paste-and-send). The
// handler surfaces this as -32602; the dashboard
// receives the bad verb in the error's Data field.
var ErrDeepLinkUnknownVerb = errors.New("remotecontrol: deep-link verb is not supported")

// ErrDeepLinkMalformed is the catch-all
// net/url.Parse failure (an unparseable URL). The
// handler surfaces this as -32602.
var ErrDeepLinkMalformed = errors.New("remotecontrol: deep-link URL is malformed")

// ParseDeepLink parses raw as an agy:// deep-link
// URL and returns the resolved DeepLink. raw is
// trimmed of surrounding whitespace before parsing;
// an empty string returns ErrDeepLinkMissingVerb
// without invoking net/url.
//
// Error sentinels (matched via errors.Is by the
// handler):
//
//   - ErrDeepLinkBadScheme      — scheme != "agy"
//   - ErrDeepLinkMissingVerb    — empty or "agy://" only
//   - ErrDeepLinkUnknownVerb    — host not in the closed set
//   - ErrDeepLinkMalformed      — net/url.Parse failed
//
// The handler also wraps the err.Error() in the
// JSON-RPC envelope's Data field so a dashboard
// receives the verbatim reason.
//
// Concurrency: ParseDeepLink is pure (no shared
// state, no goroutines, no I/O). Safe to call from
// any goroutine.
//
// Cycle rc-deep-link adds this function.
func ParseDeepLink(raw string) (DeepLink, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return DeepLink{}, fmt.Errorf("%w: empty url", ErrDeepLinkMissingVerb)
	}
	u, err := url.Parse(trimmed)
	if err != nil {
		// net/url.Parse is permissive but does fail on
		// things like "%ZZ" or "http://[::1" — the
		// canonical "not-a-url" input actually parses
		// successfully (it becomes Path="not-a-url",
		// Scheme=""), so the bad-URL test exercises the
		// scheme branch below, not this one.
		return DeepLink{}, fmt.Errorf("%w: %v", ErrDeepLinkMalformed, err)
	}
	if u.Scheme != "agy" {
		return DeepLink{}, fmt.Errorf("%w: got %q", ErrDeepLinkBadScheme, u.Scheme)
	}
	host := u.Host
	if host == "" {
		// "agy://" with nothing after the //, or
		// "agy:" without //. The brief's
		// empty-verb case.
		return DeepLink{}, ErrDeepLinkMissingVerb
	}
	q := u.Query()

	dl := DeepLink{
		Host:  host,
		Raw:   trimmed,
		Query: q,
	}
	switch host {
	case "open":
		dl.Action = DeepLinkOpen
	case "focus-cascade":
		dl.Action = DeepLinkFocusCascade
		dl.CascadeID = strings.TrimSpace(q.Get("cascade_id"))
	case "paste-and-send":
		dl.Action = DeepLinkPasteAndSend
		dl.CascadeID = strings.TrimSpace(q.Get("cascade_id"))
		// url.Query already URL-decodes, so a
		// text=hello%20world arrives here as
		// "hello world" with the space.
		dl.Text = q.Get("text")
	default:
		return DeepLink{}, fmt.Errorf("%w: %q", ErrDeepLinkUnknownVerb, host)
	}
	return dl, nil
}

// Echo renders the URL-derived echo map the handler
// returns alongside the action field. The Query is
// flattened to map[string]string (first value wins
// on repeated keys) so the dashboard's JSON shape
// stays a flat object — url.Values is map[string][]string
// and JSON-marshals as objects whose values are
// arrays, which the brief's wire sketch does not
// match.
//
// Cycle rc-deep-link adds this helper.
func (dl DeepLink) Echo() map[string]any {
	q := make(map[string]string, len(dl.Query))
	for k, vs := range dl.Query {
		if len(vs) > 0 {
			q[k] = vs[0]
		}
	}
	return map[string]any{
		"host":  dl.Host,
		"query": q,
		"raw":   dl.Raw,
	}
}

// DeepLinkState is the dispatcher-side holder for
// last_deep_link_open timestamps. The dispatcher's
// remoteControlDeepLink handler stamps it on every
// "open" verb; tests / future status-tool cycles
// read it back via Snapshot.
//
// Config.LastDeepLink carries a *DeepLinkState so
// external wiring (or a future cycle's tests) can
// inject a state holder with its own clock
// (production callers pass nil → NewDispatcher
// fabricates a fresh one).
//
// Concurrency: a sync.RWMutex guards the timestamp.
// Reads are typically infrequent (status-tool poll,
// debugging) and writes are bounded by the
// dashboard's open-event frequency, so RWMutex is
// the right primitive.
//
// Cycle rc-deep-link adds this type.
type DeepLinkState struct {
	// mu guards LastOpen. RWMutex so a status-tool
	// poll doesn't serialise behind a rare open
	// event.
	mu sync.RWMutex
	// LastOpen is the wall-clock stamp of the most
	// recent remoteControlDeepLink("open") call the
	// dispatcher served. Zero when no open has been
	// received yet.
	LastOpen time.Time
}

// Snapshot returns the most-recently-recorded
// last-deep-link-open timestamp. Nil-safe (a nil
// *DeepLinkState returns the zero time.Time).
//
// Cycle rc-deep-link adds this method.
func (s *DeepLinkState) Snapshot() time.Time {
	if s == nil {
		return time.Time{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.LastOpen
}

// RecordOpen stamps the LastOpen field with at.
// Nil-safe (a nil *DeepLinkState is a no-op). The
// dispatcher's remoteControlDeepLink handler calls
// this on every "open" verb. Future cycles that
// track other verbs add their own helpers here.
//
// Cycle rc-deep-link adds this method.
func (s *DeepLinkState) RecordOpen(at time.Time) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.LastOpen = at
	s.mu.Unlock()
}
