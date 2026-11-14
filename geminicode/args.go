package geminicode

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// CanonicalArgs returns functionCall arguments as a JSON object that survives
// the round trip: duplicate keys collapsed, everything else byte-for-byte.
//
// The model's own bytes are normally replayed verbatim — key order included —
// because the reference does, and a re-encoded map would change the request
// the model sees. But a *streamed* call can arrive with its opening key twice:
// a provider that re-sends the fragment it already sent, or a model that
// restarts its JSON after a false start. encoding/json accepts that; Google
// does not:
//
//	geminicode: upstream 400: Invalid JSON payload received. Unknown name
//	"commandLine" at 'request.contents[83].parts[1].function_call.args.fields[0]':
//	Repeated map key: 'commandLine' is already set.
//
// The poisoned call is answered by the *next* model, so one un-replayable
// history entry makes every later turn fail — with an error that names a
// model that did nothing wrong (the 2026-11-14 report: a DeepSeek turn's call
// broke every Gemini turn after it). Collapsing the duplicate is the smallest
// repair that keeps the call executable: JSON's own rule (the last value of a
// repeated key wins) is applied at the position of the first, so the key order
// and every other byte survive.
//
// A payload that is not a JSON object becomes "{}": functionCall.args is a
// protobuf Struct, so an array or a bare scalar is rejected upstream exactly
// the way a syntax error is.
func CanonicalArgs(raw json.RawMessage) json.RawMessage {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return json.RawMessage(`{}`)
	}
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.UseNumber()
	out, err := canonicalValue(dec)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	// Trailing bytes mean the payload is not the single object it looks like;
	// upstream would reject it, and guessing at the second value is worse than
	// sending an empty object.
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return json.RawMessage(`{}`)
	}
	return json.RawMessage(out)
}

// canonicalValue re-emits the next JSON value, recursing into objects and
// arrays so a duplicate key is collapsed at any depth.
func canonicalValue(dec *json.Decoder) ([]byte, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if delim, ok := tok.(json.Delim); ok {
		switch delim {
		case '{':
			return canonicalObjectBody(dec)
		case '[':
			return canonicalArrayBody(dec)
		}
		return nil, fmt.Errorf("canonical args: unexpected delimiter %q", delim)
	}
	return json.Marshal(tok)
}

// canonicalEntry is one object member, in first-seen order.
type canonicalEntry struct {
	key string
	val []byte
}

// canonicalObjectBody re-emits an object the decoder has just opened. A key
// that appears twice keeps its first position and its last value, which is
// what encoding/json (and every "last one wins" parser) would have reported.
func canonicalObjectBody(dec *json.Decoder) ([]byte, error) {
	entries := make([]canonicalEntry, 0, 8)
	at := make(map[string]int, 8)
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, ok := keyTok.(string)
		if !ok {
			return nil, errors.New("canonical args: object key is not a string")
		}
		val, err := canonicalValue(dec)
		if err != nil {
			return nil, err
		}
		if i, dup := at[key]; dup {
			entries[i].val = val
			continue
		}
		at[key] = len(entries)
		entries = append(entries, canonicalEntry{key: key, val: val})
	}
	if _, err := dec.Token(); err != nil { // the closing '}'
		return nil, err
	}
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, e := range entries {
		if i > 0 {
			buf.WriteByte(',')
		}
		name, err := json.Marshal(e.key)
		if err != nil {
			return nil, err
		}
		buf.Write(name)
		buf.WriteByte(':')
		buf.Write(e.val)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// canonicalArrayBody re-emits an array the decoder has just opened.
func canonicalArrayBody(dec *json.Decoder) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('[')
	first := true
	for dec.More() {
		val, err := canonicalValue(dec)
		if err != nil {
			return nil, err
		}
		if !first {
			buf.WriteByte(',')
		}
		first = false
		buf.Write(val)
	}
	if _, err := dec.Token(); err != nil { // the closing ']'
		return nil, err
	}
	buf.WriteByte(']')
	return buf.Bytes(), nil
}
