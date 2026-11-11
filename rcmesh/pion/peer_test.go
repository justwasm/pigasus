//go:build !js && !wasip1 && !wasm

package rcmeshpion_test

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/justwasm/pigasus/rcmesh"
	mesh "github.com/justwasm/pigasus/rcmesh/pion"
	"github.com/pion/webrtc/v4"
)

var _ rcmesh.Answerer = mesh.NewAnswer

type pair struct {
	server rcmesh.Session
	client *webrtc.PeerConnection
	dc     *webrtc.DataChannel
	inbox  chan []byte
	ctx    context.Context
	cancel context.CancelFunc
	key    *ecdsa.PrivateKey
	pem    string
	offer  string
}

func newPair(t *testing.T, handler http.Handler, authTimeout time.Duration) *pair {
	return newPairWithKeys(t, handler, authTimeout, nil)
}

func newPairWithKeys(t *testing.T, handler http.Handler, authTimeout time.Duration, keys []string) *pair {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	p := &pair{ctx: ctx, cancel: cancel, inbox: make(chan []byte, 256)}
	t.Cleanup(cancel)
	var err error
	p.key, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(&p.key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	p.pem = string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
	p.client, err = webrtc.NewAPI().NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.client.Close() })
	p.dc, err = p.client.CreateDataChannel("mesh", nil)
	if err != nil {
		t.Fatal(err)
	}
	p.dc.OnMessage(func(msg webrtc.DataChannelMessage) {
		select {
		case p.inbox <- bytes.Clone(msg.Data):
		case <-ctx.Done():
		}
	})
	offer, err := p.client.CreateOffer(nil)
	if err != nil {
		t.Fatal(err)
	}
	gathered := webrtc.GatheringCompletePromise(p.client)
	if err := p.client.SetLocalDescription(offer); err != nil {
		t.Fatal(err)
	}
	select {
	case <-gathered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	p.offer = p.client.LocalDescription().SDP
	authKeys := keys
	if authKeys == nil {
		authKeys = []string{p.pem}
	}
	p.server, err = mesh.NewAnswer(ctx, p.offer, rcmesh.PeerConfig{AuthorizedKeys: authKeys, Handler: handler, AuthTimeout: authTimeout})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.server.Close() })
	if err := p.client.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: p.server.AnswerSDP()}); err != nil {
		t.Fatal(err)
	}
	return p
}
func (p *pair) receive(t *testing.T) []byte {
	t.Helper()
	select {
	case frame := <-p.inbox:
		return frame
	case <-p.server.Done():
		t.Fatalf("peer closed: %v", p.server.Err())
	case <-p.ctx.Done():
		t.Fatal(p.ctx.Err())
	}
	return nil
}
func (p *pair) response(t *testing.T, challenge []byte, key *ecdsa.PrivateKey, publicPEM string) rcmesh.AuthMessage {
	t.Helper()
	a, err := rcmesh.FingerprintFromSDP(p.offer)
	if err != nil {
		t.Fatal(err)
	}
	b, err := rcmesh.FingerprintFromSDP(p.server.AnswerSDP())
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(rcmesh.ChannelBindingPayload(challenge, a, b))
	r, s, err := ecdsa.Sign(rand.Reader, key, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	signature := make([]byte, 64)
	r.FillBytes(signature[:32])
	s.FillBytes(signature[32:])
	return rcmesh.AuthMessage{Type: "response", PubKey: publicPEM, Signature: signature}
}
func (p *pair) send(t *testing.T, value any) {
	t.Helper()
	frame, err := json.Marshal(value)
	if err == nil {
		err = p.dc.SendText(string(frame))
	}
	if err != nil {
		t.Fatal(err)
	}
}
func (p *pair) authenticate(t *testing.T) {
	t.Helper()
	var challenge rcmesh.AuthMessage
	if err := json.Unmarshal(p.receive(t), &challenge); err != nil {
		t.Fatal(err)
	}
	if challenge.Type != "challenge" || len(challenge.Challenge) != 32 {
		t.Fatalf("bad challenge: %+v", challenge)
	}
	p.send(t, p.response(t, challenge.Challenge, p.key, p.pem))
	var success rcmesh.AuthMessage
	if err := json.Unmarshal(p.receive(t), &success); err != nil {
		t.Fatal(err)
	}
	if success.Type != "success" || success.SupportsChunking {
		t.Fatalf("bad success: %+v", success)
	}
}
func request(id, method string) rcmesh.RequestEnvelope {
	return rcmesh.RequestEnvelope{ProxyRequest: &rcmesh.ProxyRequest{RequestID: id, RPCPath: "/exa.language_server_pb.LanguageServerService/" + method}}
}

func TestAuthenticatedNativeRPCRoundTrip(t *testing.T) {
	// Actual UDP/ICE/DTLS/SCTP peers, not an in-memory channel mock.
	body := bytes.Repeat([]byte("native mesh ✓\n"), 5000)
	p := newPair(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Jetski-Remote-Control-Transport") != "datachannel" {
			t.Error("wrong transport identity")
		}
		if !pAuthenticated(r) {
			t.Error("missing tunnel identity")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}), 0)
	p.authenticate(t)
	p.send(t, request("9007199254740993", "GetStatus"))
	var got []byte
	for {
		var envelope rcmesh.ResponseEnvelope
		if err := json.Unmarshal(p.receive(t), &envelope); err != nil {
			t.Fatal(err)
		}
		f := envelope.ProxyResponse
		if f == nil || f.RequestID != "9007199254740993" || f.StatusCode != 200 {
			t.Fatalf("bad frame: %+v", envelope)
		}
		got = append(got, f.Payload...)
		got = append(got, f.PayloadText...)
		if f.IsFinal {
			break
		}
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("response changed: got %d want %d", len(got), len(body))
	}
	if !p.server.Authenticated() {
		t.Fatal("authenticated state not published")
	}
}
func pAuthenticated(r *http.Request) bool {
	return r.Header.Get("X-Jetski-Via-Remote-Control") == "true"
}

