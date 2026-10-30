package subagenttools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/builtinsubagent"
)

func TestDeepCoderAndDeepInvestigatorResolution(t *testing.T) {
	tool := &invokeTool{
		deps: Deps{
			Services: subagentTestServices(t),
			Model:    subagentTestModel(),
			CWD:      t.TempDir(),
		},
		defs: NewStore(),
	}

	tests := []struct {
		typeName     string
		expectedRole string
		promptSubstr string
		canWrite     bool
		canSubagent  bool
	}{
		{
			typeName:     string(builtinsubagent.KindDeepCoder),
			expectedRole: builtinsubagent.RoleDeepCoder,
			promptSubstr: "DeepCoderWorkerL0",
			canWrite:     true,
			canSubagent:  true,
		},
		{
			typeName:     string(builtinsubagent.KindDeepCoderWorkerL0),
			expectedRole: builtinsubagent.RoleDeepCoderWorkerL0,
			promptSubstr: "<original_task>",
			canWrite:     true,
			canSubagent:  false,
		},
		{
			typeName:     string(builtinsubagent.KindDeepCoderImprovementWorker),
			expectedRole: builtinsubagent.RoleDeepCoderImprovementWorker,
			promptSubstr: "<prior_attempt>",
			canWrite:     true,
			canSubagent:  false,
		},
		{
			typeName:     string(builtinsubagent.KindDeepInvestigator),
			expectedRole: builtinsubagent.RoleDeepInvestigator,
			promptSubstr: "DeepInvestigatorWorkerL0",
			canWrite:     false,
			canSubagent:  true,
		},
		{
			typeName:     string(builtinsubagent.KindDeepInvestigatorWorkerL0),
			expectedRole: builtinsubagent.RoleDeepInvestigatorWorkerL0,
			promptSubstr: "investigation worker",
			canWrite:     false,
			canSubagent:  false,
		},
		{
			typeName:     string(builtinsubagent.KindDeepInvestigatorImprovementWorker),
			expectedRole: builtinsubagent.RoleDeepInvestigatorImprovementWorker,
			promptSubstr: "rubber stamp",
			canWrite:     false,
			canSubagent:  false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.typeName, func(t *testing.T) {
			inv, handled, _, err := tool.resolve(context.Background(), invokeArgs{
				TypeName: tc.typeName,
				Prompt:   "Do work",
			})
			if err != nil {
				t.Fatalf("resolve error: %v", err)
			}
			if handled {
				t.Fatalf("expected delegation invocation, got handled=true")
			}
			if inv.Role != tc.expectedRole {
				t.Errorf("role = %q, want %q", inv.Role, tc.expectedRole)
			}
			if !strings.Contains(inv.SystemPrompt, tc.promptSubstr) {
				t.Errorf("system prompt missing %q:\n%s", tc.promptSubstr, inv.SystemPrompt)
			}
			if inv.Grant.Write != tc.canWrite {
				t.Errorf("grant.Write = %v, want %v", inv.Grant.Write, tc.canWrite)
			}
			if inv.Grant.Subagent != tc.canSubagent {
				t.Errorf("grant.Subagent = %v, want %v", inv.Grant.Subagent, tc.canSubagent)
			}
		})
	}
}

func TestDeepCoderCannotBeShadowed(t *testing.T) {
	tool := &invokeTool{
		defs: NewStore(),
	}
	tool.defs.Put("DeepCoder", Invocation{SystemPrompt: "impostor"})
	if _, ok := tool.definition("DeepCoder"); ok {
		t.Fatalf("DeepCoder was shadowed by custom definition")
	}
	if _, ok := tool.definition("DeepInvestigator"); ok {
		t.Fatalf("DeepInvestigator was shadowed by custom definition")
	}
}

func TestDefineSubagentRejectsReservedNames(t *testing.T) {
	dt := &defineTool{defs: NewStore()}
	reserved := []string{
		"self", "research",
		"DeepCoder", "DeepCoderWorkerL0", "DeepCoderImprovementWorker",
		"DeepInvestigator", "DeepInvestigatorWorkerL0", "DeepInvestigatorImprovementWorker",
	}
	for _, name := range reserved {
		t.Run(name, func(t *testing.T) {
			params := `{"Name":"` + name + `","SystemPrompt":"You are an impostor"}`
			_, err := dt.Execute(context.Background(), "call-1", json.RawMessage(params), nil)
			if err == nil {
				t.Fatalf("expected error when defining reserved subagent %q, got nil", name)
			}
			if !strings.Contains(err.Error(), "reserved built-in subagent name") {
				t.Fatalf("unexpected error message: %v", err)
			}
		})
	}
}
