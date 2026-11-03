package rcmesh

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// failingSignaler fails every poll and counts the attempts.
type failingSignaler struct {
	attempts atomic.Int32
}

func (s *failingSignaler) Poll(context.Context, PollRequest) (PollResponse, error) {
	s.attempts.Add(1)
	return PollResponse{}, errors.New("cloud is down")
}

func (s *failingSignaler) Send(context.Context, SendRequest) error { return nil }

// A failed poll is not fatal: the reference logs "Signaling poll failed: %v"
// and keeps polling.
func TestManagerPollFailureIsNotFatal(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	signaler := &failingSignaler{}
	mgr := &Manager{
		InstanceID:       "host",
		Signaler:         signaler,
		PollInterval:     10 * time.Millisecond,
		IdlePollInterval: 10 * time.Millisecond,
		Answer:           func(context.Context, string, PeerConfig) (Session, error) { return nil, nil },
	}
	go func() {
		time.Sleep(60 * time.Millisecond)
		cancel()
	}()
	err := mgr.Run(ctx)
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("a failed poll must not end the loop, got %v", err)
	}
	if signaler.attempts.Load() < 1 {
		t.Fatal("the signaler was never polled")
	}
}

// The reference's failed-poll wait doubles and is capped at 30s.
func TestNextPollBackoffDoublesAndCaps(t *testing.T) {
	if got := nextPollBackoff(0); got != 2*time.Second {
		t.Errorf("first backoff = %v, want 2s", got)
	}
	if got := nextPollBackoff(16 * time.Second); got != 30*time.Second {
		t.Errorf("capped backoff = %v, want 30s", got)
	}
}

// "ice_candidate" is a candidate like "candidate": the reference routes both
// to handleRemoteCandidate.
func TestManagerAcceptsIceCandidatePayloadType(t *testing.T) {
	pubPEM := generateTestPubKey(t)
	candPayload, _ := json.Marshal(ICECandidate{Candidate: "candidate:2 1 UDP 1 10.0.0.1 1 typ host"})
	candSig, _ := json.Marshal(SignalingEnvelope{
		MessageID:   "m1",
		SenderID:    "browser",
		RecipientID: "host",
		PayloadType: "ice_candidate",
		Payload:     candPayload,
	})
	offerPayload, _ := json.Marshal(SessionDescription{Type: "offer", SDP: "v=0\r\n"})
	offerSig, _ := json.Marshal(SignalingEnvelope{
		MessageID:   "m2",
		SenderID:    "browser",
		RecipientID: "host",
		PayloadType: "offer",
		Payload:     offerPayload,
	})
	sess := &mockSession{answerSDP: "v=0\r\n", done: make(chan struct{})}
	signaler := &managerSignaler{response: PollResponse{
		AuthorizedKeyBundle: &AuthorizedKeyBundle{Delivered: true, WebCryptoPubKeys: []string{pubPEM}},
		Messages: []SignalingMessageDetails{
			{SessionID: "s1", Payload: string(candSig), CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)},
			{SessionID: "s1", Payload: string(offerSig), CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)},
		},
	}}
	ctx, cancel := context.WithCancel(context.Background())
	mgr := &Manager{InstanceID: "host", Signaler: signaler, Answer: func(context.Context, string, PeerConfig) (Session, error) {
		return sess, nil
	}}
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	_ = mgr.Run(ctx)
	if len(sess.candidates) != 1 {
		t.Fatalf("candidates = %d, want 1", len(sess.candidates))
	}
}

// An "authorized_key" message adds a key to the trust set: the reference has
// a dedicated handler for it.
func TestManagerAcceptsAuthorizedKeyPayload(t *testing.T) {
	pubPEM := generateTestPubKey(t)
	keySig, _ := json.Marshal(SignalingEnvelope{
		MessageID:   "k1",
		SenderID:    "cloud",
		RecipientID: "host",
		PayloadType: "authorized_key",
		Payload:     json.RawMessage(`{"webCryptoPubKeys":["` + strings.ReplaceAll(pubPEM, "\n", "\\n") + `"]}`),
	})
	offerPayload, _ := json.Marshal(SessionDescription{Type: "offer", SDP: "v=0\r\n"})
	offerSig, _ := json.Marshal(SignalingEnvelope{
		MessageID:   "m2",
		SenderID:    "browser",
		RecipientID: "host",
		PayloadType: "offer",
		Payload:     offerPayload,
	})
	sess := &mockSession{answerSDP: "v=0\r\n", done: make(chan struct{})}
	signaler := &managerSignaler{response: PollResponse{Messages: []SignalingMessageDetails{
		{SessionID: "s1", Payload: string(keySig), CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)},
		{SessionID: "s1", Payload: string(offerSig), CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)},
	}}}
	ctx, cancel := context.WithCancel(context.Background())
	var got []string
	mgr := &Manager{InstanceID: "host", Signaler: signaler, Answer: func(_ context.Context, _ string, cfg PeerConfig) (Session, error) {
		got = cfg.AuthorizedKeys
		cancel()
		return sess, nil
	}}
	_ = mgr.Run(ctx)
	if len(got) != 1 || got[0] != pubPEM {
		t.Fatalf("authorized keys = %v, want the key the message carried", got)
	}
}

