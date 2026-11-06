package tools

import (
	"encoding/json"
	"fmt"
)

// RenderResult flattens a tool's return value into a formatted text string.
func RenderResult(out any) string {
	switch value := out.(type) {
	case nil:
		return ""
	case string:
		return value
	case []byte:
		return string(value)
	case json.RawMessage:
		return string(value)
	}
	encoded, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return fmt.Sprintf("%v", out)
	}
	return string(encoded)
}
