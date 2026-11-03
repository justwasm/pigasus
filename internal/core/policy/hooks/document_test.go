package hooks

import (
	"strings"
	"testing"
)

// TestParseDocumentedShape pins the documented shape: the document maps
// a hook's name onto its event configuration, and the tool events carry
// a matcher and the handlers under a "hooks" key.
func TestParseDocumentedShape(t *testing.T) {
	raw := []byte(`{
	  "my-linter-hook": {
	    "PostToolUse": [
	      {
	        "matcher": "run_command",
	        "hooks": [
	          {"type": "command", "command": "./scripts/lint.sh", "timeout": 10}
	        ]
	      }
	    ]
	  },
	  "reminder": {
	    "PreInvocation": [
	      {"type": "command", "command": "./scripts/reminder.sh"}
	    ]
	  }
	}`)
	entries, problems := Parse(raw)
	if len(problems) != 0 {
		t.Fatalf("Parse reported problems: %v", problems)
	}
	if len(entries) != 2 {
		t.Fatalf("Parse read %d entries, want 2", len(entries))
	}
	lint := entries[0]
	if lint.Hook != "my-linter-hook" || lint.Event != EventPostToolUse {
		t.Errorf("first entry = %+v", lint)
	}
	if lint.Matcher != "run_command" {
		t.Errorf("matcher = %q, want run_command", lint.Matcher)
	}
	if lint.Handler.Command != "./scripts/lint.sh" {
		t.Errorf("command = %q", lint.Handler.Command)
	}
	if lint.Handler.Timeout != 10 {
		t.Errorf("timeout = %d, want 10", lint.Handler.Timeout)
	}
	if got := lint.Handler.TimeoutSeconds(); got != 10 {
		t.Errorf("TimeoutSeconds() = %d, want 10", got)
	}
	reminder := entries[1]
	if reminder.Event != EventPreInvocation {
		t.Errorf("second entry = %+v", reminder)
	}
	if reminder.Handler.TimeoutSeconds() != DefaultTimeoutSeconds {
		t.Errorf("timeout = %d, want the documented default %d",
			reminder.Handler.TimeoutSeconds(), DefaultTimeoutSeconds)
	}
}

// TestParseClaudeCodeShape pins the Claude Code shape, where the events
// sit under one "hooks" key.
func TestParseClaudeCodeShape(t *testing.T) {
	raw := []byte(`{
	  "hooks": {
	    "PreToolUse": [
	      {"matcher": "run_command", "hooks": [{"command": "./check.sh"}]}
	    ]
	  }
	}`)
	entries, problems := Parse(raw)
	if len(problems) != 0 {
		t.Fatalf("Parse reported problems: %v", problems)
	}
	if len(entries) != 1 {
		t.Fatalf("Parse read %d entries, want 1", len(entries))
	}
	if entries[0].Event != EventPreToolUse || entries[0].Matcher != "run_command" {
		t.Errorf("entry = %+v", entries[0])
	}
}

// TestParseLegacyFlatShape pins the legacy flat shape, where a "hooks"
// array lists the handlers with their event.
func TestParseLegacyFlatShape(t *testing.T) {
	raw := []byte(`{"hooks": [{"event": "PreToolUse", "command": "./lint.sh"}]}`)
	entries, problems := Parse(raw)
	if len(problems) != 0 {
		t.Fatalf("Parse reported problems: %v", problems)
	}
	if len(entries) != 1 {
		t.Fatalf("Parse read %d entries, want 1", len(entries))
	}
	if entries[0].Event != EventPreToolUse || entries[0].Handler.Command != "./lint.sh" {
		t.Errorf("entry = %+v", entries[0])
	}
}

// TestParseDisabledHook pins the "enabled" flag: a hook the user turned
// off is not an error and does not load.
func TestParseDisabledHook(t *testing.T) {
	raw := []byte(`{"safety-gate": {"enabled": false, "PreToolUse": [
	  {"hooks": [{"command": "./safety.sh"}]}]}}`)
	entries, problems := Parse(raw)
	if len(problems) != 0 {
		t.Fatalf("Parse reported problems: %v", problems)
	}
	if len(entries) != 0 {
		t.Fatalf("Parse read %d entries from a disabled hook, want 0", len(entries))
	}
}

