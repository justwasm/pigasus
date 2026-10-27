package agentbrowser

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
)

const (
	// defaultTabID is the tab the reference targets when the model does
	// not name one.
	//
	// 【反编译】 2026-10-27, `tools.(*WebviewBrowserTool).Execute`:
	// an empty `tab_id` becomes "agent-tab-1".
	defaultTabID = "agent-tab-1"
	// newTabMarker is the tab id the reference sends for a "new-tab"
	// action — the frontend creates a tab under that id.
	newTabMarker = "__new_tab__"
	// defaultScrollAmount is the reference's default scroll distance.
	defaultScrollAmount = 400
	// defaultScrollDirection is the reference's default scroll
	// direction.
	defaultScrollDirection = "down"
	// defaultWaitMs is the reference's default wait.
	defaultWaitMs = 1000
	// maxWaitMs is the reference's upper bound for a wait.
	maxWaitMs = 30000
	// normalizedMaxDimension is the reference's normalized viewport
	// size: the coordinates the model sends run from 0 to 1000.
	normalizedMaxDimension = 1000
	// coordinateSystem is the wire's coordinate system: the reference
	// tells the window that the coordinates run from 0 to 1000.
	//
	// 【反编译】 2026-10-27, `tools.buildStepParams` writes the static
	// string at 0x1068aff30 — "normalized_1000" — under the
	// "coordinateSystem" key.
	coordinateSystemNormalized = "normalized_1000"
)

// args is the reference's `tools.webviewBrowserArgs`: the thirteen
// parameters the model sends.
//
// 【反编译】 2026-10-27, the type descriptor at 0x107dcf120: four
// strings (Action, TabID, URL, UID), two *float64 (X, Y), three strings
// (Text, Key, Direction), one *float64 (Amount), one string (Value), one
// *int (Ms) and the step slice. The pointer fields are how the
// reference tells "not given" from "given as zero".
type args struct {
	Action    string    `json:"action"`
	TabID     string    `json:"tab_id,omitempty"`
	URL       string    `json:"url,omitempty"`
	UID       string    `json:"uid,omitempty"`
	X         *float64  `json:"x,omitempty"`
	Y         *float64  `json:"y,omitempty"`
	Text      string    `json:"text,omitempty"`
	Key       string    `json:"key,omitempty"`
	Direction string    `json:"direction,omitempty"`
	Amount    *float64  `json:"amount,omitempty"`
	Value     string    `json:"value,omitempty"`
	Ms        *int      `json:"ms,omitempty"`
	Steps     []stepArg `json:"steps,omitempty"`
}

// stepArg is the reference's `tools.webviewBrowserBatchStep`: the same
// parameters as args minus the step list itself.
type stepArg struct {
	Action    string   `json:"action"`
	TabID     string   `json:"tab_id,omitempty"`
	URL       string   `json:"url,omitempty"`
	UID       string   `json:"uid,omitempty"`
	X         *float64 `json:"x,omitempty"`
	Y         *float64 `json:"y,omitempty"`
	Text      string   `json:"text,omitempty"`
	Key       string   `json:"key,omitempty"`
	Direction string   `json:"direction,omitempty"`
	Amount    *float64 `json:"amount,omitempty"`
	Value     string   `json:"value,omitempty"`
	Ms        *int     `json:"ms,omitempty"`
}

// paramsOptions are the flags the reference's tool carries into every
// request: whether the window returns the screenshot, the DOM HUD and
// the perception message.
type paramsOptions struct {
	includePerception bool
	captureScreenshot bool
	captureSnapshot   bool
}

// defaultParamsOptions returns the reference's own defaults: the tool
// description promises the screenshot and the HUD after every call.
func defaultParamsOptions() paramsOptions {
	return paramsOptions{
		includePerception: true,
		captureScreenshot: true,
		captureSnapshot:   true,
	}
}

