// documented_test.go — the documented `mcpServers` entry on the wire.
//
// The write shape is documented.go's: the documented members, the
// documented types, and the documented omit rules. The reader's shape is
// agyconfig.go's `ServerFile`, whose json tags carry the same member
// names. The two are pinned against each other here, so the writer and
// the reader cannot drift apart.
package mcp

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// documentedMemberNames are the members the official documentation lists
// for one server entry (official-docs/mcp.md, "MCP configuration
// properties"). The order is the documentation's.
var documentedMemberNames = []string{
	"command",
	"args",
	"env",
	"cwd",
	"serverUrl",
	"headers",
	"authProviderType",
	"oauth",
	"disabled",
	"enabledTools",
	"disabledTools",
	"timeoutSeconds",
}

// referenceMemberNames are the members of the reference binary's own
// `mcp.json` entry. They are read, and they are never written into the
// documented file.
var referenceMemberNames = []string{"type", "url", "enabled", "alwaysLoad"}

// fullDocumentedEntry is an entry with every documented member set.
func fullDocumentedEntry() ServerFile {
	return ServerFile{
		Command:          "node",
		Args:             []string{"server.js"},
		Env:              map[string]string{"TOKEN": "xxx"},
		CWD:              "/work",
		ServerURL:        "https://example.com/mcp",
		Headers:          map[string]string{"Authorization": "Bearer TOKEN"},
		AuthProviderType: "google_credentials",
		OAuth:            map[string]any{"clientId": "cid"},
		Disabled:         true,
		EnabledTools:     []string{"a"},
		DisabledTools:    []string{"b"},
		TimeoutSeconds:   42,
	}
}

// TestDocumentedMembersMatchTheReaderTags: the write shape and the read
// shape carry the same documented members, with the same types.
//
// The write shape is a map of literals and the read shape is a struct of
// json tags; the two are two representations of one schema, and this is
// the pin that keeps them equal.
func TestDocumentedMembersMatchTheReaderTags(t *testing.T) {
	members := fullDocumentedEntry().DocumentedMembers()

	tags := map[string]string{}
	typ := reflect.TypeOf(ServerFile{})
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		name := strings.Split(field.Tag.Get("json"), ",")[0]
		if name == "" || name == "-" {
			continue
		}
		tags[name] = field.Tag.Get("json")
	}

	for _, name := range documentedMemberNames {
		value, ok := members[name]
		if !ok {
			t.Errorf("the writer does not emit the documented member %q", name)
			continue
		}
		tag, ok := tags[name]
		if !ok {
			t.Errorf("the reader has no field for the documented member %q", name)
			continue
		}
		want := reflect.TypeOf(value)
		got, ok := readerFieldType(tags, name)
		if !ok {
			continue
		}
		if want != got {
			t.Errorf("the member %q is a %s in the writer and a %s in the reader (tag %q)",
				name, want, got, tag)
		}
	}

	for _, name := range referenceMemberNames {
		if _, ok := members[name]; ok {
			t.Errorf("the writer emits the reference's own member %q, which the documented schema does not have", name)
		}
	}
}

// readerFieldType returns the Go type of the reader's field for a json
// member name.
func readerFieldType(tags map[string]string, name string) (reflect.Type, bool) {
	typ := reflect.TypeOf(ServerFile{})
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if strings.Split(field.Tag.Get("json"), ",")[0] == name {
			return field.Type, true
		}
	}
	return nil, false
}

// TestDocumentedMembersOmitTheUnset: a member the user did not set is
// absent from the document — not null, not an empty object, not an empty
// array.
//
// The `disabled` flag is the one member that is always written: the
// reference always writes the flag that says the same thing.
func TestDocumentedMembersOmitTheUnset(t *testing.T) {
	members := ServerFile{}.DocumentedMembers()
	if len(members) != 1 {
		t.Fatalf("an empty entry writes %d members (%v), want only the disabled flag", len(members), members)
	}
	disabled, ok := members["disabled"]
	if !ok {
		t.Fatalf("the disabled flag is not written: %v", members)
	}
	if disabled != false {
		t.Errorf("disabled = %#v, want false", disabled)
	}
	for _, name := range documentedMemberNames {
		if name == "disabled" {
			continue
		}
		if _, ok := members[name]; ok {
			t.Errorf("the member %q is written although it was not set", name)
		}
	}
}

// TestDocumentedMembersWriteEnvAndHeadersAsObjects: `env` and `headers`
// are the documented key-value objects, never the array of "KEY=value"
// strings an older build of the clone wrote.
func TestDocumentedMembersWriteEnvAndHeadersAsObjects(t *testing.T) {
	members := fullDocumentedEntry().DocumentedMembers()
	blob, err := json.Marshal(members)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(blob, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	env, ok := decoded["env"].(map[string]any)
	if !ok {
		t.Fatalf("env is not an object: %#v", decoded["env"])
	}
	if env["TOKEN"] != "xxx" {
		t.Errorf("env[TOKEN] = %#v, want xxx", env["TOKEN"])
	}
	headers, ok := decoded["headers"].(map[string]any)
	if !ok {
		t.Fatalf("headers is not an object: %#v", decoded["headers"])
	}
	if headers["Authorization"] != "Bearer TOKEN" {
		t.Errorf("headers[Authorization] = %#v, want the header", headers["Authorization"])
	}
	oauth, ok := decoded["oauth"].(map[string]any)
	if !ok {
		t.Fatalf("oauth is not an object: %#v", decoded["oauth"])
	}
	if oauth["clientId"] != "cid" {
		t.Errorf("oauth[clientId] = %#v, want cid", oauth["clientId"])
	}
}

// TestDocumentedEntryRoundTrip: an entry written with the six documented
// members reads back with the same members and the same types.
func TestDocumentedEntryRoundTrip(t *testing.T) {
	entry := fullDocumentedEntry()
	blob, err := json.Marshal(entry.DocumentedMembers())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back ServerFile
	if err := json.Unmarshal(blob, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(entry, back) {
		t.Errorf("the entry did not survive the round trip:\n written %+v\n read    %+v", entry, back)
	}
}

// TestSetDisabledClearsTheMember: clearing the flag removes the member —
// an absent `disabled` flag is the documented default.
func TestSetDisabledClearsTheMember(t *testing.T) {
	entry := map[string]any{"command": "node", "disabled": true}
	SetDisabled(entry, false)
	if _, ok := entry["disabled"]; ok {
		t.Errorf("the disabled flag is still there: %v", entry)
	}
	if entry["command"] != "node" {
		t.Errorf("the other members were touched: %v", entry)
	}
	SetDisabled(entry, true)
	if entry["disabled"] != true {
		t.Errorf("disabled = %#v, want true", entry["disabled"])
	}
}
