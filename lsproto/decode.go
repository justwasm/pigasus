package lsproto

import (
	"fmt"
	"strings"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"
)

// DecodeNamed decodes a binary protobuf message into the proto-JSON
// form of the named message.
//
// The reference's own store keeps the summaries it wrote as binary
// protobuf — `conversation_summaries.raw_summary` is a serialised
// `exa.jetski_cortex_pb.CascadeTrajectorySummary` — so the clone has
// to be able to read a message it never wrote. The descriptor set
// already holds the schema, so the decode is a lookup and two
// conversions, not a regeneration.
//
// The name is the fully-qualified protobuf name, e.g.
// "exa.jetski_cortex_pb.CascadeTrajectorySummary".
func (c *Codec) DecodeNamed(name string, raw []byte) ([]byte, error) {
	msg, err := c.newNamed(name)
	if err != nil {
		return nil, err
	}
	if err := proto.Unmarshal(raw, msg); err != nil {
		return nil, fmt.Errorf("lsproto: decode %s: %w", name, err)
	}
	out, err := protojson.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("lsproto: marshal %s as proto-json: %w", name, err)
	}
	return out, nil
}

// newNamed returns an empty message of the named type.
func (c *Codec) newNamed(name string) (*dynamicpb.Message, error) {
	if c == nil || c.files == nil {
		return nil, fmt.Errorf("lsproto: no descriptor registry")
	}
	desc, err := c.files.FindDescriptorByName(protoreflect.FullName(name))
	if err != nil {
		return nil, fmt.Errorf("lsproto: message %s not found: %w", name, err)
	}
	md, ok := desc.(protoreflect.MessageDescriptor)
	if !ok {
		return nil, fmt.Errorf("lsproto: %s is not a message", name)
	}
	return dynamicpb.NewMessage(md), nil
}

// DecodeStep decodes a stored `gemini_coder.Step` into proto-JSON.
//
// A step can carry a google.protobuf.Any whose type the descriptor set
// does not have. The reference's tool-call steps do: they carry an
// `exa.cortex.core.tools.*` payload, and the frontend's bundle — the
// only place the clone's schemas come from — does not embed those
// schemas. proto-JSON refuses to marshal such an Any ("unable to
// resolve"), and the refusal costs the whole step: the tool call the
// Web UI renders disappears from the conversation's history.
//
// The clone drops the Any before the marshal. The Web UI never reads
// it: it renders a tool call from the step's metadata
// (`metadata.toolCall`), and its own decode of the payload — `Ae(any,
// StepSchema)` — yields nothing for a type that is not a Step.
func (c *Codec) DecodeStep(raw []byte) ([]byte, error) {
	msg, err := c.newNamed("gemini_coder.Step")
	if err != nil {
		return nil, err
	}
	if err := proto.Unmarshal(raw, msg); err != nil {
		return nil, fmt.Errorf("lsproto: decode gemini_coder.Step: %w", err)
	}
	c.dropUnknownAnys(msg, 0)
	out, err := protojson.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("lsproto: marshal gemini_coder.Step as proto-json: %w", err)
	}
	return out, nil
}

// dropUnknownAnys clears the Any fields of a message whose type the
// registry cannot resolve, recursively.
//
// The walk is depth-bounded: a step nests a handful of levels, and a
// malformed blob must not make it spin.
func (c *Codec) dropUnknownAnys(msg protoreflect.Message, depth int) {
	if c == nil || msg == nil || depth > 8 {
		return
	}
	fields := msg.Descriptor().Fields()
	for i := 0; i < fields.Len(); i++ {
		fd := fields.Get(i)
		if fd.Kind() != protoreflect.MessageKind && fd.Kind() != protoreflect.GroupKind {
			continue
		}
		if fd.IsMap() {
			// A map field is a message field whose value is a map, not
			// a list of messages.
			continue
		}
		if fd.IsList() {
			list := msg.Get(fd).List()
			for j := 0; j < list.Len(); j++ {
				elem := list.Get(j).Message()
				if isAnyMessage(elem.Descriptor()) {
					if !c.anyTypeKnown(elem) {
						msg.Clear(fd)
					}
					continue
				}
				c.dropUnknownAnys(elem, depth+1)
			}
			continue
		}
		if !msg.Has(fd) {
			continue
		}
		sub := msg.Get(fd).Message()
		if isAnyMessage(sub.Descriptor()) {
			if !c.anyTypeKnown(sub) {
				msg.Clear(fd)
			}
			continue
		}
		c.dropUnknownAnys(sub, depth+1)
	}
}

// isAnyMessage reports whether a message is google.protobuf.Any.
func isAnyMessage(md protoreflect.MessageDescriptor) bool {
	return md != nil && md.FullName() == "google.protobuf.Any"
}

// anyTypeKnown reports whether the registry knows the message the Any
// names. An Any without a type URL is empty, not unknown.
func (c *Codec) anyTypeKnown(any protoreflect.Message) bool {
	fd := any.Descriptor().Fields().ByName("type_url")
	if fd == nil {
		return true
	}
	url := any.Get(fd).String()
	if url == "" {
		return true
	}
	name := url
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	if name == "" {
		return true
	}
	_, err := c.files.FindDescriptorByName(protoreflect.FullName(name))
	return err == nil
}
