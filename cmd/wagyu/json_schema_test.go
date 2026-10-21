package main

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// jsonSchemaFlagSet builds the two flags validateJSONSchemaFlag reads.
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

// TestJSONSchemaRequiresStructuredFormat pins the first recovered check: the
// flag is refused outside json / stream-json, with a capital `Error: ` prefix
// and exit code 1.
func TestJSONSchemaRequiresStructuredFormat(t *testing.T) {
	for _, format := range []string{"", "text", "JSON"} {
		fs := jsonSchemaFlagSet(t, "hello", format)
		err := validateJSONSchemaFlag(fs)
		if err == nil {
			t.Fatalf("format %q: expected an error", format)
		}
		want := "Error: " + jsonSchemaFormatError
		if err.Error() != want {
			t.Errorf("format %q: %q, want %q", format, err.Error(), want)
		}
	}
	for _, format := range []string{"json", "stream-json"} {
		// The format gate passes; the schema itself is then rejected.
		fs := jsonSchemaFlagSet(t, "hello", format)
		err := validateJSONSchemaFlag(fs)
		if err == nil {
			t.Fatalf("format %q: expected the schema error", format)
		}
		if !strings.HasPrefix(err.Error(), "Error: invalid --json-schema: ") {
			t.Errorf("format %q: %q", format, err.Error())
		}
	}
}

// TestJSONSchemaUnsetIsAccepted covers the two values the reference treats as
// "no schema": the flag absent, and the flag set to whitespace.
func TestJSONSchemaUnsetIsAccepted(t *testing.T) {
	for _, schema := range []string{"", " ", "  \t "} {
		fs := jsonSchemaFlagSet(t, schema, "")
		if err := validateJSONSchemaFlag(fs); err != nil {
			t.Errorf("schema %q: unexpected error %v", schema, err)
		}
	}
}

// TestResolveJSONSchemaMessages pins every recovered rejection message. The
// expected strings are captured verbatim from 1.3.6.
func TestResolveJSONSchemaMessages(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	good := write("good.json", `{"type":"object","properties":{"a":{"type":"string"}}}`)
	rootArray := write("root_array.json", `{"type":"array"}`)
	noType := write("no_type.json", `{"properties":{}}`)
	notJSON := write("notjson.json", "not json at all")
	noExt := write("plainjson", `{"type":"object"}`)
	missing := filepath.Join(dir, "absent.json")

	cases := []struct {
		name  string
		value string
		want  string // empty means "accepted"
	}{
		{"inline object", `{"type":"object"}`, ""},
		{"inline object with extras", `{"type":"object","x":1}`, ""},
		{"file", good, ""},
		{"file without extension", noExt, ""},
		{"inline trailing space", `{"type":"object"} `, ""},
		{"inline bare word", "hello", "schema is not valid JSON: invalid character 'h' looking for beginning of value"},
		{"inline bare type name", "string", "schema is not valid JSON: invalid character 's' looking for beginning of value"},
		{"inline truncated", "{", "schema is not valid JSON: unexpected end of JSON input"},
		{"missing file with .json", missing, `failed to read schema file "` + missing + `": open ` + missing + `: no such file or directory`},
		{"missing relative .json", "nope.json", `failed to read schema file "nope.json": open nope.json: no such file or directory`},
		{"missing with separator", "a/b", `failed to read schema file "a/b": open a/b: no such file or directory`},
		{"missing with backslash", `a\b`, `failed to read schema file "a\\b": open a\b: no such file or directory`},
		{"directory", dir, `failed to read schema file "` + dir + `": read ` + dir + `: is a directory`},
		{"file that is not json", notJSON, "schema is not valid JSON: invalid character 'o' in literal null (expecting 'u')"},
		{"file with array root", rootArray, `schema root "type" must be "object", got array`},
		{"file with no type", noType, `schema root must specify "type": "object"`},
		{"inline array root", `{"type":"array"}`, `schema root "type" must be "object", got array`},
		{"inline wrong case", `{"type":"Object"}`, `schema root "type" must be "object", got Object`},
		{"inline null", "null", `schema root must be a JSON object with "type": "object"`},
		{"inline array", "[]", `schema root must be a JSON object with "type": "object"`},
		{"inline number", "123", `schema root must be a JSON object with "type": "object"`},
		{"inline string", `"object"`, `schema root must be a JSON object with "type": "object"`},
		{"inline empty object", "{}", `schema root must specify "type": "object"`},
	}
	for _, c := range cases {
		got, err := resolveJSONSchema(c.value)
		switch {
		case c.want == "":
			if err != nil {
				t.Errorf("%s: unexpected error %v", c.name, err)
			} else if len(got) == 0 {
				t.Errorf("%s: empty schema bytes", c.name)
			}
		default:
			if err == nil {
				t.Errorf("%s: expected error %q, got none", c.name, c.want)
			} else if err.Error() != c.want {
				t.Errorf("%s:\n got  %q\n want %q", c.name, err.Error(), c.want)
			}
		}
	}
}

// TestJSONSchemaPathLike pins the file-vs-inline discriminator: a value that
// cannot be read is reported as a bad path only when its *name* looks like one.
func TestJSONSchemaPathLike(t *testing.T) {
	pathLike := []string{"a/b", "./hello", "x/", "/", "/tmp", `a\b`, `C:\x.json`,
		"nope.json", ".json", "x.JSON", "X.JsOn", "dir.json", "x.json/"}
	inline := []string{"hello", "string", "object", "hello.txt", "x.yaml", "hello.",
		"h.json.bak", "hello world", "x.json5", "schemalike", "plainjson", "x. json"}
	for _, v := range pathLike {
		if !jsonSchemaPathLike(v) {
			t.Errorf("jsonSchemaPathLike(%q) = false, want true", v)
		}
	}
	for _, v := range inline {
		if jsonSchemaPathLike(v) {
			t.Errorf("jsonSchemaPathLike(%q) = true, want false", v)
		}
	}
}