// buildStepParams builds the reference's wire parameters for one
// action and validates what the action needs.
//
// 【反编译】 2026-10-27, `tools.buildStepParams`: the map always carries
// tabId, includePerception, captureScreenshot, captureSnapshot,
// coordinateSystem and normalizedMaxDimension; the action adds its own
// keys.
func buildStepParams(a *args, action string, opts paramsOptions) (map[string]any, error) {
	params := map[string]any{
		"tabId":                  tabID(a.TabID),
		"includePerception":      opts.includePerception,
		"captureScreenshot":      opts.captureScreenshot,
		"captureSnapshot":        opts.captureSnapshot,
		"coordinateSystem":       coordinateSystemNormalized,
		"normalizedMaxDimension": normalizedMaxDimension,
	}
	switch action {
	case "click", "hover":
		if err := applyTarget(params, a, action); err != nil {
			return nil, err
		}
	default:
		if err := applyActionParams(params, a, action); err != nil {
			return nil, err
		}
	}
	return params, nil
}

// applyActionParams adds what the action needs to the parameters.
//
// 【反编译】 2026-10-27, `tools.buildStepParams`: the action picks the
// keys, the common map carries the rest.
func applyActionParams(params map[string]any, a *args, action string) error {
	switch action {
	case "fill":
		return applyFill(params, a)
	case "type":
		return applyType(params, a)
	case "press":
		return applyPress(params, a)
	case "select_option":
		return applySelectOption(params, a)
	case "scroll":
		return applyScroll(params, a)
	case "wait":
		return applyWait(params, a)
	case "open", "navigate":
		return applyURL(params, a)
	case "new-tab":
		if err := applyURL(params, a); err != nil {
			return err
		}
		params["tabId"] = newTabMarker
		return nil
	case "snapshot", "back", "forward", "reload", "get_state",
		"list-tabs", "activate-tab", "close-tab":
		// These actions need nothing but the tab.
		return nil
	default:
		return &unsupportedActionError{action: action}
	}
}

// applyFill writes the target and the text of a fill.
func applyFill(params map[string]any, a *args) error {
	uid := strings.TrimSpace(a.UID)
	if uid == "" {
		return fmt.Errorf("webview_browser action 'fill' requires 'uid'")
	}
	params["uid"] = uid
	params["text"] = a.Text
	params["value"] = a.Value
	return nil
}

// applyType writes the text of a type.
func applyType(params map[string]any, a *args) error {
	if strings.TrimSpace(a.Text) == "" {
		return fmt.Errorf("webview_browser action 'type' requires non-empty 'text'")
	}
	params["text"] = a.Text
	params["value"] = a.Value
	return nil
}

// applyPress writes the key of a press.
func applyPress(params map[string]any, a *args) error {
	if strings.TrimSpace(a.Key) == "" {
		return fmt.Errorf("webview_browser action 'press' requires non-empty 'key'")
	}
	params["key"] = a.Key
	return nil
}

// applySelectOption writes the target and the option of a select.
func applySelectOption(params map[string]any, a *args) error {
	uid := strings.TrimSpace(a.UID)
	if uid == "" {
		return fmt.Errorf("webview_browser action 'select_option' requires 'uid'")
	}
	if strings.TrimSpace(a.Value) == "" {
		return fmt.Errorf("webview_browser action 'select_option' requires non-empty 'value'")
	}
	params["uid"] = uid
	params["value"] = a.Value
	params["text"] = a.Text
	return nil
}

// applyWait writes the duration of a wait.
func applyWait(params map[string]any, a *args) error {
	ms := defaultWaitMs
	if a.Ms != nil {
		ms = *a.Ms
	}
	if ms < 0 || ms > maxWaitMs {
		return fmt.Errorf("webview_browser action 'wait' requires 'ms' between 0 and 30000")
	}
	params["ms"] = ms
	if text := strings.TrimSpace(a.Text); text != "" {
		params["text"] = a.Text
	}
	return nil
}

// applyTarget writes the target of a click or a hover: the element's
// uid when the HUD gave one, the normalized coordinates otherwise.
func applyTarget(params map[string]any, a *args, action string) error {
	uid := strings.TrimSpace(a.UID)
	if uid != "" {
		params["uid"] = uid
		return nil
	}
	if err := validateNormalizedCoords(a.X, a.Y, action); err != nil {
		return err
	}
	params["x"] = *a.X
	params["y"] = *a.Y
	params["normalized"] = true
	return nil
}

