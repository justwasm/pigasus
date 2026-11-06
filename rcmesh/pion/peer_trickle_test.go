//go:build !js && !wasip1 && !wasm

package rcmeshpion_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/justwasm/pigasus/rcmesh"
	mesh "github.com/justwasm/pigasus/rcmesh/pion"
	"github.com/pion/webrtc/v4"
)

func TestNewAnswerTrickleICEImmediateReturn(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, _ := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	pemKey := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))

	client, err := webrtc.NewAPI().NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })

	_, err = client.CreateDataChannel("mesh", nil)
	if err != nil {
		t.Fatal(err)
	}

	offer, err := client.CreateOffer(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.SetLocalDescription(offer); err != nil {
		t.Fatal(err)
	}

	var candMu sync.Mutex
	var trickled []rcmesh.ICECandidate

	start := time.Now()
	server, err := mesh.NewAnswer(ctx, client.LocalDescription().SDP, rcmesh.PeerConfig{
		AuthorizedKeys: []string{pemKey},
		Handler:        http.NotFoundHandler(),
		GatherTimeout:  15 * time.Second,
		OnLocalCandidate: func(c rcmesh.ICECandidate) {
			candMu.Lock()
			trickled = append(trickled, c)
			candMu.Unlock()
		},
	})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("NewAnswer failed: %v", err)
	}
	t.Cleanup(func() { _ = server.Close() })

	// With Trickle ICE, NewAnswer must return immediately without waiting for GatheringCompletePromise
	if elapsed > 1*time.Second {
		t.Errorf("NewAnswer took %v, expected < 1s with Trickle ICE", elapsed)
	}

	if server.AnswerSDP() == "" {
		t.Error("expected non-empty AnswerSDP")
	}
}
