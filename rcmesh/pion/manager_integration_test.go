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
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"sync"
	"testing"
	"time"

	mesh "github.com/justwasm/pigasus/rcmesh/pion"
	"github.com/justwasm/pigasus/rcmesh"
	"github.com/pion/webrtc/v4"
)

// loopbackSignaler simulates cloud signaling between browser and host.
type loopbackSignaler struct {
	mu           sync.Mutex
	hostID       string
	authKeys     []string
	toHost       []rcmesh.SignalingMessageDetails
	toBrowser    []rcmesh.SendRequest
	hostPollCh   chan struct{}
	browserRecvCh chan rcmesh.SendRequest
}

func newLoopbackSignaler(hostID string, authKeys []string) *loopbackSignaler {
	return &loopbackSignaler{
		hostID:        hostID,
		authKeys:      authKeys,
		hostPollCh:    make(chan struct{}, 16),
		browserRecvCh: make(chan rcmesh.SendRequest, 16),
	}
}

func (s *loopbackSignaler) Poll(ctx context.Context, req rcmesh.PollRequest) (rcmesh.PollResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	res := rcmesh.PollResponse{
		Messages: s.toHost,
		AuthorizedKeyBundle: &rcmesh.AuthorizedKeyBundle{
			Delivered:        true,
			WebCryptoPubKeys: s.authKeys,
		},
	}
	s.toHost = nil
	return res, nil
}

func (s *loopbackSignaler) Send(ctx context.Context, req rcmesh.SendRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.toBrowser = append(s.toBrowser, req)
	select {
	case s.browserRecvCh <- req:
	default:
	}
	return nil
}

func (s *loopbackSignaler) PostOffer(sessionID, senderID string, offerSDP string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	offerJSON, _ := json.Marshal(rcmesh.SessionDescription{Type: "offer", SDP: offerSDP})
	nonce := make([]byte, 16)
	_, _ = rand.Read(nonce)
	env := rcmesh.SignalingEnvelope{
		MessageID:   hex.EncodeToString(nonce),
		SenderID:    senderID,
		RecipientID: s.hostID,
		PayloadType: "offer",
		Payload:     offerJSON,
	}
	envBytes, _ := json.Marshal(env)

	s.toHost = append(s.toHost, rcmesh.SignalingMessageDetails{
		SessionID: sessionID,
		Payload:   string(envBytes),
		CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
	})
}

func (s *loopbackSignaler) PostCandidate(sessionID, senderID string, cand rcmesh.ICECandidate) {
	s.mu.Lock()
	defer s.mu.Unlock()

	candJSON, _ := json.Marshal(cand)
	nonce := make([]byte, 16)
	_, _ = rand.Read(nonce)
	env := rcmesh.SignalingEnvelope{
		MessageID:   hex.EncodeToString(nonce),
		SenderID:    senderID,
		RecipientID: s.hostID,
		PayloadType: "candidate",
		Payload:     candJSON,
	}
	envBytes, _ := json.Marshal(env)

	s.toHost = append(s.toHost, rcmesh.SignalingMessageDetails{
		SessionID: sessionID,
		Payload:   string(envBytes),
		CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
	})
}

