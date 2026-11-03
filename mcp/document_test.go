// document_test.go — the writable MCP configuration file.
//
// The document owns the file's bytes: the documented `mcpServers` root
// member is the writer's own, every other root member survives a write,
// and a file the writer cannot parse is refused rather than overwritten.
//
// 【一手】 every expectation below was observed against the reference
// binary (1.4.12) under a temporary HOME.
package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// writeDocument writes a configuration file and returns its path.
func writeDocument(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "mcp_config.json")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write the document: %v", err)
	}
	return path
}

// readDocument reads a configuration file back as a raw JSON object.
func readDocument(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the document: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse the document: %v (%s)", err, raw)
	}
	return doc
}

// TestLoadDocumentKeepsTheRootMembersTheWriterDoesNotOwn: the root
// members survive a round trip.
//
// 【一手】 the reference binary (1.4.12): a file that carried the
// reference binary's own `servers` root, a `disableAllMCP` switch and an
// unknown root member kept every one of them through an `agy mcp add`.
func TestLoadDocumentKeepsTheRootMembersTheWriterDoesNotOwn(t *testing.T) {
	path := writeDocument(t, `{"mcpServers":{"a":{"command":"c"}},
		"servers":{"other":{"type":"stdio","command":"x","enabled":true}},
		"disableAllMCP":false,
		"unknownRoot":{"keep":true}}`)

	doc, err := LoadDocument(path)
	if err != nil {
		t.Fatalf("LoadDocument: %v", err)
	}
	out := filepath.Join(t.TempDir(), "mcp_config.json")
	if err := doc.Save(out); err != nil {
		t.Fatalf("Save: %v", err)
	}

	written := readDocument(t, out)
	for _, name := range []string{"servers", "disableAllMCP", "unknownRoot"} {
		if _, ok := written[name]; !ok {
			t.Errorf("the root member %q was dropped: %+v", name, written)
		}
	}
	keep, ok := written["unknownRoot"].(map[string]any)
	if !ok {
		t.Fatalf("the unknown root member is not an object: %+v", written)
	}
	if keep["keep"] != true {
		t.Errorf("unknownRoot = %#v, want the user's own member", keep)
	}
}

// TestLoadDocumentRefusesAMalformedFile: a file the writer cannot parse
// is an error, and no document comes back.
//
// 【一手】 the reference binary (1.4.12): `agy mcp add` on a malformed
// file printed
//
//	Error: failed to parse <path>: failed to parse JSON config: invalid character 'n' looking for beginning of object key string
//
// and left the file alone.
func TestLoadDocumentRefusesAMalformedFile(t *testing.T) {
	path := writeDocument(t, "{ not json")
	_, err := LoadDocument(path)
	if err == nil {
		t.Fatal("LoadDocument: want the parse error")
	}
	want := "failed to parse JSON config: invalid character 'n' looking for beginning of object key string"
	if got := err.Error(); got != want {
		t.Errorf("the error is %q, want %q", got, want)
	}
}

// TestLoadDocumentReadsAnEmptyFileAsNoServers: a file that carries no
// content is the "no servers configured" state, not a broken one.
//
// 【一手】 the reference binary (1.4.12): `agy mcp list` on an empty
// `mcp_config.json` printed "No MCP servers configured." and exited 0.
func TestLoadDocumentReadsAnEmptyFileAsNoServers(t *testing.T) {
	for _, body := range []string{"", "   \n"} {
		path := writeDocument(t, body)
		doc, err := LoadDocument(path)
		if err != nil {
			t.Fatalf("LoadDocument(%q): %v", body, err)
		}
		if len(doc.Servers) != 0 {
			t.Errorf("LoadDocument(%q): servers = %+v, want none", body, doc.Servers)
		}
	}
}

// TestLoadDocumentReadsAMissingFileAsNoServers: a file that does not
// exist is the "no servers configured" state.
func TestLoadDocumentReadsAMissingFileAsNoServers(t *testing.T) {
	doc, err := LoadDocument(filepath.Join(t.TempDir(), "mcp_config.json"))
	if err != nil {
		t.Fatalf("LoadDocument: %v", err)
	}
	if len(doc.Servers) != 0 {
		t.Errorf("servers = %+v, want none", doc.Servers)
	}
}

