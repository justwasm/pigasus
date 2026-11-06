//go:build js || wasip1 || wasm

package rcmeshpion

import (
	"context"
	"errors"

	"github.com/justwasm/pigasus/rcmesh"
)

// NewAnswer is deliberately unavailable on WASM. A browser host can implement
// rcmesh.Answerer using RTCPeerConnection without importing native Pion.
func NewAnswer(context.Context, string, rcmesh.PeerConfig) (rcmesh.Session, error) {
	return nil, errors.New("rcmeshpion: native WebRTC is unavailable on WASM")
}
