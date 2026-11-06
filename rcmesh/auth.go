// Package rcmesh contains the recovered remote-control mesh wire and
// authentication primitives. It has no native WebRTC or internal dependencies.
package rcmesh

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"sync"
)

var ErrAuthentication = errors.New("rcmesh: authentication failed")

// AuthMessage is the reference's ephemeralMessage (agy 1.4.11). Byte slices
// use JSON base64. No field has omitempty in the recovered struct tags.
type AuthMessage struct {
	Type             string `json:"type"`
	Challenge        []byte `json:"challenge"`
	Signature        []byte `json:"signature"`
	PubKey           string `json:"pubKey"`
	Error            string `json:"error"`
	SupportsChunking bool   `json:"supportsChunking"`
}

// FingerprintFromSDP returns sha-256 <lowercase hex without colons>. Different
// fingerprints in one SDP are rejected: the mesh binds one certificate per peer.
func FingerprintFromSDP(sdp string) (string, error) {
	var result string
	for _, line := range strings.Split(sdp, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "a=fingerprint:") {
			continue
		}
		fields := strings.Fields(strings.TrimPrefix(line, "a=fingerprint:"))
		if len(fields) != 2 || !strings.EqualFold(fields[0], "sha-256") {
			return "", errors.New("rcmesh: expected a SHA-256 DTLS fingerprint")
		}
		value := strings.ToLower(strings.ReplaceAll(fields[1], ":", ""))
		decoded, err := hex.DecodeString(value)
		if err != nil || len(decoded) != sha256.Size {
			return "", errors.New("rcmesh: invalid SHA-256 DTLS fingerprint")
		}
		value = "sha-256 " + value
		if result != "" && result != value {
			return "", errors.New("rcmesh: conflicting DTLS fingerprints")
		}
		result = value
	}
	if result == "" {
		return "", errors.New("rcmesh: SDP has no DTLS fingerprint")
	}
	return result, nil
}

// ChannelBindingPayload is the reference's BuildChannelBindingPayload:
// LP(nonce) || LP(LP(min(fingerprintA,fingerprintB)) || LP(max(...))).
// LP is a four-byte big-endian length followed by bytes. Sorting makes the
// payload independent of which endpoint is the offerer (confirmed in arm64).
func ChannelBindingPayload(nonce []byte, a, b string) []byte {
	if a > b {
		a, b = b, a
	}
	fingerprints := make([]byte, 0, len(a)+len(b)+8)
	fingerprints = appendLengthPrefixed(fingerprints, []byte(a))
	fingerprints = appendLengthPrefixed(fingerprints, []byte(b))
	out := make([]byte, 0, len(nonce)+len(fingerprints)+8)
	out = appendLengthPrefixed(out, nonce)
	return appendLengthPrefixed(out, fingerprints)
}
func appendLengthPrefixed(dst, data []byte) []byte {
	dst = binary.BigEndian.AppendUint32(dst, uint32(len(data)))
	return append(dst, data...)
}

// ParsePublicKey accepts the SPKI PEM public key vouched by the signaling
// service, or raw Base64-encoded SPKI DER from WebCrypto.
// The reference uses P-256 and a raw 64-byte WebCrypto r||s signature.
func ParsePublicKey(text string) (*ecdsa.PublicKey, error) {
	var der []byte
	block, rest := pem.Decode([]byte(text))
	if block != nil && block.Type == "PUBLIC KEY" && len(bytes.TrimSpace(rest)) == 0 {
		der = block.Bytes
	} else if raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(text)); err == nil && len(raw) > 0 {
		der = raw
	} else if raw, err := base64.RawStdEncoding.DecodeString(strings.TrimSpace(text)); err == nil && len(raw) > 0 {
		der = raw
	} else {
		return nil, fmt.Errorf("%w: invalid public key PEM or base64 DER", ErrAuthentication)
	}
	parsed, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid public key: %w", ErrAuthentication, err)
	}
	key, ok := parsed.(*ecdsa.PublicKey)
	if !ok || key.Curve != elliptic.P256() {
		return nil, fmt.Errorf("%w: expected P-256", ErrAuthentication)
	}
	return key, nil
}
func VerifySignature(payload, signature []byte, key *ecdsa.PublicKey) bool {
	if key == nil || key.Curve != elliptic.P256() || len(signature) != 64 {
		return false
	}
	digest := sha256.Sum256(payload)
	return ecdsa.Verify(key, digest[:], new(big.Int).SetBytes(signature[:32]), new(big.Int).SetBytes(signature[32:]))
}

