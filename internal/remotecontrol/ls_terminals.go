// ls_terminals.go: the Web UI's terminal surface.
//
// 【一手】 2026-10-27, measured against the reference's own
// language_server (a standalone `language_server` on 127.0.0.1:64019
// with `-csrf_token=…`), the Web UI's terminal is a *different*
// contract from the clone's dashboard terminal surface
// (terminals_dispatcher.go):
//
//	the methods are the LanguageServerService's own —
//	CreateTerminal, ListTerminals, SendTerminalInput,
//	StreamTerminalOutput, CloseTerminal — and the messages are the
//	reference's exa.language_server_pb ones.
//
// The reference's own answers, verbatim:
//
//	CreateTerminal {"workspaceUri":"file:///tmp/web-ws",
//	                "conversationId":"conv-1","projectId":"proj-1"}
//	  → 200 {"terminal":{"terminalId":"cab3427e-…","pid":95383,
//	                     "title":"bash","conversationId":"conv-1",
//	                     "projectId":"proj-1",
//	                     "workspaceUri":"file:///private/tmp/web-ws",
//	                     "createdAt":"2026-10-26T18:18:53.197975Z"}}
//
//	ListTerminals {"conversationId":…,"projectId":…,"workspaceUri":…}
//	  → 200 {"terminals":[{"terminalId":…,"pid":…,"title":"bash",…}]}
//
//	SendTerminalInput {"terminalId":…,"inputStreamId":"s-1",
//	                   "inputOffset":"0","input":"<base64>"}
//	  → 200 {"ackedOffset":"14"}
//
//	SendTerminalInput {"terminalId":…,"resize":{"cols":100,"rows":30}}
//	  → 200 {}
//
//	CloseTerminal {"terminalId":…}
//	  → 200 {}
//
// The rules the measurements pin:
//
//   - The workspace URI is canonicalised (`file:///tmp/web-ws` comes
//     back as `file:///private/tmp/web-ws`).
//   - `splitFromTerminalId` joins the new terminal to the source's
//     split group; the group id is `split-<the terminal that heads
//     the group>`.
//   - `ListTerminals` filters on every field it is given; an empty
//     field filters on nothing.
//   - The input acknowledgement is a byte offset per input stream:
//     a chunk that does not line up with the offset reached so far
//     is dropped, and the answer keeps the offset reached. The
//     answer omits `ackedOffset` when it is zero — proto-JSON omits
//     zero-valued fields — so a pure resize answers `{}`.
//   - A closed terminal is gone: closing it twice answers
//     `not_found`.
//
// The Web UI reads the answers through protobuf-es, whose oneof
// JSON form is the canonical proto-JSON the reference speaks: the
// member name sits at the top level (`{"output":"<base64>"}`), not
// in a `{case,value}` envelope. The oneof envelope is the
// JavaScript object shape — the transport flattens it.
package remotecontrol

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/justwasm/pigasus/internal/rcterminal"
)

// lsTerminalRequest is the reference's CreateTerminalRequest.
type lsTerminalRequest struct {
	WorkspaceURI        string `json:"workspaceUri"`
	ConversationID      string `json:"conversationId"`
	ProjectID           string `json:"projectId"`
	SplitFromTerminalID string `json:"splitFromTerminalId"`
}

// lsTerminalListRequest is the reference's ListTerminalsRequest.
type lsTerminalListRequest struct {
	ConversationID string `json:"conversationId"`
	ProjectID      string `json:"projectId"`
	WorkspaceURI   string `json:"workspaceUri"`
}

// lsTerminalCloseRequest is the reference's CloseTerminalRequest.
type lsTerminalCloseRequest struct {
	TerminalID string `json:"terminalId"`
	Force      bool   `json:"force"`
}

// lsTerminalSize is the reference's TerminalSize.
type lsTerminalSize struct {
	Cols int `json:"cols"`
	Rows int `json:"rows"`
}

// lsTerminalInputRequest is the reference's SendTerminalInputRequest.
//
// The request's action is a oneof — `input` (bytes) or `resize`
// (TerminalSize) — and proto-JSON writes the selected member at the
// top level.
type lsTerminalInputRequest struct {
	TerminalID    string          `json:"terminalId"`
	InputStreamID string          `json:"inputStreamId"`
	InputOffset   lsInt64         `json:"inputOffset"`
	Input         string          `json:"input"`
	Resize        *lsTerminalSize `json:"resize"`
}

// lsCreateTerminal answers the Web UI's CreateTerminal.
//
// The answer is the reference's: {"terminal":{…}} with the terminal's
// own fields.
func (d *Dispatcher) lsCreateTerminal(params json.RawMessage) (any, *rpcErrorBody) {
	var req lsTerminalRequest
	if len(params) > 0 {
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, invalidParams(lsUnmarshalError("CreateTerminalRequest", err, params))
		}
	}
	registry := d.cfg.WebTerminals
	if registry == nil {
		return nil, &rpcErrorBody{Code: -32603, Message: "Internal error", Data: "terminals are not wired"}
	}
	info, err := registry.Create(rcterminal.CreateRequest{
		WorkspaceURI:        req.WorkspaceURI,
		ConversationID:      req.ConversationID,
		ProjectID:           req.ProjectID,
		SplitFromTerminalID: req.SplitFromTerminalID,
	})
	if err != nil {
		return nil, lsTerminalError(err)
	}
	return map[string]any{"terminal": info}, nil
}

