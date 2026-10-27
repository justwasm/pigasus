// ls_unmarshal.go: the reference's answer for a malformed request.
//
// 【一手】 2026-10-27, the reference's own answers:
//
//	POST …/CreateTerminal {"workspaceUri":123}
//	  → 400 {"code":"invalid_argument",
//	         "message":"unmarshal message: unmarshal into
//	           *language_server_go_proto.CreateTerminalRequest: proto:
//	           (line 1:18): invalid value for string field workspaceUri: 123"}
//
//	POST …/SendTerminalInput {"terminalId":"x","input":"hello"}
//	  → 400 {"code":"invalid_argument",
//	         "message":"unmarshal message: unmarshal into
//	           *language_server_go_proto.SendTerminalInputRequest: proto:
//	           (line 1:52): invalid value for bytes field input: \"hello\""}
//
// The clone's answer keeps the shape: the same prefix, the same Go
// type name, the same "invalid value for <kind> field <name>: <value>"
// tail. The position ("(line 1:18)") is the one part the clone does
// not reproduce — the Go decoder does not report where in the payload
// the offending value sits.
package remotecontrol

import (
	"encoding/json"
	"strconv"
	"strings"
)

// lsProtoTypeName is the Go type the reference's generated messages
// carry ("*language_server_go_proto.CreateTerminalRequest").
const lsProtoTypeName = "*language_server_go_proto."

// lsUnmarshalError builds the reference's message for a request the
// decoder refused.
//
// typeName is the message's own name ("CreateTerminalRequest"); err
// is the decoder's error; raw is the request payload, used to find
// the value the decoder refused.
func lsUnmarshalError(typeName string, err error, raw []byte) string {
	message := "unmarshal message: unmarshal into " + lsProtoTypeName + typeName + ": "
	var typeErr *json.UnmarshalTypeError
	if !asUnmarshalTypeError(err, &typeErr) {
		return message + "proto: " + err.Error()
	}
	field := typeErr.Field
	value := lsRawFieldValue(raw, field)
	if value == "" {
		value = typeErr.Value
	}
	return message + "proto: invalid value for " + lsProtoKind(typeErr) +
		" field " + field + ": " + value
}

// asUnmarshalTypeError unwraps a decoder type error.
func asUnmarshalTypeError(err error, target **json.UnmarshalTypeError) bool {
	for err != nil {
		if typeErr, ok := err.(*json.UnmarshalTypeError); ok {
			*target = typeErr
			return true
		}
		unwrapper, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = unwrapper.Unwrap()
	}
	return false
}

// lsProtoKind names the proto kind the way the reference's message
// names it.
func lsProtoKind(typeErr *json.UnmarshalTypeError) string {
	switch typeErr.Type.Kind().String() {
	case "string":
		return "string"
	case "bool":
		return "bool"
	case "int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64":
		return "int64"
	case "float32", "float64":
		return "double"
	case "slice":
		return "bytes"
	case "map", "struct":
		return "message"
	}
	return "string"
}

// lsInvalidBytesField builds the reference's message for a bytes
// field whose payload is not base64.
//
// 【一手】 2026-10-27: the reference answers `{"input":"hello"}`
// with "… invalid value for bytes field input: \"hello\"".
func lsInvalidBytesField(typeName, field, value string) string {
	return "unmarshal message: unmarshal into " + lsProtoTypeName + typeName +
		": proto: invalid value for bytes field " + field + ": " + strconv.Quote(value)
}

// lsRawFieldValue finds the value the payload carries for a field.
//
// The decoder's error names the field but not its value; the payload
// still holds it.
func lsRawFieldValue(raw []byte, field string) string {
	if field == "" || len(raw) == 0 {
		return ""
	}
	needle := `"` + field + `"`
	idx := strings.Index(string(raw), needle)
	if idx < 0 {
		return ""
	}
	rest := strings.TrimLeft(string(raw[idx+len(needle):]), " \t\r\n")
	if !strings.HasPrefix(rest, ":") {
		return ""
	}
	rest = strings.TrimLeft(rest[1:], " \t\r\n")
	if rest == "" {
		return ""
	}
	switch rest[0] {
	case '"':
		end := strings.Index(rest[1:], `"`)
		if end < 0 {
			return ""
		}
		return rest[:end+2]
	case '{', '[':
		return ""
	default:
		end := strings.IndexAny(rest, ",}] \t\r\n")
		if end < 0 {
			return rest
		}
		return rest[:end]
	}
}
