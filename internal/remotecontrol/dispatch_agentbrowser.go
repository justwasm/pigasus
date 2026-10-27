// ls_agentbrowser.go: the Web UI's agent↔browser bridge.
//
// The reference's language server carries three RPCs that connect the
// agent half (the `webview_browser` tool) to the IDE half (the
// frontend's AgentBrowserListener):
//
//	SubscribeToAgentBrowserRequests   (server-streaming)
//	UpdateAgentBrowserSubscription
//	FulfillAgentBrowserRequest
//
// 【一手】 2026-10-27, the reference's own descriptor set
// (out/ide-language-server/protos/language_server.protoset):
//
//	exa.language_server_pb.SubscribeToAgentBrowserRequestsRequest
//	  window_id, surface, open_conversation_ids,
//	  supports_isolated_world, displayed_root_conversation_id,
//	  backend_instructions
//	exa.language_server_pb.SubscribeToAgentBrowserRequestsResponse
//	  request  (AgentBrowserWireRequest)
//	exa.language_server_pb.UpdateAgentBrowserSubscriptionRequest
//	  window_id, displayed_root_conversation_id,
//	  supports_isolated_world
//	exa.language_server_pb.FulfillAgentBrowserRequestRequest
//	  response  (AgentBrowserWireResponse)
//
// The member names on the wire are proto-JSON's lowerCamelCase
// spellings; the reference's frontend reads exactly these
// (`J.paramsJson`, `J.requestId`, `J.targetWindowId`, `windowId`,
// `openConversationIds`, `displayedRootConversationId`,
// `supportsIsolatedWorld`, `backendInstructions`).
//
// The subscribe stream answers one frame per browser action:
//
//	{"request":{"requestId":"…","action":"click","paramsJson":"{…}",…}}
package remotecontrol

import (
	"context"
	"encoding/json"

	"github.com/justwasm/pigasus/agentbrowser"
)

// lsSubscribeToAgentBrowserRequests serves the Web UI's
// SubscribeToAgentBrowserRequests stream.
//
// The stream is the IDE half's side of the bridge: the window
// registers itself and then receives one frame per browser action the
// agent asked for. It stays open until the client goes away.
//
// 【一手】 2026-10-27, the reference's frontend opens this stream from
// its AgentBrowserListener and answers every frame with
// FulfillAgentBrowserRequest.
func (d *Dispatcher) lsSubscribeToAgentBrowserRequests(ctx context.Context, payload string, emit func(json.RawMessage) error) (bool, error) {
	broker := d.cfg.Browser
	if broker == nil {
		return false, nil
	}
	var req agentbrowser.SubscribeRequest
	if len(payload) > 0 {
		if err := json.Unmarshal([]byte(payload), &req); err != nil {
			return true, err
		}
	}
	sub := broker.Subscribe(ctx, req)
	defer sub.Close()
	for {
		select {
		case <-ctx.Done():
			return true, nil
		case request, ok := <-sub.Requests:
			if !ok {
				return true, nil
			}
			frame, err := json.Marshal(map[string]any{"request": request})
			if err != nil {
				return true, err
			}
			if err := emit(frame); err != nil {
				return true, err
			}
		}
	}
}

// routeAgentBrowserMethods routes the two unary methods of the
// agent↔browser bridge.
//
// The stream method (SubscribeToAgentBrowserRequests) lives in the
// dispatcher's stream table instead.
func (d *Dispatcher) routeAgentBrowserMethods(method string, params json.RawMessage) (any, *rpcErrorBody, bool) {
	switch method {
	case "UpdateAgentBrowserSubscription", "updateAgentBrowserSubscription":
		// The Web UI's half of the bridge: the window tells the
		// server which conversation it shows now.
		return d.lsUpdateAgentBrowserSubscription(params)
	case "FulfillAgentBrowserRequest", "fulfillAgentBrowserRequest":
		// The Web UI's answer to one browser action the agent asked
		// for.
		return d.lsFulfillAgentBrowserRequest(params)
	}
	return nil, nil, false
}

// lsUpdateAgentBrowserSubscription serves the Web UI's
// UpdateAgentBrowserSubscription.
//
// The window tells the server which conversation it shows now; the
// server uses that to pick the window that runs an action.
//
// 【反编译】 2026-10-27, the reference refuses a request without a
// window id with "UpdateAgentBrowserSubscription requires window_id".
func (d *Dispatcher) lsUpdateAgentBrowserSubscription(params json.RawMessage) (any, *rpcErrorBody, bool) {
	broker := d.cfg.Browser
	if broker == nil {
		return nil, &rpcErrorBody{Code: -32603, Message: "agent browser bridge is not available"}, true
	}
	var req agentbrowser.UpdateSubscriptionRequest
	if len(params) > 0 {
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, &rpcErrorBody{Code: -32602, Message: err.Error()}, true
		}
	}
	if err := broker.UpdateWindow(req); err != nil {
		return nil, &rpcErrorBody{Code: -32602, Message: err.Error()}, true
	}
	return map[string]any{}, nil, true
}

// lsFulfillAgentBrowserRequest serves the Web UI's
// FulfillAgentBrowserRequest.
//
// The window answers one browser action; the broker hands the answer
// to the agent half, which is waiting for it.
//
// 【反编译】 2026-10-27, the reference refuses a request without a
// response with "FulfillAgentBrowserRequest requires a non-nil
// response payload" and a response without a request id with
// "FulfillAgentBrowserRequest requires response.request_id".
func (d *Dispatcher) lsFulfillAgentBrowserRequest(params json.RawMessage) (any, *rpcErrorBody, bool) {
	broker := d.cfg.Browser
	if broker == nil {
		return nil, &rpcErrorBody{Code: -32603, Message: "agent browser bridge is not available"}, true
	}
	var req agentbrowser.FulfillRequest
	if len(params) > 0 {
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, &rpcErrorBody{Code: -32602, Message: err.Error()}, true
		}
	}
	if req.Response == nil {
		return nil, &rpcErrorBody{
			Code:    -32602,
			Message: "FulfillAgentBrowserRequest requires a non-nil response payload",
		}, true
	}
	if req.Response.RequestID == "" {
		return nil, &rpcErrorBody{
			Code:    -32602,
			Message: "FulfillAgentBrowserRequest requires response.request_id",
		}, true
	}
	if err := broker.Fulfill(req.Response); err != nil {
		return nil, &rpcErrorBody{Code: -32018, Message: "not_found", Data: err.Error()}, true
	}
	return map[string]any{}, nil, true
}
