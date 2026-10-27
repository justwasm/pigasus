// print_pig.go — the print-mode turn on PiG's coding runtime.
//
// Cycle 209 moved the default runtime off the clone's own loop; cycle 210
// finishes the move for `-p`: PiG is now the only engine headless prints use.
// The clone's loop, its cascade/lifecycle shadow walk and the OPENAGY_RUNTIME
// rollback switch are gone from this path.
//
// The reference behaviour that matters (what lands on stdout, the exit code,
// the stderr envelope) is unchanged, because this file feeds the SAME chunk
// handler runPrintMode always had: it synthesises a backend.StreamChunk
// sequence from PiG's agent events, so the sanitizer, the `user_input` step,
// the usage block and the trailing newline keep coming from one place.
//
// What actually changes when a turn runs here:
//
//   - MCP servers from the agy mcp.json are connected (PiG's built-in mcp
//     extension, via internal/pigrt's translation) and their tools are
//     declared to the model;
//   - skills from the agy roots are discovered and injected into the system
//     prompt by PiG's resource loader;
//   - tool calls are executed by PiG's session, gated by the clone's recovered
//     permission policy through a BeforeToolCall hook;
//   - a resumed conversation's earlier turns are replayed into the session's
//     log before the new prompt (see pigrt.SeedHistory).
//
// Any backend drives the session. An gateway client describes its endpoint, so
// agent.NativeProvider builds the native gateway provider; every other backend
// (the offline mock the shims and parity harnesses use, and any future one) is
// wrapped by piagentbridge.BackendProvider, which reuses the BackendStreamFn
// translation the clone's loop already exercised.
package printmode

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	piagent "github.com/MichaelKinsy/PiG/agent"
	pi "github.com/MichaelKinsy/PiG/ai"
	"github.com/MichaelKinsy/PiG/coding"

	"github.com/justwasm/pigasus/internal/agent"
	"github.com/justwasm/pigasus/internal/agentmessage"
	"github.com/justwasm/pigasus/internal/agenttools"
	"github.com/justwasm/pigasus/internal/agyprompt"
	"github.com/justwasm/pigasus/internal/backend"
	"github.com/justwasm/pigasus/internal/browsersubagent"
	"github.com/justwasm/pigasus/internal/builtinsubagent"
	"github.com/justwasm/pigasus/internal/convstore"
	"github.com/justwasm/pigasus/internal/cumulativeprompt"
	"github.com/justwasm/pigasus/internal/implicit"
	"github.com/justwasm/pigasus/internal/permissions"
	"github.com/justwasm/pigasus/internal/piagentbridge"
	"github.com/justwasm/pigasus/internal/pigrt"
	"github.com/justwasm/pigasus/internal/sandbox"
	"github.com/justwasm/pigasus/internal/subagentquestions"
	"github.com/justwasm/pigasus/internal/subagenttools"
	"github.com/justwasm/pigasus/internal/toolrunner"
	"github.com/justwasm/pigasus/internal/tools"
)

// pigPrintModel builds the PiG model the session calls, keeping the clone's
// transport in the loop. An gateway backend yields the native provider (the
// gateway, the fingerprint and the *backend.HTTPError classification are
// unchanged); anything else is adapted from the backend.Client itself.
func pigPrintModel(be backend.Client, modelName string) *pi.Model {
	if provider := agent.NativeProvider(be); provider != nil {
		return provider.Model(modelName)
	}
	return piagentbridge.NewBackendProvider(be, modelName).Model(modelName)
}

// PigPrintModel builds the PiG model for a backend and model name.
func PigPrintModel(be backend.Client, modelName string) *pi.Model {
	return pigPrintModel(be, modelName)
}

