package agentturn

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"reflect"
	"testing"
	"time"

	acp "github.com/ironpark/acp-go"
	"github.com/justwasm/pigasus/agentlink"
)

// These scenarios drive the production constructor over newline-delimited
// JSON-RPC, not a mocked session. The fixture owns and joins its sole peer
// goroutine. Timeouts are failure bounds; ordering uses wire reads/channels.
type acpScenarioPeer struct {
	reader  *bufio.Reader
	encoder *json.Encoder
}

type acpScenarioMessage struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
}

func (p *acpScenarioPeer) decode(msg *acpScenarioMessage) error {
	line, err := p.reader.ReadBytes('\n')
	if err != nil {
		return err
	}
	return json.Unmarshal(line, msg)
}

func (p *acpScenarioPeer) read(method string) (acpScenarioMessage, error) {
	var msg acpScenarioMessage
	if err := p.decode(&msg); err != nil {
		return msg, err
	}
	if msg.Method != method {
		return msg, fmt.Errorf("method = %q, want %q", msg.Method, method)
	}
	return msg, nil
}

func (p *acpScenarioPeer) reply(id json.RawMessage, result any) error {
	return p.encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}

func (p *acpScenarioPeer) handshake() error {
	msg, err := p.read("initialize")
	if err != nil {
		return err
	}
	if err := p.reply(msg.ID, map[string]any{"protocolVersion": 1, "agentCapabilities": map[string]any{}}); err != nil {
		return err
	}
	msg, err = p.read("session/new")
	if err != nil {
		return err
	}
	return p.reply(msg.ID, map[string]any{"sessionId": "scenario-session"})
}

func (p *acpScenarioPeer) text(text string) error {
	return p.encoder.Encode(map[string]any{
		"jsonrpc": "2.0", "method": "session/update",
		"params": map[string]any{"sessionId": "scenario-session", "update": map[string]any{
			"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": text},
		}},
	})
}

func newACPScenario(t *testing.T, script func(*acpScenarioPeer) error) (*ACPRemote, context.Context) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	local, peer := net.Pipe()
	done := make(chan error, 1)
	go func() {
		defer peer.Close()
		p := &acpScenarioPeer{reader: bufio.NewReader(peer), encoder: json.NewEncoder(peer)}
		if err := p.handshake(); err != nil {
			done <- err
			return
		}
		done <- script(p)
	}()
	// Existing stream seam is shared with the production subprocess adapter.
	remote, err := NewACPRemote(ctx, local, local, nil, "/scenario", "1")
	t.Cleanup(func() {
		cancel()
		_ = local.Close()
		_ = peer.Close()
		if remote != nil {
			_ = remote.Close()
		}
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("peer script: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("peer goroutine did not exit")
		}
	})
	if err != nil {
		t.Fatalf("initialize stream remote: %v", err)
	}
	return remote, ctx
}

func acpScenarioFrames(t *testing.T, ctx context.Context, frames <-chan agentlink.Frame) []agentlink.Frame {
	t.Helper()
	var out []agentlink.Frame
	for {
		select {
		case frame, ok := <-frames:
			if !ok {
				return out
			}
			out = append(out, frame)
		case <-ctx.Done():
			t.Fatalf("frame stream did not terminate: %v", ctx.Err())
		}
	}
}

