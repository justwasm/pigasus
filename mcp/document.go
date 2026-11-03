// document.go — the writable form of a documented MCP configuration file.
//
// The file is the user's own: the writer maintains the documented
// `mcpServers` root member, and every other root member survives a write.
//
// The evidence
// ------------
//
// 【一手】 the reference binary (1.4.12), under a temporary HOME:
//
//   - A file that carried the `mcpServers` root, the reference binary's
//     own `servers` root, a `disableAllMCP` switch and an unknown root
//     member survived an `agy mcp add` with every one of them intact.
//     Only the entry the command named was replaced; a sibling entry
//     kept its own members, including one the documented schema does not
//     list.
//   - A malformed file is refused rather than overwritten:
//
//     Error: failed to parse <path>: failed to parse JSON config: invalid character 'n' looking for beginning of object key string
//
//   - The bytes are the reference's own: two-space indentation, the
//     members sorted by name, a trailing newline, and the escaped form
//     Go's encoder produces (`&` becomes the six characters `\u0026`,
//     `<` becomes `\u003c`).
package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Document is a writable MCP configuration file.
//
// The root members are kept raw, so a member the writer does not own —
// the reference binary's own `servers` root, the `disableAllMCP` switch,
// a member a newer build adds — survives a write.
type Document struct {
	// root holds every root member of the file, raw. The documented
	// `mcpServers` member is not among them: Servers owns it.
	root map[string]json.RawMessage
	// Servers is the documented `mcpServers` root member: one entry per
	// server, and every entry a raw JSON object, so that a member the
	// documented schema does not list survives a write.
	Servers map[string]map[string]any
}

// NewDocument returns an empty document.
func NewDocument() *Document {
	return &Document{
		root:    map[string]json.RawMessage{},
		Servers: map[string]map[string]any{},
	}
}

// LoadDocument reads a configuration file.
//
// A file that does not exist is the "no servers configured" state: the
// document comes back empty and the error is nil. A file that carries no
// content at all is the same state — the reference reads an empty file as
// a file with no servers, not as a broken one.
//
// A file whose JSON is malformed is an error and no document comes back —
// the reference refuses to write over a file it could not read, and its
// error names the file.
func LoadDocument(path string) (*Document, error) {
	doc := NewDocument()
	if path == "" {
		return doc, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return doc, nil
		}
		return nil, err
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return doc, nil
	}
	if err := json.Unmarshal(raw, &doc.root); err != nil {
		return nil, fmt.Errorf("failed to parse JSON config: %w", err)
	}
	if member, ok := doc.root[RootMCPServers]; ok {
		if err := json.Unmarshal(member, &doc.Servers); err != nil {
			return nil, fmt.Errorf("failed to parse JSON config: %w", err)
		}
	}
	if doc.Servers == nil {
		doc.Servers = map[string]map[string]any{}
	}
	return doc, nil
}

// Save writes the document.
//
// The root members are written back sorted by name, with the documented
// `mcpServers` member among them, in the reference's own byte shape:
// two-space indentation and a trailing newline.
func (d *Document) Save(path string) error {
	if path == "" {
		return fmt.Errorf("mcp: no config path")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	root := make(map[string]json.RawMessage, len(d.root)+1)
	for name, member := range d.root {
		root[name] = member
	}
	servers, err := json.Marshal(d.Servers)
	if err != nil {
		return err
	}
	root[RootMCPServers] = servers
	buf, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return err
	}
	buf = append(buf, '\n')
	return os.WriteFile(path, buf, 0o644)
}

// SetServerDisabled suspends a server, and resumes it.
//
// Suspending writes the documented `disabled` flag; resuming removes it,
// and a server whose entry carries nothing else is dropped from the
// document.
//
// A server the document does not know is created on the spot when it is
// suspended, and is left alone when it is resumed.
//
// 【一手】 the reference binary (1.4.12): `agy mcp disable zzz` on a file
// that did not know `zzz` wrote the entry `{"disabled": true}`, and
// `agy mcp enable` on an entry that carried nothing but the flag removed
// the entry altogether.
func (d *Document) SetServerDisabled(name string, disabled bool) {
	entry, ok := d.Servers[name]
	if !ok {
		if !disabled {
			return
		}
		entry = map[string]any{}
		d.Servers[name] = entry
	}
	SetDisabled(entry, disabled)
	if len(entry) == 0 {
		delete(d.Servers, name)
	}
}
