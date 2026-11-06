package rcmesh

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"testing"
	"time"
)

func TestParseAuthorizedKeyPayloadVariations(t *testing.T) {
	pubPEM := generateTestPubKey(t)

	// 1. Singular object (Google Cloud server-injected format)
	singularJSON, _ := json.Marshal(map[string]string{"webCryptoPubKey": pubPEM})
	keys1, err := parseAuthorizedKeyPayload(singularJSON)
	if err != nil {
		t.Fatalf("singular failed: %v", err)
	}
	if len(keys1) != 1 || keys1[0] != pubPEM {
		t.Errorf("singular = %v, want [%s]", keys1, pubPEM)
	}

	// 2. Plural bundle
	pluralJSON, _ := json.Marshal(map[string][]string{"webCryptoPubKeys": {pubPEM}})
	keys2, err := parseAuthorizedKeyPayload(pluralJSON)
	if err != nil {
		t.Fatalf("plural failed: %v", err)
	}
	if len(keys2) != 1 || keys2[0] != pubPEM {
		t.Errorf("plural = %v, want [%s]", keys2, pubPEM)
	}

	// 3. Array of strings
	listJSON, _ := json.Marshal([]string{pubPEM})
	keys3, err := parseAuthorizedKeyPayload(listJSON)
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(keys3) != 1 || keys3[0] != pubPEM {
		t.Errorf("list = %v, want [%s]", keys3, pubPEM)
	}

	// 4. Bare JSON string
	strJSON, _ := json.Marshal(pubPEM)
	keys4, err := parseAuthorizedKeyPayload(strJSON)
	if err != nil {
		t.Fatalf("str failed: %v", err)
	}
	if len(keys4) != 1 || keys4[0] != pubPEM {
		t.Errorf("str = %v, want [%s]", keys4, pubPEM)
	}
}

func TestParsePublicKeyPEMAndBase64DER(t *testing.T) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	pemStr := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
	b64Str := base64.StdEncoding.EncodeToString(der)

	// PEM format
	k1, err := ParsePublicKey(pemStr)
	if err != nil {
		t.Fatalf("ParsePublicKey(PEM) failed: %v", err)
	}
	if !k1.Equal(&priv.PublicKey) {
		t.Errorf("k1 != expected public key")
	}

	// Base64 DER format (WebCrypto subtle.exportKey("spki"))
	k2, err := ParsePublicKey(b64Str)
	if err != nil {
		t.Fatalf("ParsePublicKey(Base64DER) failed: %v", err)
	}
	if !k2.Equal(&priv.PublicKey) {
		t.Errorf("k2 != expected public key")
	}
}