func TestACPRemoteStreamScenarios(t *testing.T) {
	t.Run("normal_initialize_prompt_and_shutdown", func(t *testing.T) {
		remote, ctx := newACPScenario(t, func(p *acpScenarioPeer) error {
			msg, err := p.read("session/prompt")
			if err != nil {
				return err
			}
			var params struct {
				SessionID string `json:"sessionId"`
				Prompt    []struct {
					Text string `json:"text"`
				} `json:"prompt"`
			}
			if err := json.Unmarshal(msg.Params, &params); err != nil {
				return err
			}
			if params.SessionID != "scenario-session" || len(params.Prompt) != 1 || params.Prompt[0].Text != "hello" {
				return fmt.Errorf("unexpected prompt: %s", msg.Params)
			}
			if err := p.text("streamed answer"); err != nil {
				return err
			}
			if err := p.reply(msg.ID, map[string]any{"stopReason": "end_turn", "usage": map[string]any{"inputTokens": 2, "outputTokens": 3, "totalTokens": 5}}); err != nil {
				return err
			}
			// Keep the peer alive until the remote explicitly closes it.
			var closeRequest acpScenarioMessage
			for {
				if err := p.decode(&closeRequest); err != nil {
					return nil
				}
				if closeRequest.Method == "session/close" {
					if err := p.reply(closeRequest.ID, map[string]any{}); err != nil {
						return err
					}
				}
			}
		})
		frames, err := remote.Turn(ctx, Turn{History: []agentlink.Message{{Role: "user", Content: "hello"}}})
		if err != nil {
			t.Fatal(err)
		}
		got := acpScenarioFrames(t, ctx, frames)
		var kinds []string
		for _, f := range got {
			kinds = append(kinds, string(f.Kind))
		}
		if !reflect.DeepEqual(kinds, []string{"text", "result"}) || got[0].Text != "streamed answer" || got[1].Error != "" || got[1].Usage == nil || got[1].Usage.TotalTokens != 5 {
			t.Fatalf("frames = %+v", got)
		}
		if err := remote.Close(); err != nil {
			t.Fatal(err)
		}
		if err := remote.Close(); err != nil {
			t.Fatalf("second Close: %v", err)
		}
		if _, err := remote.Turn(ctx, Turn{}); err == nil {
			t.Fatal("closed remote accepted a turn")
		}
	})

	t.Run("permission_rejection_round_trip", func(t *testing.T) {
		remote, ctx := newACPScenario(t, func(p *acpScenarioPeer) error {
			prompt, err := p.read("session/prompt")
			if err != nil {
				return err
			}
			if err := p.encoder.Encode(map[string]any{
				"jsonrpc": "2.0", "id": "permission-1", "method": "session/request_permission",
				"params": map[string]any{"sessionId": "scenario-session", "toolCall": map[string]any{"toolCallId": "tool-1", "title": "write file"}, "options": []any{
					map[string]any{"optionId": "allow", "name": "Allow", "kind": "allow_once"},
					map[string]any{"optionId": "deny", "name": "Deny", "kind": "reject_once"},
				}},
			}); err != nil {
				return err
			}
			response, err := p.read("")
			if err != nil {
				return err
			}
			var result struct {
				Outcome struct {
					Outcome  string `json:"outcome"`
					OptionID string `json:"optionId"`
				} `json:"outcome"`
			}
			if err := json.Unmarshal(response.Result, &result); err != nil {
				return err
			}
			if string(response.ID) != `"permission-1"` || result.Outcome.Outcome != "selected" || result.Outcome.OptionID != "deny" {
				return fmt.Errorf("permission response = %s", response.Result)
			}
			return p.reply(prompt.ID, map[string]any{"stopReason": "end_turn"})
		})
		requests := make(chan PermissionRequest, 1)
		remote.SetPermissionHandler(func(_ context.Context, req PermissionRequest) (PermissionDecision, error) {
			requests <- req
			return PermissionDecision{Approved: false, Feedback: "scenario denial"}, nil
		})
		frames, err := remote.Turn(ctx, Turn{})
		if err != nil {
			t.Fatal(err)
		}
		got := acpScenarioFrames(t, ctx, frames)
		if len(got) != 2 || got[0].Kind != agentlink.KindNotice || got[0].Notice == nil || got[0].Notice.Text != "Permission rejected: scenario denial" || got[1].Kind != agentlink.KindResult || got[1].Error != "" {
			t.Fatalf("frames = %+v", got)
		}
		select {
		case req := <-requests:
			if req.SessionID != "scenario-session" || req.ToolCallID != "tool-1" || req.Title != "write file" || len(req.Options) != 2 {
				t.Fatalf("permission request = %+v", req)
			}
		default:
			t.Fatal("permission handler was not invoked")
		}
	})

	t.Run("cancel_active_turn_then_reuse_session", func(t *testing.T) {
		started := make(chan struct{})
		remote, ctx := newACPScenario(t, func(p *acpScenarioPeer) error {
			prompt, err := p.read("session/prompt")
			if err != nil {
				return err
			}
			close(started)
			msg, err := p.read("session/cancel")
			if err != nil {
				return err
			}
			if len(msg.ID) != 0 {
				return errors.New("cancel must be a notification")
			}
			if err := p.reply(prompt.ID, map[string]any{"stopReason": "cancelled"}); err != nil {
				return err
			}
			for {
				var next acpScenarioMessage
				if err := p.decode(&next); err != nil {
					return err
				}
				if next.Method != "session/prompt" {
					return fmt.Errorf("next method = %q", next.Method)
				}
				if err := p.text("next turn"); err != nil {
					return err
				}
				return p.reply(next.ID, map[string]any{"stopReason": "end_turn"})
			}
		})
		turnCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		frames, err := remote.Turn(turnCtx, Turn{})
		if err != nil {
			t.Fatal(err)
		}
		select {
		case <-started:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
		if _, err := remote.Turn(ctx, Turn{}); err == nil {
			t.Fatal("concurrent turn accepted")
		}
		cancel()
		got := acpScenarioFrames(t, ctx, frames)
		if len(got) != 1 || got[0].Kind != agentlink.KindResult || got[0].Error != "" {
			t.Fatalf("cancel frames = %+v", got)
		}
		frames, err = remote.Turn(ctx, Turn{})
		if err != nil {
			t.Fatalf("reuse after cancel: %v", err)
		}
		got = acpScenarioFrames(t, ctx, frames)
		if len(got) != 2 || got[0].Text != "next turn" || got[1].Kind != agentlink.KindResult || got[1].Error != "" {
			t.Fatalf("next frames = %+v", got)
		}
	})
}

func TestACPRemoteStreamInitializeErrorPreservesType(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	local, peer := net.Pipe()
	defer local.Close()
	defer peer.Close()
	done := make(chan error, 1)
	go func() {
		defer peer.Close()
		p := &acpScenarioPeer{reader: bufio.NewReader(peer), encoder: json.NewEncoder(peer)}
		msg, err := p.read("initialize")
		if err == nil {
			err = p.encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": msg.ID, "error": map[string]any{"code": -32602, "message": "scenario invalid initialize"}})
		}
		done <- err
	}()
	_, err := NewACPRemote(ctx, local, local, nil, "/scenario", "1")
	if !acp.IsCode(err, acp.ErrorCodeInvalidParams) {
		t.Errorf("initialize error lost type: %T %v", err, err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("peer did not finish")
	}
}
