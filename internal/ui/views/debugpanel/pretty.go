// pretty.go — the reader's two structured renderings.
//
// # What "pretty" means here
//
// The reader shows a body as the host stored it, and offers two
// reformattings of it: indented JSON and YAML. Neither invents content —
// a body that does not parse is shown unchanged, because a reader that
// silently swallowed a tool result it could not parse would be the one
// place in this panel where the text on screen is not the text on disk.
//
// # Why the key order is preserved
//
// A tool's arguments are a JSON object whose key order is the model's,
// and the order is part of what the user is reading: `{"path":…,"line":…}`
// and `{"line":…,"path":…}` are the same value and different evidence.
// Both renderings therefore walk the source in order rather than
// round-tripping through a map:
//
//	JSON  encoding/json's Indent, which only re-indents the bytes
//	YAML  a token walk into yaml.Node, whose Content keeps document order
//
// yaml.v3's own Marshal of a map[string]any sorts the keys, so it is used
// only after the walk has already put them in an ordered node.
package debugpanel

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// ReaderFormat is how the reader renders a body.
type ReaderFormat int

const (
	// FormatRaw is the body as the host stored it.
	FormatRaw ReaderFormat = iota
	// FormatJSON is the body re-indented, when it parses as JSON.
	FormatJSON
	// FormatYAML is the body converted to YAML, when it parses as JSON.
	FormatYAML
)

// Name is the format's label in the reader's title row.
func (f ReaderFormat) Name() string {
	switch f {
	case FormatJSON:
		return "json"
	case FormatYAML:
		return "yaml"
	default:
		return "raw"
	}
}

// Next cycles the format. The caller decides whether the cycle is
// available (see readerState.hasJSON); this only advances.
func (f ReaderFormat) Next() ReaderFormat {
	switch f {
	case FormatRaw:
		return FormatJSON
	case FormatJSON:
		return FormatYAML
	default:
		return FormatRaw
	}
}

// structured reports whether text is a JSON object or array.
//
// A bare scalar is not structured: `1480000` is valid JSON, and offering
// to "pretty-print" it as a number would put a format key on the reader
// that changes nothing. The top-level shape is what makes the offer
// meaningful.
func structured(text string) bool {
	t := strings.TrimSpace(text)
	if t == "" {
		return false
	}
	if t[0] != '{' && t[0] != '[' {
		return false
	}
	var v any
	return json.Unmarshal([]byte(t), &v) == nil
}

// indentJSON re-indents a JSON body, preserving its key order.
//
// json.Indent is the only step: it copies the source and inserts
// whitespace, so a number literal keeps the exact digits the model
// produced ("1.0" does not become "1", and a large integer does not lose
// its last digits to float64).
func indentJSON(text string) (string, bool) {
	t := strings.TrimSpace(text)
	if !structured(t) {
		return "", false
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, []byte(t), "", "  "); err != nil {
		return "", false
	}
	return buf.String(), true
}

// yamlFromJSON converts a JSON body to YAML, preserving key order.
//
// The walk reads the JSON token stream rather than unmarshalling into a
// map, because a map has no order to preserve. Scalars carry their JSON
// type as an explicit YAML tag: without it a string "true" would print
// as the boolean true, and the reader would be showing a different value
// than the body it claims to show.
func yamlFromJSON(text string) (string, bool) {
	t := strings.TrimSpace(text)
	if !structured(t) {
		return "", false
	}
	dec := json.NewDecoder(strings.NewReader(t))
	dec.UseNumber()
	node, err := yamlNode(dec)
	if err != nil {
		return "", false
	}
	// The whole body must be one document: a value followed by garbage is
	// not a body this function can claim to have converted.
	if _, err := dec.Token(); err == nil {
		return "", false
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(node); err != nil {
		return "", false
	}
	if err := enc.Close(); err != nil {
		return "", false
	}
	return strings.TrimRight(buf.String(), "\n"), true
}

// yamlNode reads one JSON value from dec into a yaml.Node.
func yamlNode(dec *json.Decoder) (*yaml.Node, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	return yamlNodeFrom(dec, tok)
}

func yamlNodeFrom(dec *json.Decoder, tok json.Token) (*yaml.Node, error) {
	if delim, ok := tok.(json.Delim); ok {
		switch delim {
		case '{':
			node := &yaml.Node{Kind: yaml.MappingNode}
			for dec.More() {
				kt, err := dec.Token()
				if err != nil {
					return nil, err
				}
				key, ok := kt.(string)
				if !ok {
					return nil, fmt.Errorf("json: object key is %T", kt)
				}
				vt, err := dec.Token()
				if err != nil {
					return nil, err
				}
				val, err := yamlNodeFrom(dec, vt)
				if err != nil {
					return nil, err
				}
				node.Content = append(node.Content,
					&yaml.Node{Kind: yaml.ScalarNode, Value: key}, val)
			}
			if _, err := dec.Token(); err != nil { // consume '}'
				return nil, err
			}
			return node, nil
		case '[':
			node := &yaml.Node{Kind: yaml.SequenceNode}
			for dec.More() {
				vt, err := dec.Token()
				if err != nil {
					return nil, err
				}
				val, err := yamlNodeFrom(dec, vt)
				if err != nil {
					return nil, err
				}
				node.Content = append(node.Content, val)
			}
			if _, err := dec.Token(); err != nil { // consume ']'
				return nil, err
			}
			return node, nil
		}
		return nil, fmt.Errorf("json: unexpected delimiter %v", delim)
	}
	return yamlScalar(tok)
}

// yamlScalar is one JSON scalar as a tagged YAML scalar.
func yamlScalar(tok json.Token) (*yaml.Node, error) {
	switch v := tok.(type) {
	case string:
		// An untagged scalar lets yaml.v3 choose the style, which is what
		// decides whether "hi" needs quotes. A string that looks like a
		// number stays a string because the tag says so.
		return &yaml.Node{Kind: yaml.ScalarNode, Value: v, Tag: "!!str"}, nil
	case json.Number:
		s := v.String()
		if _, err := strconv.ParseInt(s, 10, 64); err == nil {
			return &yaml.Node{Kind: yaml.ScalarNode, Value: s, Tag: "!!int"}, nil
		}
		return &yaml.Node{Kind: yaml.ScalarNode, Value: s, Tag: "!!float"}, nil
	case bool:
		return &yaml.Node{Kind: yaml.ScalarNode, Value: strconv.FormatBool(v), Tag: "!!bool"}, nil
	case nil:
		return &yaml.Node{Kind: yaml.ScalarNode, Value: "null", Tag: "!!null"}, nil
	}
	return nil, fmt.Errorf("json: unexpected token %T", tok)
}

// renderPart applies a format to one part's text.
//
// A part the format cannot read is returned unchanged: the reader's job
// is to show the body, and the format keys are an offer, not a filter.
func renderPart(text string, f ReaderFormat) string {
	switch f {
	case FormatJSON:
		if s, ok := indentJSON(text); ok {
			return s
		}
	case FormatYAML:
		if s, ok := yamlFromJSON(text); ok {
			return s
		}
	}
	return text
}