func TestServerInjectedControlMessages(t *testing.T) {
	pubPEM := generateTestPubKey(t)

	// Google server-injected authorized_key has singular webCryptoPubKey and empty envelope headers
	authPayload, _ := json.Marshal(map[string]string{"webCryptoPubKey": pubPEM})
	authEnvelope, _ := json.Marshal(SignalingEnvelope{
		PayloadType: "authorized_key",
		Payload:     authPayload,
	})

	// Server-injected ice_config with empty headers
	icePayload := json.RawMessage(`{"stunServers":["stun:stun.l.google.com:19302"],"maxConcurrentSessions":12}`)
	iceEnvelope, _ := json.Marshal(SignalingEnvelope{
		PayloadType: "ice_config",
		Payload:     icePayload,
	})

	// Subsequent browser offer with type omitted
	offerEnvelope, _ := json.Marshal(SignalingEnvelope{
		MessageID:   "msg-offer-1",
		SenderID:    "browser-client",
		RecipientID: "test-instance",
		PayloadType: "offer",
		Payload:     json.RawMessage(`{"sdp":"v=0\r\no=- 1 1 IN IP4 127.0.0.1\r\n"}`),
	})

	ctx, cancel := context.WithCancel(context.Background())
	sess := &mockSession{answerSDP: "v=0\r\no=- 2 2 IN IP4 127.0.0.1\r\n", done: make(chan struct{})}
	signaler := &testQueueSignaler{
		responses: []PollResponse{
			{
				Messages: []SignalingMessageDetails{
					// 1. Server-injected authorized_key (no sender/recipient/message ID)
					{SessionID: "sess-1", Payload: string(authEnvelope), CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)},
					// 2. Server-injected ice_config (no sender/recipient/message ID)
					{SessionID: "sess-1", Payload: string(iceEnvelope), CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)},
					// 3. Browser offer (type omitted, {"sdp":"..."})
					{SessionID: "sess-1", Payload: string(offerEnvelope), CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)},
				},
			},
		},
	}

	mgr := &Manager{
		InstanceID: "test-instance",
		Signaler:   signaler,
		Answer: func(ctx context.Context, sdp string, cfg PeerConfig) (Session, error) {
			cancel()
			return sess, nil
		},
	}

	err := mgr.Run(ctx)
	if err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("mgr.Run returned unexpected error: %v", err)
	}

	if mgr.MaxPeers != 12 {
		t.Errorf("MaxPeers = %d, want 12", mgr.MaxPeers)
	}
	if len(mgr.PeerConfig.ICEServers) != 1 || mgr.PeerConfig.ICEServers[0].URLs[0] != "stun:stun.l.google.com:19302" {
		t.Errorf("ICEServers not updated from server ice_config: %v", mgr.PeerConfig.ICEServers)
	}
	if len(signaler.sent) != 1 {
		t.Fatalf("expected 1 answer sent over signaling, got %d", len(signaler.sent))
	}
	if signaler.sent[0].PayloadType != "answer" {
		t.Errorf("expected answer message type, got %s", signaler.sent[0].PayloadType)
	}
}

func TestMalformedSignalingMessageNonFatal(t *testing.T) {
	pubPEM := generateTestPubKey(t)

	offerEnvelope, _ := json.Marshal(SignalingEnvelope{
		MessageID:   "m-offer",
		SenderID:    "browser",
		RecipientID: "inst",
		PayloadType: "offer",
		Payload:     json.RawMessage(`{"type":"offer","sdp":"v=0\r\n"}`),
	})

	ctx, cancel := context.WithCancel(context.Background())
	sess := &mockSession{answerSDP: "v=0\r\n", done: make(chan struct{})}
	signaler := &testQueueSignaler{
		responses: []PollResponse{
			{
				AuthorizedKeyBundle: &AuthorizedKeyBundle{Delivered: true, WebCryptoPubKeys: []string{pubPEM}},
				Messages: []SignalingMessageDetails{
					// 1. Corrupt JSON payload that failed unmarshaling
					{SessionID: "s1", Payload: `{not valid json!`, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)},
					// 2. Corrupt candidate payload
					{SessionID: "s1", Payload: `{"payload_type":"candidate","payload":"corrupt"}`, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)},
					// 3. Valid offer
					{SessionID: "s1", Payload: string(offerEnvelope), CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)},
				},
			},
		},
	}

	mgr := &Manager{
		InstanceID: "inst",
		Signaler:   signaler,
		Answer: func(ctx context.Context, sdp string, cfg PeerConfig) (Session, error) {
			cancel()
			return sess, nil
		},
	}

	err := mgr.Run(ctx)
	if err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("mgr.Run failed on malformed messages: %v", err)
	}
	if len(signaler.sent) != 1 || signaler.sent[0].PayloadType != "answer" {
		t.Fatalf("expected answer to be sent after skipping malformed messages, got %v", signaler.sent)
	}
}

type testQueueSignaler struct {
	responses []PollResponse
	sent      []SendRequest
	index     int
}

func (s *testQueueSignaler) Poll(ctx context.Context, req PollRequest) (PollResponse, error) {
	if s.index < len(s.responses) {
		resp := s.responses[s.index]
		s.index++
		return resp, nil
	}
	<-ctx.Done()
	return PollResponse{}, ctx.Err()
}

func (s *testQueueSignaler) Send(ctx context.Context, req SendRequest) error {
	s.sent = append(s.sent, req)
	return nil
}
