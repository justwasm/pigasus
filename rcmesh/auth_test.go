package rcmesh

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"strings"
	"testing"
)

func testKey(t *testing.T) (*ecdsa.PrivateKey, string) {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(&k.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	return k, string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
}
func signBinding(t *testing.T, k *ecdsa.PrivateKey, payload []byte) []byte {
	t.Helper()
	h := sha256.Sum256(payload)
	r, s, err := ecdsa.Sign(rand.Reader, k, h[:])
	if err != nil {
		t.Fatal(err)
	}
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	return sig
}
func TestBindingMatchesRecoveredArm64Layout(t *testing.T) {
	// Independent golden from the two allocations and BE32 stores in
	// 0x1025962d0: nonce and a sorted, length-delimited fingerprint pair.
	want, _ := hex.DecodeString("0000000201020000000a0000000141000000015a")
	got := ChannelBindingPayload([]byte{1, 2}, "Z", "A")
	if !bytes.Equal(got, want) {
		t.Fatalf("%x != %x", got, want)
	}
	if !bytes.Equal(got, ChannelBindingPayload([]byte{1, 2}, "A", "Z")) {
		t.Fatal("endpoint order changed binding")
	}
}
func TestFingerprint(t *testing.T) {
	fp := strings.TrimSuffix(strings.Repeat("AB:", 32), ":")
	want := "sha-256 " + strings.Repeat("ab", 32)
	got, err := FingerprintFromSDP("v=0\r\na=fingerprint:SHA-256 " + fp + "\r\n")
	if err != nil || got != want {
		t.Fatalf("%q %v", got, err)
	}
	for _, sdp := range []string{"v=0", "a=fingerprint:sha-1 " + fp, "a=fingerprint:sha-256 AA", "a=fingerprint:sha-256 " + fp + "\na=fingerprint:sha-256 " + strings.ReplaceAll(fp, "AB", "CD")} {
		if _, err := FingerprintFromSDP(sdp); err == nil {
			t.Fatalf("accepted invalid SDP %q", sdp)
		}
	}
}
func TestAuthenticatorBindsTrustNonceAndBothPeers(t *testing.T) {
	key, pemText := testKey(t)
	for _, mode := range []string{"valid", "untrusted", "wrong-peer", "wrong-nonce", "wrong-type", "bad-signature"} {
		t.Run(mode, func(t *testing.T) {
			auth, err := NewAuthenticator("local", "remote", []string{pemText})
			if err != nil {
				t.Fatal(err)
			}
			nonce := auth.Challenge().Challenge
			remote := "remote"
			if mode == "untrusted" {
				key, pemText = testKey(t)
			}
			if mode == "wrong-peer" {
				remote = "other"
			}
			if mode == "wrong-nonce" {
				nonce = bytes.Repeat([]byte{0}, 32)
			}
			response := AuthMessage{Type: "response", PubKey: pemText, Signature: signBinding(t, key, ChannelBindingPayload(nonce, "local", remote))}
			if mode == "wrong-type" {
				response.Type = "request"
			}
			if mode == "bad-signature" {
				response.Signature = response.Signature[:63]
			}
			err = auth.Accept(response)
			if (err == nil) != (mode == "valid") {
				t.Fatalf("accept: %v", err)
			}
			if err = auth.Accept(response); err == nil {
				t.Fatal("accepted replay")
			}
		})
	}
}
func TestNoTrustNoSession(t *testing.T) {
	if _, err := NewAuthenticator("a", "b", nil); err == nil {
		t.Fatal("accepted unpinned session")
	}
}