// Without an ICE configuration the reference falls back to the public STUN
// server.
func TestManagerFallsBackToPublicSTUN(t *testing.T) {
	pubPEM := generateTestPubKey(t)
	offerPayload, _ := json.Marshal(SessionDescription{Type: "offer", SDP: "v=0\r\n"})
	offerSig, _ := json.Marshal(SignalingEnvelope{
		MessageID:   "m2",
		SenderID:    "browser",
		RecipientID: "host",
		PayloadType: "offer",
		Payload:     offerPayload,
	})
	sess := &mockSession{answerSDP: "v=0\r\n", done: make(chan struct{})}
	signaler := &managerSignaler{response: PollResponse{
		AuthorizedKeyBundle: &AuthorizedKeyBundle{Delivered: true, WebCryptoPubKeys: []string{pubPEM}},
		Messages: []SignalingMessageDetails{
			{SessionID: "s1", Payload: string(offerSig), CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)},
		},
	}}
	ctx, cancel := context.WithCancel(context.Background())
	var servers []ICEServer
	mgr := &Manager{InstanceID: "host", Signaler: signaler, Answer: func(_ context.Context, _ string, cfg PeerConfig) (Session, error) {
		servers = cfg.ICEServers
		cancel()
		return sess, nil
	}}
	_ = mgr.Run(ctx)
	if len(servers) != 1 || servers[0].URLs[0] != defaultSTUNServer {
		t.Fatalf("ice servers = %+v, want the public STUN fallback", servers)
	}
}

// The answer's message id is the reference's "ans-%d".
func TestManagerAnswerMessageIDFormat(t *testing.T) {
	pubPEM := generateTestPubKey(t)
	offerPayload, _ := json.Marshal(SessionDescription{Type: "offer", SDP: "v=0\r\n"})
	offerSig, _ := json.Marshal(SignalingEnvelope{
		MessageID:   "m2",
		SenderID:    "browser",
		RecipientID: "host",
		PayloadType: "offer",
		Payload:     offerPayload,
	})
	sess := &mockSession{answerSDP: "v=0\r\n", done: make(chan struct{})}
	signer := &recordingSignaler{response: PollResponse{
		AuthorizedKeyBundle: &AuthorizedKeyBundle{Delivered: true, WebCryptoPubKeys: []string{pubPEM}},
		Messages: []SignalingMessageDetails{
			{SessionID: "s1", Payload: string(offerSig), CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)},
		},
	}}
	ctx, cancel := context.WithCancel(context.Background())
	mgr := &Manager{InstanceID: "host", Signaler: signer, Answer: func(context.Context, string, PeerConfig) (Session, error) {
		cancel()
		return sess, nil
	}}
	_ = mgr.Run(ctx)
	if len(signer.sent) != 1 {
		t.Fatalf("sends = %d, want 1", len(signer.sent))
	}
	if !strings.HasPrefix(signer.sent[0].MessageID, "ans-") {
		t.Errorf("message id = %q, want the reference's ans- prefix", signer.sent[0].MessageID)
	}
}

// The reference's candidate message id is "cand-%d" and payload type is "candidate".
func TestManagerOutboundTrickleCandidateFormat(t *testing.T) {
	pubPEM := generateTestPubKey(t)
	offerPayload, _ := json.Marshal(SessionDescription{Type: "offer", SDP: "v=0\r\n"})
	offerSig, _ := json.Marshal(SignalingEnvelope{
		MessageID:   "m2",
		SenderID:    "browser",
		RecipientID: "host",
		PayloadType: "offer",
		Payload:     offerPayload,
	})
	sess := &mockSession{answerSDP: "v=0\r\n", done: make(chan struct{})}
	signer := &recordingSignaler{response: PollResponse{
		AuthorizedKeyBundle: &AuthorizedKeyBundle{Delivered: true, WebCryptoPubKeys: []string{pubPEM}},
		Messages: []SignalingMessageDetails{
			{SessionID: "s1", Payload: string(offerSig), CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)},
		},
	}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mgr := &Manager{
		InstanceID: "host",
		Signaler:   signer,
		Answer: func(_ context.Context, _ string, cfg PeerConfig) (Session, error) {
			if cfg.OnLocalCandidate != nil {
				cfg.OnLocalCandidate(ICECandidate{Candidate: "candidate:1 1 UDP 1 127.0.0.1 5000 typ host"})
			}
			return sess, nil
		},
	}
	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()
	_ = mgr.Run(ctx)
	if len(signer.sent) < 2 {
		t.Fatalf("sent %d messages, want at least answer and candidate", len(signer.sent))
	}
	// First message is answer
	if signer.sent[0].PayloadType != "answer" || !strings.HasPrefix(signer.sent[0].MessageID, "ans-") {
		t.Errorf("first message = %+v, want answer", signer.sent[0])
	}
	// Second message is candidate
	candReq := signer.sent[1]
	if candReq.PayloadType != "candidate" {
		t.Errorf("candidate payload type = %q, want 'candidate'", candReq.PayloadType)
	}
	if !strings.HasPrefix(candReq.MessageID, "cand-") {
		t.Errorf("candidate message id = %q, want 'cand-' prefix", candReq.MessageID)
	}
	var cand ICECandidate
	if err := json.Unmarshal([]byte(candReq.Payload), &cand); err != nil {
		t.Fatalf("unmarshal candidate payload: %v", err)
	}
	if !strings.Contains(cand.Candidate, "127.0.0.1") {
		t.Errorf("candidate = %s, want candidate body", cand.Candidate)
	}
}

