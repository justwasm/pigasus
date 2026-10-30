// ls_terminals_test.go: the Web UI's terminal surface at the
// dispatcher's level.
//
// The expectations are the reference's own answers, measured against
// the reference's language_server on 2026-10-27 (see
// ls_terminals.go's header for the captures).
package remotecontrol

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/rcterminal"
)

// newTerminalDispatcher builds a dispatcher whose Web UI's terminal
// surface is wired to a registry.
func newTerminalDispatcher(t *testing.T) *Dispatcher {
	t.Helper()
	registry := rcterminal.NewRegistry()
	registry.SetWorkspace(t.TempDir())
	disp, err := NewDispatcher(Config{
		Session:      NewSessionState("1.4.7"),
		WebTerminals: registry,
	})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	return disp
}

// TestCreateTerminalAnswersTheReferenceShape pins the create answer:
// the terminal's own fields, under the "terminal" member.
func TestCreateTerminalAnswersTheReferenceShape(t *testing.T) {
	disp := newTerminalDispatcher(t)
	body, err := disp.DispatchDirect(context.Background(), "CreateTerminal",
		`{"workspaceUri":"file:///tmp/web-ws","conversationId":"c-1","projectId":"p-1"}`)
	if err != nil {
		t.Fatalf("CreateTerminal: %v", err)
	}
	var answer struct {
		Terminal struct {
			TerminalID     string `json:"terminalId"`
			PID            int    `json:"pid"`
			Title          string `json:"title"`
			ConversationID string `json:"conversationId"`
			ProjectID      string `json:"projectId"`
			WorkspaceURI   string `json:"workspaceUri"`
			CreatedAt      string `json:"createdAt"`
		} `json:"terminal"`
	}
	if err := json.Unmarshal([]byte(body), &answer); err != nil {
		t.Fatalf("the answer is not JSON: %v (%s)", err, body)
	}
	terminal := answer.Terminal
	if terminal.TerminalID == "" {
		t.Fatalf("the answer has no terminal id: %s", body)
	}
	if terminal.Title != "bash" {
		t.Fatalf("the title is %q, want the shell's name", terminal.Title)
	}
	if terminal.PID <= 0 {
		t.Fatalf("the pid is %d", terminal.PID)
	}
	if terminal.ConversationID != "c-1" || terminal.ProjectID != "p-1" {
		t.Fatalf("the conversation/project are %q/%q", terminal.ConversationID, terminal.ProjectID)
	}
	if !strings.HasPrefix(terminal.WorkspaceURI, "file:///") {
		t.Fatalf("the workspace uri is %q", terminal.WorkspaceURI)
	}
	if terminal.CreatedAt == "" {
		t.Fatalf("the answer has no createdAt: %s", body)
	}
	_, _ = disp.DispatchDirect(context.Background(), "CloseTerminal",
		`{"terminalId":"`+terminal.TerminalID+`","force":true}`)
}

// TestListTerminalsOmitsAnEmptyList pins the proto-JSON rule: an empty
// repeated field is omitted, so a filter that matches nothing answers
// `{}`.
func TestListTerminalsOmitsAnEmptyList(t *testing.T) {
	disp := newTerminalDispatcher(t)
	body, err := disp.DispatchDirect(context.Background(), "ListTerminals",
		`{"conversationId":"nobody"}`)
	if err != nil {
		t.Fatalf("ListTerminals: %v", err)
	}
	if strings.Contains(body, "terminals") {
		t.Fatalf("the answer carries an empty list: %s", body)
	}
	if strings.TrimSpace(body) != "{}" {
		t.Fatalf("the answer is %s, want {}", body)
	}
}

// TestSendTerminalInputAcknowledgesTheOffset pins the input rule: the
// answer carries the offset the stream reached, and the field is
// omitted when it is zero (a pure resize answers `{}`).
func TestSendTerminalInputAcknowledgesTheOffset(t *testing.T) {
	disp := newTerminalDispatcher(t)
	body, err := disp.DispatchDirect(context.Background(), "CreateTerminal", `{}`)
	if err != nil {
		t.Fatalf("CreateTerminal: %v", err)
	}
	var answer struct {
		Terminal struct {
			TerminalID string `json:"terminalId"`
		} `json:"terminal"`
	}
	if err := json.Unmarshal([]byte(body), &answer); err != nil {
		t.Fatalf("the answer is not JSON: %v", err)
	}
	id := answer.Terminal.TerminalID

	input := base64.StdEncoding.EncodeToString([]byte("echo hi\n"))
	body, err = disp.DispatchDirect(context.Background(), "SendTerminalInput",
		`{"terminalId":"`+id+`","inputStreamId":"s-1","inputOffset":"0","input":"`+input+`"}`)
	if err != nil {
		t.Fatalf("SendTerminalInput: %v", err)
	}
	if !strings.Contains(body, `"ackedOffset":"8"`) {
		t.Fatalf("the answer is %s, want the acknowledged offset", body)
	}

	// A resize answers an empty body: the offset is zero and
	// proto-JSON omits zero-valued fields.
	body, err = disp.DispatchDirect(context.Background(), "SendTerminalInput",
		`{"terminalId":"`+id+`","resize":{"cols":100,"rows":30}}`)
	if err != nil {
		t.Fatalf("SendTerminalInput: %v", err)
	}
	if strings.TrimSpace(body) != "{}" {
		t.Fatalf("the resize answer is %s, want {}", body)
	}
	_, _ = disp.DispatchDirect(context.Background(), "CloseTerminal", `{"terminalId":"`+id+`","force":true}`)
}

