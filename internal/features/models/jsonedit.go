// jsonedit.go — the ordered JSON object the provider configuration is
// edited through.
//
// The provider configuration is edited one field at a time, and an edit
// has to leave everything else in the file exactly as it was: a key the
// clone does not model, a field a newer version of the file carries, the
// order the keys are in. The two Go-native approaches each lose one of
// those:
//
//   - a map[string]any loses the order (encoding/json sorts object keys),
//     so every write reshuffles the whole file;
//   - a typed struct loses the fields it does not declare, so a write
//     silently deletes them.
//
// This file keeps a document as an ordered list of raw JSON values, so an
// edit touches the bytes of one field and nothing else. It is the local
// equivalent of the JSONPath writes crush performs on its own config
// (sjson.Set on "providers.<id>.api_key"), without the dependency.
package models

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// jsonObject is an ordered JSON object: the keys keep the order they were
// read in, and a key that is set again keeps its original position.
type jsonObject struct {
	keys []string
	vals map[string]json.RawMessage
}

// newJSONObject returns an empty object.
func newJSONObject() *jsonObject {
	return &jsonObject{vals: map[string]json.RawMessage{}}
}

// decodeJSONObject parses a JSON object, keeping the key order. A document
// that is not an object is an error: silently accepting it would let a
// write replace the user's file with something else.
func decodeJSONObject(raw []byte) (*jsonObject, error) {
	o := newJSONObject()
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return o, nil
	}
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	tok, err := dec.Token()
	if err != nil {
		return nil, fmt.Errorf("parse JSON: %w", err)
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil, fmt.Errorf("parse JSON: expected an object")
	}
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("parse JSON: %w", err)
		}
		key, ok := keyTok.(string)
		if !ok {
			return nil, fmt.Errorf("parse JSON: expected an object key")
		}
		var val json.RawMessage
		if err := dec.Decode(&val); err != nil {
			return nil, fmt.Errorf("parse JSON: %w", err)
		}
		o.SetRaw(key, val)
	}
	return o, nil
}

// Keys returns the object's keys in order.
func (o *jsonObject) Keys() []string {
	if o == nil {
		return nil
	}
	out := make([]string, len(o.keys))
	copy(out, o.keys)
	return out
}

// Get returns the raw value stored under key.
func (o *jsonObject) Get(key string) (json.RawMessage, bool) {
	if o == nil {
		return nil, false
	}
	v, ok := o.vals[key]
	return v, ok
}

// Has reports whether the object carries key.
func (o *jsonObject) Has(key string) bool {
	_, ok := o.Get(key)
	return ok
}

// SetRaw stores a raw JSON value under key. A key that is already present
// keeps its position; a new key is appended.
func (o *jsonObject) SetRaw(key string, raw json.RawMessage) {
	if o == nil {
		return
	}
	if o.vals == nil {
		o.vals = map[string]json.RawMessage{}
	}
	if _, exists := o.vals[key]; !exists {
		o.keys = append(o.keys, key)
	}
	o.vals[key] = raw
}

// Set stores a value under key, marshalling it to JSON. A nil value
// removes the key.
func (o *jsonObject) Set(key string, value any) error {
	if o == nil {
		return fmt.Errorf("models: nil JSON object")
	}
	if value == nil {
		o.Delete(key)
		return nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshal %q: %w", key, err)
	}
	o.SetRaw(key, raw)
	return nil
}

// SetString stores a string under key. An empty value removes the key:
// the configuration treats an absent field as "not set", and writing an
// empty string would pin it to a value the user cannot change.
func (o *jsonObject) SetString(key, value string) {
	if value == "" {
		o.Delete(key)
		return
	}
	_ = o.Set(key, value)
}

// SetInt stores an integer under key. Zero removes the key, for the same
// reason SetString removes an empty string.
func (o *jsonObject) SetInt(key string, value int) {
	if value == 0 {
		o.Delete(key)
		return
	}
	_ = o.Set(key, value)
}

// Delete removes a key.
func (o *jsonObject) Delete(key string) {
	if o == nil {
		return
	}
	if _, ok := o.vals[key]; !ok {
		return
	}
	delete(o.vals, key)
	for i, k := range o.keys {
		if k == key {
			o.keys = append(o.keys[:i], o.keys[i+1:]...)
			break
		}
	}
}

// Object returns the child object stored under key. A missing key yields
// an empty object; a key that holds something else is an error.
//
// The child is detached: the caller mutates it and writes it back with
// SetRaw, so the change reaches the document.
func (o *jsonObject) Object(key string) (*jsonObject, error) {
	if o == nil {
		return nil, fmt.Errorf("models: nil JSON object")
	}
	raw, ok := o.Get(key)
	if !ok {
		return newJSONObject(), nil
	}
	return decodeJSONObject(raw)
}

// Array returns the elements stored under key. A missing key yields an
// empty slice; a key that holds something else is an error.
func (o *jsonObject) Array(key string) ([]json.RawMessage, error) {
	if o == nil {
		return nil, fmt.Errorf("models: nil JSON object")
	}
	raw, ok := o.Get(key)
	if !ok {
		return nil, nil
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil, nil
	}
	if trimmed[0] != '[' {
		return nil, fmt.Errorf("models: %q is not an array", key)
	}
	var out []json.RawMessage
	if err := json.Unmarshal(trimmed, &out); err != nil {
		return nil, fmt.Errorf("models: parse %q: %w", key, err)
	}
	return out, nil
}

// MarshalJSON renders the object as compact JSON, in key order.
func (o *jsonObject) MarshalJSON() ([]byte, error) {
	if o == nil {
		return []byte("{}"), nil
	}
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, k := range o.keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		key, err := json.Marshal(k)
		if err != nil {
			return nil, err
		}
		buf.Write(key)
		buf.WriteByte(':')
		buf.Write(o.vals[k])
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}