// TestParseSkipsBrokenEntries pins the loader's tolerance: a broken
// entry is skipped with a reason, and the rest of the document still
// loads.
func TestParseSkipsBrokenEntries(t *testing.T) {
	raw := []byte(`{
	  "no-command": {"PreToolUse": [{"hooks": [{"type": "command"}]}]},
	  "wrong-type": {"PreToolUse": [{"hooks": [{"type": "prompt", "command": "./x.sh"}]}]},
	  "bad-matcher": {"PostToolUse": [{"matcher": "run_command(", "hooks": [{"command": "./x.sh"}]}]},
	  "good": {"SessionStart": [{"command": "./init.sh"}]}
	}`)
	entries, problems := Parse(raw)
	if len(entries) != 1 {
		t.Fatalf("Parse read %d entries, want 1 (the good hook)", len(entries))
	}
	if entries[0].Hook != "good" || entries[0].Event != EventSessionStart {
		t.Errorf("entry = %+v", entries[0])
	}
	if len(problems) != 3 {
		t.Fatalf("Parse reported %d problems, want 3: %v", len(problems), problems)
	}
	joined := strings.Join(problems, "\n")
	for _, want := range []string{"command hook must specify 'command'", "unsupported hook type", "is not a regular expression"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the problems do not name %q:\n%s", want, joined)
		}
	}
}

// TestParseRejectsBrokenDocument pins the loader's answer to a document
// that is not JSON at all.
func TestParseRejectsBrokenDocument(t *testing.T) {
	entries, problems := Parse([]byte("not json"))
	if len(entries) != 0 {
		t.Fatalf("Parse read %d entries from a broken document, want 0", len(entries))
	}
	if len(problems) != 1 || !strings.Contains(problems[0], "failed to parse") {
		t.Fatalf("problems = %v", problems)
	}
}

// TestMatchUsesTheMatcher pins the matcher: an anchored regular
// expression over the tool's name, and "" or "*" match every tool.
func TestMatchUsesTheMatcher(t *testing.T) {
	reg := &Registry{Entries: []Entry{
		{Hook: "exact", Event: EventPreToolUse, Matcher: "run_command", Handler: Handler{Command: "x"}},
		{Hook: "all", Event: EventPreToolUse, Matcher: "*", Handler: Handler{Command: "x"}},
		{Hook: "any", Event: EventPreToolUse, Matcher: "", Handler: Handler{Command: "x"}},
		{Hook: "prefix", Event: EventPreToolUse, Matcher: "browser_.*", Handler: Handler{Command: "x"}},
		{Hook: "either", Event: EventPreToolUse, Matcher: "run_command|view_file", Handler: Handler{Command: "x"}},
		{Hook: "lifecycle", Event: EventStop, Handler: Handler{Command: "x"}},
	}}
	cases := []struct {
		tool string
		want []string
	}{
		{"run_command", []string{"exact", "all", "any", "either"}},
		{"view_file", []string{"all", "any", "either"}},
		{"browser_click", []string{"all", "any", "prefix"}},
		{"xrun_command", []string{"all", "any"}},
	}
	for _, c := range cases {
		var got []string
		for _, e := range reg.Match(EventPreToolUse, c.tool) {
			got = append(got, e.Hook)
		}
		if strings.Join(got, ",") != strings.Join(c.want, ",") {
			t.Errorf("Match(%q) = %v, want %v", c.tool, got, c.want)
		}
	}
	// The lifecycle events ignore their matcher.
	if got := len(reg.Match(EventStop, "run_command")); got != 1 {
		t.Errorf("Match(Stop) = %d entries, want 1", got)
	}
	if got := len(reg.Match(EventPreToolUse, "run_command")); got == 0 {
		t.Error("Match(PreToolUse) found nothing")
	}
}
