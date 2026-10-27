// Package agentbrowser reproduces the reference Antigravity language
// server's agent↔browser bridge.
//
// The reference keeps this bridge in two halves that live in the same
// process but talk over three LanguageServerService RPCs:
//
//	SubscribeToAgentBrowserRequests   (server-streaming)
//	UpdateAgentBrowserSubscription
//	FulfillAgentBrowserRequest
//
// The agent half calls the `webview_browser` tool; the tool asks the
// broker for a browser action; the broker hands the action to the IDE
// half, which subscribed to the stream; the IDE half runs the action in
// the webview and fulfils the request.
//
// 【一手】 2026-10-27, the wire shapes come from the reference's own
// descriptor set (out/ide-language-server/protos/language_server.protoset):
// `exa.language_server_pb.AgentBrowserWireRequest` carries
// request_id / cascade_id / root_conversation_id / action / params_json /
// target_window_id, and `AgentBrowserWireResponse` carries request_id /
// ok / result_json / error / error_code / window_id / screenshot_png.
//
// The member names below are protojson's default lowerCamelCase
// spellings; the reference's own frontend reads exactly these
// (`J.paramsJson`, `J.requestId`, `J.targetWindowId`, `windowId`,
// `openConversationIds`, `displayedRootConversationId`,
// `supportsIsolatedWorld`, `backendInstructions`).
package agentbrowser

// Request is the reference's AgentBrowserWireRequest: one browser
// action the agent asked for.
type Request struct {
	// RequestID identifies the action. The broker mints one when the
	// caller left it empty.
	RequestID string `json:"requestId,omitempty"`
	// CascadeID is the cascade (conversation run) the action belongs to.
	CascadeID string `json:"cascadeId,omitempty"`
	// RootConversationID is the conversation the action belongs to. It
	// is what the broker selects the IDE window with.
	RootConversationID string `json:"rootConversationId,omitempty"`
	// Action is the browser action, e.g. "click" or "batch".
	Action string `json:"action,omitempty"`
	// ParamsJSON is the action's parameters, JSON-encoded. The
	// reference's frontend parses it and spreads it over the browser
	// runtime's own arguments.
	ParamsJSON string `json:"paramsJson,omitempty"`
	// TargetWindowID names the IDE window that must run the action.
	// Empty means the broker picks one.
	TargetWindowID string `json:"targetWindowId,omitempty"`
}

// Response is the reference's AgentBrowserWireResponse: the IDE half's
// answer to one browser action.
type Response struct {
	// RequestID is the action this answers.
	RequestID string `json:"requestId,omitempty"`
	// OK reports whether the action succeeded.
	OK bool `json:"ok,omitempty"`
	// ResultJSON is the browser runtime's result, JSON-encoded.
	ResultJSON string `json:"resultJson,omitempty"`
	// Error is the failure message of a failed action.
	Error string `json:"error,omitempty"`
	// ErrorCode is the machine-readable failure code. The reference's
	// frontend produces "WRONG_WINDOW" and "INTERNAL_ERROR".
	ErrorCode string `json:"errorCode,omitempty"`
	// WindowID is the window that ran the action.
	WindowID string `json:"windowId,omitempty"`
	// ScreenshotPNG is the optional screenshot the window returns.
	ScreenshotPNG []byte `json:"screenshotPng,omitempty"`
}

// SubscribeRequest is the reference's
// SubscribeToAgentBrowserRequestsRequest: one IDE window registering
// itself with the language server.
type SubscribeRequest struct {
	// WindowID is the window's own identifier.
	WindowID string `json:"windowId,omitempty"`
	// Surface names the product the window runs in. The reference's
	// frontend sends "hub".
	Surface string `json:"surface,omitempty"`
	// OpenConversationIDs are the conversations the window has open.
	OpenConversationIDs []string `json:"openConversationIds,omitempty"`
	// SupportsIsolatedWorld reports whether the window's webview runs
	// in an isolated world.
	SupportsIsolatedWorld bool `json:"supportsIsolatedWorld,omitempty"`
	// DisplayedRootConversationID is the conversation the window
	// currently shows.
	DisplayedRootConversationID string `json:"displayedRootConversationId,omitempty"`
	// BackendInstructions is the window's own instruction text for the
	// browser agent.
	BackendInstructions string `json:"backendInstructions,omitempty"`
}

// UpdateSubscriptionRequest is the reference's
// UpdateAgentBrowserSubscriptionRequest: the window's state changed.
type UpdateSubscriptionRequest struct {
	// WindowID is the window whose state changed.
	WindowID string `json:"windowId,omitempty"`
	// DisplayedRootConversationID is the conversation the window now
	// shows.
	DisplayedRootConversationID string `json:"displayedRootConversationId,omitempty"`
	// SupportsIsolatedWorld reports the window's current webview mode.
	SupportsIsolatedWorld bool `json:"supportsIsolatedWorld,omitempty"`
}

// FulfillRequest is the reference's FulfillAgentBrowserRequestRequest:
// the IDE half's answer to one browser action.
type FulfillRequest struct {
	// Response is the answer. A nil response is what the reference
	// refuses with "FulfillAgentBrowserRequest requires a non-nil
	// response payload" — the field is a message, so an absent one
	// and an empty one are not the same request.
	Response *Response `json:"response,omitempty"`
}