func TestPreAuthenticationRPCNeverReachesHandler(t *testing.T) {
	var calls atomic.Int32
	p := newPair(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }), 0)
	_ = p.receive(t) // challenge
	p.send(t, request("1", "GetStatus"))
	select {
	case <-p.server.Done():
	case <-p.ctx.Done():
		t.Fatal(p.ctx.Err())
	}
	if calls.Load() != 0 || p.server.Authenticated() || !errors.Is(p.server.Err(), rcmesh.ErrAuthentication) {
		t.Fatalf("authentication bypass: calls=%d err=%v", calls.Load(), p.server.Err())
	}
}
func TestUntrustedKeyClosesPeer(t *testing.T) {
	p := newPair(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("untrusted RPC") }), 0)
	var challenge rcmesh.AuthMessage
	if err := json.Unmarshal(p.receive(t), &challenge); err != nil {
		t.Fatal(err)
	}
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	der, _ := x509.MarshalPKIXPublicKey(&key.PublicKey)
	public := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
	p.send(t, p.response(t, challenge.Challenge, key, public))
	select {
	case <-p.server.Done():
	case <-p.ctx.Done():
		t.Fatal(p.ctx.Err())
	}
	if !errors.Is(p.server.Err(), rcmesh.ErrAuthentication) {
		t.Fatalf("untrusted key accepted: %v", p.server.Err())
	}
}
func TestAuthenticationTimeoutAndCancellation(t *testing.T) {
	t.Run("timeout", func(t *testing.T) {
		p := newPair(t, http.NotFoundHandler(), time.Second)
		select {
		case <-p.server.Done():
		case <-p.ctx.Done():
			t.Fatal(p.ctx.Err())
		}
		if p.server.Authenticated() || p.server.Err() == nil {
			t.Fatal("authentication deadline ignored")
		}
	})
	t.Run("active stream cancellation", func(t *testing.T) {
		started, stopped := make(chan struct{}), make(chan struct{})
		p := newPair(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/exa.language_server_pb.LanguageServerService/StreamStatus" {
				close(started)
				<-r.Context().Done()
				close(stopped)
				return
			}
			_, _ = io.WriteString(w, "unary")
		}), 0)
		p.authenticate(t)
		p.send(t, request("1", "StreamStatus"))
		select {
		case <-started:
		case <-p.ctx.Done():
			t.Fatal(p.ctx.Err())
		}
		// A live stream must not serialize all subsequent RPCs behind it.
		p.send(t, request("2", "GetStatus"))
		var e rcmesh.ResponseEnvelope
		if err := json.Unmarshal(p.receive(t), &e); err != nil {
			t.Fatal(err)
		}
		if e.ProxyResponse.RequestID != "2" || e.ProxyResponse.PayloadText != "unary" {
			t.Fatalf("unary starved: %+v", e)
		}
		p.cancel()
		select {
		case <-p.server.Done():
		case <-time.After(3 * time.Second):
			t.Fatal("peer did not close")
		}
		select {
		case <-stopped:
		case <-time.After(3 * time.Second):
			t.Fatal("stream context was not canceled")
		}
	})
}

