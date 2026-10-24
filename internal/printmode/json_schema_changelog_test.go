package printmode

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestChangelogJSONSchemaRejectsPlainText(t *testing.T) {
	fs := jsonSchemaFlagSet(t, "hello", "json")
	err := ValidateJSONSchemaFlag(fs)
	if err == nil {
		t.Fatalf("plain text schema: expected an error, got nil")
	}
	msg := err.Error()
	if !strings.HasPrefix(msg, "Error: invalid --json-schema: ") {
		t.Fatalf("plain text schema: missing prefix, got %q", msg)
	}
	if !strings.Contains(msg, "schema is not valid JSON") || !strings.Contains(msg, "'h'") {
		t.Errorf("plain text schema: expected JSON parse error, got %q", msg)
	}
}

func TestChangelogJSONSchemaRejectsBareTypeName(t *testing.T) {
	fs := jsonSchemaFlagSet(t, "string", "json")
	err := ValidateJSONSchemaFlag(fs)
	if err == nil {
		t.Fatalf("bare type name schema: expected an error, got nil")
	}
	msg := err.Error()
	if !strings.HasPrefix(msg, "Error: invalid --json-schema: ") {
		t.Fatalf("bare type name schema: missing prefix, got %q", msg)
	}
	if !strings.Contains(msg, "schema is not valid JSON") || !strings.Contains(msg, "'s'") {
		t.Errorf("bare type name schema: expected JSON parse error, got %q", msg)
	}
}

func TestChangelogJSONSchemaRejectsMissingSchemaFile(t *testing.T) {
	badPath := "/nonexistent/path/to/schema.json"
	fs := jsonSchemaFlagSet(t, badPath, "json")
	err := ValidateJSONSchemaFlag(fs)
	if err == nil {
		t.Fatalf("missing schema file: expected an error, got nil")
	}
	msg := err.Error()
	if !strings.HasPrefix(msg, "Error: invalid --json-schema: ") {
		t.Fatalf("missing schema file: missing prefix, got %q", msg)
	}
	if !strings.Contains(msg, "failed to read schema file") {
		t.Errorf("missing schema file: expected 'failed to read schema file', got %q", msg)
	}
}

func TestChangelogJSONSchemaRejectsNonObjectRoot(t *testing.T) {
	cases := []struct {
		name       string
		schema     string
		wantSubstr string
	}{
		{"array root", `[]`, `schema root must be a JSON object with "type": "object"`},
		{"number root", `123`, `schema root must be a JSON object with "type": "object"`},
		{"string root", `"hello"`, `schema root must be a JSON object with "type": "object"`},
		{"null root", `null`, `schema root must be a JSON object with "type": "object"`},
		{"missing type key", `{"properties":{}}`, `schema root must specify "type": "object"`},
		{"array type", `{"type":"array"}`, `schema root "type" must be "object", got array`},
		{"string type", `{"type":"string"}`, `schema root "type" must be "object", got string`},
		{"integer type", `{"type":"integer"}`, `schema root "type" must be "object", got integer`},
		{"numeric type value", `{"type":123}`, `schema root "type" must be "object", got 123`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fs := jsonSchemaFlagSet(t, tc.schema, "json")
			err := ValidateJSONSchemaFlag(fs)
			if err == nil {
				t.Fatalf("schema %q: expected an error, got nil", tc.schema)
			}
			msg := err.Error()
			if !strings.Contains(msg, tc.wantSubstr) {
				t.Errorf("schema %q: expected %q in error, got %q", tc.schema, tc.wantSubstr, msg)
			}
		})
	}
}

func TestChangelogJSONSchemaAcceptsValidObjectRoot(t *testing.T) {
	validInline := `{"type":"object","properties":{"name":{"type":"string"}}}`
	fsInline := jsonSchemaFlagSet(t, validInline, "json")
	if err := ValidateJSONSchemaFlag(fsInline); err != nil {
		t.Fatalf("valid inline schema: expected nil error, got %v", err)
	}

	dir := t.TempDir()
	filePath := filepath.Join(dir, "valid.json")
	if err := os.WriteFile(filePath, []byte(validInline), 0o600); err != nil {
		t.Fatalf("writing temp schema: %v", err)
	}
	fsFile := jsonSchemaFlagSet(t, filePath, "json")
	if err := ValidateJSONSchemaFlag(fsFile); err != nil {
		t.Fatalf("valid file schema: expected nil error, got %v", err)
	}
}
