package customize

import (
	"strings"
	"testing"
)

// TestTokenBudgetExceededTrimsCustomization verifies that, when the
// customization tokens collectively exceed the budget's capacity,
// the trimmer drops *something* — and that the thing it drops is
// the lowest-priority candidate (/customize).
func TestTokenBudgetExceededTrimsCustomization(t *testing.T) {
	// 4 chars / token → a 4000-char body is 1000 tokens.
	big := strings.Repeat("a", 4000) // 1000 tokens
	// Budget big enough for ONE customization payload only.
	budget := NewBudget(200_000, 50_000, 148_000) // capacity = 2000
	tr := NewTrimmer(budget)
	tr.Add("customize", big)
	tr.Add("config", big)
	tr.Add("agents", big)

	kept := tr.Trim()
	if len(kept) != 2 {
		t.Fatalf("Trim kept %d tokens, want 2: dropped=%v", len(kept), tr.Dropped())
	}
	for _, k := range kept {
		if k.Name == "customize" {
			t.Errorf("customize survived; expected it to be dropped first")
		}
	}
	if len(tr.Dropped()) != 1 {
		t.Fatalf("Trim dropped %d, want 1", len(tr.Dropped()))
	}
	if tr.Dropped()[0].Name != "customize" {
		t.Errorf("Dropped[0] = %q, want %q", tr.Dropped()[0].Name, "customize")
	}
}

// TestCustomizeTrimmedFirst pins the canonical ordering the
// changelog calls out: /customize is the first to go when the
// context is tight.
func TestCustomizeTrimmedFirst(t *testing.T) {
	// Each body is 800 tokens; capacity is 1500, so exactly one
	// body fits. The trimmer keeps the highest-priority candidate
	// — /hooks (priority 30) — and drops the other three. /customize
	// is the FIRST entry in the dropped list because the trimmer
	// reports drops in ascending priority order.
	big := strings.Repeat("x", 3200)              // 800 tokens
	budget := NewBudget(200_000, 50_000, 148_500) // capacity = 1500

	tr := NewTrimmer(budget)
	tr.Add("customize", big)
	tr.Add("config", big)
	tr.Add("agents", big)
	tr.Add("hooks", big)

	kept := tr.Trim()
	if len(kept) != 1 {
		t.Fatalf("Trim kept %d, want 1 (one 800-token body in a 1500-token capacity)", len(kept))
	}
	if kept[0].Name != "hooks" {
		t.Errorf("kept[0] = %q, want %q (highest priority should win)", kept[0].Name, "hooks")
	}
	dropped := tr.Dropped()
	if len(dropped) != 3 {
		t.Fatalf("Dropped = %d, want 3", len(dropped))
	}
	// Dropped list is in ascending priority order: /customize
	// first, then /config, then /agents.
	wantDropped := []string{"customize", "config", "agents"}
	gotDropped := namesOf(dropped)
	for i, name := range wantDropped {
		if gotDropped[i] != name {
			t.Errorf("dropped[%d] = %q, want %q (got %v)", i, gotDropped[i], name, gotDropped)
		}
	}
}

// TestAgentsTrimmedAfterCustomize is the parallel assertion: when
// the budget is tight enough that exactly one of the two has to
// go, the lower-priority /customize is dropped and /agents
// survives. Mirrors the changelog's "less-important tokens get
// trimmed first" wording applied to the /customize-vs-/agents pair.
func TestAgentsTrimmedAfterCustomize(t *testing.T) {
	// capacity = 2 tokens: one of the two 2-token bodies fits,
	// the other does not. The trimmer keeps the higher-priority
	// /agents and drops /customize.
	budget := NewBudget(200_000, 50_000, 149_998) // capacity = 2
	tr := NewTrimmer(budget)
	tr.Add("customize", strings.Repeat("a", 8)) // 2 tokens
	tr.Add("agents", strings.Repeat("b", 8))    // 2 tokens

	kept := tr.Trim()
	if len(kept) != 1 {
		t.Fatalf("Trim kept %d, want 1 (capacity = 2, two 2-token bodies)", len(kept))
	}
	if kept[0].Name != "agents" {
		t.Errorf("kept[0] = %q, want %q (lower priority /customize should be dropped)", kept[0].Name, "agents")
	}
	if len(tr.Dropped()) != 1 || tr.Dropped()[0].Name != "customize" {
		t.Errorf("dropped = %v, want [customize]", namesOf(tr.Dropped()))
	}
}

