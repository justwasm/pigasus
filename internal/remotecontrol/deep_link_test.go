// In-process coverage for the rc-deep-link cycle's
// new surface. The pure-parse side lives in
// ParseDeepLink (deep_link.go); the dispatcher's
// handler is exercised via Dispatch — wire-level
// coverage lives in remotetransport/tcp_test.go.
//
// Split rationale: deep_link_test.go is pure parse +
// dispatch-only tests. Wire tests would just be
// re-skinning the same assertions over a JSON
// round-trip; the parse path's coverage is
// hermetic and stays in this file.
package remotecontrol

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

// TestParseDeepLink_Open pins the headline
// happy-path parse for the "open" verb. The
// resolved action is DeepLinkOpen, the host is
// preserved verbatim, and the query map carries
// `project=foo` as a single string (the brief's
// flat-map wire shape, not url.Values' slice
// shape).
func TestParseDeepLink_Open(t *testing.T) {
	dl, err := ParseDeepLink("agy://open?project=foo")
	if err != nil {
		t.Fatalf("ParseDeepLink: %v", err)
	}
	if dl.Action != DeepLinkOpen {
		t.Errorf("Action = %q, want %q", dl.Action, DeepLinkOpen)
	}
	if dl.Host != "open" {
		t.Errorf("Host = %q, want %q", dl.Host, "open")
	}
	if dl.Raw != "agy://open?project=foo" {
		t.Errorf("Raw = %q, want verbatim", dl.Raw)
	}
	if got := dl.Query.Get("project"); got != "foo" {
		t.Errorf("Query.project = %q, want foo", got)
	}
	// CascadeID / Text are zero for open.
	if dl.CascadeID != "" {
		t.Errorf("CascadeID = %q, want empty", dl.CascadeID)
	}
	if dl.Text != "" {
		t.Errorf("Text = %q, want empty", dl.Text)
	}
}

// TestParseDeepLink_FocusCascade pins the focus
// verb's URL-derived cascade_id resolution. The
// handler resolves cascade_id either from the
// top-level params field (preferred) or from the
// URL's `cascade_id=...` query param; ParseDeepLink
// only handles the URL-embedded path.
func TestParseDeepLink_FocusCascade(t *testing.T) {
	dl, err := ParseDeepLink("agy://focus-cascade?cascade_id=cas_abc")
	if err != nil {
		t.Fatalf("ParseDeepLink: %v", err)
	}
	if dl.Action != DeepLinkFocusCascade {
		t.Errorf("Action = %q, want %q", dl.Action, DeepLinkFocusCascade)
	}
	if dl.Host != "focus-cascade" {
		t.Errorf("Host = %q, want focus-cascade", dl.Host)
	}
	if dl.CascadeID != "cas_abc" {
		t.Errorf("CascadeID = %q, want cas_abc", dl.CascadeID)
	}
	if dl.Text != "" {
		t.Errorf("Text = %q, want empty", dl.Text)
	}
}

// TestParseDeepLink_PasteAndSend pins the
// paste-and-send verb's two-param URL resolution:
// cascade_id is the routing target, text is the
// URL-encoded message body. `hello%20world`
// arrives as `hello world` (net/url auto-decodes).
func TestParseDeepLink_PasteAndSend(t *testing.T) {
	dl, err := ParseDeepLink("agy://paste-and-send?cascade_id=cas_abc&text=hello%20world")
	if err != nil {
		t.Fatalf("ParseDeepLink: %v", err)
	}
	if dl.Action != DeepLinkPasteAndSend {
		t.Errorf("Action = %q, want %q", dl.Action, DeepLinkPasteAndSend)
	}
	if dl.CascadeID != "cas_abc" {
		t.Errorf("CascadeID = %q, want cas_abc", dl.CascadeID)
	}
	if dl.Text != "hello world" {
		t.Errorf("Text = %q, want %q (url-decoded)", dl.Text, "hello world")
	}
}