func TestCancelOneRPCPreservesPeer(t *testing.T) {
	started, stopped := make(chan struct{}), make(chan struct{})
	p := newPair(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/exa.language_server_pb.LanguageServerService/StreamStatus" {
			close(started)
			<-r.Context().Done()
			close(stopped)
			return
		}
		_, _ = io.WriteString(w, "still connected")
	}), 0)
	p.authenticate(t)
	p.send(t, request("1", "StreamStatus"))
	select {
	case <-started:
	case <-p.ctx.Done():
		t.Fatal(p.ctx.Err())
	}
	p.send(t, rcmesh.RequestEnvelope{ProxyRequest: &rcmesh.ProxyRequest{RequestID: "1", Headers: map[string]string{rcmesh.CancelRequestHeader: "true"}}})
	select {
	case <-stopped:
	case <-p.ctx.Done():
		t.Fatal(p.ctx.Err())
	}
	var end rcmesh.ResponseEnvelope
	if err := json.Unmarshal(p.receive(t), &end); err != nil {
		t.Fatal(err)
	}
	if end.ProxyResponse == nil || end.ProxyResponse.RequestID != "1" || !end.ProxyResponse.IsFinal {
		t.Fatalf("bad canceled stream end: %+v", end)
	}
	p.send(t, request("2", "GetStatus"))
	var reply rcmesh.ResponseEnvelope
	if err := json.Unmarshal(p.receive(t), &reply); err != nil {
		t.Fatal(err)
	}
	if reply.ProxyResponse == nil || reply.ProxyResponse.RequestID != "2" || reply.ProxyResponse.PayloadText != "still connected" {
		t.Fatalf("peer did not survive cancellation: %+v", reply)
	}
}

func TestInvalidOfferReturnsNilSession(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	public := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
	// Has a valid fingerprint but is not a valid SDP session. This fails after
	// constructing the native peer, exercising the cleanup/error return path.
	offer := "v=0\r\na=fingerprint:sha-256 " + strings.TrimSuffix(strings.Repeat("AA:", 32), ":") + "\r\n"
	peer, err := mesh.NewAnswer(context.Background(), offer, rcmesh.PeerConfig{AuthorizedKeys: []string{public}, Handler: http.NotFoundHandler()})
	if err == nil || peer != nil {
		t.Fatalf("error path returned a session: %v, %v", peer, err)
	}
}

func TestSetSessionPinAllowsLateKeyUpdate(t *testing.T) {
	dummyKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	der, _ := x509.MarshalPKIXPublicKey(&dummyKey.PublicKey)
	dummyPEM := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))

	// Start with an unrelated authorized key so client key would be untrusted
	p := newPairWithKeys(t, http.NotFoundHandler(), 0, []string{dummyPEM})
	pinner, ok := p.server.(rcmesh.SessionPinner)
	if !ok {
		t.Fatal("peer does not implement SessionPinner")
	}
	// Pin client key before authenticating
	if err := pinner.SetSessionPin([]string{p.pem}); err != nil {
		t.Fatalf("SetSessionPin error: %v", err)
	}
	p.authenticate(t)
	if !p.server.Authenticated() {
		t.Fatal("peer not authenticated after late key pinning")
	}
}

func TestSetSessionPinRejectsInvalidKey(t *testing.T) {
	p := newPair(t, http.NotFoundHandler(), 0)
	pinner := p.server.(rcmesh.SessionPinner)
	if err := pinner.SetSessionPin([]string{"invalid-pem"}); err == nil {
		t.Fatal("expected error for invalid PEM")
	}
}

func TestChunkedRequestReassemblyOverDataChannel(t *testing.T) {
	receivedChan := make(chan []byte, 1)
	p := newPair(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		receivedChan <- body
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}), 0)
	p.authenticate(t)

	// Send chunk 1: isFinal: false, text "chunk-part-1,"
	falseVal := false
	chunk1 := rcmesh.RequestEnvelope{
		ProxyRequest: &rcmesh.ProxyRequest{
			RequestID:   "8888",
			RPCPath:     "/exa.language_server_pb.LanguageServerService/GetStatus",
			PayloadText: "chunk-part-1,",
			IsFinal:     &falseVal,
		},
	}
	p.send(t, chunk1)

	// Send chunk 2: isFinal: true, text "chunk-part-2"
	trueVal := true
	chunk2 := rcmesh.RequestEnvelope{
		ProxyRequest: &rcmesh.ProxyRequest{
			RequestID:   "8888",
			PayloadText: "chunk-part-2",
			IsFinal:     &trueVal,
		},
	}
	p.send(t, chunk2)

	// Wait for response
	var envelope rcmesh.ResponseEnvelope
	if err := json.Unmarshal(p.receive(t), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.ProxyResponse == nil || envelope.ProxyResponse.RequestID != "8888" {
		t.Fatalf("unexpected response: %+v", envelope)
	}
	select {
	case receivedBody := <-receivedChan:
		if string(receivedBody) != "chunk-part-1,chunk-part-2" {
			t.Fatalf("received body = %q, want 'chunk-part-1,chunk-part-2'", string(receivedBody))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for request body")
	}
}

func TestChunkedRequestAbortedOnCancellation(t *testing.T) {
	var called atomic.Bool
	p := newPair(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called.Store(true)
	}), 0)
	p.authenticate(t)

	// Send chunk 1: isFinal: false
	falseVal := false
	chunk1 := rcmesh.RequestEnvelope{
		ProxyRequest: &rcmesh.ProxyRequest{
			RequestID:   "9999",
			RPCPath:     "/exa.language_server_pb.LanguageServerService/GetStatus",
			PayloadText: "chunk-part-1",
			IsFinal:     &falseVal,
		},
	}
	p.send(t, chunk1)

	// Send cancel request for "9999"
	cancelReq := rcmesh.RequestEnvelope{
		ProxyRequest: &rcmesh.ProxyRequest{
			RequestID: "9999",
			Headers:   map[string]string{rcmesh.CancelRequestHeader: "true"},
		},
	}
	p.send(t, cancelReq)

	time.Sleep(50 * time.Millisecond)
	if called.Load() {
		t.Fatal("handler was called for canceled chunked request")
	}
}

