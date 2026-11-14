package geminicode

import (
	"encoding/json"
	"testing"
)

// TestCanonicalArgsCollapsesRepeatedKey is the exact payload from the
// 2026-11-14 report: a streamed run_command call whose opening key arrived
// twice. Google answers it with
//
//	400 Invalid JSON payload received. Unknown name "commandLine" at
//	'request.contents[83].parts[1].function_call.args.fields[0]':
//	Repeated map key: 'commandLine' is already set.
//
// and every later turn that replays the call fails on it — whichever model
// the turn is sent to.
func TestCanonicalArgsCollapsesRepeatedKey(t *testing.T) {
	poisoned := json.RawMessage(`{"commandLine": "sed -n '1,80p' internal/features/models/registry.go", ` +
		`"commandLine": "sed -n '1,80p' internal/features/models/registry.go", ` +
		`"toolSummary": "Read registry head", "WaitMsBeforeAsync": 10000}`)

	got := string(CanonicalArgs(poisoned))
	want := `{"commandLine":"sed -n '1,80p' internal/features/models/registry.go",` +
		`"toolSummary":"Read registry head","WaitMsBeforeAsync":10000}`
	if got != want {
		t.Fatalf("CanonicalArgs() =\n  %s\nwant\n  %s", got, want)
	}
	if !json.Valid([]byte(got)) {
		t.Fatalf("CanonicalArgs() produced invalid JSON: %s", got)
	}
}

func TestCanonicalArgsLastValueWins(t *testing.T) {
	got := string(CanonicalArgs(json.RawMessage(`{"a":1,"b":2,"a":3}`)))
	if want := `{"a":3,"b":2}`; got != want {
		t.Fatalf("CanonicalArgs() = %s, want %s", got, want)
	}
}

// A duplicate nested inside an argument object is the same failure upstream,
// so the rewrite recurses.
func TestCanonicalArgsRecurses(t *testing.T) {
	got := string(CanonicalArgs(json.RawMessage(
		`{"outer":{"k":"one","k":"two"},"list":[{"x":1,"x":2}]}`)))
	if want := `{"outer":{"k":"two"},"list":[{"x":2}]}`; got != want {
		t.Fatalf("CanonicalArgs() = %s, want %s", got, want)
	}
}

// Everything that is not a JSON object becomes an empty object: args is a
// protobuf Struct and upstream rejects an array or a scalar the same way it
// rejects a syntax error.
func TestCanonicalArgsRejectsNonObjects(t *testing.T) {
	for _, raw := range []string{
		``,
		`   `,
		`null`,
		`[]`,
		`[{"a":1}]`,
		`"text"`,
		`42`,
		`{"a":1`,
		`{"a":1}{"b":2}`,
		`{bad}`,
	} {
		if got := string(CanonicalArgs(json.RawMessage(raw))); got != `{}` {
			t.Errorf("CanonicalArgs(%q) = %s, want {}", raw, got)
		}
	}
}

// A call that is already canonical keeps its bytes, key order included: the
// reference replays the model's own JSON and this must not rewrite it.
func TestCanonicalArgsLeavesCleanPayloadAlone(t *testing.T) {
	clean := `{"z":1,"a":{"n":[1,2.5,true,null,"s"]},"m":"x"}`
	if got := string(CanonicalArgs(json.RawMessage(clean))); got != clean {
		t.Fatalf("CanonicalArgs(clean) = %s, want the input verbatim", got)
	}
}