// pigPrintConfig is everything the pig-runtime turn needs from runPrintMode.
type pigPrintConfig struct {
	// Backend supplies the transport the provider is derived from.
	Backend backend.Client
	// Model is the wire model id.
	Model string
	// Prompt is the user text (after slash expansion).
	Prompt string
	// History is the resumed conversation's earlier turns, replayed ahead of
	// the prompt. Nil for a fresh run.
	History []pigrt.HistoryTurn
	// Catalog is the clone's tool registry, handed to the session as extra
	// tools so the advertised surface is the reference's own names.
	Catalog *tools.Registry
	// Policy gates every tool call, MCP tools included.
	Policy *permissions.Policy
	// Handler receives the synthesised chunk stream. It is runPrintMode's
	// existing handler, so the output contract is unchanged.
	Handler func(backend.StreamChunk) error
	// Workspace is the session cwd.
	Workspace string
	// AgentDir is the agy config home.
	AgentDir string
	// ConversationID is the id the system prompt's <user_information> section
	// names. Empty mints one in pigrt.
	ConversationID string
	// Effort is the reasoning-effort level forwarded to PiG's
	// SetThinkingLevel. Empty leaves the model default.
	Effort string
	// AgentBody is the Markdown body of a --agent custom agent. Non-empty
	// selects that agent's system prompt and its restricted seven-tool
	// surface instead of the default prompt and full catalogue.
	AgentBody string
	// Definitions is the conversation's subagent definition store, filled
	// by the rehydration pass when the run continues a conversation.
	//
	// The reference scopes a definition to the conversation, so a
	// continued conversation keeps the subagents it defined (【反编译】
	// the reference's `cortex/rehydration` package; see
	// out/ide-language-server/README.md §10.9).
	Definitions *subagenttools.Store
	// Registry is the conversation's live-subagent registry.
	//
	// A delegated subagent runs in the background: the reference's
	// `invoke_subagent` returns a launch acknowledgement and the
	// subagent reports back later, as a message to the parent (【一手】
	// the 1.4.4 capture, docs/cycles/subagent-async-framing-capture/).
	// The registry holds the live children and carries their messages
	// back to the parent.
	Registry *subagenttools.Registry
}

// runPigPrintTurn runs one prompt on PiG's coding runtime and returns the names
// of the tool calls the permission policy denied (for the headless notice).
func runPigPrintTurn(ctx context.Context, cfg pigPrintConfig) ([]string, error) {
	// The conversation id names the prompt's <user_information> section.
	// It is minted here, not inside pigrt, so the prompt a `self`
	// delegation inherits names the same conversation as the parent's
	// own prompt.
	if cfg.ConversationID == "" {
		cfg.ConversationID = agyprompt.NewConversationID()
	}
	model, slot, err := preparePigPrintModel(cfg)
	if err != nil {
		return nil, err
	}
	var denials []string
	hook := createPigPrintHook(cfg.Policy, &denials)

	// The conversation's live-subagent registry. A delegated subagent
	// runs in the background and reports back as a message; the message
	// waits in the channel until the parent's next turn asks for it.
	registry := subagenttools.NewRegistry(cfg.ConversationID)
	deliveries := make(chan subagentDelivery, 8)
	registry.SetNotify(func(sender, content string) {
		select {
		case deliveries <- subagentDelivery{sender: sender, content: content}:
		default:
		}
	})
	cfg.Registry = registry
	// The children outlive the turn that launched them, so they are
	// killed when the run ends — not when the turn does.
	defer registry.Close()

	opts := pigPrintOptions(cfg, model, hook, slot)
	extraTools, cleanup, err := buildPigPrintTools(cfg, model, opts)
	if err != nil {
		return nil, err
	}
	if cleanup != nil {
		defer cleanup()
	}
	setupPrintAskQuestion(cfg.Catalog)

	opts.ExtraTools = extraTools
	rt, err := pigrt.New(ctx, opts)
	if err != nil {
		return denials, err
	}
	defer func() {
		if cerr := rt.Close(); cerr != nil {
			printModeSilenceLogger.Printf("[pigrt] close: %v", cerr)
		}
	}()

	if err := rt.SeedHistory(pigSeedHistory(cfg)); err != nil {
		return denials, err
	}
	return denials, executePigPrintRunWith(ctx, rt, cfg, deliveries)
}

// subagentDelivery is one message a subagent sent to its parent.
type subagentDelivery struct {
	// sender is the subagent's conversation id.
	sender string
	// content is the message text.
	content string
}

// deliveryWaitTimeout bounds how long the print run waits for a subagent
// to report back.
//
// The reference's headless run waits: the delivery starts the parent's
// next invocation. The clone bounds the wait so that a subagent which
// never reports cannot hang the run forever.
var deliveryWaitTimeout = 10 * time.Minute

