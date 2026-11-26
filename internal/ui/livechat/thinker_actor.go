package livechat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/justwasm/pigasus/actor"
	"github.com/justwasm/pigasus/internal/app/agentturn"
	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/features/models/catalog"
	"github.com/justwasm/pigasus/internal/infra/backend/backendselect"
	"github.com/justwasm/pigasus/internal/voice"
)

// nativeLiveSessionAdapter adapts *voice.NativeLiveSession to voice.LiveSession
// for voice.ThinkerBridge.Dispatch.
type nativeLiveSessionAdapter struct {
	sess *voice.NativeLiveSession
}

func (a *nativeLiveSessionAdapter) SendAudio([]byte) error { return nil }

func (a *nativeLiveSessionAdapter) SendText(text string) error {
	if a.sess == nil {
		return nil
	}
	return a.sess.SendText(text)
}

func (a *nativeLiveSessionAdapter) SendToolResponse(id string, name string, result any) error {
	if a.sess == nil || id == "" {
		return nil
	}
	if name == "call_thinker_agent" {
		return a.sess.CompleteHandoff(id, result)
	}
	return a.sess.SendToolResponse(id, name, result)
}

func (a *nativeLiveSessionAdapter) SendThinkerNote(note string) error {
	if a.sess == nil {
		return nil
	}
	return a.sess.SendThinkerNote(note)
}

func (a *nativeLiveSessionAdapter) SendThinkerAnswer(answer string) error {
	if a.sess == nil {
		return nil
	}
	return a.sess.SendThinkerAnswer(answer)
}

func (a *nativeLiveSessionAdapter) SendWaitingOnUser(question string) error {
	if a.sess == nil {
		return nil
	}
	return a.sess.SendWaitingOnUser(question)
}

func (a *nativeLiveSessionAdapter) SendTaskStopped() error {
	if a.sess == nil {
		return nil
	}
	return a.sess.SendTaskStopped()
}

func (a *nativeLiveSessionAdapter) SendMedia(mimeType string, data []byte) error {
	if a.sess == nil {
		return nil
	}
	return a.sess.SendMedia(mimeType, data)
}

func (a *nativeLiveSessionAdapter) Receive() (*voice.LiveServerEvent, error) {
	return nil, io.EOF
}

func (a *nativeLiveSessionAdapter) Close() error { return nil }

func readWorkspaceFile(workspace string, path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", errors.New("read_file: path is required")
	}
	if strings.TrimSpace(workspace) == "" {
		workspace = "."
	}
	absWs, err := filepath.Abs(workspace)
	if err != nil {
		return "", fmt.Errorf("read_file: resolve workspace: %w", err)
	}
	realWs, err := filepath.EvalSymlinks(absWs)
	if err != nil {
		return "", fmt.Errorf("read_file: resolve workspace symlinks: %w", err)
	}

	candidate := path
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(absWs, candidate)
	}
	absTarget, err := filepath.Abs(candidate)
	if err != nil {
		return "", fmt.Errorf("read_file: resolve path: %w", err)
	}
	realTarget, err := filepath.EvalSymlinks(absTarget)
	if err != nil {
		return "", err
	}

	rel, err := filepath.Rel(realWs, realTarget)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("read_file: path %q escapes workspace %q", path, workspace)
	}

	data, err := os.ReadFile(realTarget)
	if err != nil {
		return "", err
	}
	content := string(data)
	if len(content) > 4000 {
		content = content[:4000] + "\n...(truncated)"
	}
	return content, nil
}

func parseToolArgs(input string) (map[string]any, error) {
	if strings.TrimSpace(input) == "" {
		return map[string]any{}, nil
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(input), &args); err != nil {
		return nil, err
	}
	if args == nil {
		args = map[string]any{}
	}
	return args, nil
}

func firstStringArg(args map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := args[k].(string); ok {
			if trimmed := strings.TrimSpace(v); trimmed != "" {
				return trimmed
			}
		}
	}
	return ""
}