// TestParseDeepLink_BadURL covers the
// non-agy scheme / "not-a-url" path. The
// handler surfaces this as -32602 wrapping
// ErrDeepLinkBadScheme.
func TestParseDeepLink_BadURL(t *testing.T) {
	dl, err := ParseDeepLink("not-a-url")
	if err == nil {
		t.Fatalf("ParseDeepLink(\"not-a-url\"): err = nil, dl = %#v", dl)
	}
	if !errors.Is(err, ErrDeepLinkBadScheme) {
		t.Errorf("err = %v, want ErrDeepLinkBadScheme", err)
	}
	if dl.Action != "" {
		t.Errorf("Action on error = %q, want empty", dl.Action)
	}
}

// TestParseDeepLink_UnknownVerb covers the
// closed-set guard: a verb the dispatcher doesn't
// recognise (e.g. `nuke`) returns
// ErrDeepLinkUnknownVerb. The handler surfaces
// this as -32602; dashboards never crash on a
// future-verb preview.
func TestParseDeepLink_UnknownVerb(t *testing.T) {
	dl, err := ParseDeepLink("agy://nuke?cascade_id=cas_abc")
	if err == nil {
		t.Fatalf("ParseDeepLink: err = nil, dl = %#v", dl)
	}
	if !errors.Is(err, ErrDeepLinkUnknownVerb) {
		t.Errorf("err = %v, want ErrDeepLinkUnknownVerb", err)
	}
	// The err message should mention the verb
	// ("nuke") so a dashboard's renderer can
	// route the message into the "unsupported
	// URL" surface verbatim.
	if got := err.Error(); !contains(got, "nuke") {
		t.Errorf("err.Error() = %q, want substring 'nuke'", got)
	}
	if dl.Action != "" {
		t.Errorf("Action on error = %q, want empty", dl.Action)
	}
}

// TestParseDeepLink_Empty covers the
// empty-string input path. The handler surfaces
// this as -32602 wrapping ErrDeepLinkMissingVerb.
func TestParseDeepLink_Empty(t *testing.T) {
	dl, err := ParseDeepLink("")
	if err == nil {
		t.Fatalf("ParseDeepLink(\"\"): err = nil, dl = %#v", dl)
	}
	if !errors.Is(err, ErrDeepLinkMissingVerb) {
		t.Errorf("err = %v, want ErrDeepLinkMissingVerb", err)
	}
	if dl.Action != "" {
		t.Errorf("Action on error = %q, want empty", dl.Action)
	}
}

// TestParseDeepLink_Whitespace trims
// surrounding whitespace before parsing so a
// dashboard that ships "\nagy://open " arrives
// cleanly. The parsed Action / Host / Raw reflect
// the trimmed input.
func TestParseDeepLink_Whitespace(t *testing.T) {
	dl, err := ParseDeepLink("   agy://open?x=1   ")
	if err != nil {
		t.Fatalf("ParseDeepLink: %v", err)
	}
	if dl.Action != DeepLinkOpen {
		t.Errorf("Action = %q, want %q", dl.Action, DeepLinkOpen)
	}
	if dl.Raw != "agy://open?x=1" {
		t.Errorf("Raw = %q, want trimmed", dl.Raw)
	}
}

// TestDeepLink_Echo pins the wire-shape helper:
// the handler's response includes an `echo` map
// whose `host` is the verb, whose `query` is a
// flat map[string]string (first value wins on
// repeated keys), and whose `raw` is the trimmed
// URL.
func TestDeepLink_Echo(t *testing.T) {
	dl, err := ParseDeepLink("agy://open?project=foo&conv=bar&project=baz")
	if err != nil {
		t.Fatalf("ParseDeepLink: %v", err)
	}
	echo := dl.Echo()
	if echo["host"] != "open" {
		t.Errorf("echo.host = %v, want open", echo["host"])
	}
	if echo["raw"] != "agy://open?project=foo&conv=bar&project=baz" {
		t.Errorf("echo.raw = %v, want verbatim", echo["raw"])
	}
	q, ok := echo["query"].(map[string]string)
	if !ok {
		t.Fatalf("echo.query type = %T, want map[string]string", echo["query"])
	}
	if q["project"] != "foo" {
		t.Errorf("echo.query.project = %q, want foo (first value)", q["project"])
	}
	if q["conv"] != "bar" {
		t.Errorf("echo.query.conv = %q, want bar", q["conv"])
	}
}

