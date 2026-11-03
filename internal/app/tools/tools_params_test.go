package tools

import (
	"testing"
)

// Cycle 133: case-insensitive JSON key unmarshaling for tool
// arguments. The recovered v1.3.6 wire format uses capitalised
// keys (AbsolutePath, TargetFile, CommandLine, Query, OldString,
// NewString, Content, etc.); the cycle-72 catalog decodes lowercase
// keys. The canonicalisation pre-pass in ArgsOf() closes the gap.
func TestArgsOf_CaseInsensitiveKeys(t *testing.T) {
	type p struct {
		Path    string `json:"path"`
		Command string `json:"command"`
		Query   string `json:"query"`
	}
	// All capitalised keys — the recovered wire format.
	raw := []byte(`{"AbsolutePath":"foo.txt","CommandLine":"ls","Query":"bar"}`)
	var got p
	if err := ArgsOf(raw, &got); err != nil {
		t.Fatalf("ArgsOf: %v", err)
	}
	if got.Path != "foo.txt" || got.Command != "ls" || got.Query != "bar" {
		t.Errorf("capitalised keys did not decode: %+v", got)
	}
	// Lowercase still works (cycle-72 path).
	raw3 := []byte(`{"path":"foo.txt","command":"ls","query":"bar"}`)
	var got3 p
	if err := ArgsOf(raw3, &got3); err != nil {
		t.Fatalf("ArgsOf: %v", err)
	}
	if got3.Path != "foo.txt" || got3.Command != "ls" || got3.Query != "bar" {
		t.Errorf("lowercase keys did not decode: %+v", got3)
	}
	// Nested objects are recursively canonicalised.
	type inner struct {
		OldStr string `json:"oldString"`
		NewStr string `json:"newString"`
	}
	type outer struct {
		Path  string `json:"path"`
		Inner inner  `json:"edit"`
	}
	raw4 := []byte(`{"AbsolutePath":"foo.txt","edit":{"OldString":"a","NewString":"b"}}`)
	var got4 outer
	if err := ArgsOf(raw4, &got4); err != nil {
		t.Fatalf("ArgsOf nested: %v", err)
	}
	if got4.Path != "foo.txt" || got4.Inner.OldStr != "a" || got4.Inner.NewStr != "b" {
		t.Errorf("nested capitalised keys did not decode: %+v", got4)
	}
	// Empty payload still produces a default-initialised value.
	var got5 p
	if err := ArgsOf(nil, &got5); err != nil {
		t.Fatalf("ArgsOf nil: %v", err)
	}
	if got5.Path != "" {
		t.Errorf("nil payload should leave fields zero, got %+v", got5)
	}
}
