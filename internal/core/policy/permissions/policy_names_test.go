package permissions

import "testing"

// TestDecideNamesJudgesBothSpellings pins the rule that makes a
// permission rule reach a subagent.
//
// A subagent calls the catalogue's spelling of a tool (its tool set is
// drawn from the catalogue), while the parent calls the declaration's. A
// rule the user wrote for the parent must not stop applying to the child,
// so the call is judged under both.
func TestDecideNamesJudgesBothSpellings(t *testing.T) {
	p := Policy{
		Deny: []Rule{{Tool: "write_file", Pattern: "/etc/"}},
	}

	// The call is judged under both spellings, so the rule reaches it.
	if got := p.DecideNames("/etc/passwd", "write_to_file", "write_file"); got != Deny {
		t.Fatalf("DecideNames under both spellings = %v, want Deny", got)
	}
	// The rule names the declaration's spelling only: on its own, the
	// catalogue's spelling does not match it. The pairing
	// (internal/tools.PermissionNames) is what carries the rule across.
	if got := p.DecideNames("/etc/passwd", "write_to_file"); got != Allow {
		t.Fatalf("DecideNames under the catalogue's spelling alone = %v, want Allow", got)
	}
	// A path the rule does not match is not denied.
	if got := p.DecideNames("/tmp/x", "write_to_file", "write_file"); got != Allow {
		t.Fatalf("DecideNames on an unmatched path = %v, want Allow", got)
	}
}

// TestDecideNamesHonoursAnAllowUnderEitherSpelling pins the other
// direction: a rule that permits the call under one spelling permits it.
func TestDecideNamesHonoursAnAllowUnderEitherSpelling(t *testing.T) {
	p := Policy{
		Mode:   ModePlan,
		Strict: true,
		Allow:  []Rule{{Tool: "write_to_file", Pattern: "/tmp/"}},
	}

	// Plan mode refuses a file edit — unless a rule allows it.
	if got := p.DecideNames("/tmp/x", "write_to_file", "write_file"); got != Allow {
		t.Fatalf("DecideNames = %v, want Allow", got)
	}
	// Without the rule, plan mode refuses the edit.
	if got := p.DecideNames("/var/x", "write_to_file", "write_file"); got != Ask {
		t.Fatalf("DecideNames = %v, want Ask", got)
	}
	// A read is allowed in plan mode.
	if got := p.DecideNames("/var/x", "view_file", "read_file"); got != Allow {
		t.Fatalf("DecideNames for a read = %v, want Allow", got)
	}
}

// TestDecideNamesKeepsTheReferenceOrder pins the order the reference's own
// rules follow: skip-permissions wins outright, a deny rule blocks, an
// allow rule permits, and only then do the mode's rules decide.
func TestDecideNamesKeepsTheReferenceOrder(t *testing.T) {
	p := Policy{
		SkipPermissions: true,
		Deny:            []Rule{{Tool: "write_file"}},
	}
	if got := p.DecideNames("/etc/passwd", "write_file"); got != Allow {
		t.Fatalf("DecideNames with SkipPermissions = %v, want Allow", got)
	}

	p = Policy{
		Strict: true,
		Deny:   []Rule{{Tool: "write_file"}},
		Allow:  []Rule{{Tool: "write_file"}},
	}
	if got := p.DecideNames("/etc/passwd", "write_file"); got != Deny {
		t.Fatalf("DecideNames with a deny and an allow = %v, want Deny", got)
	}

	p = Policy{Strict: true}
	if got := p.DecideNames("/etc/passwd", "view_file", "read_file"); got != Ask {
		t.Fatalf("DecideNames in strict mode = %v, want Ask", got)
	}
}

// TestDecideIsDecideNames pins that the single-name path is the same
// decision.
func TestDecideIsDecideNames(t *testing.T) {
	p := Policy{Deny: []Rule{{Tool: "write_file"}}}
	if got := p.Decide(Request{Tool: "write_file", Target: "/etc/passwd"}); got != Deny {
		t.Fatalf("Decide = %v, want Deny", got)
	}
	if got := p.Decide(Request{Tool: "view_file", Target: "/etc/passwd"}); got != Allow {
		t.Fatalf("Decide = %v, want Allow", got)
	}
}
