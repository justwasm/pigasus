// json_schema.go — cycle 118: `--json-schema` validation.
//
// Changelog 1.3.4: "Changed `--json-schema` to reject plain text, bare type
// names such as `string`, and missing schema files instead of silently treating
// them as a string schema; these inputs, and any schema whose root is not
// `"type": "object"`, now fail at startup with an error and exit code `1`."
//
// The reference also refuses the flag outside the structured output formats.
// Every message below is captured verbatim from 1.3.6 on this machine:
//
//	$ agy -p=x --json-schema=hello
//	Error: --json-schema can only be used when --output-format is 'json' or 'stream-json'
//
//	$ agy -p=x --output-format json --json-schema=hello
//	Error: invalid --json-schema: schema is not valid JSON: invalid character 'h' looking for beginning of value
//
//	$ agy -p=x --output-format json --json-schema=/nonexistent/s.json
//	Error: invalid --json-schema: failed to read schema file "/nonexistent/s.json": open /nonexistent/s.json: no such file or directory
//
//	$ agy -p=x --output-format json --json-schema='{"type":"array"}'
//	Error: invalid --json-schema: schema root "type" must be "object", got array
//
//	$ agy -p=x --output-format json --json-schema='{"properties":{}}'
//	Error: invalid --json-schema: schema root must specify "type": "object"
//
//	$ agy -p=x --output-format json --json-schema='[]'
//	Error: invalid --json-schema: schema root must be a JSON object with "type": "object"
//
// All of them exit 1, and the message goes to stderr with a trailing newline.
// The check runs before the agent check and before --model/--effort validation,
// and it applies to the interactive path too.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
)

// jsonSchemaFormatError is the recovered rejection for using the flag with a
// non-structured output format. Note the capital `Error: ` prefix the caller
// adds, which differs from the lowercase `error: ` used by the slash-command
// rejections.
const jsonSchemaFormatError = "--json-schema can only be used when --output-format is 'json' or 'stream-json'"

// validateJSONSchemaFlag applies both recovered checks. It returns nil when
// --json-schema is unset (or whitespace-only, which the reference also treats as
// unset) and otherwise the fully-formed error text, `Error: ` prefix included.
func validateJSONSchemaFlag(fs *flag.FlagSet) error {
	value := flagStringValue(fs, "json-schema")
	if strings.TrimSpace(value) == "" {
		// `--json-schema=` and `--json-schema='  '` are both accepted: the
		// reference proceeds to the model in each case.
		return nil
	}
	switch printOutputFormat(fs) {
	case printFormatJSON, printFormatStreamJSON:
	default:
		return errors.New("Error: " + jsonSchemaFormatError)
	}
	if _, err := resolveJSONSchema(value); err != nil {
		return errors.New("Error: invalid --json-schema: " + err.Error())
	}
	return nil
}

// resolveJSONSchema turns the flag's value into the schema bytes, applying the
// recovered file-vs-inline rule.
//
// The reference tries to read the value as a file first. If that succeeds the
// file's contents are the schema. If it fails, the value is treated as inline
// JSON — unless it *looks* like a path, in which case the read error is
// reported instead of falling back. "Looks like a path" is a name-shape test
// (a separator, or a `.json` suffix ignoring case), not a stat:
//
//	plainjson   exists, no separator, no .json  -> the FILE is used
//	hello       missing, no separator           -> parsed as inline JSON
//	nope.json   missing, .json suffix           -> "failed to read schema file"
//	a/b         missing, separator              -> "failed to read schema file"
//	x.json5     missing, neither                -> parsed as inline JSON
func resolveJSONSchema(value string) ([]byte, error) {
	raw := []byte(value)
	if b, err := os.ReadFile(value); err == nil {
		raw = b
	} else if jsonSchemaPathLike(value) {
		return nil, fmt.Errorf("failed to read schema file %q: %w", value, err)
	}

	var root any
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil, fmt.Errorf("schema is not valid JSON: %w", err)
	}
	obj, ok := root.(map[string]any)
	if !ok {
		// Covers null, arrays, numbers and strings.
		return nil, errors.New(`schema root must be a JSON object with "type": "object"`)
	}
	t, ok := obj["type"]
	if !ok {
		return nil, errors.New(`schema root must specify "type": "object"`)
	}
	if s, _ := t.(string); s != "object" {
		// `%v` renders the offending value: `array`, `Object`, `123`, …
		return nil, fmt.Errorf(`schema root "type" must be "object", got %v`, t)
	}
	return raw, nil
}

// jsonSchemaPathLike reports whether a value the reference could not read as a
// file should be reported as a bad path rather than parsed as inline JSON.
func jsonSchemaPathLike(v string) bool {
	return strings.ContainsAny(v, `/\`) || strings.HasSuffix(strings.ToLower(v), ".json")
}