// waitForSubagentDelivery waits for a subagent's message.
//
// The wait happens only while a subagent is still working: the
// reference's subagent stays alive after it reports (the parent can
// message it again), but there is nothing left to wait for once every
// child has finished its turn.
func waitForSubagentDelivery(ctx context.Context, registry *subagenttools.Registry, deliveries <-chan subagentDelivery) (subagentDelivery, bool) {
	// A delivery that already arrived is taken first: the child may
	// have reported while the parent's turn was still running.
	select {
	case d := <-deliveries:
		return d, true
	default:
	}
	if registry == nil || !registry.Busy() {
		return subagentDelivery{}, false
	}
	timer := time.NewTimer(deliveryWaitTimeout)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return subagentDelivery{}, false
	case <-timer.C:
		return subagentDelivery{}, false
	case d := <-deliveries:
		return d, true
	}
}

// pigSeedHistory is the session seed: the implicit trajectory's messages
// first, then the resumed conversation's earlier turns.
//
// 【反编译】 agy 1.4.7: the reference's cumulative-prompt handler converts
// the implicit trajectory — the `!` commands the user ran outside the
// conversation — into chat messages and carries them ahead of the turn. The
// messages stay out of the conversation: the reference records a `!` command
// in the implicit trajectory only.
//
// The read is best-effort: a trajectory that cannot be read leaves the seed
// as it was, and the turn runs.
func pigSeedHistory(cfg pigPrintConfig) []pigrt.HistoryTurn {
	seed := pigImplicitHistory(cfg.Workspace)
	if len(seed) == 0 {
		return cfg.History
	}
	return append(seed, cfg.History...)
}

// pigImplicitHistory turns the workspace's implicit trajectory into the
// session seed's leading turns.
func pigImplicitHistory(workspace string) []pigrt.HistoryTurn {
	if workspace == "" {
		return nil
	}
	steps, err := implicit.Open(convstore.StoreDir()).Steps(implicit.WorkspaceURI(workspace))
	if err != nil {
		return nil
	}
	msgs := cumulativeprompt.Convert(steps, cumulativeprompt.Options{})
	out := make([]pigrt.HistoryTurn, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, pigrt.HistoryTurn{Role: m.Role, Text: m.Content})
	}
	return out
}

func preparePigPrintModel(cfg pigPrintConfig) (*pi.Model, *pigrt.ErrorSlot, error) {
	model := pigPrintModel(cfg.Backend, cfg.Model)
	if model == nil {
		return nil, nil, fmt.Errorf("pig runtime: backend %T has no provider", cfg.Backend)
	}
	slot := &pigrt.ErrorSlot{}
	model.Provider = pigrt.RecordProvider(model.Provider, slot)
	return model, slot, nil
}

func buildPigPrintTools(cfg pigPrintConfig, model *pi.Model, opts pigrt.Options) ([]piagent.AgentTool, func(), error) {
	if cfg.AgentBody != "" {
		return agenttools.Tools(cfg.Catalog), nil, nil
	}
	subServices, subErr := coding.CreateAgentSessionServices(coding.CreateAgentSessionServicesOptions{
		CWD:      cfg.Workspace,
		AgentDir: cfg.AgentDir,
	})
	if subErr != nil {
		return nil, nil, fmt.Errorf("pig runtime: subagent services: %w", subErr)
	}
	subTools := subagenttools.Tools(subagenttools.Deps{
		Services: subServices,
		Model:    model,
		CWD:      cfg.Workspace,
		AgentDir: cfg.AgentDir,
		// The conversation's definition store, so a continued
		// conversation keeps the subagents it defined.
		Definitions: cfg.Definitions,
		// The parent's own prompt: the reference's `self` type
		// "inherits the parent agent's full configuration including
		// tools, system prompt, and model".
		//
		// It is resolved on demand — the prompt build walks the skills
		// directories, and a turn that spawns no subagent should not
		// pay for it.
		SystemPrompt: func() string { return pigrt.SystemPromptFor(opts) },
		// The parent's tool catalogue: a subagent's tool set is drawn
		// from it, because the reference's grants say which of the
		// parent's tools a subagent may call.
		Catalogue: piagentbridge.Tools(cfg.Catalog),
		// The conversation's live-subagent registry: a delegated
		// subagent runs in the background and is reachable by its
		// conversation id until it is killed.
		Registry: cfg.Registry,
		// The parent's own conversation id. A subagent's message
		// reaches the parent by it, and the parent's messages carry
		// it as the sender.
		ConversationID: cfg.ConversationID,
		// The conversation's one permission policy, applied to the
		// subagent's own tool calls as well.
		PermissionGateFor: printSubagentGateFactory(cfg),
	})
	// The browser_subagent tool needs the parent's model and the browser
	// tools; the runner is installed process-wide, like the MCP pool.
	installBrowserSubagent(cfg, model, subServices)
	// The browser agent is also a built-in subagent *type*, under the
	// reference's own name: that is how the model reaches it, because the
	// model's twelve wire declarations carry no browser tool (the
	// reference reaches its browser agent the same way).
	if cfg.Catalog != nil {
		reg := builtinsubagent.Default()
		if err := browsersubagent.InstallBuiltin(reg, subagenttools.Deps{
			Services: subServices,
			Model:    model,
			CWD:      cfg.Workspace,
			AgentDir: cfg.AgentDir,
		}, cfg.Catalog); err != nil {
			printModeSilenceLogger.Printf("[pigrt] browser built-in: %v", err)
		} else {
			subagenttools.SetBuiltinSubagentRegistry(reg)
		}
	}
	extraTools := piagentbridge.SurfaceTools(cfg.Catalog, subTools)
	return extraTools, func() { subServices.Close() }, nil
}

