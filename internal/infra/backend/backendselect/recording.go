// recording.go — installing the request recorder into the transports.
//
// # Why the transport and not the clients
//
// Every client this package builds ends up in one of two places: CloudCode's
// geminicode client, or a gateway client. Both take an *http.Client whose
// Transport is unset, so both send through http.DefaultTransport. Wrapping
// the transport is therefore one edit that covers the official backend, the
// configured backend and every failover target between them — and a backend
// added later is recorded without being told this package exists.
//
// # Why it is opt-in at the call site
//
// The recorder keeps request and response bodies in memory. A headless
// `--print` run, a test, or an agent server that answers a thousand turns
// should not pay for that, so nothing here runs unless the entry point asks
// for it (see EnableRecording). The default is off, and off costs a nil
// check.
package backendselect

import (
	"net/http"
	"sync"

	"github.com/justwasm/pigasus/internal/core/wirelog"
)

var (
	recordingOnce sync.Once
	// recording is the transport every client built by this package
	// installs, or nil when recording was never enabled.
	recording http.RoundTripper
)

// EnableRecording turns on request recording for every backend client built
// from now on, and returns the store they record into.
//
// It is safe to call more than once; later calls return the same store. The
// entry points that want the /logs Requests view call it once at startup:
// the interactive TUI does, and the headless paths do not.
func EnableRecording() *wirelog.Store {
	recordingOnce.Do(func() {
		st := wirelog.Shared()
		recording = &wirelog.Transport{Store: st}
	})
	return wirelog.Shared()
}

// RecordingEnabled reports whether EnableRecording has run.
func RecordingEnabled() bool { return recording != nil }

// recordingClient returns c with the recording transport installed, or c
// unchanged when recording is off or the caller supplied its own client.
//
// A caller-supplied *http.Client is left alone: it is a test's stub server
// or an embedder's tuned client, and replacing its transport would change
// where its requests go, not just what is recorded.
func recordingClient(c *http.Client) *http.Client {
	if recording == nil {
		return c
	}
	if c == nil {
		return &http.Client{Transport: recording}
	}
	if c.Transport != nil {
		// The caller chose a transport; wrapping it is still useful and
		// still does not change where the request goes, so wrap rather
		// than replace.
		cp := *c
		cp.Transport = &wirelog.Transport{Base: c.Transport, Store: wirelog.Shared()}
		return &cp
	}
	cp := *c
	cp.Transport = recording
	return &cp
}