// TestDocumentSaveWritesTheReferenceBytes: the file's bytes are the
// reference's own — two-space indentation, the members sorted by name,
// and a trailing newline.
//
// 【一手】 the reference binary (1.4.12): `agy mcp add` wrote
//
//	{
//	  "mcpServers": {
//	    "gh": {
//	      "args": [
//	        "server.js"
//	      ],
//	      "command": "node",
//	      "disabled": false
//	    }
//	  }
//	}
//
// with a trailing newline.
func TestDocumentSaveWritesTheReferenceBytes(t *testing.T) {
	doc := NewDocument()
	doc.Servers["gh"] = map[string]any{
		"command":  "node",
		"args":     []string{"server.js"},
		"disabled": false,
	}
	out := filepath.Join(t.TempDir(), "mcp_config.json")
	if err := doc.Save(out); err != nil {
		t.Fatalf("Save: %v", err)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read the document: %v", err)
	}
	want := `{
  "mcpServers": {
    "gh": {
      "args": [
        "server.js"
      ],
      "command": "node",
      "disabled": false
    }
  }
}
`
	if string(raw) != want {
		t.Errorf("the file is\n%q\nwant\n%q", raw, want)
	}
}

// TestDocumentSaveKeepsTheEntriesItDoesNotOwn: the entries the writer did
// not touch keep their members, including the ones the documented schema
// does not list.
func TestDocumentSaveKeepsTheEntriesItDoesNotOwn(t *testing.T) {
	path := writeDocument(t, `{"mcpServers":{
		"sibling":{"command":"sib","cwd":"/other","futureMember":{"nested":true}}}}`)

	doc, err := LoadDocument(path)
	if err != nil {
		t.Fatalf("LoadDocument: %v", err)
	}
	doc.Servers["gh"] = map[string]any{"command": "node"}
	out := filepath.Join(t.TempDir(), "mcp_config.json")
	if err := doc.Save(out); err != nil {
		t.Fatalf("Save: %v", err)
	}

	written := readDocument(t, out)
	servers := written["mcpServers"].(map[string]any)
	sibling := servers["sibling"].(map[string]any)
	if sibling["command"] != "sib" || sibling["cwd"] != "/other" {
		t.Errorf("the sibling entry lost a member: %+v", sibling)
	}
	future, ok := sibling["futureMember"].(map[string]any)
	if !ok {
		t.Fatalf("the sibling entry lost its unknown member: %+v", sibling)
	}
	if future["nested"] != true {
		t.Errorf("futureMember = %#v, want the user's own member", future)
	}
}

// TestSetServerDisabledWritesTheFlag: suspending a server writes the
// documented `disabled` flag, and a server the document does not know is
// created on the spot.
//
// 【一手】 the reference binary (1.4.12): `agy mcp disable zzz` on a file
// that did not know `zzz` wrote the entry `{"disabled": true}` and
// exited 0.
func TestSetServerDisabledWritesTheFlag(t *testing.T) {
	doc := NewDocument()
	doc.SetServerDisabled("zzz", true)
	entry, ok := doc.Servers["zzz"]
	if !ok {
		t.Fatalf("the entry was not created: %+v", doc.Servers)
	}
	if entry["disabled"] != true {
		t.Errorf("disabled = %#v, want true", entry["disabled"])
	}
}

// TestSetServerDisabledClearsTheFlagAndDropsTheEntry: resuming a server
// removes the documented `disabled` flag, and a server whose entry
// carries nothing else is dropped.
//
// 【一手】 the reference binary (1.4.12): `agy mcp enable` on an entry
// that carried only `{"disabled": true}` removed the entry; on an entry
// that carried a member too, it kept the member and removed the flag.
func TestSetServerDisabledClearsTheFlagAndDropsTheEntry(t *testing.T) {
	doc := NewDocument()
	doc.Servers["only"] = map[string]any{"disabled": true}
	doc.Servers["withMore"] = map[string]any{
		"command": "c", "disabled": true, "futureMember": map[string]any{"nested": true},
	}

	doc.SetServerDisabled("only", false)
	if _, ok := doc.Servers["only"]; ok {
		t.Errorf("the emptied entry survived: %+v", doc.Servers)
	}

	doc.SetServerDisabled("withMore", false)
	entry, ok := doc.Servers["withMore"]
	if !ok {
		t.Fatalf("the entry is gone: %+v", doc.Servers)
	}
	if _, ok := entry["disabled"]; ok {
		t.Errorf("the disabled flag survived: %+v", entry)
	}
	if entry["command"] != "c" {
		t.Errorf("the other members were touched: %+v", entry)
	}
	if !reflect.DeepEqual(entry["futureMember"], map[string]any{"nested": true}) {
		t.Errorf("the unknown member was dropped: %+v", entry)
	}
}

// TestSetServerDisabledLeavesAnUnknownServerAlone: resuming a server the
// document does not know is a no-op.
//
// 【一手】 the reference binary (1.4.12): `agy mcp enable zzz` on a file
// that did not know `zzz` printed "Enabled MCP server \"zzz\"" and left
// the file without the entry.
func TestSetServerDisabledLeavesAnUnknownServerAlone(t *testing.T) {
	doc := NewDocument()
	doc.SetServerDisabled("zzz", false)
	if _, ok := doc.Servers["zzz"]; ok {
		t.Errorf("the entry was created: %+v", doc.Servers)
	}
}
