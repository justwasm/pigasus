package agentbrowser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Options are the tool's knobs.
type Options struct {
	// RootConversationID resolves the conversation the action belongs
	// to. It is what the broker selects the IDE window with.
	RootConversationID func() string
	// CascadeID resolves the cascade the action belongs to.
	CascadeID func() string
	// OwnerID names the owner of the conversation's browser. It is the
	// fence's subject: a second owner is refused.
	OwnerID string
	// Perception carries the flags the reference's tool sends with
	// every request.
	Perception paramsOptions
}

// Tool is the reference's `webview_browser` tool.
//
// It turns the model's arguments into the reference's wire parameters,
// asks the broker to run the action in the IDE half and reports the
// browser's answer back.
type Tool struct {
	broker *Broker
	opts   Options
}

// Result is what the tool hands back to the model.
type Result struct {
	// Text is the tool's rendered output.
	Text string
	// OK reports whether the action succeeded.
	OK bool
	// ErrorCode is the browser's machine-readable failure code.
	ErrorCode string
	// ScreenshotPNG is the screenshot the window returned.
	ScreenshotPNG []byte
}

// payload is the browser runtime's own answer — the reference calls it
// the AUIT browser response payload.
//
// 【一手】 2026-10-27, the reference's frontend builds it: requestId,
// ok, tabId, pageId, url, title, error, errorMessage, errorCode and
// the result oneof. A batch adds completedSteps, totalSteps,
// failedStepIndex and failedStepAction.
type payload struct {
	RequestID        string `json:"requestId,omitempty"`
	OK               *bool  `json:"ok,omitempty"`
	TabID            string `json:"tabId,omitempty"`
	PageID           string `json:"pageId,omitempty"`
	URL              string `json:"url,omitempty"`
	Title            string `json:"title,omitempty"`
	Error            string `json:"error,omitempty"`
	ErrorMessage     string `json:"errorMessage,omitempty"`
	ErrorCode        string `json:"errorCode,omitempty"`
	CompletedSteps   int    `json:"completedSteps,omitempty"`
	TotalSteps       int    `json:"totalSteps,omitempty"`
	FailedStepIndex  *int   `json:"failedStepIndex,omitempty"`
	FailedStepAction string `json:"failedStepAction,omitempty"`
}

// summary is what the reference hands the model when the action
// succeeded.
type summary struct {
	Action    string `json:"action"`
	TabID     string `json:"tabId"`
	OK        bool   `json:"ok"`
	URL       string `json:"url,omitempty"`
	Title     string `json:"title,omitempty"`
	Error     string `json:"error,omitempty"`
	ErrorCode string `json:"errorCode,omitempty"`
	Result    string `json:"result,omitempty"`
}

// unsupportedActionError marks an action the reference does not know.
type unsupportedActionError struct {
	action string
}

func (e *unsupportedActionError) Error() string {
	return fmt.Sprintf("unsupported webview_browser action %q", e.action)
}

// NewTool returns the browser tool.
func NewTool(broker *Broker, opts Options) *Tool {
	if broker == nil {
		broker = NewBroker()
	}
	if opts.Perception == (paramsOptions{}) {
		opts.Perception = defaultParamsOptions()
	}
	return &Tool{broker: broker, opts: opts}
}

// Execute runs one browser action.
//
// 【反编译】 2026-10-27, `tools.(*WebviewBrowserTool).Execute`: the
// arguments are parsed, the action is lower-cased, and a batch goes to
// executeBatch while every other action goes to executeSingleStep.
func (t *Tool) Execute(ctx context.Context, raw json.RawMessage) (Result, error) {
	var a args
	if err := json.Unmarshal(raw, &a); err != nil {
		return Result{}, fmt.Errorf("failed to parse webview_browser arguments: %w", err)
	}
	action := strings.ToLower(strings.TrimSpace(a.Action))
	if action == "" {
		return Result{}, errors.New("webview_browser requires a non-empty 'action'")
	}
	root := ""
	if t.opts.RootConversationID != nil {
		root = strings.TrimSpace(t.opts.RootConversationID())
	}
	if err := t.claim(root); err != nil {
		return Result{}, err
	}
	if action == "batch" {
		return t.executeBatch(ctx, &a)
	}
	return t.executeSingleStep(ctx, &a, action)
}

// claim takes the conversation's browser for this tool's owner.
func (t *Tool) claim(root string) error {
	owner := strings.TrimSpace(t.opts.OwnerID)
	if owner == "" {
		owner = root
	}
	if root == "" || owner == "" {
		return nil
	}
	return t.broker.ClaimOwner(root, owner, "")
}

// executeSingleStep runs one action.
//
// 【反编译】 2026-10-27, `tools.(*WebviewBrowserTool).executeSingleStep`.
func (t *Tool) executeSingleStep(ctx context.Context, a *args, action string) (Result, error) {
	params, err := buildStepParams(a, action, t.opts.Perception)
	if err != nil {
		return Result{}, err
	}
	paramsJSON, err := marshalParams(params)
	if err != nil {
		return Result{}, fmt.Errorf("failed to marshal browser request params: %w", err)
	}
	resp, err := t.broker.Request(ctx, t.request(action, paramsJSON))
	if err != nil {
		return Result{}, err
	}
	var p payload
	if err := decodePayload(resp.ResultJSON, &p); err != nil {
		return Result{}, fmt.Errorf("failed to unmarshal AUIT browser response payload: %w", err)
	}
	return t.result(action, p, resp)
}

