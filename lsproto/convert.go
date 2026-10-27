// Package lsproto — convert.go: the two dialect conversions.
//
//	binary protobuf  →  proto-JSON
//	proto-JSON       →  binary protobuf
//
// The JSON → binary direction is deliberately lenient. The clone's
// dispatcher answers with hand-written maps that may carry extra keys
// (`cascade_id` next to `cascadeId`) or a value whose JSON type does
// not match the schema. The reference's own client parses with
// `ignoreUnknownFields: true`, so a field the schema does not know —
// or one the dispatcher got wrong — must not take the whole response
// down: such fields are dropped, the rest of the message survives.
package lsproto

import (
	"encoding/json"
	"fmt"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
)

// unmarshalProtoset parses the embedded FileDescriptorSet.
func unmarshalProtoset(raw []byte, fds *descriptorpb.FileDescriptorSet) error {
	return proto.Unmarshal(raw, fds)
}

// newFiles turns a FileDescriptorSet into a registry.
//
// The closure is not quite complete: three of the types the bundle's
// descriptors mention (`exa.cortex_pb.CustomExtensions`,
// `google.internal.cloud.code.v1internal.Entitlement` and
// `google.rpc.Status`) are not embedded in the bundle at all, so they
// resolve to placeholders. No message of the language-server service
// uses them, and a placeholder keeps the registry buildable.
func newFiles(fds *descriptorpb.FileDescriptorSet) (*protoregistry.Files, error) {
	return protodesc.FileOptions{AllowUnresolvable: true}.NewFiles(fds)
}

// DecodeRequest converts a binary-protobuf request body into the
// proto-JSON the dispatcher expects.
//
// An empty body decodes to an empty JSON object: Connect allows a
// request with no fields, and the dispatcher's handlers treat an empty
// object as "no parameters".
func (c *Codec) DecodeRequest(method string, body []byte) (out []byte, err error) {
	defer recoverAsError(&err)
	md, ok := c.InputMessage(method)
	if !ok {
		return nil, fmt.Errorf("lsproto: unknown method %q", method)
	}
	msg := dynamicpb.NewMessage(md)
	if len(body) > 0 {
		if err := proto.Unmarshal(body, msg); err != nil {
			return nil, fmt.Errorf("lsproto: decode %s request: %w", method, err)
		}
	}
	return marshalProtoJSON(msg)
}

// DecodeResponse converts a binary-protobuf response body into
// proto-JSON. It exists for symmetry with DecodeRequest and for tests.
func (c *Codec) DecodeResponse(method string, body []byte) (out []byte, err error) {
	defer recoverAsError(&err)
	md, ok := c.OutputMessage(method)
	if !ok {
		return nil, fmt.Errorf("lsproto: unknown method %q", method)
	}
	msg := dynamicpb.NewMessage(md)
	if len(body) > 0 {
		if err := proto.Unmarshal(body, msg); err != nil {
			return nil, fmt.Errorf("lsproto: decode %s response: %w", method, err)
		}
	}
	return marshalProtoJSON(msg)
}

// EncodeResponse converts the dispatcher's proto-JSON answer into a
// binary-protobuf body.
//
// Fields the schema does not know are dropped, and a field whose JSON
// type does not fit the schema is dropped as well; the rest of the
// message is still encoded.
func (c *Codec) EncodeResponse(method string, jsonBody []byte) (out []byte, err error) {
	defer recoverAsError(&err)
	md, ok := c.OutputMessage(method)
	if !ok {
		return nil, fmt.Errorf("lsproto: unknown method %q", method)
	}
	msg, err := jsonToMessage(md, jsonBody)
	if err != nil {
		return nil, err
	}
	out, err = proto.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("lsproto: encode %s response: %w", method, err)
	}
	return out, nil
}

// recoverAsError turns a panic into an error.
//
// The schema is closed-source and only reconstructed, so a corner of
// it can still surprise the protobuf runtime. A panic inside the
// tunnel's goroutine takes the whole daemon down; an error only costs
// one call.
func recoverAsError(err *error) {
	if r := recover(); r != nil {
		*err = fmt.Errorf("lsproto: recovered from a protobuf panic: %v", r)
	}
}

// marshalProtoJSON renders a dynamic message as proto-JSON.
func marshalProtoJSON(msg proto.Message) ([]byte, error) {
	out, err := protojson.MarshalOptions{}.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("lsproto: marshal proto-json: %w", err)
	}
	return out, nil
}

// jsonToMessage fills a dynamic message from a proto-JSON object,
// field by field, so one bad field cannot lose the whole message.
func jsonToMessage(md protoreflect.MessageDescriptor, jsonBody []byte) (*dynamicpb.Message, error) {
	msg := dynamicpb.NewMessage(md)
	trimmed := trimJSONObject(jsonBody)
	if len(trimmed) == 0 {
		return msg, nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &fields); err != nil {
		return nil, fmt.Errorf("lsproto: response is not a JSON object: %w", err)
	}
	descs := md.Fields()
	for i := 0; i < descs.Len(); i++ {
		fd := descs.Get(i)
		value, ok := lookupJSONField(fields, fd)
		if !ok {
			continue
		}
		if err := setJSONField(msg, fd, value); err != nil {
			// A field the dispatcher filled with the wrong shape is
			// dropped; the reference's client ignores unknown fields
			// the same way.
			continue
		}
	}
	return msg, nil
}

// trimJSONObject returns the body when it is a JSON object and an
// empty slice otherwise.
func trimJSONObject(body []byte) []byte {
	trimmed := trimSpace(body)
	if len(trimmed) == 0 {
		return nil
	}
	if trimmed[0] != '{' {
		return nil
	}
	return trimmed
}

// trimSpace trims ASCII whitespace from both ends.
func trimSpace(b []byte) []byte {
	start := 0
	for start < len(b) && isSpace(b[start]) {
		start++
	}
	end := len(b)
	for end > start && isSpace(b[end-1]) {
		end--
	}
	return b[start:end]
}

// isSpace reports whether b is ASCII whitespace.
func isSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}

// lookupJSONField finds a field's value under its JSON name or under
// its protobuf name. proto-JSON accepts both spellings.
func lookupJSONField(fields map[string]json.RawMessage, fd protoreflect.FieldDescriptor) (json.RawMessage, bool) {
	if v, ok := fields[fd.JSONName()]; ok {
		return v, true
	}
	if v, ok := fields[string(fd.Name())]; ok {
		return v, true
	}
	return nil, false
}

// setJSONField copies one JSON value into a message field.
func setJSONField(msg protoreflect.Message, fd protoreflect.FieldDescriptor, value json.RawMessage) error {
	if string(trimSpace(value)) == "null" {
		return nil
	}
	one := make(map[string]json.RawMessage, 1)
	one[fd.JSONName()] = value
	raw, err := json.Marshal(one)
	if err != nil {
		return err
	}
	tmp := dynamicpb.NewMessage(msg.Descriptor())
	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(raw, tmp); err != nil {
		return err
	}
	// An unset field has to be left alone: `Get` on an unset list
	// returns the package's empty-list singleton, and assigning that
	// to the message panics ("assigning invalid type
	// dynamicpb.emptyList"). A field that carries its default value
	// encodes to nothing either way, so skipping it changes nothing.
	if !tmp.Has(fd) {
		return nil
	}
	msg.Set(fd, tmp.Get(fd))
	return nil
}
