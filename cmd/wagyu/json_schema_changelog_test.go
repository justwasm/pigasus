package main

// Tests covering AGY 1.2.14 changelog entry verbatim:
//
//	Changed `--json-schema` to reject plain text, bare type names such as
//	`string`, and missing schema files instead of silently treating them as a
//	string schema; these inputs, and any schema whose root is not
//	`"type": "object"`, now fail at startup with an error and exit code `1`.
//
// These are acceptance tests — one per clause in the bullet — pinned against the
// recovered messages from cycle 118 (see docs/cycles/cycle-118/deliverable-A-json-schema.md).
// Cycle 118 implemented the validation; this file gives the changelog clause a
// named, easy-to-find test so a future refactor cannot silently regress any of
// the four rejection cases.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestChangelogJSONSchemaRejectsPlainText pins the "plain text" clause:
// `--json-schema=hello` (any non-JSON, non-path-like word) must fail with a
// Go encoding/json parse error.
func TestChangelogJSONSchemaRejectsPlainText(t *testing.T) {
	fs := jsonSchemaFlagSet(t, "hello", "json")
	err := validateJSONSchemaFlag(fs)
	if err == nil {
		t.Fatalf("plain text schema: expected an error, got nil")
	}
	msg := err.Error()
	if !strings.HasPrefix(msg, "Error: invalid --json-schema: ") {
		t.Fatalf("plain text schema: missing 'Error: invalid --json-schema: ' prefix, got %q", msg)
	}
	// Go's encoding/json text: "invalid character 'h' looking for beginning of value".
	if !strings.Contains(msg, "schema is not valid JSON") || !strings.Contains(msg, "'h'") {
		t.Errorf("plain text schema: expected a JSON parse error pointing at 'h', got %q", msg)
	}
}

// TestChangelogJSONSchemaRejectsBareTypeName pins the "bare type names such as
// `string`" clause: a single JSON-typed word like `string`, `number`, `object`,
// `boolean` is itself a JSON token (a bareword in the grammar sense) and must be
// rejected the same way any other non-JSON text is. We pin `string` because
// that is the example named in the changelog text.
func TestChangelogJSONSchemaRejectsBareTypeName(t *testing.T) {
	fs := jsonSchemaFlagSet(t, "string", "json")
	err := validateJSONSchemaFlag(fs)
	if err == nil {
		t.Fatalf("bare type name schema: expected an error, got nil")
	}
	msg := err.Error()
	if !strings.HasPrefix(msg, "Error: invalid --json-schema: ") {
		t.Fatalf("bare type name schema: missing 'Error: invalid --json-schema: ' prefix, got %q", msg)
	}
	if !strings.Contains(msg, "schema is not valid JSON") || !strings.Contains(msg, "'s'") {
		t.Errorf("bare type name schema: expected a JSON parse error pointing at 's', got %q", msg)
	}
}

// TestChangelogJSONSchemaRejectsMissingFile pins the "missing schema files"
// clause: a value that *looks* like a path (an absolute path here) but does not
// exist on disk must fail with the recovered "failed to read schema file" wording,
// not silently fall back to inline JSON.
func TestChangelogJSONSchemaRejectsMissingFile(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "absent.json")
	fs := jsonSchemaFlagSet(t, missing, "json")
	err := validateJSONSchemaFlag(fs)
	if err == nil {
		t.Fatalf("missing file schema: expected an error, got nil")
	}
	msg := err.Error()
	if !strings.HasPrefix(msg, "Error: invalid --json-schema: ") {
		t.Fatalf("missing file schema: missing 'Error: invalid --json-schema: ' prefix, got %q", msg)
	}
	// Must mention both the "failed to read schema file" wording and the path,
	// in that order, so a future change to the message does not silently regress
	// to a generic os.IsNotExist render.
	if !strings.Contains(msg, "failed to read schema file") {
		t.Errorf("missing file schema: missing 'failed to read schema file' wording, got %q", msg)
	}
	if !strings.Contains(msg, missing) {
		t.Errorf("missing file schema: missing the offending path %q, got %q", missing, msg)
	}
}

// TestChangelogJSONSchemaRejectsNonObjectRoot pins the "schema whose root is
// not `"type": "object`" clause. The cleanest example from the changelog is
// `{"type":"array"}` — a perfectly valid JSON schema, but its root type is
// `array`, so it is rejected as the changelog states. We also pin a bare
// "type": "Object" (wrong-case) variant so a future case-folding change cannot
// silently start accepting either form.
func TestChangelogJSONSchemaRejectsNonObjectRoot(t *testing.T) {
	cases := []struct {
		name    string
		schema  string
		wantSub string // a stable substring of the recovered message
	}{
		{
			name:    "array root",
			schema:  `{"type":"array"}`,
			wantSub: `schema root "type" must be "object", got array`,
		},
		{
			name:    "Object root wrong case",
			schema:  `{"type":"Object"}`,
			wantSub: `schema root "type" must be "object", got Object`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fs := jsonSchemaFlagSet(t, c.schema, "json")
			err := validateJSONSchemaFlag(fs)
			if err == nil {
				t.Fatalf("non-object root: expected an error, got nil")
			}
			msg := err.Error()
			if !strings.HasPrefix(msg, "Error: invalid --json-schema: ") {
				t.Fatalf("non-object root: missing 'Error: invalid --json-schema: ' prefix, got %q", msg)
			}
			if !strings.Contains(msg, c.wantSub) {
				t.Errorf("non-object root: expected %q in message, got %q", c.wantSub, msg)
			}
		})
	}
}

// TestChangelogJSONSchemaAcceptsValidObjectSchema is the single positive case:
// the changelog only describes the four rejections; the *acceptance* of a valid
// `"type": "object"` schema must continue to work. Both inline and file forms
// are exercised, because the file-vs-inline discriminator was the entire reason
// cycle 118 had to ship.
func TestChangelogJSONSchemaAcceptsValidObjectSchema(t *testing.T) {
	t.Run("inline", func(t *testing.T) {
		fs := jsonSchemaFlagSet(t, `{"type":"object"}`, "json")
		if err := validateJSONSchemaFlag(fs); err != nil {
			t.Errorf("valid inline object schema: unexpected error %v", err)
		}
	})
	t.Run("inline with properties", func(t *testing.T) {
		fs := jsonSchemaFlagSet(t, `{"type":"object","properties":{"x":{"type":"number"}}}`, "json")
		if err := validateJSONSchemaFlag(fs); err != nil {
			t.Errorf("valid inline object schema with properties: unexpected error %v", err)
		}
	})
	t.Run("file", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "good.json")
		if err := os.WriteFile(path, []byte(`{"type":"object","properties":{"a":{"type":"string"}}}`), 0o644); err != nil {
			t.Fatal(err)
		}
		fs := jsonSchemaFlagSet(t, path, "json")
		if err := validateJSONSchemaFlag(fs); err != nil {
			t.Errorf("valid object schema file %q: unexpected error %v", path, err)
		}
	})
}