// TestPositiveEndToEndFlow walks through the complete forward lifecycle:
// 1. Signaling polling
// 2. Offer & trickle ICE exchange
// 3. Pion WebRTC PeerConnection & DataChannel establishment
// 4. Challenge-response channel-binding ECDSA authentication
// 5. ConnectInstanceRequestV2 RPC dispatch through local HTTP handler
// 6. ConnectInstanceResponseV2 return over WebRTC DataChannel
func TestPositiveEndToEndFlow(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	// 1. Generate client ECDSA P-256 key
	privKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(&privKey.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	clientPubPEM := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))

	// 2. Set up loopback signaler with client public key in authorized keys
	hostID := "host-instance-uuid-1234"
	signaler := newLoopbackSignaler(hostID, []string{clientPubPEM})

	// 3. Local HTTP RPC handler simulating LanguageServerService
	expectedBody := `{"fileType":"FILE_TYPE_FILE","size":"1024"}`
	rpcHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Jetski-Remote-Control-Transport") != "datachannel" {
			t.Errorf("expected datachannel transport header, got %q", r.Header.Get("X-Jetski-Remote-Control-Transport"))
		}
		if r.URL.Path != "/exa.language_server_pb.LanguageServerService/StatUri" {
			t.Errorf("unexpected RPC path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(expectedBody))
	})

	// 4. Start rcmesh.Manager in background
	mgr := &rcmesh.Manager{
		InstanceID: hostID,
		Signaler:   signaler,
		Answer:     mesh.NewAnswer,
		PeerConfig: rcmesh.PeerConfig{
			Handler:    rpcHandler,
			ICEServers: []rcmesh.ICEServer{},
		},
		MaxPeers:     4,
		PollInterval:     20 * time.Millisecond,
		IdlePollInterval: 20 * time.Millisecond,
	}

	mgrErrCh := make(chan error, 1)
	go func() {
		mgrErrCh <- mgr.Run(ctx)
	}()

	// 5. Browser client setup
	clientPC, err := webrtc.NewAPI().NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	defer clientPC.Close()

	dcInbox := make(chan []byte, 128)
	dc, err := clientPC.CreateDataChannel("mesh", nil)
	if err != nil {
		t.Fatal(err)
	}
	dc.OnMessage(func(msg webrtc.DataChannelMessage) {
		dcInbox <- bytes.Clone(msg.Data)
	})

	offer, err := clientPC.CreateOffer(nil)
	if err != nil {
		t.Fatal(err)
	}
	gathered := webrtc.GatheringCompletePromise(clientPC)
	if err := clientPC.SetLocalDescription(offer); err != nil {
		t.Fatal(err)
	}
	<-gathered
	offerSDP := clientPC.LocalDescription().SDP

	sessionID := "session-uuid-5678"
	browserID := "browser-client-9999"

	// 6. Post offer to signaler
	signaler.PostOffer(sessionID, browserID, offerSDP)

	// 7. Wait for answer message from host
	var answerSDP string
	select {
	case req := <-signaler.browserRecvCh:
		if req.SessionID != sessionID {
			t.Fatalf("unexpected answer sessionID: %s", req.SessionID)
		}
		if req.PayloadType != "answer" {
			t.Fatalf("expected payload_type 'answer', got %q", req.PayloadType)
		}
		var desc rcmesh.SessionDescription
		if err := json.Unmarshal([]byte(req.Payload), &desc); err != nil {
			t.Fatalf("unmarshal answer SDP: %v", err)
		}
		answerSDP = desc.SDP
	case <-time.After(5 * time.Second):
		select {
		case err := <-mgrErrCh:
			t.Fatalf("manager exited with error: %v", err)
		default:
		}
		t.Fatal("timed out waiting for answer from manager")
	}

	// 8. Browser sets remote description
	if err := clientPC.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeAnswer,
		SDP:  answerSDP,
	}); err != nil {
		t.Fatalf("SetRemoteDescription: %v", err)
	}

	// 9. DataChannel opens -> host sends challenge
	var challenge rcmesh.AuthMessage
	select {
	case raw := <-dcInbox:
		if err := json.Unmarshal(raw, &challenge); err != nil {
			t.Fatalf("unmarshal challenge: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for challenge")
	}

	if challenge.Type != "challenge" || len(challenge.Challenge) != 32 {
		t.Fatalf("invalid challenge message: %+v", challenge)
	}

	// 10. Browser computes channel binding signature and sends response
	remoteFP, err := rcmesh.FingerprintFromSDP(answerSDP)
	if err != nil {
		t.Fatal(err)
	}
	localFP, err := rcmesh.FingerprintFromSDP(offerSDP)
	if err != nil {
		t.Fatal(err)
	}

	cbPayload := rcmesh.ChannelBindingPayload(challenge.Challenge, localFP, remoteFP)
	digest := sha256.Sum256(cbPayload)
	rInt, sInt, err := ecdsa.Sign(rand.Reader, privKey, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	sigBytes := make([]byte, 64)
	rInt.FillBytes(sigBytes[:32])
	sInt.FillBytes(sigBytes[32:])

	authResp := rcmesh.AuthMessage{
		Type:      "response",
		PubKey:    clientPubPEM,
		Signature: sigBytes,
	}
	authRespJSON, _ := json.Marshal(authResp)
	if err := dc.SendText(string(authRespJSON)); err != nil {
		t.Fatal(err)
	}

	// 11. Host verifies signature and sends success
	var success rcmesh.AuthMessage
	select {
	case raw := <-dcInbox:
		if err := json.Unmarshal(raw, &success); err != nil {
			t.Fatalf("unmarshal success: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for auth success")
	}

	if success.Type != "success" {
		t.Fatalf("authentication failed, got message: %+v", success)
	}

	// 12. Send RPC over DataChannel: ConnectInstanceRequestV2
	rpcEnvelope := rcmesh.RequestEnvelope{
		ProxyRequest: &rcmesh.ProxyRequest{
			RequestID: "1001",
			RPCPath:   "/exa.language_server_pb.LanguageServerService/StatUri",
		},
	}
	rpcJSON, _ := json.Marshal(rpcEnvelope)
	if err := dc.SendText(string(rpcJSON)); err != nil {
		t.Fatal(err)
	}

	// 13. Receive ConnectInstanceResponseV2 from host
	select {
	case raw := <-dcInbox:
		var respEnvelope rcmesh.ResponseEnvelope
		if err := json.Unmarshal(raw, &respEnvelope); err != nil {
			t.Fatalf("unmarshal response envelope: %v (raw=%s)", err, string(raw))
		}
		if respEnvelope.ProxyResponse == nil {
			t.Fatalf("missing proxy response: %+v", respEnvelope)
		}
		if respEnvelope.ProxyResponse.RequestID != "1001" {
			t.Errorf("expected requestID 1001, got %s", respEnvelope.ProxyResponse.RequestID)
		}
		if respEnvelope.ProxyResponse.StatusCode != 200 {
			t.Errorf("expected status 200, got %d", respEnvelope.ProxyResponse.StatusCode)
		}
		body := respEnvelope.ProxyResponse.PayloadText
		if body != expectedBody {
			t.Errorf("response body = %q, want %q", body, expectedBody)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for RPC response over DataChannel")
	}

	// Clean shutdown
	cancel()
	select {
	case err := <-mgrErrCh:
		if err != nil && err != context.Canceled {
			t.Errorf("manager returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("manager did not shut down on cancel")
	}
}

// TestPositiveEndToEndFlowWithTrickleCandidates verifies that remote trickle ICE candidates
// arriving via signaling are properly received by the manager and added to the peer session.
func TestPositiveEndToEndFlowWithTrickleCandidates(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	privKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(&privKey.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	clientPubPEM := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))

	hostID := "host-instance-uuid-cand"
	signaler := newLoopbackSignaler(hostID, []string{clientPubPEM})

	rpcHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	mgr := &rcmesh.Manager{
		InstanceID: hostID,
		Signaler:   signaler,
		Answer:     mesh.NewAnswer,
		PeerConfig: rcmesh.PeerConfig{
			Handler:    rpcHandler,
			ICEServers: []rcmesh.ICEServer{},
		},
		MaxPeers:     4,
		PollInterval:     20 * time.Millisecond,
		IdlePollInterval: 20 * time.Millisecond,
	}

	go func() {
		_ = mgr.Run(ctx)
	}()

	clientPC, err := webrtc.NewAPI().NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	defer clientPC.Close()

	dcInbox := make(chan []byte, 128)
	dc, err := clientPC.CreateDataChannel("mesh", nil)
	if err != nil {
		t.Fatal(err)
	}
	dc.OnMessage(func(msg webrtc.DataChannelMessage) {
		dcInbox <- bytes.Clone(msg.Data)
	})

	sessionID := "session-uuid-trickle"
	browserID := "browser-client-trickle"

	// Intercept client candidates and post them via signaling
	clientPC.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c == nil {
			return
		}
		init := c.ToJSON()
		signaler.PostCandidate(sessionID, browserID, rcmesh.ICECandidate{
			Candidate:        init.Candidate,
			SDPMid:           init.SDPMid,
			SDPMLineIndex:    init.SDPMLineIndex,
			UsernameFragment: init.UsernameFragment,
		})
	})

	offer, err := clientPC.CreateOffer(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := clientPC.SetLocalDescription(offer); err != nil {
		t.Fatal(err)
	}

	// Post offer to signaler
	signaler.PostOffer(sessionID, browserID, offer.SDP)

	// Wait for answer
	var answerSDP string
	select {
	case req := <-signaler.browserRecvCh:
		var desc rcmesh.SessionDescription
		_ = json.Unmarshal([]byte(req.Payload), &desc)
		answerSDP = desc.SDP
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for answer")
	}

	if err := clientPC.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeAnswer,
		SDP:  answerSDP,
	}); err != nil {
		t.Fatalf("SetRemoteDescription: %v", err)
	}

	// Wait for challenge
	var challenge rcmesh.AuthMessage
	select {
	case raw := <-dcInbox:
		_ = json.Unmarshal(raw, &challenge)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for challenge")
	}

	// Sign & send auth response
	remoteFP, _ := rcmesh.FingerprintFromSDP(answerSDP)
	localFP, _ := rcmesh.FingerprintFromSDP(offer.SDP)
	cbPayload := rcmesh.ChannelBindingPayload(challenge.Challenge, localFP, remoteFP)
	digest := sha256.Sum256(cbPayload)
	rInt, sInt, _ := ecdsa.Sign(rand.Reader, privKey, digest[:])
	sigBytes := make([]byte, 64)
	rInt.FillBytes(sigBytes[:32])
	sInt.FillBytes(sigBytes[32:])

	authRespJSON, _ := json.Marshal(rcmesh.AuthMessage{
		Type:      "response",
		PubKey:    clientPubPEM,
		Signature: sigBytes,
	})
	_ = dc.SendText(string(authRespJSON))

	// Verify auth success
	select {
	case raw := <-dcInbox:
		var success rcmesh.AuthMessage
		_ = json.Unmarshal(raw, &success)
		if success.Type != "success" {
			t.Fatalf("auth failed: %+v", success)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for auth success")
	}

	// Send RPC
	rpcJSON, _ := json.Marshal(rcmesh.RequestEnvelope{
		ProxyRequest: &rcmesh.ProxyRequest{
			RequestID: "2002",
			RPCPath:   "/exa.language_server_pb.LanguageServerService/StatUri",
		},
	})
	_ = dc.SendText(string(rpcJSON))

	// Verify RPC response
	select {
	case raw := <-dcInbox:
		var respEnvelope rcmesh.ResponseEnvelope
		_ = json.Unmarshal(raw, &respEnvelope)
		if respEnvelope.ProxyResponse == nil || respEnvelope.ProxyResponse.StatusCode != 200 {
			t.Fatalf("bad response: %+v", respEnvelope)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for RPC response")
	}
}
