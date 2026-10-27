// frame.go — the protojson frame codec, shared by both sides of the
// localharness websocket.
//
// The protocol has two wire dialects and they must not be mixed:
//
//	phase 1 (stdio) — binary protobuf behind a 4-byte little-endian prefix
//	phase 2 (ws)    — protojson text frames
//
// This file owns the second one. It is deliberately the standard library's
// protojson with default options, because that is exactly the dialect the
// shipped Python SDK speaks:
//
//   - field names are camelCase;
//   - int64 / uint64 are JSON **strings**;
//   - int32 / uint32 are JSON **numbers**;
//   - enums are their names;
//   - a `oneof` is flattened into the parent object;
//   - bytes are base64.
//
// Decoding discards unknown fields. The descriptor this clone speaks was
// recovered from a shipped SDK rather than from the harness binary's own
// schema, so a field the harness adds must not break the connection.
//
// 【一手】 The dialect is the SDK's own: `google/antigravity/proto/
// localharness_pb2.py` uses `json_format.MessageToJson` /
// `ParseDict` with the default `preserving_proto_field_name=False`, which is
// protojson's camelCase spelling.
package localharness

import (
	"fmt"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// frameMarshaler encodes a message as a protojson text frame.
//
// The default `protojson.MarshalOptions` emits a compacted document with no
// trailing newline, which is what the SDK's `MessageToJson` produces too
// (minus the SDK's default indentation, which the wire does not care
// about).
var frameMarshaler = protojson.MarshalOptions{}

// frameUnmarshaler decodes a protojson text frame.
//
// `DiscardUnknown` is not optional: the harness may ship a field this
// build's recovered descriptor does not know about, and a strict decode
// would break the connection the moment it does.
var frameUnmarshaler = protojson.UnmarshalOptions{DiscardUnknown: true}

// MarshalFrame encodes m as a protojson text frame.
func MarshalFrame(m proto.Message) ([]byte, error) {
	data, err := frameMarshaler.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("localharness: marshal frame: %w", err)
	}
	return data, nil
}

// UnmarshalFrame decodes a protojson text frame into m.
//
// Unknown fields are discarded. A malformed frame is an error; the caller
// decides whether that is fatal.
func UnmarshalFrame(data []byte, m proto.Message) error {
	if err := frameUnmarshaler.Unmarshal(data, m); err != nil {
		return fmt.Errorf("localharness: decode frame: %w", err)
	}
	return nil
}