// recordingSignaler records what the manager sends.
type recordingSignaler struct {
	response PollResponse
	sent     []SendRequest
}

func (s *recordingSignaler) Poll(context.Context, PollRequest) (PollResponse, error) {
	return s.response, nil
}

func (s *recordingSignaler) Send(_ context.Context, request SendRequest) error {
	s.sent = append(s.sent, request)
	return nil
}

// The reference treats HTTP 200 as the only success for a signaling call.
func TestSignalingClientRequiresStatus200(t *testing.T) {
	for _, status := range []int{http.StatusAccepted, http.StatusNoContent, http.StatusBadGateway} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{}`))
		}))
		client := SignalingClient{PollURL: server.URL}
		_, err := client.Poll(context.Background(), PollRequest{InstanceID: "host"})
		if err == nil {
			t.Errorf("status %d: expected an error", status)
		}
		server.Close()
	}
}

// When an authorized_key message arrives, keys are pinned per session.
// If the session exists, SetSessionPin is called. If not yet created, the offer
// inherits the pinned keys.
func TestManagerPerSessionKeyPinning(t *testing.T) {
	pubGlobal := generateTestPubKey(t)
	pubS1 := generateTestPubKey(t)
	pubS2 := generateTestPubKey(t)

	offerPayload, _ := json.Marshal(SessionDescription{Type: "offer", SDP: "v=0\r\n"})
	offerS1Sig, _ := json.Marshal(SignalingEnvelope{
		MessageID:   "m1",
		SenderID:    "browser1",
		RecipientID: "host",
		PayloadType: "offer",
		Payload:     offerPayload,
	})
	keyS1Sig, _ := json.Marshal(SignalingEnvelope{
		MessageID:   "k1",
		SenderID:    "cloud",
		RecipientID: "host",
		PayloadType: "authorized_key",
		Payload:     json.RawMessage(`{"webCryptoPubKeys":["` + strings.ReplaceAll(pubS1, "\n", "\\n") + `"]}`),
	})
	keyS2Sig, _ := json.Marshal(SignalingEnvelope{
		MessageID:   "k2",
		SenderID:    "cloud",
		RecipientID: "host",
		PayloadType: "authorized_key",
		Payload:     json.RawMessage(`{"webCryptoPubKeys":["` + strings.ReplaceAll(pubS2, "\n", "\\n") + `"]}`),
	})
	offerS2Sig, _ := json.Marshal(SignalingEnvelope{
		MessageID:   "m2",
		SenderID:    "browser2",
		RecipientID: "host",
		PayloadType: "offer",
		Payload:     offerPayload,
	})

	sessS1 := &mockSession{answerSDP: "v=0\r\n", done: make(chan struct{})}
	sessS2 := &mockSession{answerSDP: "v=0\r\n", done: make(chan struct{})}

	signaler := &managerSignaler{response: PollResponse{
		AuthorizedKeyBundle: &AuthorizedKeyBundle{Delivered: true, WebCryptoPubKeys: []string{pubGlobal}},
		Messages: []SignalingMessageDetails{
			// s1: offer arrives first, then authorized_key
			{SessionID: "s1", Payload: string(offerS1Sig), CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)},
			{SessionID: "s1", Payload: string(keyS1Sig), CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)},
			// s2: authorized_key arrives first, then offer
			{SessionID: "s2", Payload: string(keyS2Sig), CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)},
			{SessionID: "s2", Payload: string(offerS2Sig), CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)},
		},
	}}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var s2Config PeerConfig
	mgr := &Manager{
		InstanceID: "host",
		Signaler:   signaler,
		Answer: func(_ context.Context, offer string, cfg PeerConfig) (Session, error) {
			if len(cfg.AuthorizedKeys) == 1 && cfg.AuthorizedKeys[0] == pubS2 {
				s2Config = cfg
				return sessS2, nil
			}
			return sessS1, nil
		},
	}

	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	_ = mgr.Run(ctx)

	// Check s1: session was created, and then SetSessionPin was called with pubS1
	if len(sessS1.pinned) != 1 || sessS1.pinned[0] != pubS1 {
		t.Fatalf("sessS1 pinned = %v, want [%s]", sessS1.pinned, pubS1)
	}

	// Check s2: offer inherited pinned key pubS2 rather than pubGlobal
	if len(s2Config.AuthorizedKeys) != 1 || s2Config.AuthorizedKeys[0] != pubS2 {
		t.Fatalf("sessS2 config keys = %v, want [%s]", s2Config.AuthorizedKeys, pubS2)
	}
}