// TestDeepLinkState_NilSafe pins the
// nil-receiver safety of (*DeepLinkState)'s
// accessors. A dispatcher built without a
// LastDeepLink would have nil fields here; the
// methods must not panic.
func TestDeepLinkState_NilSafe(t *testing.T) {
	var s *DeepLinkState
	if got := s.Snapshot(); !got.IsZero() {
		t.Errorf("nil Snapshot = %v, want zero", got)
	}
	// RecordOpen on nil must not panic.
	s.RecordOpen(time.Now().UTC())
}

// TestDeepLinkState_RecordOpen pins the
// record-then-snapshot round-trip. The snapshot
// returns the timestamp the caller passed in
// (not time.Now()), so a test can pin the value.
func TestDeepLinkState_RecordOpen(t *testing.T) {
	s := &DeepLinkState{}
	at := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	s.RecordOpen(at)
	if got := s.Snapshot(); !got.Equal(at) {
		t.Errorf("Snapshot = %v, want %v", got, at)
	}
	// A second record overwrites the first.
	at2 := time.Date(2026, 10, 19, 12, 30, 0, 0, time.UTC)
	s.RecordOpen(at2)
	if got := s.Snapshot(); !got.Equal(at2) {
		t.Errorf("Snapshot after second RecordOpen = %v, want %v", got, at2)
	}
}

// TestCascade_Focus pins the new
// Focused-flag accessors. Focus() flips the
// atomic to true; IsFocused() reflects it; both
// are nil-safe.
func TestCascade_Focus(t *testing.T) {
	c, err := NewCascade(CascadeOpts{})
	if err != nil {
		t.Fatalf("NewCascade: %v", err)
	}
	if c.IsFocused() {
		t.Errorf("fresh cascade IsFocused = true, want false")
	}
	c.Focus()
	if !c.IsFocused() {
		t.Errorf("after Focus: IsFocused = false, want true")
	}
	// Re-calling Focus is idempotent (Focus
	// always stores true).
	c.Focus()
	if !c.IsFocused() {
		t.Errorf("after second Focus: IsFocused = false, want true")
	}

	// Nil-safe.
	var nilC *Cascade
	if nilC.IsFocused() {
		t.Errorf("nil cascade IsFocused = true, want false")
	}
	nilC.Focus() // must not panic
}

// ----- dispatcher-level (in-process) coverage
// for handleRemoteControlDeepLink. Wire-level
// coverage lives in remotetransport/
// tcp_test.go.

// TestDispatcher_RemoteControlDeepLink_Open pins
// the headline ask: a deep-link open stamps
// (*Dispatcher).Config.LastDeepLink and returns
// the open envelope with `action:"open"`,
// `echo`, and `received_at`. In-process: no ws,
// no notifications — the handler runs as a pure
// dispatch call.
func TestDispatcher_RemoteControlDeepLink_Open(t *testing.T) {
	sess := NewSessionState("")
	d, err := NewDispatcher(Config{Session: sess})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}

	req := []byte(`{"jsonrpc":"2.0","id":1,"method":"remoteControlDeepLink","params":{"url":"agy://open?project=foo"}}`)
	resp := d.Dispatch(context.Background(), req)
	if errObj, ok := resp["error"]; ok {
		t.Fatalf("Dispatch returned error: %#v", errObj)
	}
	res, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("Dispatch: no result, got %#v", resp)
	}
	if got, _ := res["ok"].(bool); !got {
		t.Errorf("result.ok = %v, want true", res["ok"])
	}
	if got, _ := res["action"].(string); got != "open" {
		t.Errorf("result.action = %q, want open", got)
	}
	if _, ok := res["received_at"].(string); !ok {
		t.Errorf("result.received_at missing or non-string: %#v", res["received_at"])
	}
	echo, ok := res["echo"].(map[string]any)
	if !ok {
		t.Fatalf("result.echo type = %T, want map[string]any", res["echo"])
	}
	if echo["host"] != "open" {
		t.Errorf("echo.host = %v, want open", echo["host"])
	}
	if echo["raw"] != "agy://open?project=foo" {
		t.Errorf("echo.raw = %v, want verbatim", echo["raw"])
	}
	q, ok := echo["query"].(map[string]string)
	if !ok {
		t.Fatalf("echo.query type = %T, want map[string]string", echo["query"])
	}
	if q["project"] != "foo" {
		t.Errorf("echo.query.project = %q, want foo", q["project"])
	}

	// The dispatcher's LastDeepLink state was
	// stamped. NewDispatcher fabricates a fresh
	// *DeepLinkState when the host didn't install
	// one — read it back via Snapshot().
	got := d.cfg.LastDeepLink.Snapshot()
	if got.IsZero() {
		t.Errorf("LastDeepLink.Snapshot() = zero; want non-zero after open verb")
	}
	if time.Since(got) > 5*time.Second {
		t.Errorf("LastDeepLink.Snapshot() = %v; not recent (delta = %v)", got, time.Since(got))
	}
}

