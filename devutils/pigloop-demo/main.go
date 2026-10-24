// Command pigloop-demo is the PiG migration harness. It runs PiG's agent loop
// (github.com/MichaelKinsy/PiG/agent) over the openagy clone's
// provider-neutral backend and its recovered tool registry, either directly
// through the bridge or through the migrated internal/agent.Loop.
//
// It exists to answer one question with running code rather than prose: can
// the clone's backend.Client and tools.Tool be driven by a foreign agent loop
// without changing either one — and can the clone's own Loop keep its contract
// while PiG walks the model -> tool -> model cycle underneath it?
//
// Usage:
//
//	go run ./cmd/pigloop-demo                    # bridge, scripted 2-turn tool round trip
//	go run ./cmd/pigloop-demo -mode loop         # the migrated internal/agent.Loop
//	go run ./cmd/pigloop-demo -provider mock     # the clone's backend.NewMock
//	go run ./cmd/pigloop-demo -provider gateway -model MiniMax-M3
//	                                             # the native the configured gateway provider
//	                                             # (OPENAGY_GATEWAY_URL / OPENAGY_GATEWAY_KEY)
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	piagent "github.com/MichaelKinsy/PiG/agent"
	pi "github.com/MichaelKinsy/PiG/ai"

	"github.com/justwasm/pigasus/internal/agent"
	"github.com/justwasm/pigasus/internal/backend"
	"github.com/justwasm/pigasus/internal/piagentbridge"
	"github.com/justwasm/pigasus/internal/tools"
)

// gatewayDefaultModel is the wire model the configured gateway serves; the Antigravity model
// names are not accepted there.
const gatewayDefaultModel = "MiniMax-M3"

func main() {
	mode := flag.String("mode", "bridge", `"bridge" (PiG agent over piagentbridge) or "loop" (the migrated internal/agent.Loop)`)
	provider := flag.String("provider", "scripted", `"scripted" (2-turn tool round trip), "mock" (backend.NewMock) or "gateway" (the native the configured gateway provider)`)
	model := flag.String("model", "claude-sonnet-4-6", "model id sent upstream")
	prompt := flag.String("prompt", "What Go module is this repository?", "user prompt for the run")
	flag.Parse()

	if *provider == "gateway" && !flagPassed("model") {
		// gateway is not an Antigravity model router: it serves its own wire ids.
		*model = gatewayDefaultModel
	}

	// The clone's real tool registry, backed by its real filesystem executor.
	workspace, err := os.Getwd()
	if err != nil {
		fatal("getwd", err)
	}
	registry := tools.NewRegistry()
	registry.Register(&tools.ReadFile{Workspace: workspace})

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	fmt.Printf("mode=%s provider=%s model=%s tools=%d\n", *mode, *provider, *model, len(registry.List()))
	fmt.Printf("user   %s\n", *prompt)

	started := time.Now()
	if *mode == "loop" {
		runLoop(ctx, *provider, *model, *prompt, registry)
	} else {
		runBridge(ctx, *provider, *model, *prompt, registry)
	}
	fmt.Printf("--- settled in %s ---\n", time.Since(started).Round(time.Millisecond))
}

func flagPassed(name string) bool {
	found := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == name {
			found = true
		}
	})
	return found
}

func fatal(what string, err error) {
	fmt.Fprintf(os.Stderr, "%s: %v\n", what, err)
	os.Exit(1)
}

// ─── mode "bridge": PiG directly over the bridge ─────────────────────────────

func runBridge(ctx context.Context, providerName, model, prompt string, registry *tools.Registry) {
	// PiG's EventCh is a blocking send; a dedicated reader keeps the loop
	// goroutine free. EventDone is left open so no event is ever dropped.
	events := make(chan piagent.AgentEvent, 256)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for event := range events {
			fmt.Println("  event  " + describe(event))
		}
	}()

	options := piagentbridge.Options{
		ModelID:      model,
		SystemPrompt: "You are a coding agent. Use the provided tools when they help.",
		Registry:     registry,
		Events:       events,
	}
	if providerName == "gateway" {
		options.Provider = piagentbridge.NewProvider(piagentbridge.ProviderConfig{Model: model})
	} else {
		options.Client = chooseClient(providerName)
	}

	agent, err := piagentbridge.NewAgent(options)
	if err != nil {
		fatal("agent", err)
	}

	messages, err := agent.Send(ctx, prompt)
	close(events)
	wg.Wait()
	if err != nil {
		fatal("send", err)
	}
	for _, message := range messages {
		if line := summarize(message); line != "" {
			fmt.Println(line)
		}
	}
}

// ─── mode "loop": the migrated internal/agent.Loop ───────────────────────────