// lsListTerminals answers the Web UI's ListTerminals.
func (d *Dispatcher) lsListTerminals(params json.RawMessage) (any, *rpcErrorBody) {
	var req lsTerminalListRequest
	if len(params) > 0 {
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, invalidParams(lsUnmarshalError("ListTerminalsRequest", err, params))
		}
	}
	registry := d.cfg.WebTerminals
	if registry == nil {
		return nil, &rpcErrorBody{Code: -32603, Message: "Internal error", Data: "terminals are not wired"}
	}
	terminals := registry.List(req.ConversationID, req.ProjectID, req.WorkspaceURI)
	if len(terminals) == 0 {
		// proto-JSON omits an empty repeated field: the reference
		// answers a filter that matches nothing with `{}`.
		return map[string]any{}, nil
	}
	return map[string]any{"terminals": terminals}, nil
}

// lsCloseTerminal answers the Web UI's CloseTerminal.
func (d *Dispatcher) lsCloseTerminal(params json.RawMessage) (any, *rpcErrorBody) {
	var req lsTerminalCloseRequest
	if len(params) > 0 {
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, invalidParams(lsUnmarshalError("CloseTerminalRequest", err, params))
		}
	}
	registry := d.cfg.WebTerminals
	if registry == nil {
		return nil, &rpcErrorBody{Code: -32603, Message: "Internal error", Data: "terminals are not wired"}
	}
	if err := registry.Close(req.TerminalID, req.Force); err != nil {
		return nil, lsTerminalError(err)
	}
	return map[string]any{}, nil
}

// lsSendTerminalInput answers the Web UI's SendTerminalInput.
//
// The answer carries the offset the input stream has reached. The
// field is omitted when it is zero — the reference's proto-JSON
// omits zero-valued fields, so a pure resize answers `{}`.
func (d *Dispatcher) lsSendTerminalInput(params json.RawMessage) (any, *rpcErrorBody) {
	var req lsTerminalInputRequest
	if len(params) > 0 {
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, invalidParams(lsUnmarshalError("SendTerminalInputRequest", err, params))
		}
	}
	registry := d.cfg.WebTerminals
	if registry == nil {
		return nil, &rpcErrorBody{Code: -32603, Message: "Internal error", Data: "terminals are not wired"}
	}
	if req.Resize != nil {
		if err := registry.Resize(req.TerminalID, req.Resize.Cols, req.Resize.Rows); err != nil {
			return nil, lsTerminalError(err)
		}
		return map[string]any{}, nil
	}
	var data []byte
	if req.Input != "" {
		decoded, err := base64.StdEncoding.DecodeString(req.Input)
		if err != nil {
			return nil, invalidParams(lsInvalidBytesField("SendTerminalInputRequest", "input", req.Input))
		}
		data = decoded
	}
	acked, err := registry.SendInput(req.TerminalID, req.InputStreamID, int64(req.InputOffset), data)
	if err != nil {
		return nil, lsTerminalError(err)
	}
	answer := map[string]any{}
	if acked > 0 {
		answer["ackedOffset"] = strconv.FormatInt(acked, 10)
	}
	return answer, nil
}

// lsTerminalError maps a registry error onto the dispatcher's error
// body.
//
// The reference answers a terminal that is gone with `not_found` and
// the message "not_found: terminal <id> not found"; the clone's
// JSON-RPC surface has no such code, so the sentinel the dashboard's
// terminal surface already uses (-32018) carries the reference's own
// message.
func lsTerminalError(err error) *rpcErrorBody {
	var notFound *rcterminal.NotFoundError
	if errors.As(err, &notFound) {
		return &rpcErrorBody{
			Code:    -32018,
			Message: "not_found: terminal " + notFound.TerminalID + " not found",
		}
	}
	if errors.Is(err, rcterminal.ErrClosed) {
		return &rpcErrorBody{Code: -32019, Message: "terminal closed"}
	}
	return &rpcErrorBody{Code: -32603, Message: "Internal error", Data: err.Error()}
}

// lsInt64 reads an int64 the way proto-JSON writes one: as a
// string. A bare number is accepted too, because the Web UI's own
// optimistic copy sends the number.
type lsInt64 int64

// UnmarshalJSON reads the number.
func (v *lsInt64) UnmarshalJSON(b []byte) error {
	text := strings.Trim(strings.TrimSpace(string(b)), `"`)
	if text == "" || text == "null" {
		*v = 0
		return nil
	}
	n, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return err
	}
	*v = lsInt64(n)
	return nil
}