// TestDispatcher_RemoteControlDeepLink_FocusCascade_NotFound
// pins the brief's -32004 envelope for
// focus-cascade with an unknown cascade_id.
func TestDispatcher_RemoteControlDeepLink_FocusCascade_NotFound(t *testing.T) {
	sess := NewSessionState("")
	d, err := NewDispatcher(Config{Session: sess})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}

	req := []byte(`{"jsonrpc":"2.0","id":1,"method":"remoteControlDeepLink","params":{"url":"agy://focus-cascade?cascade_id=cas_does_not_exist"}}`)
	resp := d.Dispatch(context.Background(), req)
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("no error envelope, got %#v", resp)
	}
	if got := errorCode(errObj); got != ErrCodeCascadeNotFound {
		t.Errorf("error.code = %d, want %d", got, ErrCodeCascadeNotFound)
	}
}

// TestDispatcher_RemoteControlDeepLink_FocusCascade_Ok
// pins the happy-path focus-cascade path:
// registering a cascade, dispatching the
// deep-link, asserting cascade.IsFocused() flips
// to true. The in-process dispatcher has no
// Notifier wired (no transport) so the
// cascade/state notification does not go out —
// the wire-level test (tcp_test.go) covers the
// notification shape.
func TestDispatcher_RemoteControlDeepLink_FocusCascade_Ok(t *testing.T) {
	sess := NewSessionState("")
	d, err := NewDispatcher(Config{Session: sess})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	c, err := NewCascade(CascadeOpts{})
	if err != nil {
		t.Fatalf("NewCascade: %v", err)
	}
	if err := d.cascades.Register(c); err != nil {
		t.Fatalf("Register: %v", err)
	}

	req := []byte(fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"remoteControlDeepLink","params":{"url":"agy://focus-cascade?cascade_id=%s"}}`, c.ID))
	resp := d.Dispatch(context.Background(), req)
	if errObj, ok := resp["error"]; ok {
		t.Fatalf("Dispatch returned error: %#v", errObj)
	}
	res, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("Dispatch: no result, got %#v", resp)
	}
	if got, _ := res["action"].(string); got != "focus-cascade" {
		t.Errorf("result.action = %q, want focus-cascade", got)
	}
	if got, _ := res["cascade_id"].(string); got != c.ID {
		t.Errorf("result.cascade_id = %q, want %q", got, c.ID)
	}
	if !c.IsFocused() {
		t.Errorf("after focus-cascade: cascade.IsFocused = false, want true")
	}
}

// TestDispatcher_RemoteControlDeepLink_FocusCascade_TopLevel
// pins the brief's "top-level cascade_id wins
// over URL-embedded cascade_id" rule. A
// dashboard that sends both should see the
// top-level field used.
func TestDispatcher_RemoteControlDeepLink_FocusCascade_TopLevel(t *testing.T) {
	sess := NewSessionState("")
	d, err := NewDispatcher(Config{Session: sess})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	// Two cascades; the URL says cas_a, the
	// top-level says cas_b — the top-level wins.
	a, _ := NewCascade(CascadeOpts{})
	b, _ := NewCascade(CascadeOpts{})
	if err := d.cascades.Register(a); err != nil {
		t.Fatalf("Register a: %v", err)
	}
	if err := d.cascades.Register(b); err != nil {
		t.Fatalf("Register b: %v", err)
	}

	req := []byte(fmt.Sprintf(
		`{"jsonrpc":"2.0","id":1,"method":"remoteControlDeepLink","params":{"url":"agy://focus-cascade?cascade_id=%s","cascade_id":"%s"}}`,
		a.ID, b.ID,
	))
	resp := d.Dispatch(context.Background(), req)
	if errObj, ok := resp["error"]; ok {
		t.Fatalf("Dispatch returned error: %#v", errObj)
	}
	if a.IsFocused() {
		t.Errorf("cascade a IsFocused = true; want false (top-level wins)")
	}
	if !b.IsFocused() {
		t.Errorf("cascade b IsFocused = false; want true (top-level won)")
	}
}

// TestDispatcher_RemoteControlDeepLink_InvalidURL
// pins the -32602 envelope for both
// malformed-URL and unknown-verb inputs. The
// bad-URL test (TestParseDeepLink_BadURL) is the
// parse-side coverage; this is the dispatcher-
// side envelope check.
func TestDispatcher_RemoteControlDeepLink_InvalidURL(t *testing.T) {
	sess := NewSessionState("")
	d, err := NewDispatcher(Config{Session: sess})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}

	// unknown verb
	req := []byte(`{"jsonrpc":"2.0","id":1,"method":"remoteControlDeepLink","params":{"url":"agy://nuke?cascade_id=cas_abc"}}`)
	resp := d.Dispatch(context.Background(), req)
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("unknown-verb: no error envelope, got %#v", resp)
	}
	if got := errorCode(errObj); got != -32602 {
		t.Errorf("unknown-verb error.code = %d, want -32602", got)
	}

	// bad scheme
	req2 := []byte(`{"jsonrpc":"2.0","id":2,"method":"remoteControlDeepLink","params":{"url":"not-a-url"}}`)
	resp2 := d.Dispatch(context.Background(), req2)
	errObj2, ok := resp2["error"].(map[string]any)
	if !ok {
		t.Fatalf("bad-scheme: no error envelope, got %#v", resp2)
	}
	if got := errorCode(errObj2); got != -32602 {
		t.Errorf("bad-scheme error.code = %d, want -32602", got)
	}

	// empty url
	req3 := []byte(`{"jsonrpc":"2.0","id":3,"method":"remoteControlDeepLink","params":{"url":""}}`)
	resp3 := d.Dispatch(context.Background(), req3)
	errObj3, ok := resp3["error"].(map[string]any)
	if !ok {
		t.Fatalf("empty url: no error envelope, got %#v", resp3)
	}
	if got := errorCode(errObj3); got != -32602 {
		t.Errorf("empty-url error.code = %d, want -32602", got)
	}
}

// TestDispatcher_RemoteControlDeepLink_PasteAndSend_MissingFields
// pins the brief's -32602 envelopes for
// paste-and-send with missing required fields.
// cascade_id missing → "cascade_id is required";
// text missing → "text is required".
func TestDispatcher_RemoteControlDeepLink_PasteAndSend_MissingFields(t *testing.T) {
	sess := NewSessionState("")
	d, err := NewDispatcher(Config{Session: sess})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}

	// cascade_id missing — URL has no
	// cascade_id, top-level absent.
	req := []byte(`{"jsonrpc":"2.0","id":1,"method":"remoteControlDeepLink","params":{"url":"agy://paste-and-send?text=hello"}}`)
	resp := d.Dispatch(context.Background(), req)
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("no error envelope for missing cascade_id, got %#v", resp)
	}
	if got := errorCode(errObj); got != -32602 {
		t.Errorf("missing cascade_id error.code = %d, want -32602", got)
	}

	// text missing.
	req2 := []byte(`{"jsonrpc":"2.0","id":2,"method":"remoteControlDeepLink","params":{"url":"agy://paste-and-send?cascade_id=cas_abc"}}`)
	resp2 := d.Dispatch(context.Background(), req2)
	errObj2, ok := resp2["error"].(map[string]any)
	if !ok {
		t.Fatalf("no error envelope for missing text, got %#v", resp2)
	}
	if got := errorCode(errObj2); got != -32602 {
		t.Errorf("missing text error.code = %d, want -32602", got)
	}
}