// installBrowserSubagent gives the browser_subagent tool a runner.
//
// The runner is a browser agent: a subagent whose tool set is the
// browser-control surface (internal/browsersubagent). The provider is
// process-wide because the catalogue is built without it (tools.Catalog
// takes no provider argument) and the browser tools are reachable through
// the same registry the caller already holds.
func installBrowserSubagent(cfg pigPrintConfig, model *pi.Model, services *coding.AgentSessionServices) {
	if model == nil || services == nil || cfg.Catalog == nil {
		return
	}
	tools.SetBrowserSubagentProvider(browsersubagent.New(subagenttools.Deps{
		Services: services,
		Model:    model,
		CWD:      cfg.Workspace,
		AgentDir: cfg.AgentDir,
	}, browsersubagent.Tools(cfg.Catalog)))
}

func setupPrintAskQuestion(catalog *tools.Registry) {
	if catalog != nil {
		catalog.Register(&tools.AskQuestion{
			SubAgentQuestions: subagentquestions.NewManager(true),
		})
	}
}

func createPigPrintHook(policy *permissions.Policy, denials *[]string) piagent.BeforeToolCallHook {
	return piagent.BeforeToolCallHook(func(_ context.Context, _ string, toolName string, args json.RawMessage) piagent.ToolCallHookResult {
		target := toolTarget(backend.ToolCall{Name: toolName, Arguments: args})
		if policy != nil && policy.Decide(permissions.Request{Tool: toolName, Target: target}) != permissions.Allow {
			*denials = append(*denials, toolName)
			return piagent.ToolCallHookResult{
				Block:  true,
				Reason: permissions.DeniedToolResult(toolName, target),
			}
		}
		return piagent.ToolCallHookResult{}
	})
}

// printSubagentGate judges a subagent's tool calls.
//
// The policy is the conversation's one policy — the same *permissions.Policy
// the parent's own calls go through — so a rule the user wrote for the
// parent governs the subagent too. The call is judged under both of the
// tool's names (the catalogue's and the declaration's), because the
// subagent calls the catalogue's spelling while the parent calls the
// declaration's.
//
// The sandbox is the session's own, with the subagent's worktree added: a
// subagent that runs in its own directory must be allowed to work in it.
type printSubagentGate struct {
	policy   *permissions.Policy
	sandbox  sandbox.Sandbox
	worktree string
}

// Decide reports whether the call may run.
func (g *printSubagentGate) Decide(_ context.Context, toolName string, args json.RawMessage) subagenttools.GateDecision {
	sb := g.sandbox
	if sb.Enabled && g.worktree != "" {
		extra := make([]string, 0, len(sb.ExtraPaths)+1)
		extra = append(extra, sb.ExtraPaths...)
		extra = append(extra, g.worktree)
		sb.ExtraPaths = extra
	}
	decision := toolrunner.Decide(g.policy, sb, backend.ToolCall{Name: toolName, Arguments: args})
	return subagenttools.GateDecision{Allow: decision.Allow, Reason: decision.Reason}
}