// runLoop drives the clone's own Loop, whose Send is now PiG's loop. Nothing
// about the call site is cycle-183 specific: this is the same Options /
// TurnInput / Handler / ToolRunner shape cmd/pigasus and both HTTP shims use.
func runLoop(ctx context.Context, providerName, model, prompt string, registry *tools.Registry) {
	options := agent.Options{
		DefaultModel: model,
		SystemPrompt: "You are a coding agent. Use the provided tools when they help.",
		Handler:      chunkLogger(),
		Tools:        declsFrom(registry),
		ToolRunner:   &registryRunner{reg: registry},
	}
	if providerName == "gateway" {
		options.Provider = piagentbridge.NewProvider(piagentbridge.ProviderConfig{Model: model})
	} else {
		options.Backend = chooseClient(providerName)
	}

	loop, err := agent.New(options)
	if err != nil {
		fatal("agent.New", err)
	}
	defer loop.Close()

	if err := loop.Send(ctx, agent.TurnInput{
		Messages: []backend.Message{{Role: "user", Content: prompt}},
	}); err != nil {
		fatal("send", err)
	}
	fmt.Printf("cascade %s\n", loop.Cascade().State())
}

func chunkLogger() agent.Handler {
	return func(chunk backend.StreamChunk) error {
		switch {
		case chunk.Error != nil:
			fmt.Fprintf(os.Stderr, "  chunk  ERROR  %v\n", chunk.Error)
		case len(chunk.ToolCalls) > 0:
			for _, call := range chunk.ToolCalls {
				fmt.Printf("  chunk  TOOL   %s %s\n", call.Name, compact(call.Arguments))
			}
		case chunk.Done:
			fmt.Printf("  chunk  DONE   %s\n", metaLine(chunk.Meta))
		case chunk.Text != "":
			fmt.Printf("  chunk  TEXT   %q\n", clip(chunk.Text))
		}
		return nil
	}
}

func metaLine(meta *backend.ChunkMeta) string {
	if meta == nil {
		return "(no usage)"
	}
	return fmt.Sprintf("prompt=%d completion=%d total=%d reasoning=%d",
		meta.PromptTokens, meta.CompletionTokens, meta.TotalTokens, meta.ReasoningTokens)
}

// registryRunner adapts a tools.Registry to agent.ToolRunner, the same way
// cmd/pigasus/main.go does.
type registryRunner struct{ reg *tools.Registry }

func (r *registryRunner) Run(ctx context.Context, call backend.ToolCall) (string, error) {
	return r.reg.Dispatch(ctx, call.Name, call.Arguments)
}

// declsFrom advertises the registry to the model. It exists in the demo only
// because cmd/pigasus/main.go's toolDeclsFromRegistry is package-private; note
// that it can carry tools.Describe now that the catalog exposes descriptions.
func declsFrom(reg *tools.Registry) []backend.ToolDecl {
	names := reg.List()
	decls := make([]backend.ToolDecl, 0, len(names))
	for _, name := range names {
		tool, ok := reg.Get(name)
		if !ok {
			continue
		}
		decls = append(decls, backend.ToolDecl{
			Name:        name,
			Description: tools.Describe(tool),
			Parameters:  tool.Schema(),
		})
	}
	return decls
}

func chooseClient(name string) backend.Client {
	if name == "mock" {
		return backend.NewMock()
	}
	return &scriptedClient{}
}

// scriptedClient is a deterministic stand-in for the Antigravity backend that
// exercises the two behaviours the adapter has to get right: a first turn
// that asks for a tool, and a second turn (after the tool result is fed back)
// that streams plain text. It satisfies backend.Client unchanged.
type scriptedClient struct {
	turn atomic.Int32
}

func (c *scriptedClient) Stream(ctx context.Context, req backend.StreamRequest) (<-chan backend.StreamChunk, error) {
	if strings.TrimSpace(req.Model) == "" {
		return nil, fmt.Errorf("scripted backend: model is required")
	}
	turn := c.turn.Add(1)
	out := make(chan backend.StreamChunk, 16)
	go func() {
		defer close(out)
		if turn == 1 {
			send(ctx, out, backend.StreamChunk{
				ToolCalls: []backend.ToolCall{{
					ID:        "call_1",
					Name:      "read_file",
					Arguments: json.RawMessage(`{"path":"go.mod"}`),
				}},
			})
			send(ctx, out, backend.StreamChunk{
				Done: true,
				Meta: &backend.ChunkMeta{PromptTokens: 24, CompletionTokens: 11, TotalTokens: 35},
			})
			return
		}
		reply := "This repository is the Go module github.com/justwasm/pigasus."
		for _, chunk := range chunkString(reply, 18) {
			send(ctx, out, backend.StreamChunk{Text: chunk})
		}
		send(ctx, out, backend.StreamChunk{
			Done: true,
			Meta: &backend.ChunkMeta{
				PromptTokens:     96,
				CompletionTokens: len(reply) / 4,
				TotalTokens:      96 + len(reply)/4,
				ReasoningTokens:  3,
			},
		})
	}()
	return out, nil
}

