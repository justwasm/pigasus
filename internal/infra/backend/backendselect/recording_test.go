package backendselect

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/justwasm/pigasus/internal/core/wirelog"
)

// TestEnableRecordingInstallsATransportThatRecords is the wiring test: the
// store EnableRecording hands back must be the one the clients built
// afterwards write into.
//
// The unit tests in wirelog cover the recorder itself. What they cannot
// cover is the seam this file exists for — that enabling recording actually
// reaches the http.Client a backend is constructed with. A recorder nobody
// installs is a feature nobody has.
func TestEnableRecordingInstallsATransportThatRecords(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	// EnableRecording is a sync.Once; give this test its own store so it
	// does not inherit whatever another test left in the process ring.
	prev := wirelog.Shared()
	wirelog.SetShared(wirelog.New(8))
	defer wirelog.SetShared(prev)

	st := EnableRecording()
	if st == nil {
		t.Fatal("EnableRecording returned no store")
	}
	if !RecordingEnabled() {
		t.Fatal("RecordingEnabled is false after EnableRecording")
	}

	// The client a backend would be built with.
	c := recordingClient(nil)
	if c == nil || c.Transport == nil {
		t.Fatal("recordingClient(nil) has no transport")
	}

	resp, err := c.Get(srv.URL + "/v1/probe")
	if err != nil {
		t.Fatalf("GET through the recording client: %v", err)
	}
	resp.Body.Close()

	recs := st.Records()
	if len(recs) != 1 {
		t.Fatalf("the store holds %d records, want 1 — the transport is not installed", len(recs))
	}
	if recs[0].Path != "/v1/probe" {
		t.Errorf("recorded path %q, want /v1/probe", recs[0].Path)
	}
	if recs[0].Status != 200 {
		t.Errorf("recorded status %d, want 200", recs[0].Status)
	}
}

// TestRecordingClientLeavesACallerSuppliedClientAlone pins the boundary: a
// test's stub client keeps working, and its transport is wrapped rather than
// replaced so its requests still go where it pointed them.
func TestRecordingClientLeavesACallerSuppliedClientAlone(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("stub"))
	}))
	defer srv.Close()

	prev := wirelog.Shared()
	wirelog.SetShared(wirelog.New(4))
	defer wirelog.SetShared(prev)
	st := EnableRecording()

	stub := srv.Client() // carries its own transport for the test server
	got := recordingClient(stub)
	if got == stub {
		t.Fatal("recordingClient returned the caller's client untouched; its traffic would not be recorded")
	}
	if got.Timeout != stub.Timeout {
		t.Error("recordingClient changed the caller's timeout")
	}

	resp, err := got.Get(srv.URL + "/x")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	resp.Body.Close()
	if st.Len() != 1 {
		t.Errorf("the store holds %d records, want 1", st.Len())
	}
}

// TestRecordingIsOffByDefault pins the cost: a build that never enables
// recording installs nothing.
func TestRecordingIsOffByDefault(t *testing.T) {
	if RecordingEnabled() {
		t.Skip("another test in this package already enabled recording; " +
			"the once-guard makes the default unobservable from here")
	}
	if c := recordingClient(nil); c != nil {
		t.Error("recordingClient(nil) built a client while recording is off")
	}
}

// TestRecordingIsConcurrencySafe is the race check: the TUI's recorder is
// written from whichever goroutine is doing the turn while the view reads
// it on the update goroutine.
func TestRecordingIsConcurrencySafe(t *testing.T) {
	prev := wirelog.Shared()
	wirelog.SetShared(wirelog.New(64))
	defer wirelog.SetShared(prev)
	st := EnableRecording()

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				st.Add(wirelog.Record{Method: "POST", Path: "/x", Status: 200})
			}
		}()
	}
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				_ = st.Records()
				_ = st.Len()
				_, _ = st.Get(int64(j))
			}
		}()
	}
	wg.Wait()

	if got := st.Len(); got != 64 {
		t.Errorf("the ring holds %d, want its capacity 64", got)
	}
}