// executeBatch runs a list of actions.
//
// 【反编译】 2026-10-27, `tools.(*WebviewBrowserTool).executeBatch`: the
// steps are validated one by one, and the whole batch travels as one
// request whose action is "batch".
func (t *Tool) executeBatch(ctx context.Context, a *args) (Result, error) {
	if len(a.Steps) == 0 {
		return Result{}, errors.New("webview_browser action 'batch' requires a non-empty 'steps' array")
	}
	steps := make([]map[string]any, 0, len(a.Steps))
	for i, step := range a.Steps {
		action := strings.ToLower(strings.TrimSpace(step.Action))
		params, err := buildStepParams(step.toArgs(), action, t.opts.Perception)
		if err != nil {
			var unsupported *unsupportedActionError
			if errors.As(err, &unsupported) {
				return Result{}, fmt.Errorf("batch step %d has invalid action %q", i, step.Action)
			}
			return Result{}, fmt.Errorf("batch step %d (%s) invalid: %w", i, action, err)
		}
		steps = append(steps, params)
	}
	params := t.commonParams(a.TabID)
	params["steps"] = steps
	paramsJSON, err := marshalParams(params)
	if err != nil {
		return Result{}, fmt.Errorf("failed to marshal batch browser request params: %w", err)
	}
	resp, err := t.broker.Request(ctx, t.request("batch", paramsJSON))
	if err != nil {
		return Result{}, err
	}
	var steps2 []payload
	if err := decodePayload(resp.ResultJSON, &steps2); err != nil {
		return Result{}, fmt.Errorf("failed to unmarshal AUIT browser batch response payload: %w", err)
	}
	last := payload{}
	if len(steps2) > 0 {
		last = steps2[len(steps2)-1]
	}
	return t.result("batch", last, resp)
}

// result turns the browser's answer into the tool's result.
//
// The wire answer and the browser runtime's own payload carry two
// verdicts: the response's `ok` says the window ran the action, the
// payload's `ok` says the action itself succeeded. Both have to hold.
func (t *Tool) result(action string, p payload, resp *Response) (Result, error) {
	ok := resp.OK
	if p.OK != nil {
		ok = ok && *p.OK
	}
	if p.Error == "" {
		p.Error = resp.Error
	}
	if p.ErrorCode == "" {
		p.ErrorCode = resp.ErrorCode
	}
	text, err := t.renderSummary(action, p, resp, ok)
	if err != nil {
		return Result{}, err
	}
	out := Result{
		Text:          text,
		OK:            ok,
		ErrorCode:     p.ErrorCode,
		ScreenshotPNG: resp.ScreenshotPNG,
	}
	if !ok {
		return out, errors.New("webview_browser action failed")
	}
	return out, nil
}

// renderSummary marshals the summary the reference hands the model.
func (t *Tool) renderSummary(action string, p payload, resp *Response, ok bool) (string, error) {
	s := summary{
		Action:    action,
		TabID:     p.TabID,
		OK:        ok,
		URL:       p.URL,
		Title:     p.Title,
		Error:     p.Error,
		ErrorCode: p.ErrorCode,
		Result:    resp.ResultJSON,
	}
	if s.TabID == "" {
		s.TabID = p.PageID
	}
	raw, err := json.Marshal(s)
	if err != nil {
		return "", fmt.Errorf("failed to marshal webview_browser summary: %w", err)
	}
	return string(raw), nil
}

// request builds the wire request of one action.
func (t *Tool) request(action, paramsJSON string) *Request {
	req := &Request{Action: action, ParamsJSON: paramsJSON}
	if t.opts.RootConversationID != nil {
		req.RootConversationID = strings.TrimSpace(t.opts.RootConversationID())
	}
	if t.opts.CascadeID != nil {
		req.CascadeID = strings.TrimSpace(t.opts.CascadeID())
	}
	return req
}

// commonParams returns the parameters every request carries.
func (t *Tool) commonParams(tab string) map[string]any {
	return map[string]any{
		"tabId":                  tabID(tab),
		"includePerception":      t.opts.Perception.includePerception,
		"captureScreenshot":      t.opts.Perception.captureScreenshot,
		"captureSnapshot":        t.opts.Perception.captureSnapshot,
		"coordinateSystem":       coordinateSystemNormalized,
		"normalizedMaxDimension": normalizedMaxDimension,
	}
}

// toArgs turns one batch step into the argument struct the parameter
// builder takes.
func (s stepArg) toArgs() *args {
	return &args{
		Action:    s.Action,
		TabID:     s.TabID,
		URL:       s.URL,
		UID:       s.UID,
		X:         s.X,
		Y:         s.Y,
		Text:      s.Text,
		Key:       s.Key,
		Direction: s.Direction,
		Amount:    s.Amount,
		Value:     s.Value,
		Ms:        s.Ms,
	}
}

// decodePayload decodes the browser runtime's answer.
func decodePayload(raw string, out any) error {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	return json.Unmarshal([]byte(raw), out)
}