// printSubagentGateFactory builds the gate for a subagent that runs in the
// given directory.
//
// The sandbox is the session's own. The `--dangerously-skip-permissions`
// flag is read through the policy (the policy carries it as
// SkipPermissions), so the flagset is not needed here.
func printSubagentGateFactory(cfg pigPrintConfig) func(worktree string) subagenttools.BeforeToolCallGate {
	sb := sandbox.Sandbox{Enabled: false, Cwd: cfg.Workspace}
	if cfg.Policy == nil || !cfg.Policy.SkipPermissions {
		sb = printModeSandbox(nil, cfg.Workspace, cfg.ConversationID)
	}
	return func(worktree string) subagenttools.BeforeToolCallGate {
		return &printSubagentGate{policy: cfg.Policy, sandbox: sb, worktree: worktree}
	}
}

// printSubagents lists the subagents the prompt advertises.
//
// The browser agent is a built-in subagent *type*, and it comes before
// the user's own agents: the reference's list puts its built-ins first
// (the 1.4.6 capture lists self, research and image-generator ahead of
// the user's alpha and zeta).
//
// A custom agent gets neither the browser agent nor the user's own:
// its prompt has no "Available subagents" section (agyprompt.BuildAgent
// renders none) and its tool set is the restricted seven, so a type it
// cannot route to would be a lie.
func printSubagents(agentBody string) []agyprompt.Subagent {
	if agentBody != "" {
		return nil
	}
	return append([]agyprompt.Subagent{browsersubagent.Subagent()}, customSubagents()...)
}

// pigPrintOptions assembles the runtime options.
//
// The prompt-relevant fields live here, and only here, so the prompt a
// `self` delegation inherits is the parent's own prompt: pigrt.SystemPromptFor
// reads the same options the runtime does.
func pigPrintOptions(cfg pigPrintConfig, model *pi.Model, hook piagent.BeforeToolCallHook, slot *pigrt.ErrorSlot) pigrt.Options {
	return pigrt.Options{
		CWD:              cfg.Workspace,
		AgentDir:         cfg.AgentDir,
		ConversationID:   cfg.ConversationID,
		Model:            model,
		SkipBuiltinTools: true,
		AgentBody:        cfg.AgentBody,
		CustomSubagents:  printSubagents(cfg.AgentBody),
		BeforeToolCall:   []piagent.BeforeToolCallHook{hook},
		ErrorSlot:        slot,
		AutoCont:         true,
		NoSession:        true,
		MCPConfigPath:    mcpConfigPath(cfg.AgentDir),
		CompactEnabled:   ptrBool(true),
		Effort:           cfg.Effort,
	}
}

// executePigPrintRunWith runs the turn — and the turns a subagent's report
// starts.
//
// The reference's headless run waits for a delegated subagent: the launch
// acknowledgement ends the first invocation, and the subagent's message
// starts the next one (【一手】 the 1.4.4 capture: the delivery is a
// user-role content part in the second invocation). The clone runs the
// same way: the delivery is the next turn's prompt, wrapped in the
// reference's own envelope.
func executePigPrintRunWith(ctx context.Context, rt *pigrt.Runtime, cfg pigPrintConfig, deliveries <-chan subagentDelivery) error {
	prompt := cfg.Prompt
	var msgs []piagent.AgentMessage
	for {
		var runErr error
		msgs, runErr = rt.RunWith(ctx, prompt, func(delta string) {
			if err := cfg.Handler(backend.StreamChunk{Text: delta}); err != nil {
				printModeSilenceLogger.Printf("[pigrt] handler: %v", err)
			}
		}, printCompactionObserver())

		if runErr != nil {
			if herr := cfg.Handler(backend.StreamChunk{Error: runErr, Done: true}); herr != nil {
				return herr
			}
			return runErr
		}

		delivery, ok := waitForSubagentDelivery(ctx, cfg.Registry, deliveries)
		if !ok {
			break
		}
		prompt = agentmessage.Render(delivery.sender, delivery.content, time.Now(), agentmessage.PriorityHigh)
	}
	meta := chunkMetaFromMessages(msgs)
	return cfg.Handler(backend.StreamChunk{Done: true, Meta: meta})
}

