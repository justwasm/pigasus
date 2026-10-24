package printmode

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func jsonSchemaFlagSet(t *testing.T, schema, format string) *flag.FlagSet {
	t.Helper()
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	fs.SetOutput(new(strings.Builder))
	fs.String("json-schema", "", "")
	fs.String("output-format", "", "")
	var args []string
	if schema != "" {
		args = append(args, "--json-schema="+schema)
	}
	if format != "" {
		args = append(args, "--output-format="+format)
	}
	if err := fs.Parse(args); err != nil {
		t.Fatal(err)
	}
	return fs
}

func TestJSONSchemaRequiresStructuredFormat(t *testing.T) {
	for _, format := range []string{"", "text", "JSON"} {
		fs := jsonSchemaFlagSet(t, "hello", format)
		err := ValidateJSONSchemaFlag(fs)
		if err == nil {
			t.Fatalf("format %q: expected an error", format)
		}
		want := "Error: " + JSONSchemaFormatError
		if err.Error() != want {
			t.Errorf("format %q: %q, want %q", format, err.Error(), want)
		}
	}
	for _, format := range []string{"json", "stream-json"} {
		fs := jsonSchemaFlagSet(t, "hello", format)
		err := ValidateJSONSchemaFlag(fs)
		if err == nil {
			t.Fatalf("format %q: expected the schema error", format)
		}
		if !strings.HasPrefix(err.Error(), "Error: invalid --json-schema: ") {
			t.Errorf("format %q: %q", format, err.Error())
		}
	}
}

func TestJSONSchemaUnsetIsAccepted(t *testing.T) {
	for _, val := range []string{"", "   ", "\t\n"} {
		fs := jsonSchemaFlagSet(t, val, "text")
		if err := ValidateJSONSchemaFlag(fs); err != nil {
			t.Errorf("value %q: unexpected error %v", val, err)
		}
	}
}

func TestJSONSchemaFileResolution(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.json")
	if err := os.WriteFile(path, []byte(`{"type":"object"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	fs := jsonSchemaFlagSet(t, path, "json")
	if err := ValidateJSONSchemaFlag(fs); err != nil {
		t.Fatalf("valid schema file %q: %v", path, err)
	}

	badPath := filepath.Join(dir, "nonexistent.json")
	fsBad := jsonSchemaFlagSet(t, badPath, "json")
	err := ValidateJSONSchemaFlag(fsBad)
	if err == nil {
		t.Fatal("expected error on missing schema file")
	}
	if !strings.Contains(err.Error(), "failed to read schema file") {
		t.Errorf("error %q should mention failed to read schema file", err.Error())
	}
}