// Authenticator consumes exactly one response. Keys must come from a trusted
// signaling source, never from the incoming data channel or SDP offer itself.
type Authenticator struct {
	mu       sync.Mutex
	nonce    [32]byte
	payload  []byte
	keys     []*ecdsa.PublicKey
	pinned   []*ecdsa.PublicKey
	pinnedOK bool
	authed   *ecdsa.PublicKey
	consumed bool
}

func NewAuthenticator(localFingerprint, remoteFingerprint string, authorizedPEMs []string) (*Authenticator, error) {
	if localFingerprint == "" || remoteFingerprint == "" || len(authorizedPEMs) == 0 {
		return nil, fmt.Errorf("%w: missing channel binding or authorized keys", ErrAuthentication)
	}
	a := &Authenticator{}
	for _, text := range authorizedPEMs {
		key, err := ParsePublicKey(text)
		if err != nil {
			return nil, err
		}
		a.keys = append(a.keys, key)
	}
	if _, err := rand.Read(a.nonce[:]); err != nil {
		return nil, err
	}
	a.payload = ChannelBindingPayload(a.nonce[:], localFingerprint, remoteFingerprint)
	return a, nil
}
func (a *Authenticator) Challenge() AuthMessage {
	return AuthMessage{Type: "challenge", Challenge: bytes.Clone(a.nonce[:])}
}

// SetPinnedKeys replaces the session's pinned keys — the exact public keys the
// signaling service vouched for this session.
//
// 【反编译】 the reference pins the server-vouched key on the session
// (MeshDaemon.SetSessionPin, 0x102596850) and verifies the
// challenge-response against that pin (ExecuteChallengeResponse, 0x10259b090).
// A pin that arrives after the handshake must still cover the key the session
// authenticated with: the reference revokes the session otherwise ("[MeshDaemon]
// Revoking session %q: a refreshed authorized bundle no longer contains its
// authenticated key").
func (a *Authenticator) SetPinnedKeys(keys []*ecdsa.PublicKey) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.authed != nil && !containsPublicKey(keys, a.authed) {
		return fmt.Errorf("%w: the pinned keys do not cover the authenticated key", ErrAuthentication)
	}
	a.pinned = append([]*ecdsa.PublicKey(nil), keys...)
	a.pinnedOK = true
	return nil
}

// AuthenticatedKey returns the PEM of the key that authenticated the session.
// It is empty until the challenge-response has been accepted.
func (a *Authenticator) AuthenticatedKey() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.authed == nil {
		return ""
	}
	return publicKeyPEM(a.authed)
}

func containsPublicKey(keys []*ecdsa.PublicKey, key *ecdsa.PublicKey) bool {
	for _, candidate := range keys {
		if candidate != nil && key != nil && candidate.Equal(key) {
			return true
		}
	}
	return false
}

// publicKeyPEM encodes a P-256 public key the way the signaling service does:
// a PKIX "PUBLIC KEY" PEM block.
func publicKeyPEM(key *ecdsa.PublicKey) string {
	der, err := x509.MarshalPKIXPublicKey(key)
	if err != nil {
		return ""
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
}

func (a *Authenticator) Accept(response AuthMessage) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.consumed {
		return fmt.Errorf("%w: challenge already consumed", ErrAuthentication)
	}
	a.consumed = true
	if response.Type != "response" {
		return fmt.Errorf("%w: expected response", ErrAuthentication)
	}
	key, err := ParsePublicKey(response.PubKey)
	if err != nil {
		return err
	}
	// The pin wins over the instance-wide trust set: a pinned session is
	// verified against exactly the keys the service vouched for it.
	trusted := a.keys
	if a.pinnedOK {
		trusted = a.pinned
	}
	pinned := false
	for _, candidate := range trusted {
		if key.Equal(candidate) {
			pinned = true
			break
		}
	}
	if !pinned || !VerifySignature(a.payload, response.Signature, key) {
		return ErrAuthentication
	}
	a.authed = key
	return nil
}