// TestAMissingTerminalAnswersNotFound pins the error: a terminal that
// is not there answers with the not-found code, the way the reference
// answers it.
func TestAMissingTerminalAnswersNotFound(t *testing.T) {
	disp := newTerminalDispatcher(t)
	_, err := disp.DispatchDirect(context.Background(), "SendTerminalInput",
		`{"terminalId":"no-such-terminal","inputStreamId":"s-1","inputOffset":"0","input":"aGk="}`)
	if err == nil {
		t.Fatal("the call answered, want a not-found")
	}
	var coded *rpcCodedError
	if !errors.As(err, &coded) {
		t.Fatalf("the error is %T, want a coded error", err)
	}
	if coded.Code != -32018 {
		t.Fatalf("the code is %d, want -32018", coded.Code)
	}
	if !strings.Contains(coded.ConnectMessage(), "not_found") {
		t.Fatalf("the message is %q, want the reference's shape", coded.ConnectMessage())
	}
}

// TestAMalformedRequestAnswersInvalidArgument pins the other error:
// a request the decoder refuses answers with the invalid-argument
// code and the reference's message shape.
func TestAMalformedRequestAnswersInvalidArgument(t *testing.T) {
	disp := newTerminalDispatcher(t)
	_, err := disp.DispatchDirect(context.Background(), "CreateTerminal", `{"workspaceUri":123}`)
	if err == nil {
		t.Fatal("the call answered, want an invalid-argument")
	}
	var coded *rpcCodedError
	if !errors.As(err, &coded) {
		t.Fatalf("the error is %T, want a coded error", err)
	}
	if coded.Code != -32602 {
		t.Fatalf("the code is %d, want -32602", coded.Code)
	}
	message := coded.ConnectMessage()
	for _, want := range []string{
		"unmarshal message: unmarshal into *language_server_go_proto.CreateTerminalRequest",
		"invalid value for string field workspaceUri: 123",
	} {
		if !strings.Contains(message, want) {
			t.Fatalf("the message is %q, want it to carry %q", message, want)
		}
	}
}

// TestTheDashboardTerminalContractIsUntouched pins the separation: the
// clone's own RemoteAgentTerminal* surface keeps its own contract.
func TestTheDashboardTerminalContractIsUntouched(t *testing.T) {
	disp := newTerminalDispatcher(t)
	// The dashboard's create asks for a kind; the Web UI's does not.
	_, err := disp.DispatchDirect(context.Background(), "CreateRemoteAgentTerminal",
		`{"conversation_id":"c-1","kind":"output"}`)
	if err != nil {
		t.Fatalf("CreateRemoteAgentTerminal: %v", err)
	}
	// The same name spelled the way the reference's remote agent
	// proxy spells it reaches the Web UI's own handler now.
	_, err = disp.DispatchDirect(context.Background(), "CreateTerminal",
		`{"conversationId":"c-1"}`)
	if err != nil {
		t.Fatalf("CreateTerminal: %v", err)
	}
}

func TestStreamTerminalOutputEndToEnd(t *testing.T) {
	disp := newTerminalDispatcher(t)
	body, err := disp.DispatchDirect(context.Background(), "CreateTerminal",
		`{"workspaceUri":"file:///tmp/web-ws","conversationId":"c-1","projectId":"p-1"}`)
	if err != nil {
		t.Fatalf("CreateTerminal: %v", err)
	}
	var answer struct {
		Terminal struct {
			TerminalID string `json:"terminalId"`
		} `json:"terminal"`
	}
	if err := json.Unmarshal([]byte(body), &answer); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	terminalID := answer.Terminal.TerminalID
	if terminalID == "" {
		t.Fatalf("no terminalId in answer: %s", body)
	}

	frames := make(chan json.RawMessage, 10)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	streamErr := make(chan error, 1)
	go func() {
		handled, err := disp.DispatchStream(ctx, "StreamTerminalOutput", `{"terminalId":"`+terminalID+`"}`, func(msg json.RawMessage) error {
			frames <- msg
			return nil
		})
		if !handled {
			streamErr <- errors.New("StreamTerminalOutput not handled")
			return
		}
		streamErr <- err
	}()

	// Expect title frame first
	select {
	case frame := <-frames:
		t.Logf("Got first frame: %s", string(frame))
		if !strings.Contains(string(frame), "title") {
			t.Fatalf("expected title frame, got: %s", string(frame))
		}
	case err := <-streamErr:
		t.Fatalf("stream ended unexpectedly: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for first frame")
	}

	// Close terminal
	_, err = disp.DispatchDirect(context.Background(), "CloseTerminal", `{"terminalId":"`+terminalID+`","force":true}`)
	if err != nil {
		t.Fatalf("CloseTerminal: %v", err)
	}
}
