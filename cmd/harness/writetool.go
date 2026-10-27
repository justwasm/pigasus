// writetool.go — the reference's write tool, the part the clone's own tool
// does not do.
//
// # What the reference's write tool does
//
// The reference's harness writes a file with its own tool, and that tool
// does two things the clone's catalogue tool does not:
//
//   - it ends the file with a newline — the content it is given is written
//     with one, and a content that already ends with one does not get a
//     second;
//   - it appends when the call says so (`Append`), and the file's own
//     content is left alone.
//
// Both were measured against the reference binary: a write of `x` leaves a
// file of two bytes (`x\n`), a write of `x\n` leaves the same two bytes, and
// a write of `x` followed by an append of `y` leaves `x\ny\n`.
//
// The reference's *edit* tool does not add a newline — an edit of a file
// that ends without one leaves it that way — so the compensation is the
// write tool's alone.
//
// # Why the compensation lives here
//
// The clone's catalogue tool writes the content as it is and refuses to
// touch a file that exists, so the harness does the reference's part here,
// before the call reaches the catalogue. The catalogue is shared with the
// CLI, whose own reference has a different write tool; the harness's
// reference is this one.
package main

import (
	"encoding/json"
	"strings"
)

// writeToolNames are the names the write tool answers to.
//
// The harness's own name is `write_to_file`; the catalogue's names are
// kept so a session that advertises one of them behaves the same way.
func writeToolNames(name string) bool {
	switch name {
	case "write_to_file", "write_file", "create_file":
		return true
	}
	return false
}

// writeArgs rewrites a write call's arguments the way the reference's write
// tool takes them.
//
// before is the file's content before the call — the append needs it. The
// rewritten arguments are the canonical ones the catalogue tool reads:
// `content`, `overwrite` and `append`.
func (t *harnessTool) writeArgs(args json.RawMessage, before string) json.RawMessage {
	if !writeToolNames(t.inner.Name()) {
		return args
	}
	return rewriteWriteArgs(args, before)
}

// rewriteWriteArgs does the rewrite itself.
func rewriteWriteArgs(args json.RawMessage, before string) json.RawMessage {
	var fields map[string]json.RawMessage
	if len(args) == 0 || json.Unmarshal(args, &fields) != nil || fields == nil {
		return args
	}

	key, content, ok := stringField(fields, "CodeContent", "Content", "code_content", "content")
	if !ok {
		return args
	}
	if content != "" && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	if boolField(fields, "Append", "append") {
		// The reference's append leaves the file's own content alone and
		// puts the new content behind it.
		content = before + content
		setField(fields, "overwrite", "Overwrite", json.RawMessage("true"))
	}
	encoded, err := json.Marshal(content)
	if err != nil {
		return args
	}
	setField(fields, "content", key, encoded)
	dropField(fields, "CodeContent", "Content", "code_content")

	out, err := json.Marshal(fields)
	if err != nil {
		return args
	}
	return out
}

// stringField returns the first string-valued key among names.
func stringField(fields map[string]json.RawMessage, names ...string) (string, string, bool) {
	for _, name := range names {
		raw, ok := fields[name]
		if !ok {
			continue
		}
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			continue
		}
		return name, value, true
	}
	return "", "", false
}

// boolField reports whether one of the names carries a true boolean.
func boolField(fields map[string]json.RawMessage, names ...string) bool {
	for _, name := range names {
		raw, ok := fields[name]
		if !ok {
			continue
		}
		var value bool
		if err := json.Unmarshal(raw, &value); err != nil {
			continue
		}
		if value {
			return true
		}
	}
	return false
}

// setField writes a value under the canonical key and drops the other
// spellings.
//
// The catalogue's canonicalisation turns every key into snake_case, so two
// spellings of one argument would race: the last one the map walk reaches
// wins. One key is left.
func setField(fields map[string]json.RawMessage, canonical string, replace string, value json.RawMessage) {
	fields[canonical] = value
	if replace != canonical {
		delete(fields, replace)
	}
}

// dropField removes the spellings of an argument.
func dropField(fields map[string]json.RawMessage, names ...string) {
	for _, name := range names {
		delete(fields, name)
	}
}