func send(ctx context.Context, out chan<- backend.StreamChunk, chunk backend.StreamChunk) {
	select {
	case out <- chunk:
	case <-ctx.Done():
	}
}

func chunkString(s string, size int) []string {
	var chunks []string
	for i := 0; i < len(s); i += size {
		end := i + size
		if end > len(s) {
			end = len(s)
		}
		chunks = append(chunks, s[i:end])
	}
	return chunks
}

// describe renders one AgentEvent as a single line. Only the events this
// harness waits on are spelled out; everything else reports its Go type.
func describe(event piagent.AgentEvent) string {
	switch value := event.(type) {
	case piagent.AgentStartEvent:
		return "agent_start"
	case piagent.AgentEndEvent:
		return fmt.Sprintf("agent_end messages=%d retry=%v", len(value.Messages), value.WillRetry)
	case piagent.AgentSettledEvent:
		return "agent_settled"
	case piagent.TurnStartEvent:
		return fmt.Sprintf("turn_start #%d", value.TurnIndex)
	case piagent.TurnEndEvent:
		return fmt.Sprintf("turn_end #%d tool_results=%d", value.TurnIndex, len(value.ToolResults))
	case piagent.MessageStartEvent:
		return fmt.Sprintf("message_start role=%s", value.Message.Role())
	case piagent.MessageUpdateEvent:
		return fmt.Sprintf("message_update role=%s %s", value.Message.Role(), deltaOf(value.AssistantMessageEvent))
	case piagent.MessageEndEvent:
		return fmt.Sprintf("message_end role=%s stop=%s", value.Message.Role(), stopOf(value.Message))
	case piagent.ToolExecutionStartEvent:
		return fmt.Sprintf("tool_start %s args=%s", value.ToolName, compact(value.Args))
	case piagent.ToolExecutionEndEvent:
		return fmt.Sprintf("tool_end %s error=%v in %s -> %s",
			value.ToolName, value.IsError, toolDuration(value.DurationMs),
			compact([]byte(piagentbridge.RenderResult(value.Result.Content))))
	default:
		return fmt.Sprintf("%T", event)
	}
}

// toolDuration renders ToolExecutionEndEvent.DurationMs. PiG 0.5.1 replaced the
// old time.Duration with a nil-able millisecond count, nil for a call that did
// not run.
func toolDuration(ms *int64) time.Duration {
	if ms == nil {
		return 0
	}
	return time.Duration(*ms) * time.Millisecond
}

func deltaOf(event pi.AssistantMessageEvent) string {
	switch value := event.(type) {
	case pi.TextDeltaEvent:
		return fmt.Sprintf("text+%d=%q", len(value.Delta), clip(value.Delta))
	case pi.TextStartEvent:
		return "text_start"
	case pi.TextEndEvent:
		return fmt.Sprintf("text_end %d chars", len(value.Content))
	case pi.ToolCallEndEvent:
		return fmt.Sprintf("toolcall %s", value.ToolCall.Name)
	case pi.StartEvent:
		return "start"
	case pi.DoneEvent:
		return fmt.Sprintf("done reason=%s", value.Reason)
	case pi.ErrorEvent:
		return fmt.Sprintf("error reason=%s", value.Reason)
	default:
		return string(event.EventType())
	}
}

func stopOf(message piagent.AgentMessage) string {
	if message.Assistant != nil {
		return string(message.Assistant.StopReason)
	}
	return "-"
}

// summarize renders the messages Send returned, so the final text and the
// tool round trip are both visible.
func summarize(message piagent.AgentMessage) string {
	switch {
	case message.User != nil:
		return fmt.Sprintf("user       %s", contentOf(message.ContentBlocks()))
	case message.Assistant != nil:
		if text := contentOf(message.ContentBlocks()); text != "" {
			return fmt.Sprintf("assistant  %s", text)
		}
		return fmt.Sprintf("assistant  (stop=%s)", message.Assistant.StopReason)
	case message.ToolResult != nil:
		return fmt.Sprintf("tool       %s -> %s",
			message.ToolResult.ToolName, clip(contentOf(message.ContentBlocks())))
	}
	return ""
}

func contentOf(blocks []pi.ContentBlock) string {
	var builder strings.Builder
	for _, block := range blocks {
		if text, ok := block.(pi.TextContent); ok {
			builder.WriteString(text.Text)
		}
	}
	return builder.String()
}

func compact(raw []byte) string { return clip(strings.Join(strings.Fields(string(raw)), " ")) }

func clip(s string) string {
	const limit = 72
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= limit {
		return s
	}
	return s[:limit] + "…"
}
