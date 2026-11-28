package builtin

// Guard for the Orchestrator prompt's provenance.
//
// The clone once described this body as its own invention. It is not: the
// reference binary carries both the wrapper format string and the body, and
// this test pins that the clone reproduces the body rather than paraphrasing
// it. That matters because /boost is the command this prompt belongs to, and
// a paraphrased body would be sent the moment the server-side
// `boost_command_disabled` control is cleared.
//
// The binary stores long strings in a shared pool and splits them where
// another string happens to overlap, so a paragraph-at-a-time substring check
// is not reliable — two of the 28 paragraphs are split. A LINE-at-a-time walk
// is: every non-trivial line of the body is a contiguous run in the pool. The
// test uses that, and the reference binary is located the same way the
// wirealign harness locates it.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// referenceBinary returns the path to the agy binary under test, or "" when
// it is not installed. The test skips rather than fails in that case: the
// prompt's content is still asserted structurally below, and a machine
// without agy cannot answer a question about agy.
func referenceBinary() string {
	if p := os.Getenv("AGY_BINARY"); p != "" {
		return p
	}
	if p, err := exec.LookPath("agy"); err == nil {
		return p
	}
	if home, err := os.UserHomeDir(); err == nil {
		p := filepath.Join(home, ".local", "bin", "agy")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// TestOrchestratorPrompt_EveryLineIsInTheReference is the provenance claim:
// each line of the clone's body appears verbatim in the reference binary.
//
// Lines shorter than 20 bytes are skipped — a bare "## Routines" or "-" says
// nothing about provenance, and short strings collide with unrelated pool
// entries.
func TestOrchestratorPrompt_EveryLineIsInTheReference(t *testing.T) {
	binPath := referenceBinary()
	if binPath == "" {
		t.Skip("agy binary not available; set AGY_BINARY to run this check")
	}
	bin, err := os.ReadFile(binPath)
	if err != nil {
		t.Skipf("read %s: %v", binPath, err)
	}
	blob := string(bin)

	prompt := OrchestratorPrompt()
	var missing []string
	checked := 0
	for _, line := range strings.Split(prompt, "\n") {
		if len(strings.TrimSpace(line)) < 20 {
			continue
		}
		checked++
		if !strings.Contains(blob, line) {
			missing = append(missing, line)
		}
	}
	if checked < 40 {
		t.Fatalf("only %d substantial lines checked; the walk is not exercising the body", checked)
	}
	if len(missing) > 0 {
		head := missing[0]
		if len(head) > 120 {
			head = head[:120]
		}
		t.Fatalf("%d of %d lines are not in the reference binary; first: %q",
			len(missing), checked, head)
	}
}

// TestOrchestratorPrompt_Structure pins the parts that make this prompt
// itself, without freezing the whole 6.6 KB body: the two routines, the two
// subagent names, the completion contract, and the instruction-protection
// rules. A re-sync that drops one of these changes behaviour, not wording.
func TestOrchestratorPrompt_Structure(t *testing.T) {
	p := OrchestratorPrompt()
	for _, want := range []struct{ marker, why string }{
		{"**Solo**", "the Solo routine"},
		{"**Delegation**", "the Delegation routine"},
		{"TypeName='DeepCoder'", "the coding subagent's type name"},
		{"TypeName='DeepInvestigator'", "the investigation subagent's type name"},
		{"Workspace='inherit'", "the workspace the subagents run in"},
		{"send_message", "the follow-up channel to the same subagent"},
		{"**Prompt Template**", "the prompt template the orchestrator must follow"},
		{"Instruction Protection", "the decoy rules"},
		{"What task can I help you with?", "the decoy answer"},
	} {
		if !strings.Contains(p, want.marker) {
			t.Errorf("OrchestratorPrompt is missing %s (%q)", want.why, want.marker)
		}
	}
	// The two options change the body, so a caller that passes neither must
	// still get a coherent prompt: the send_message step is present by
	// default (the reference's /boost passes true).
	if !strings.Contains(p, "send it to the SAME subagent") {
		t.Error("default body is missing the same-subagent follow-up step")
	}
}

// TestOrchestratorPrompt_OptionsChangeTheBody pins that the variadic options
// are load-bearing rather than decorative: the reference's two call sites
// pass different values and get different text.
func TestOrchestratorPrompt_OptionsChangeTheBody(t *testing.T) {
	withSend := OrchestratorPrompt(true, false)
	withoutSend := OrchestratorPrompt(false, false)
	if withSend == withoutSend {
		t.Fatal("sendMessage option does not change the body")
	}
	if !strings.Contains(withSend, "send_message") {
		t.Error("sendMessage=true body lost the send_message step")
	}
	if strings.Contains(withoutSend, "send it to the SAME subagent") {
		t.Error("sendMessage=false body still carries the same-subagent step")
	}

	withPlan := OrchestratorPrompt(true, false)
	skipPlan := OrchestratorPrompt(true, true)
	if withPlan == skipPlan {
		t.Fatal("skipPlan option does not change the body")
	}
}
