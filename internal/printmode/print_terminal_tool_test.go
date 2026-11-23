package printmode

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/justwasm/pigasus/actor"
	"github.com/justwasm/pigasus/internal/app/tools/surface"
	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/core/policy/permissions"
)

func TestPrintMode_TerminalToolAndSkillAvailableWithoutApproval(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Setenv("SHELL", "/bin/sh")
	}
	home := t.TempDir()
	t.Setenv("OPENAGY_CONFIG_HOME", home)
	ws := t.TempDir()

	cfg := actorPrintConfig{
		Backend:   backend.NewMock(),
		Model:     "gemini-2.5-pro",
		Workspace: ws,
		AgentDir:  t.TempDir(),
		Catalog:   surface.RuntimeCatalog(ws),
		Policy: &permissions.Policy{
			Mode:           permissions.ModePlan,
			ToolPermission: permissions.ToolPermissionStrict,
		},
	}

	prompt := buildPrintSystemPrompt(cfg)
	wantSkillPath := filepath.Join(home, "builtin-skills", "terminal", "SKILL.md")
	if !strings.Contains(prompt, wantSkillPath) {
		t.Fatalf("system prompt missing unpacked terminal skill path %q", wantSkillPath)
	}
	if _, err := os.Stat(wantSkillPath); err != nil {
		t.Fatalf("terminal SKILL.md not unpacked: %v", err)
	}

	var denials []string
	toolsList, cleanup, err := buildPrintTools(cfg, nil, func() string { return prompt }, &denials)
	if err != nil {
		t.Fatalf("buildPrintTools: %v", err)
	}
	defer cleanup()

	var termTool actor.Tool
	for _, tl := range toolsList {
		if tl.Name() == "terminal" {
			termTool = tl
			break
		}
	}
	if termTool == nil {
		t.Fatal("buildPrintTools did not include terminal tool")
	}
	if termTool.RequiresApproval(`{"action":"new"}`) {
		t.Fatal("terminal tool RequiresApproval = true, want false")
	}

	ctx := context.Background()
	res, err := termTool.Execute(ctx, `{"action":"new","args":{"name":"p1","command":"printf 'PRINT_%s\\n' TERM_OK","wait_ms":200}}`)
	if err != nil {
		t.Fatalf("Execute new: %v", err)
	}
	if res.Error != "" {
		t.Fatalf("Execute new error: %s", res.Error)
	}

	deadline := time.Now().Add(10 * time.Second)
	for {
		capRes, err := termTool.Execute(ctx, `{"action":"capture","args":{"id":"p1"}}`)
		if err != nil {
			t.Fatalf("Execute capture: %v", err)
		}
		if strings.Contains(capRes.Output, "PRINT_TERM_OK") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for PRINT_TERM_OK:\n%s", capRes.Output)
		}
		time.Sleep(30 * time.Millisecond)
	}
	if len(denials) != 0 {
		t.Fatalf("unexpected denials: %v", denials)
	}
}