// applyScroll writes the scroll's direction, distance and deltas.
func applyScroll(params map[string]any, a *args) error {
	direction := strings.ToLower(strings.TrimSpace(a.Direction))
	if direction == "" {
		direction = defaultScrollDirection
	}
	switch direction {
	case "up", "down", "left", "right":
	default:
		return fmt.Errorf("invalid scroll direction %q: must be one of 'up', 'down', 'left', 'right'", direction)
	}
	amount := float64(defaultScrollAmount)
	if a.Amount != nil {
		amount = *a.Amount
	}
	params["direction"] = direction
	params["amount"] = amount
	deltaX, deltaY := 0.0, 0.0
	switch direction {
	case "up":
		deltaY = -amount
	case "down":
		deltaY = amount
	case "left":
		deltaX = -amount
	case "right":
		deltaX = amount
	}
	params["deltaX"] = deltaX
	params["deltaY"] = deltaY
	if uid := strings.TrimSpace(a.UID); uid != "" {
		params["uid"] = uid
		return nil
	}
	if a.X != nil || a.Y != nil {
		if err := validateNormalizedCoords(a.X, a.Y, "scroll"); err != nil {
			return err
		}
		params["x"] = *a.X
		params["y"] = *a.Y
		params["normalized"] = true
	}
	return nil
}

// applyURL writes the target URL of a navigation.
func applyURL(params map[string]any, a *args) error {
	url := strings.TrimSpace(a.URL)
	if err := validateHTTPOrHTTPSURL(url); err != nil {
		return err
	}
	params["url"] = url
	return nil
}

// validateNormalizedCoords checks the normalized coordinates of a
// click, a hover or a scroll.
//
// 【反编译】 2026-10-27, `tools.validateNormalizedCoords`: the message
// is the reference's own.
func validateNormalizedCoords(x, y *float64, action string) error {
	if x == nil || y == nil {
		return fmt.Errorf("webview_browser action %q requires either 'uid' or both normalized coordinates 'x' and 'y' in [0, 1000]", action)
	}
	if *x < 0 || *x > normalizedMaxDimension || *y < 0 || *y > normalizedMaxDimension {
		return fmt.Errorf("webview_browser action %q requires either 'uid' or both normalized coordinates 'x' and 'y' in [0, 1000]", action)
	}
	return nil
}

// validateHTTPOrHTTPSURL is the reference's own URL check.
//
// 【反编译】 2026-10-27, `tools.validateHTTPOrHTTPSURL`: only http://,
// https:// and about:blank are permitted, a host is required, and the
// cloud metadata and link-local addresses are refused.
func validateHTTPOrHTTPSURL(raw string) error {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return errors.New("URL is required for 'open' / 'navigate'")
	}
	if strings.EqualFold(trimmed, "about:blank") {
		return nil
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return fmt.Errorf("invalid URL %q: %w", trimmed, err)
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
	case "about":
		if !strings.EqualFold(parsed.Opaque, "blank") && !strings.EqualFold(parsed.Path, "blank") {
			return fmt.Errorf("disallowed URL scheme %q in %q: only http://, https://, and about:blank URLs are permitted", parsed.Scheme, trimmed)
		}
		return nil
	default:
		return fmt.Errorf("disallowed URL scheme %q in %q: only http://, https://, and about:blank URLs are permitted", parsed.Scheme, trimmed)
	}
	if parsed.Host == "" {
		return fmt.Errorf("invalid URL %q: host is required", trimmed)
	}
	host := parsed.Hostname()
	if host == "169.254.169.254" {
		return fmt.Errorf("disallowed cloud metadata URL %q", trimmed)
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLinkLocalUnicast() {
		return fmt.Errorf("disallowed link-local address in URL %q", trimmed)
	}
	return nil
}

// tabID returns the tab the action targets.
func tabID(id string) string {
	trimmed := strings.TrimSpace(id)
	if trimmed == "" {
		return defaultTabID
	}
	return trimmed
}

// marshalParams encodes the wire parameters.
func marshalParams(params map[string]any) (string, error) {
	raw, err := json.Marshal(params)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}