func newLiveChatThinkerTools(workspace string) []actor.Tool {
	searchTool := actor.NewFuncTool(
		"search_web",
		"Search the web for up-to-date information, definitions, documentation, news, or general knowledge queries.",
		func(ctx context.Context, input string) (string, error) {
			args, err := parseToolArgs(input)
			if err != nil {
				return "", fmt.Errorf("search_web: invalid arguments: %w", err)
			}
			query := firstStringArg(args, "query", "q")
			if query == "" {
				return "", errors.New("search_web: query is required")
			}
			domain := firstStringArg(args, "domain")
			searchCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
			defer cancel()
			return performWebSearch(searchCtx, query, domain)
		},
		actor.WithParameters(map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "The search query string.",
				},
				"domain": map[string]any{
					"type":        "string",
					"description": "Optional domain filter (e.g. github.com).",
				},
			},
			"required": []string{"query"},
		}),
	)

	fetchTool := actor.NewFuncTool(
		"fetch_url",
		"Fetch and read the text content of an HTTP or HTTPS URL (documentation, GitHub files, changelogs, web pages).",
		func(ctx context.Context, input string) (string, error) {
			args, err := parseToolArgs(input)
			if err != nil {
				return "", fmt.Errorf("fetch_url: invalid arguments: %w", err)
			}
			rawURL := firstStringArg(args, "url", "Url")
			if rawURL == "" {
				return "", errors.New("fetch_url: url is required")
			}
			fetchCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
			defer cancel()
			return fetchURLContent(fetchCtx, rawURL)
		},
		actor.WithParameters(map[string]any{
			"type": "object",
			"properties": map[string]any{
				"url": map[string]any{
					"type":        "string",
					"description": "The HTTP or HTTPS URL to fetch.",
				},
			},
			"required": []string{"url"},
		}),
	)

	runCmdTool := actor.NewFuncTool(
		"run_command",
		"Execute a shell command in the workspace terminal and return its output.",
		func(ctx context.Context, input string) (string, error) {
			args, err := parseToolArgs(input)
			if err != nil {
				return "", fmt.Errorf("run_command: invalid arguments: %w", err)
			}
			cmdStr := firstStringArg(args, "command", "cmd", "CommandLine")
			if cmdStr == "" {
				return "", errors.New("run_command: command is required")
			}
			cmdCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
			defer cancel()
			return executeRawCommand(cmdCtx, workspace, cmdStr)
		},
		actor.WithParameters(map[string]any{
			"type": "object",
			"properties": map[string]any{
				"command": map[string]any{
					"type":        "string",
					"description": "The exact shell command to execute.",
				},
			},
			"required": []string{"command"},
		}),
	)

	readFileTool := actor.NewFuncTool(
		"read_file",
		"Read the contents of a file in the local workspace.",
		func(ctx context.Context, input string) (string, error) {
			args, err := parseToolArgs(input)
			if err != nil {
				return "", fmt.Errorf("read_file: invalid arguments: %w", err)
			}
			path := firstStringArg(args, "path", "Path", "file_path", "file")
			return readWorkspaceFile(workspace, path)
		},
		actor.WithParameters(map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Path to the file to read.",
				},
			},
			"required": []string{"path"},
		}),
	)

	return []actor.Tool{searchTool, fetchTool, runCmdTool, readFileTool}
}

func liveChatThinkerSystemPrompt(workspace string) string {
	return fmt.Sprintf(`You are the background Thinker agent for LiveChat operating in workspace: %s.
Use your tools (search_web, fetch_url, run_command, read_file) to gather real ground-truth information and execute requested tasks.
Never fabricate command outputs, file contents, or web results.
Return a clear, concise, factual final answer suitable for spoken summarization.`, workspace)
}

func formatThinkerPromptWithHistory(task string, history []voice.LiveTurnRecord) string {
	task = strings.TrimSpace(task)
	if len(history) == 0 {
		return task
	}
	var sb strings.Builder
	sb.WriteString("Recent conversation context:\n")
	for _, rec := range history {
		content := strings.TrimSpace(rec.Content)
		if content == "" {
			continue
		}
		sb.WriteString(fmt.Sprintf("- %s: %s\n", rec.Role, content))
	}
	sb.WriteString("\nTask:\n")
	sb.WriteString(task)
	return sb.String()
}

func newLiveChatThinkerWithProvider(
	workspace string,
	model string,
	provider actor.Provider,
	opts ...voice.ThinkerBridgeOption,
) (*actor.Actor, *voice.ThinkerBridge, error) {
	if provider == nil {
		return nil, nil, nil
	}
	act, err := actor.Spawn(actor.Config{
		Default: actor.TurnConfig{
			Model:        model,
			SystemPrompt: liveChatThinkerSystemPrompt(workspace),
			Provider:     provider,
			Tools:        newLiveChatThinkerTools(workspace),
		},
	})
	if err != nil {
		return nil, nil, err
	}
	return act, voice.NewThinkerBridge(act, opts...), nil
}

func newLiveChatThinker(
	workspace string,
	model string,
	mock bool,
	opts ...voice.ThinkerBridgeOption,
) (*actor.Actor, *voice.ThinkerBridge, error) {
	if mock {
		return nil, nil, nil
	}
	model = strings.TrimSpace(model)
	if model == "" {
		model, _ = catalog.ResolveEffectiveModel("")
	}
	be := backendselect.Pick(nil, model, nil)
	if be == nil || backend.IsMock(be) {
		return nil, nil, nil
	}
	provider := agentturn.NewBackendProviderAdapter(be, model)
	return newLiveChatThinkerWithProvider(workspace, model, provider, opts...)
}