func TestPrintMode_ActorLoopReadsSkillAndDrivesTerminalTool(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Setenv("SHELL", "/bin/sh")
	}
	home := t.TempDir()
	t.Setenv("OPENAGY_CONFIG_HOME", home)
	ws := t.TempDir()

	cfg := actorPrintConfig{
		Model:     "gemini-2.5-pro",
		Workspace: ws,
		AgentDir:  t.TempDir(),
		Catalog:   surface.RuntimeCatalog(ws),
		Policy: &permissions.Policy{
			Mode: permissions.ModePlan,
		},
	}

	prompt := buildPrintSystemPrompt(cfg)
	wantSkillPath := filepath.Join(home, "builtin-skills", "terminal", "SKILL.md")

	var denials []string
	toolsList, cleanup, err := buildPrintTools(cfg, nil, func() string { return prompt }, &denials)
	if err != nil {
		t.Fatalf("buildPrintTools: %v", err)
	}
	defer cleanup()

	mp := actor.NewMockProvider()
	// Turn 1: read the unpacked terminal SKILL.md.
	mp.EnqueueHandler(func(req actor.GenerateRequest) ([]actor.StreamChunk, error) {
		var foundTermDecl bool
		for _, td := range req.Tools {
			if td.Name == "terminal" {
				foundTermDecl = true
				if strings.Contains(td.Description, "Ctrl+C") {
					t.Errorf("terminal decl leaked detailed usage: %q", td.Description)
				}
			}
		}
		if !foundTermDecl {
			t.Error("actor GenerateRequest missing terminal tool declaration")
		}
		return []actor.StreamChunk{{
			ToolCalls: []actor.ToolCall{{
				ID:    "call-1",
				Name:  "view_file",
				Input: `{"AbsolutePath":"` + wantSkillPath + `"}`,
			}},
			FinishReason: "tool_calls",
		}}, nil
	})
	// Turn 2: verify SKILL.md was returned, then call terminal new.
	mp.EnqueueHandler(func(req actor.GenerateRequest) ([]actor.StreamChunk, error) {
		last := req.Messages[len(req.Messages)-1]
		if !strings.Contains(last.Content, "tmux send-keys") {
			t.Errorf("expected SKILL.md content in tool result, got: %q", last.Content)
		}
		return []actor.StreamChunk{{
			ToolCalls: []actor.ToolCall{{
				ID:    "call-2",
				Name:  "terminal",
				Input: `{"action":"new","args":{"name":"e2e","command":"printf 'ACTOR_%s\\n' E2E_1","wait_ms":1500}}`,
			}},
			FinishReason: "tool_calls",
		}}, nil
	})
	// Turn 3: send another command.
	mp.EnqueueHandler(func(req actor.GenerateRequest) ([]actor.StreamChunk, error) {
		return []actor.StreamChunk{{
			ToolCalls: []actor.ToolCall{{
				ID:    "call-3",
				Name:  "terminal",
				Input: `{"action":"send","args":{"id":"e2e","text":"printf 'ACTOR_%s\\n' E2E_2","keys":["Enter"],"wait_ms":1000}}`,
			}},
			FinishReason: "tool_calls",
		}}, nil
	})
	// Turn 4: verify output appeared on screen, then kill session.
	mp.EnqueueHandler(func(req actor.GenerateRequest) ([]actor.StreamChunk, error) {
		last := req.Messages[len(req.Messages)-1]
		if !strings.Contains(last.Content, "ACTOR_E2E_2") {
			t.Errorf("expected ACTOR_E2E_2 in terminal send capture, got:\n%s", last.Content)
		}
		return []actor.StreamChunk{{
			ToolCalls: []actor.ToolCall{{
				ID:    "call-4",
				Name:  "terminal",
				Input: `{"action":"kill","args":{"id":"e2e"}}`,
			}},
			FinishReason: "tool_calls",
		}}, nil
	})
	// Turn 5: verify kill succeeded and finish.
	mp.EnqueueHandler(func(req actor.GenerateRequest) ([]actor.StreamChunk, error) {
		last := req.Messages[len(req.Messages)-1]
		if !strings.Contains(last.Content, "Closed terminal") {
			t.Errorf("expected Closed terminal result, got: %q", last.Content)
		}
		return []actor.StreamChunk{{
			Delta:        "done",
			FinishReason: "stop",
		}}, nil
	})

	act, err := actor.Spawn(actor.Config{
		ID: "conv-actor-term-e2e",
		Default: actor.TurnConfig{
			SystemPrompt: prompt,
			Model:        "gemini-2.5-pro",
			Provider:     mp,
			Tools:        toolsList,
		},
	})
	if err != nil {
		t.Fatalf("actor.Spawn: %v", err)
	}
	defer func() { _ = act.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	run, err := act.Prompt(ctx, "test terminal tool")
	if err != nil {
		t.Fatalf("act.Prompt: %v", err)
	}
	var reply strings.Builder
	for ev := range run.Events() {
		switch ev.Type {
		case actor.EventApprovalRequired:
			if ev.ToolCall != nil && ev.ToolCall.Name == "terminal" {
				t.Fatalf("terminal tool unexpectedly requested approval")
			}
			if ev.ToolCall != nil {
				_ = act.Approve(ev.ToolCall.ID, true, "")
			}
		case actor.EventDelta:
			reply.WriteString(ev.Delta)
		case actor.EventError:
			if ev.Error != "" {
				t.Fatalf("actor error event: %s", ev.Error)
			}
		}
	}
	if reply.String() != "done" {
		t.Fatalf("reply = %q, want done", reply.String())
	}
	if len(denials) != 0 {
		t.Fatalf("unexpected denials: %v", denials)
	}
}
