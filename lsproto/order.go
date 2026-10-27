// Package lsproto — order.go: the answers in the reference's own field
// order.
//
// The reference's answers are protojson, and protojson writes a
// message's fields in the order the *descriptor* declares them — not
// in field-number order, and not alphabetically:
//
//	exa.codeium_common_pb.ExperimentWithVariant declares
//	key (1), key_string (5), disabled (6), string (2), json (3),
//	csv (4), source (7)
//
// and the reference's own answer carries them in exactly that order.
//
// The clone's dispatcher answers with Go maps, and encoding/json
// writes a map's keys in alphabetical order — so the clone's answers
// carry the same data in a different order, and the bytes differ from
// the reference's.
//
// This file reorders an answer: the fields the schema knows come
// first, in the descriptor's declaration order, and the fields the
// schema does not know follow, in alphabetical order. The values are
// not touched — only the order of the keys is.
//
// Evidence:
//   - the descriptor closure the Web UI parses
//     (lsproto/descriptor/language_server.protoset)
//   - the reference's own captured GetMendelFlags answer, whose
//     experiment entries read keyString, disabled, string, json, csv,
//     source — the declaration order, with the enum `key` left out
//     because protojson omits a default value.
package lsproto

import (
	"bytes"
	"encoding/json"
	"sort"

	"google.golang.org/protobuf/reflect/protoreflect"
)

// MarshalAnswer encodes an answer as JSON, with the fields in the
// schema's declaration order.
//
// A method the schema does not know is encoded the ordinary way: the
// answer is a clone's own, and the schema is not the authority on
// what the clone serves.
func (c *Codec) MarshalAnswer(method string, v any) ([]byte, error) {
	md, ok := c.OutputMessage(method)
	if !ok {
		return json.Marshal(v)
	}
	return c.MarshalMessage(md, v)
}

// MarshalMessage encodes a value as JSON, with the fields of a message
// in the schema's declaration order.
//
// The message is named by its full proto name — the pushed frames are
// messages of the cortex service, not of the language server service,
// so they are not a method's answer.
func (c *Codec) MarshalMessage(md protoreflect.MessageDescriptor, v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	if md == nil {
		return raw, nil
	}
	ordered, err := orderJSON(raw, md)
	if err != nil {
		// The answer is not a JSON object, or it does not parse: the
		// ordinary encoding is the honest answer.
		return raw, nil
	}
	return ordered, nil
}

// MarshalMessageNamed encodes a value with the fields of the named
// message in the schema's declaration order.
//
// A name the schema does not carry is encoded the ordinary way.
func (c *Codec) MarshalMessageNamed(name string, v any) ([]byte, error) {
	if c == nil || c.files == nil {
		return json.Marshal(v)
	}
	d, err := c.files.FindDescriptorByName(protoreflect.FullName(name))
	if err != nil {
		return json.Marshal(v)
	}
	md, ok := d.(protoreflect.MessageDescriptor)
	if !ok {
		return json.Marshal(v)
	}
	return c.MarshalMessage(md, v)
}

// orderJSON rewrites a JSON object with its keys in the descriptor's
// declaration order.
func orderJSON(raw []byte, md protoreflect.MessageDescriptor) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var value any
	if err := dec.Decode(&value); err != nil {
		return nil, err
	}
	obj, ok := value.(map[string]any)
	if !ok {
		return nil, errNotAnObject
	}
	var buf bytes.Buffer
	writeMessage(&buf, obj, md)
	return buf.Bytes(), nil
}

// errNotAnObject is the error orderJSON reports when the answer is
// not a JSON object.
var errNotAnObject = errNotAnObjectType{}

type errNotAnObjectType struct{}

func (errNotAnObjectType) Error() string { return "the answer is not a JSON object" }

