// Package lsproto — default.go: the default instance of a method's
// response message.
//
// The clone answers a method it does not implement with the *default
// instance* of that method's response message — the answer a method
// that has nothing to report gives. The descriptor set knows the
// shape of every answer, so the default is derived from the schema
// rather than guessed from the method name.
//
// 【一手】 2026-10-27: the reference itself answers most of the
// methods with exactly that. Of the 351 methods of the service, the
// reference answers 153 — and 111 of those 153 answers are exactly
// `{}`, the default instance of their response message
// (out/langserver/coverage-351.json).
//
// The default instance is not always the whole answer: a method whose
// response carries content (the skills catalogue, the workspace
// directories) is answered from the clone's own state instead — see
// internal/lsserver/coverage.go.
package lsproto

import (
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/dynamicpb"
)

// DefaultAnswer returns the default instance of a method's response
// message as proto-JSON, and reports whether the method is known.
//
// The default instance of a proto3 message is the message with every
// field at its zero value, and proto-JSON omits those fields — so the
// answer is `{}` for a response whose fields are all unset, and the
// fields' zero values for a response that carries them.
//
// A method the descriptor set does not know has no default: the
// caller keeps its own answer for it.
func (c *Codec) DefaultAnswer(method string) ([]byte, bool) {
	md, ok := c.OutputMessage(method)
	if !ok {
		return nil, false
	}
	out, err := protojson.Marshal(dynamicpb.NewMessage(md))
	if err != nil {
		return nil, false
	}
	return out, true
}