// chunkMetaFromMessages maps PiG's usage to the clone's terminating-chunk
// shape. cache_read_tokens has no counterpart on the gateway transport and stays 0
// in the envelope, exactly as the clone's own parser left it.
func chunkMetaFromMessages(msgs []piagent.AgentMessage) *backend.ChunkMeta {
	usage, ok := pigrt.UsageOf(msgs)
	if !ok {
		return nil
	}
	meta := &backend.ChunkMeta{
		PromptTokens:     usage.Input,
		CompletionTokens: usage.Output,
		TotalTokens:      usage.TotalTokens,
	}
	if usage.Reasoning != nil {
		meta.ReasoningTokens = *usage.Reasoning
	}
	return meta
}

// mcpConfigPath is where the agy MCP servers live: <agent dir>/mcp.json.
func mcpConfigPath(agentDir string) string {
	if agentDir == "" {
		return ""
	}
	return filepath.Join(agentDir, "mcp.json")
}

// pigHistory converts the resolved conversation selection into the session
// seed. It lives here so main.go stays free of pigrt types.
func pigHistory(sel ConversationSelection) []pigrt.HistoryTurn {
	if len(sel.History) == 0 {
		return nil
	}
	out := make([]pigrt.HistoryTurn, 0, len(sel.History))
	for _, turn := range sel.History {
		out = append(out, pigrt.HistoryTurn{Role: turn.Role, Text: turn.Text})
	}
	return out
}

// ptrBool returns &b — used at the call site so the Options.CompactEnabled
// override reads as `ptrBool(true)` instead of the noisy `&[]bool{true}[0]`
// idiom. Cycle 227 added the *bool override specifically because
// `Options.CompactEnabled = true` would compile but read as always-on.
func ptrBool(b bool) *bool { return &b }

// printCompactionObserver is the OnEvent hook that the print path hands to
// rt.RunWith in cycle 227. It listens for PiG's CompactionStartEvent /
// CompactionEndEvent and writes one dim-gray line per event to stdout, the
// format the reference prints when AGY 1.4.0 runs auto-compact during a
// headless turn:
//
//	⎿  Compacting context… 184 K → 42 K tokens
//
// The end-event line only carries tokens-after when the upstream end event
// reported it; on overflow / aborted compact the end line falls back to
// the start line so the user still sees *something*. The values themselves
// come from event.TokensBefore / event.EstimatedTokensAfter — these are the
// numbers the reference echoes, and they are the only ones PiG guarantees.
//
// Channel / context-budget limits push compact to fire during a long
// headless run; the print path was silent on this before cycle 227.
func printCompactionObserver() pigrt.EventFunc {
	return func(ev piagent.AgentEvent) {
		switch e := ev.(type) {
		case piagent.CompactionStartEvent:
			fmt.Fprintf(os.Stdout, "%s  \u23bf  %sCompacting context…%s\n",
				cDimGrayOpen, cDimGrayClose, "",
			)
			_ = e
		case piagent.CompactionEndEvent:
			if e.TokensBefore > 0 && e.EstimatedTokensAfter > 0 {
				fmt.Fprintf(os.Stdout, "%s  \u23bf  %sCompacted context %s K → %s K tokens%s\n",
					cDimGrayOpen, cDimGrayClose,
					TokenK(e.TokensBefore), TokenK(e.EstimatedTokensAfter),
					"",
				)
			}
		}
	}
}

// TokenK renders a count of tokens as "184 K" / "1.2 M" — the dim gray
// formatting matches cycle-225's quota countdowns and agy's own
// post-compact stdout line. 0 yields "-" so the print path never prints
// "0 K → 0 K tokens" on an event that lacked the upstream figure.
func TokenK(tokens int) string {
	if tokens <= 0 {
		return "-"
	}
	if tokens < 1000 {
		return strconv.Itoa(tokens)
	}
	if tokens < 1_000_000 {
		// Round to 1 decimal, drop trailing ".0" for whole kilo figures.
		kilo := float64(tokens) / 1000.0
		return strings.TrimSuffix(strconv.FormatFloat(kilo, 'f', 1, 64), ".0") + " K"
	}
	mega := float64(tokens) / 1_000_000.0
	return strings.TrimSuffix(strconv.FormatFloat(mega, 'f', 1, 64), ".0") + " M"
}
