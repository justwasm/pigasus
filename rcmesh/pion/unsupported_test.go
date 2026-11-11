//go:build js || wasip1 || wasm

package rcmeshpion_test

import (
	"context"
	"testing"

	"github.com/justwasm/pigasus/rcmesh"
	mesh "github.com/justwasm/pigasus/rcmesh/pion"
)

var _ rcmesh.Answerer = mesh.NewAnswer

func TestUnsupported(t *testing.T) {
	if peer, err := mesh.NewAnswer(context.Background(), "", rcmesh.PeerConfig{}); peer != nil || err == nil {
		t.Fatal("native adapter advertised on WASM")
	}
}
