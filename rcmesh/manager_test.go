package rcmesh

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"testing"
	"time"
)

type managerSignaler struct {
	response PollResponse
	sends    int
}

func (s *managerSignaler) Poll(context.Context, PollRequest) (PollResponse, error) {
	return s.response, nil
}
func (s *managerSignaler) Send(context.Context, SendRequest) error { s.sends++; return nil }
func TestManagerRejectsUntrustedAndMisdirectedOffers(t *testing.T) {
	for _, recipient := range []string{"host", "other"} {
		payload, _ := json.Marshal(SignalingEnvelope{MessageID: "m", SenderID: "browser", RecipientID: recipient, PayloadType: "offer", Payload: json.RawMessage(`{"type":"offer","sdp":"x"}`)})
		signaler := &managerSignaler{response: PollResponse{Messages: []SignalingMessageDetails{{SessionID: "s", Payload: string(payload), CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}}}}
		called := false
		mgr := &Manager{InstanceID: "host", Signaler: signaler, Answer: func(context.Context, string, PeerConfig) (Session, error) {
			called = true
			return nil, errors.New("unexpected")
		}}
		err := mgr.Run(context.Background())
		if err == nil || called || signaler.sends != 0 {
			t.Fatal("untrusted offer reached answerer")
		}
	}
}
func TestManagerCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	mgr := &Manager{InstanceID: "host", Signaler: &managerSignaler{}, Answer: func(context.Context, string, PeerConfig) (Session, error) { return nil, nil }}
	err := mgr.Run(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

type mockSession struct {
	answerSDP  string
	candidates []ICECandidate
	authed     bool
	done       chan struct{}
	err        error
	pinned     []string
}

func (s *mockSession) AnswerSDP() string                     { return s.answerSDP }
func (s *mockSession) AddICECandidate(c ICECandidate) error { s.candidates = append(s.candidates, c); return nil }
func (s *mockSession) Authenticated() bool                  { return s.authed }
func (s *mockSession) Done() <-chan struct{}                 { return s.done }
func (s *mockSession) Err() error                           { return s.err }
func (s *mockSession) Close() error                         { return nil }
func (s *mockSession) SetSessionPin(keys []string) error    { s.pinned = append([]string(nil), keys...); return nil }

func generateTestPubKey(t *testing.T) string {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
}

func TestManagerCandidateAndPendingQueue(t *testing.T) {
	pubPEM := generateTestPubKey(t)
	candPayload, _ := json.Marshal(ICECandidate{Candidate: "candidate:1 1 UDP 2122260223 192.168.1.1 50000 typ host"})
	candSig, _ := json.Marshal(SignalingEnvelope{
		MessageID:   "m1",
		SenderID:    "browser",
		RecipientID: "host",
		PayloadType: "candidate",
		Payload:     candPayload,
	})

	offerPayload, _ := json.Marshal(SessionDescription{Type: "offer", SDP: "v=0\r\no=- 0 0 IN IP4 127.0.0.1\r\n"})
	offerSig, _ := json.Marshal(SignalingEnvelope{
		MessageID:   "m2",
		SenderID:    "browser",
		RecipientID: "host",
		PayloadType: "offer",
		Payload:     offerPayload,
	})

	sess := &mockSession{answerSDP: "v=0\r\no=- 1 1 IN IP4 127.0.0.1\r\n", done: make(chan struct{})}
	signaler := &managerSignaler{
		response: PollResponse{
			AuthorizedKeyBundle: &AuthorizedKeyBundle{Delivered: true, WebCryptoPubKeys: []string{pubPEM}},
			Messages: []SignalingMessageDetails{
				// Candidate arrives BEFORE offer
				{SessionID: "s1", Payload: string(candSig), CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)},
				{SessionID: "s1", Payload: string(offerSig), CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)},
			},
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	mgr := &Manager{
		InstanceID: "host",
		Signaler:   signaler,
		Answer: func(ctx context.Context, sdp string, cfg PeerConfig) (Session, error) {
			cancel()
			return sess, nil
		},
	}

	_ = mgr.Run(ctx)
	if len(sess.candidates) != 1 {
		t.Fatalf("expected 1 candidate flushed to session, got %d", len(sess.candidates))
	}
	if sess.candidates[0].Candidate != "candidate:1 1 UDP 2122260223 192.168.1.1 50000 typ host" {
		t.Errorf("unexpected candidate: %v", sess.candidates[0])
	}
}

func TestManagerICEConfigAndUnknownType(t *testing.T) {
	icePayload := json.RawMessage(`{"stunServers":["stun:stun.l.google.com:19302"],"maxConcurrentSessions":16}`)
	iceSig, _ := json.Marshal(SignalingEnvelope{
		MessageID:   "m-ice",
		SenderID:    "cloud",
		RecipientID: "host",
		PayloadType: "ice_config",
		Payload:     icePayload,
	})
	unknownSig, _ := json.Marshal(SignalingEnvelope{
		MessageID:   "m-unk",
		SenderID:    "cloud",
		RecipientID: "host",
		PayloadType: "ping",
		Payload:     json.RawMessage(`{}`),
	})

	ctx, cancel := context.WithCancel(context.Background())
	signaler := &managerSignaler{
		response: PollResponse{
			Messages: []SignalingMessageDetails{
				{SessionID: "s", Payload: string(iceSig), CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)},
				{SessionID: "s", Payload: string(unknownSig), CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)},
			},
		},
	}

	mgr := &Manager{
		InstanceID: "host",
		Signaler:   signaler,
		Answer: func(ctx context.Context, sdp string, cfg PeerConfig) (Session, error) {
			return nil, nil
		},
	}

	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	err := mgr.Run(ctx)
	if err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("unexpected error: %v", err)
	}
	if mgr.MaxPeers != 16 {
		t.Errorf("MaxPeers = %d, want 16", mgr.MaxPeers)
	}
	if len(mgr.PeerConfig.ICEServers) != 1 || mgr.PeerConfig.ICEServers[0].URLs[0] != "stun:stun.l.google.com:19302" {
		t.Errorf("unexpected ICEServers: %v", mgr.PeerConfig.ICEServers)
	}
}