// TestLowPriorityTrimmedBeforeHighPriority is the canonical
// ordering check across all eight commands. The trimmer has to
// walk the priorities in ascending order and drop from the front.
func TestLowPriorityTrimmedBeforeHighPriority(t *testing.T) {
	// Each body is exactly half the capacity. Capacity is 1500
	// tokens (3 bodies worth), so only the 3 highest-priority
	// candidates survive.
	body := strings.Repeat("z", 2000)             // 500 tokens
	budget := NewBudget(200_000, 50_000, 148_500) // capacity = 1500

	tr := NewTrimmer(budget)
	tr.Add("customize", body)
	tr.Add("config", body)
	tr.Add("agents", body)
	tr.Add("hooks", body)
	tr.Add("mcp", body)
	tr.Add("plugin", body)
	tr.Add("skills", body)
	tr.Add("commands", body)

	kept := tr.Trim()
	if len(kept) != 3 {
		t.Fatalf("Trim kept %d, want 3 (kept=%v, dropped=%v)", len(kept), namesOf(kept), namesOf(tr.Dropped()))
	}
	// The three kept are the three highest priorities
	// (commands=70, skills=60, plugin=50); the trimmer returns
	// them in REGISTRATION order (the order they were added),
	// which is plugin first, then skills, then commands. The
	// membership check is what the contract actually cares
	// about — the trimmer preserves registration order so
	// downstream renderers see a stable layout.
	wantKept := map[string]bool{"commands": true, "skills": true, "plugin": true}
	for _, k := range kept {
		if !wantKept[k.Name] {
			t.Errorf("kept has unexpected name %q", k.Name)
		}
	}
	for _, name := range []string{"commands", "skills", "plugin"} {
		found := false
		for _, k := range kept {
			if k.Name == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected %q in kept, missing", name)
		}
	}
	// The dropped list, in priority order (the trimmer reverses
	// its walk so /customize appears first): customize, config,
	// agents, hooks, mcp — every one of the lower priorities.
	wantDropped := []string{"customize", "config", "agents", "hooks", "mcp"}
	gotDropped := namesOf(tr.Dropped())
	if len(gotDropped) != len(wantDropped) {
		t.Fatalf("Dropped count = %d, want %d (got %v)", len(gotDropped), len(wantDropped), gotDropped)
	}
	for i, name := range wantDropped {
		if gotDropped[i] != name {
			t.Errorf("dropped[%d] = %q, want %q (got %v)", i, gotDropped[i], name, gotDropped)
		}
	}
}

// TestNoTrimWhenBudgetSufficient verifies the empty case: when
// the customization tokens collectively fit in the budget, the
// trimmer returns every registered token unchanged.
func TestNoTrimWhenBudgetSufficient(t *testing.T) {
	// Each body is 100 tokens; capacity = 1000; seven bodies
	// total = 700 tokens. Plenty of room.
	body := strings.Repeat("q", 400)              // 100 tokens
	budget := NewBudget(200_000, 50_000, 149_000) // capacity = 1000

	tr := NewTrimmer(budget)
	tr.Add("customize", body)
	tr.Add("config", body)
	tr.Add("agents", body)
	tr.Add("hooks", body)
	tr.Add("mcp", body)
	tr.Add("plugin", body)
	tr.Add("skills", body)

	kept := tr.Trim()
	if len(kept) != 7 {
		t.Fatalf("Trim kept %d, want 7", len(kept))
	}
	if dropped := tr.Dropped(); len(dropped) != 0 {
		t.Fatalf("Dropped = %v, want none", namesOf(dropped))
	}
	// Kept order is registration order — the trimmer restores it
	// so a downstream renderer does not have to re-think its
	// layout after every call.
	wantOrder := []string{"customize", "config", "agents", "hooks", "mcp", "plugin", "skills"}
	gotOrder := namesOf(kept)
	for i, name := range wantOrder {
		if gotOrder[i] != name {
			t.Errorf("kept[%d] = %q, want %q (got %v)", i, gotOrder[i], name, gotOrder)
		}
	}
}

// ---- Additional unit tests (extras; the five above are the
// hard minimum the changelog names) -----------------------------

