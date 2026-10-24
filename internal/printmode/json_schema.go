package printmode

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
)

// JSONSchemaFormatError is the error returned when --json-schema is used with a non-structured output format.
const JSONSchemaFormatError = "--json-schema can only be used when --output-format is 'json' or 'stream-json'"

// ValidateJSONSchemaFlag validates the --json-schema flag on fs.
func ValidateJSONSchemaFlag(fs *flag.FlagSet) error {
	value := flagStringValue(fs, "json-schema")
	if strings.TrimSpace(value) == "" {
		return nil
	}
	switch PrintOutputFormat(fs) {
	case PrintFormatJSON, PrintFormatStreamJSON:
	default:
		return errors.New("Error: " + JSONSchemaFormatError)
	}
	if _, err := ResolveJSONSchema(value); err != nil {
		return errors.New("Error: invalid --json-schema: " + err.Error())
	}
	return nil
}

// ResolveJSONSchema turns value into schema bytes, reading files or parsing inline JSON.
func ResolveJSONSchema(value string) ([]byte, error) {
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
		return nil, errors.New(`schema root must be a JSON object with "type": "object"`)
	}
	t, ok := obj["type"]
	if !ok {
		return nil, errors.New(`schema root must specify "type": "object"`)
	}
	if s, _ := t.(string); s != "object" {
		return nil, fmt.Errorf(`schema root "type" must be "object", got %v`, t)
	}
	return raw, nil
}

func jsonSchemaPathLike(v string) bool {
	return strings.ContainsAny(v, `/\`) || strings.HasSuffix(strings.ToLower(v), ".json")
}
