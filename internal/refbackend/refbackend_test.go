package refbackend

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeReference is a stand-in for the reference language_server: it
// answers unary calls with a fixed JSON body and one streaming call
// with a Connect envelope stream.
func fakeReference(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/JetboxSubscribeToState") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"userStatus":{"pro":true}}`)
			return
		}
		w.Header().Set("Content-Type", "application/connect+json")
		w.WriteHeader(http.StatusOK)
		for _, payload := range []string{`{"appState":{"n":1}}`, `{"appState":{"n":2}}`} {
			frame := connectEnvelope([]byte(payload))
			_, _ = w.Write(frame)
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
	}))
}

// TestDispatchDirectUnary checks that a unary call reaches the
// reference and its answer comes back verbatim.
func TestDispatchDirectUnary(t *testing.T) {
	srv := fakeReference(t)
	defer srv.Close()

	disp, err := New(srv.URL)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	res, err := disp.DispatchDirect(context.Background(), "GetUserStatus", "{}")
	if err != nil {
		t.Fatalf("DispatchDirect: %v", err)
	}
	if !strings.Contains(res, `"pro":true`) {
		t.Fatalf("unexpected answer: %s", res)
	}
}

// TestDispatchDirectStreamingIsAnError checks that a server-streaming
// method is refused on the unary path: the transport asks for those
// through DispatchStream.
func TestDispatchDirectStreamingIsAnError(t *testing.T) {
	disp, err := New("http://127.0.0.1:1")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := disp.DispatchDirect(context.Background(), "JetboxSubscribeToState", "{}"); err == nil {
		t.Fatal("expected an error for a server-streaming method")
	}
}

// TestDispatchStreamRelaysFrames checks that the reference's envelope
// stream reaches the transport's emit callback, one frame per message.
func TestDispatchStreamRelaysFrames(t *testing.T) {
	srv := fakeReference(t)
	defer srv.Close()

	disp, err := New(srv.URL)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	var frames []string
	handled, err := disp.DispatchStream(context.Background(), "JetboxSubscribeToState", "{}",
		func(raw json.RawMessage) error {
			frames = append(frames, string(raw))
			return nil
		})
	if !handled {
		t.Fatal("expected the dispatcher to handle the stream")
	}
	if err != nil {
		t.Fatalf("DispatchStream: %v", err)
	}
	if len(frames) != 2 {
		t.Fatalf("expected 2 frames, got %d: %v", len(frames), frames)
	}
	if !strings.Contains(frames[1], `"n":2`) {
		t.Fatalf("unexpected second frame: %s", frames[1])
	}
}

// TestDispatchStreamIgnoresUnaryMethods checks that a method the
// reference does not stream is left to the transport's fallback.
func TestDispatchStreamIgnoresUnaryMethods(t *testing.T) {
	disp, err := New("http://127.0.0.1:1")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	handled, err := disp.DispatchStream(context.Background(), "GetUserStatus", "{}", nil)
	if handled || err != nil {
		t.Fatalf("expected handled=false, got %v %v", handled, err)
	}
}

// TestNormalizeBase checks the endpoint spellings the flag accepts.
func TestNormalizeBase(t *testing.T) {
	cases := map[string]string{
		"127.0.0.1:8791":         "http://127.0.0.1:8791",
		"http://127.0.0.1:8791":  "http://127.0.0.1:8791",
		"http://127.0.0.1:8791/": "http://127.0.0.1:8791",
		"https://host:443":       "https://host:443",
	}
	for in, want := range cases {
		got, err := NormalizeBase(in)
		if err != nil {
			t.Fatalf("NormalizeBase(%q): %v", in, err)
		}
		if got != want {
			t.Fatalf("NormalizeBase(%q) = %q, want %q", in, got, want)
		}
	}
	if _, err := NormalizeBase("  "); err == nil {
		t.Fatal("expected an error for an empty endpoint")
	}
}

// TestConnectEnvelope checks the envelope framing: a flags byte, a
// big-endian length and the payload.
func TestConnectEnvelope(t *testing.T) {
	frame := connectEnvelope([]byte(`{"a":1}`))
	if len(frame) != 5+7 {
		t.Fatalf("unexpected frame length: %d", len(frame))
	}
	if frame[0] != 0 {
		t.Fatalf("unexpected flags: %d", frame[0])
	}
	if size := binary.BigEndian.Uint32(frame[1:5]); size != 7 {
		t.Fatalf("unexpected size: %d", size)
	}
}

// TestDispatchJSONRPCFrame checks the JSON-RPC path the transport also
// offers.
func TestDispatchJSONRPCFrame(t *testing.T) {
	srv := fakeReference(t)
	defer srv.Close()

	disp, err := New(srv.URL)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	out := disp.Dispatch(context.Background(), []byte(`{"id":1,"method":"GetUserStatus","params":{}}`))
	if _, ok := out["result"]; !ok {
		t.Fatalf("expected a result, got %v", out)
	}
}

// TestGeminiDirArg checks the caller's `-gemini_dir` override: the
// value comes back and the argument leaves the list, so the spawn can
// put the effective directory last (the reference takes the last
// occurrence of a flag).
func TestGeminiDirArg(t *testing.T) {
	cases := []struct {
		args  []string
		want  string
		found bool
		left  []string
	}{
		{args: nil},
		{args: []string{"-use_mocked_data=false"}, left: []string{"-use_mocked_data=false"}},
		{args: []string{"-gemini_dir=/tmp/x"}, want: "/tmp/x", found: true},
		{args: []string{"-gemini_dir", "/tmp/y"}, want: "/tmp/y", found: true},
		{args: []string{"-gemini_dir=/tmp/a", "-gemini_dir=/tmp/b"}, want: "/tmp/b", found: true},
		{
			args:  []string{"-a", "-gemini_dir=/tmp/x", "-b"},
			want:  "/tmp/x",
			found: true,
			left:  []string{"-a", "-b"},
		},
	}
	for _, tc := range cases {
		got, left, found := geminiDirArg(tc.args)
		if found != tc.found || got != tc.want {
			t.Fatalf("geminiDirArg(%v) = %q, %v; want %q, %v", tc.args, got, found, tc.want, tc.found)
		}
		if strings.Join(left, " ") != strings.Join(tc.left, " ") {
			t.Fatalf("geminiDirArg(%v) left %v, want %v", tc.args, left, tc.left)
		}
	}
}