// writeMessage writes a JSON object with the descriptor's field order.
func writeMessage(buf *bytes.Buffer, obj map[string]any, md protoreflect.MessageDescriptor) {
	buf.WriteByte('{')
	first := true
	for _, key := range orderedKeys(obj, md) {
		if !first {
			buf.WriteByte(',')
		}
		first = false
		name, _ := json.Marshal(key)
		buf.Write(name)
		buf.WriteByte(':')
		writeField(buf, obj[key], fieldFor(md, key))
	}
	buf.WriteByte('}')
}

// orderedKeys returns the object's keys: the fields the descriptor
// declares, in the declaration order, and then the keys the schema
// does not know, in alphabetical order.
func orderedKeys(obj map[string]any, md protoreflect.MessageDescriptor) []string {
	keys := make([]string, 0, len(obj))
	if md != nil {
		fields := md.Fields()
		for i := 0; i < fields.Len(); i++ {
			fd := fields.Get(i)
			for _, name := range []string{fd.JSONName(), string(fd.Name())} {
				if _, ok := obj[name]; ok {
					keys = append(keys, name)
					break
				}
			}
		}
	}
	known := map[string]bool{}
	for _, k := range keys {
		known[k] = true
	}
	rest := make([]string, 0, len(obj))
	for k := range obj {
		if !known[k] {
			rest = append(rest, k)
		}
	}
	sort.Strings(rest)
	return append(keys, rest...)
}

// fieldFor finds the descriptor of a field by its JSON name, and then
// by its proto name.
func fieldFor(md protoreflect.MessageDescriptor, key string) protoreflect.FieldDescriptor {
	if md == nil {
		return nil
	}
	if fd := md.Fields().ByJSONName(key); fd != nil {
		return fd
	}
	return md.Fields().ByName(protoreflect.Name(key))
}

// writeField writes one field's value.
//
// A field the schema does not know, and a value that does not fit the
// field's own type, is written the ordinary way.
func writeField(buf *bytes.Buffer, value any, fd protoreflect.FieldDescriptor) {
	if fd == nil {
		writeAny(buf, value)
		return
	}
	switch fd.Kind() {
	case protoreflect.MessageKind, protoreflect.GroupKind:
		if fd.IsMap() {
			// A map field's keys are the map's own, not the
			// schema's.
			writeAny(buf, value)
			return
		}
		if fd.IsList() {
			writeList(buf, value, fd.Message())
			return
		}
		obj, ok := value.(map[string]any)
		if !ok {
			writeAny(buf, value)
			return
		}
		writeMessage(buf, obj, fd.Message())
	default:
		if fd.IsList() {
			writeList(buf, value, nil)
			return
		}
		writeAny(buf, value)
	}
}

// writeList writes a repeated field's values.
func writeList(buf *bytes.Buffer, value any, elem protoreflect.MessageDescriptor) {
	arr, ok := value.([]any)
	if !ok {
		writeAny(buf, value)
		return
	}
	buf.WriteByte('[')
	for i, item := range arr {
		if i > 0 {
			buf.WriteByte(',')
		}
		if elem != nil {
			if obj, ok := item.(map[string]any); ok {
				writeMessage(buf, obj, elem)
				continue
			}
		}
		writeAny(buf, item)
	}
	buf.WriteByte(']')
}

// writeAny writes a value the schema does not shape: a scalar, or an
// object whose keys are the data's own.
func writeAny(buf *bytes.Buffer, value any) {
	switch t := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		buf.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				buf.WriteByte(',')
			}
			name, _ := json.Marshal(k)
			buf.Write(name)
			buf.WriteByte(':')
			writeAny(buf, t[k])
		}
		buf.WriteByte('}')
	case []any:
		buf.WriteByte('[')
		for i, item := range t {
			if i > 0 {
				buf.WriteByte(',')
			}
			writeAny(buf, item)
		}
		buf.WriteByte(']')
	default:
		raw, err := json.Marshal(value)
		if err != nil {
			buf.WriteString("null")
			return
		}
		buf.Write(raw)
	}
}
