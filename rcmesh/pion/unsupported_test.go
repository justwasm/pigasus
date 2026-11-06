//go:build js || wasip1 || wasm

package rcmeshpion_test

import (
	"context"
	"testing"

	mesh "github.com/justwasm/pigasus/rcmesh/pion"
	"github.com/justwasm/pigasus/rcmesh"
)

var _ rcmesh.Answerer = mesh.NewAnswer

func TestUnsupported(t *testing.T) {
	if peer, err := mesh.NewAnswer(context.Background(), "", rcmesh.PeerConfig{}); peer != nil || err == nil {
		t.Fatal("native adapter advertised on WASM")
	}
}
