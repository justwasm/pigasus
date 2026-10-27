// Package remotecontrol — answer_order.go: the answers carry their
// fields in the schema's own order.
//
// The reference's answers are protojson, and protojson writes a
// message's fields in the order the descriptor declares them. The
// clone's dispatcher answers with Go maps, and encoding/json writes
// a map's keys in alphabetical order — the same data, a different
// order, and therefore different bytes from the reference's.
//
// The reordering lives in lsproto (order.go), which drives
// it from the same descriptor the reference's own schema was
// recovered from.
//
// 【一手】 the reference's own GetMendelFlags answer carries its
// experiment entries as keyString, disabled, string, json, csv,
// source — the declaration order of
// exa.codeium_common_pb.ExperimentWithVariant (key 1, key_string 5,
// disabled 6, string 2, json 3, csv 4, source 7).
package remotecontrol

import (
	"encoding/json"

	"github.com/justwasm/pigasus/lsproto"
)

// marshalAnswer encodes an answer with the fields in the schema's
// declaration order.
//
// A method the schema does not know — a clone's own method, or one
// the descriptor closure does not carry — is encoded the ordinary
// way: the schema is not the authority on what the clone serves.
func marshalAnswer(method string, result any) ([]byte, error) {
	codec, err := lsproto.Shared()
	if err != nil {
		return json.Marshal(result)
	}
	return codec.MarshalAnswer(method, result)
}

// marshalLSMessage encodes a pushed frame with the fields of the named
// message in the schema's declaration order.
//
// The pushed frames are messages of the cortex service, not answers of
// the language server service, so they are named by their proto name.
func marshalLSMessage(name string, frame any) ([]byte, error) {
	codec, err := lsproto.Shared()
	if err != nil {
		return json.Marshal(frame)
	}
	return codec.MarshalMessageNamed(name, frame)
}