// A chunk that names no active buffer and carries no rpcPath is an orphan:
// the reference answers HTTP 400 "orphaned request chunk without active
// reassembly buffer" and keeps the session.
func TestOrphanChunkAnswersErrorFrameAndKeepsPeer(t *testing.T) {
	var calls atomic.Int32
	p := newPair(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = io.WriteString(w, "ok")
	}), 0)
	p.authenticate(t)

	falseVal := false
	p.send(t, rcmesh.RequestEnvelope{ProxyRequest: &rcmesh.ProxyRequest{
		RequestID:   "7001",
		PayloadText: "orphan",
		IsFinal:     &falseVal,
	}})

	var envelope rcmesh.ResponseEnvelope
	if err := json.Unmarshal(p.receive(t), &envelope); err != nil {
		t.Fatal(err)
	}
	f := envelope.ProxyResponse
	if f == nil || f.RequestID != "7001" || f.StatusCode != 400 || !f.IsFinal {
		t.Fatalf("bad error frame: %+v", envelope)
	}
	if f.PayloadText != "orphaned request chunk without active reassembly buffer" {
		t.Fatalf("bad error message: %q", f.PayloadText)
	}
	if calls.Load() != 0 {
		t.Fatal("handler ran for an orphan chunk")
	}
	// The peer survives the rejection.
	p.send(t, request("7002", "GetStatus"))
	var reply rcmesh.ResponseEnvelope
	if err := json.Unmarshal(p.receive(t), &reply); err != nil {
		t.Fatal(err)
	}
	if reply.ProxyResponse == nil || reply.ProxyResponse.RequestID != "7002" {
		t.Fatalf("peer did not survive the rejection: %+v", reply)
	}
}

// A non-positive request id is answered with HTTP 400 "request_id must be a
// positive integer".
func TestNonPositiveRequestIDAnswersErrorFrame(t *testing.T) {
	p := newPair(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("handler must not run for a non-positive request id")
	}), 0)
	p.authenticate(t)

	p.send(t, rcmesh.RequestEnvelope{ProxyRequest: &rcmesh.ProxyRequest{RequestID: "0"}})

	var envelope rcmesh.ResponseEnvelope
	if err := json.Unmarshal(p.receive(t), &envelope); err != nil {
		t.Fatal(err)
	}
	f := envelope.ProxyResponse
	if f == nil || f.RequestID != "0" || f.StatusCode != 400 || !f.IsFinal {
		t.Fatalf("bad error frame: %+v", envelope)
	}
	if f.PayloadText != "request_id must be a positive integer" {
		t.Fatalf("bad error message: %q", f.PayloadText)
	}
}

// An ingress message the reference cannot parse is dropped, and the session
// survives it.
func TestUnparseableFrameIsDroppedAndPeerSurvives(t *testing.T) {
	p := newPair(t, http.NotFoundHandler(), 0)
	p.authenticate(t)

	if err := p.dc.SendText("{not json"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	select {
	case <-p.server.Done():
		t.Fatalf("peer closed on an unparseable frame: %v", p.server.Err())
	default:
	}
	p.send(t, request("7003", "GetStatus"))
	var reply rcmesh.ResponseEnvelope
	if err := json.Unmarshal(p.receive(t), &reply); err != nil {
		t.Fatal(err)
	}
	if reply.ProxyResponse == nil || reply.ProxyResponse.RequestID != "7003" {
		t.Fatalf("peer did not survive the unparseable frame: %+v", reply)
	}
}