// TestZeroTokensKept verifies that an empty customization payload
// is kept unconditionally — even when the budget is exhausted —
// because dropping a zero-cost token would not free any headroom
// and would just shuffle the produced token order for the caller.
func TestZeroTokensKept(t *testing.T) {
	budget := NewBudget(200_000, 50_000, 200_000) // capacity = 0
	tr := NewTrimmer(budget)
	tr.Add("customize", "")
	tr.Add("config", "anything")
	kept := tr.Trim()
	if len(kept) != 1 {
		t.Fatalf("Trim kept %d, want 1 (zero-cost + empty-budget)", len(kept))
	}
	if kept[0].Name != "customize" {
		t.Errorf("kept[0] = %q, want %q (zero-cost should always win)", kept[0].Name, "customize")
	}
}

// TestBudgetCapacityZeroWhenUsedExceeds checks the Budget.Capacity
// safety net: a budget whose Used already exceeds Window - Reserve
// reports zero capacity so the trimmer cannot accidentally
// approve a customization token when the conversation itself is
// already over budget.
func TestBudgetCapacityZeroWhenUsedExceeds(t *testing.T) {
	b := NewBudget(200_000, 50_000, 200_000) // Used == Window - Reserve
	if got := b.Capacity(); got != 0 {
		t.Errorf("Capacity() = %d, want 0", got)
	}
	b = NewBudget(200_000, 50_000, 1_000_000) // Used > Window - Reserve
	if got := b.Capacity(); got != 0 {
		t.Errorf("Capacity() = %d, want 0", got)
	}
}

// TestPriorityForKnownCommands checks the PriorityFor lookup
// table — the canonical names the trimmer knows about.
func TestPriorityForKnownCommands(t *testing.T) {
	cases := []struct {
		name string
		want Priority
	}{
		{"customize", PriorityCustomize},
		{"config", PriorityConfig},
		{"agents", PriorityAgents},
		{"hooks", PriorityHooks},
		{"mcp", PriorityMCP},
		{"plugin", PriorityPlugin},
		{"skills", PrioritySkills},
		{"commands", PriorityCommands},
	}
	for _, c := range cases {
		if got := PriorityFor(c.name); got != c.want {
			t.Errorf("PriorityFor(%q) = %d, want %d", c.name, got, c.want)
		}
	}
	// Unknown names fall through to PriorityCommands (the most
	// conservative default).
	if got := PriorityFor("future-command"); got != PriorityCommands {
		t.Errorf("PriorityFor(unknown) = %d, want %d", got, PriorityCommands)
	}
}

// TestRegistrationOrderAfterTrim confirms the trimmer restores
// the registration order in its returned slice, so a downstream
// renderer can rely on "the slice I get back is in the same
// order I added them".
func TestRegistrationOrderAfterTrim(t *testing.T) {
	// Add in a non-priority order, then verify Trim returns them
	// in registration order.
	body := strings.Repeat("a", 40)               // 10 tokens
	budget := NewBudget(200_000, 50_000, 148_000) // capacity = 2000
	tr := NewTrimmer(budget)
	tr.Add("commands", body)
	tr.Add("config", body)
	tr.Add("skills", body)
	tr.Add("customize", body)

	kept := tr.Trim()
	want := []string{"commands", "config", "skills", "customize"}
	got := namesOf(kept)
	for i, name := range want {
		if got[i] != name {
			t.Errorf("kept[%d] = %q, want %q (got %v)", i, got[i], name, got)
		}
	}
}

// TestRegisterSourcesSkipsEmpty verifies the bridge helper does
// not register empty payloads — the trimmer's zero-cost handling
// would already keep them, but skipping them keeps the Trimmer's
// bookkeeping tight.
func TestRegisterSourcesSkipsEmpty(t *testing.T) {
	budget := DefaultBudget()
	tr := NewTrimmer(budget)
	RegisterSources(tr,
		&CustomizeSource{Body: "anything"}, // non-empty → registered
		&ConfigSource{Body: ""},            // empty → skipped
		&AgentsSource{Body: "anything"},    // non-empty → registered
	)
	if got := len(tr.Tokens()); got != 2 {
		t.Errorf("Tokens() = %d, want 2 (empty body should be skipped)", got)
	}
}

// namesOf is the test-only helper that returns the names of a
// token slice in order.
func namesOf(ts []*Token) []string {
	out := make([]string, len(ts))
	for i, t := range ts {
		out[i] = t.Name
	}
	return out
}
