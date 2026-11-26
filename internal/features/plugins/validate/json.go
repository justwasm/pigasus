package validate

import (
	"encoding/json"
	"fmt"
	"strings"
)

// jsonPosMessage renders a JSON parse error the way the reference does.
func jsonPosMessage(raw []byte, err error) string {
	if err.Error() == "unexpected end of JSON input" {
		line, col := jsonPosition(raw, len(raw)+1)
		return fmt.Sprintf("line %d, column %d: %s: unexpected EOF", line, col, jsonEOFContext(raw))
	}
	se, ok := err.(*json.SyntaxError)
	if !ok {
		return err.Error()
	}
	off := int(se.Offset)
	if off > len(raw) {
		off = len(raw)
	}
	line, col := jsonPosition(raw, off)
	start := off
	if start > 0 {
		start--
	}
	for start < len(raw) && !isJSONIdentByte(raw[start]) {
		start++
	}
	end := start
	for end < len(raw) && isJSONIdentByte(raw[end]) {
		end++
	}
	if start < end {
		return fmt.Sprintf("line %d, column %d: invalid literal: %s", line, col, string(raw[start:end]))
	}
	return fmt.Sprintf("line %d, column %d: %s", line, col, err.Error())
}

// jsonPosition converts a 1-based byte offset to line/column.
func jsonPosition(raw []byte, off int) (line, col int) {
	line, col = 1, 1
	for i := 0; i < off-1 && i < len(raw); i++ {
		if raw[i] == '\n' {
			line++
			col = 1
		} else {
			col++
		}
	}
	return line, col
}

type jsonEOFFrame struct {
	kind       byte
	afterValue bool
}

// jsonEOFContext walks the document far enough to name what the reference
// parser was reading when the input ended.
func jsonEOFContext(raw []byte) string {
	var stack []jsonEOFFrame
	i := 0
	for i < len(raw) {
		switch c := raw[i]; c {
		case ' ', '\t', '\n', '\r':
			i++
		case '"':
			var closed bool
			i, closed = scanJSONString(raw, i+1)
			if !closed {
				return "parsing string"
			}
			setTopAfterValue(stack, true)
		case '{', '[':
			stack = append(stack, jsonEOFFrame{kind: c})
			i++
		case '}', ']':
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			setTopAfterValue(stack, true)
			i++
		case ':', ',':
			setTopAfterValue(stack, false)
			i++
		default:
			i = scanJSONToken(raw, i)
			setTopAfterValue(stack, true)
		}
	}
	return topEOFContext(stack)
}

func setTopAfterValue(stack []jsonEOFFrame, val bool) {
	if len(stack) > 0 {
		stack[len(stack)-1].afterValue = val
	}
}

func topEOFContext(stack []jsonEOFFrame) string {
	if len(stack) == 0 {
		return "parsing value"
	}
	top := stack[len(stack)-1]
	if top.afterValue {
		if top.kind == '{' {
			return "parsing object after value"
		}
		return "parsing array after value"
	}
	return "parsing value"
}

func scanJSONString(raw []byte, i int) (int, bool) {
	for i < len(raw) {
		if raw[i] == '\\' {
			i += 2
			continue
		}
		if raw[i] == '"' {
			return i + 1, true
		}
		i++
	}
	return i, false
}

func scanJSONToken(raw []byte, i int) int {
	for i < len(raw) && !strings.ContainsRune(" \t\n\r,]}:", rune(raw[i])) {
		i++
	}
	return i
}

func isJSONIdentByte(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '_' || b == '.' || b == '-'
}
