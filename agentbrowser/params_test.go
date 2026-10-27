package agentbrowser

import (
	"encoding/json"
	"strings"
	"testing"
)

// stepParams builds the parameters of one action and returns them
// decoded.
func stepParams(t *testing.T, action string, raw string) map[string]any {
	t.Helper()
	var a args
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		t.Fatalf("the arguments: %v", err)
	}
	params, err := buildStepParams(&a, action, defaultParamsOptions())
	if err != nil {
		t.Fatalf("buildStepParams(%q): %v", action, err)
	}
	return params
}

func TestBuildStepParamsCarriesTheCommonKeys(t *testing.T) {
	params := stepParams(t, "snapshot", `{"action":"snapshot"}`)
	want := map[string]any{
		"tabId":                  "agent-tab-1",
		"includePerception":      true,
		"captureScreenshot":      true,
		"captureSnapshot":        true,
		"coordinateSystem":       "normalized_1000",
		"normalizedMaxDimension": 1000,
	}
	for key, value := range want {
		if params[key] != value {
			t.Errorf("the parameter %q is %#v, want %#v", key, params[key], value)
		}
	}
}

func TestBuildStepParamsClickByUIDAndByCoordinates(t *testing.T) {
	params := stepParams(t, "click", `{"action":"click","uid":"e-12"}`)
	if params["uid"] != "e-12" {
		t.Fatalf("the uid is %#v", params["uid"])
	}
	params = stepParams(t, "click", `{"action":"click","x":500,"y":250}`)
	if params["x"] != 500.0 || params["y"] != 250.0 {
		t.Fatalf("the coordinates are %#v/%#v", params["x"], params["y"])
	}
	if params["normalized"] != true {
		t.Fatalf("the normalized flag is %#v", params["normalized"])
	}
}

func TestBuildStepParamsScroll(t *testing.T) {
	params := stepParams(t, "scroll", `{"action":"scroll"}`)
	if params["direction"] != "down" || params["amount"] != 400.0 {
		t.Fatalf("the scroll is %#v", params)
	}
	if params["deltaY"] != 400.0 {
		t.Fatalf("the delta is %#v", params["deltaY"])
	}
	params = stepParams(t, "scroll", `{"action":"scroll","direction":"left","amount":100}`)
	if params["deltaX"] != -100.0 {
		t.Fatalf("the delta is %#v", params["deltaX"])
	}
}

func TestBuildStepParamsFillTypePressSelect(t *testing.T) {
	params := stepParams(t, "fill", `{"action":"fill","uid":"e-1","text":"hello"}`)
	if params["uid"] != "e-1" || params["text"] != "hello" {
		t.Fatalf("the fill is %#v", params)
	}
	params = stepParams(t, "type", `{"action":"type","text":"hello"}`)
	if params["text"] != "hello" {
		t.Fatalf("the type is %#v", params)
	}
	params = stepParams(t, "press", `{"action":"press","key":"Enter"}`)
	if params["key"] != "Enter" {
		t.Fatalf("the press is %#v", params)
	}
	params = stepParams(t, "select_option", `{"action":"select_option","uid":"e-1","value":"v"}`)
	if params["uid"] != "e-1" || params["value"] != "v" {
		t.Fatalf("the select is %#v", params)
	}
}

func TestBuildStepParamsWaitAndNavigation(t *testing.T) {
	params := stepParams(t, "wait", `{"action":"wait"}`)
	if params["ms"] != 1000 {
		t.Fatalf("the wait is %#v", params["ms"])
	}
	params = stepParams(t, "wait", `{"action":"wait","ms":250,"text":"done"}`)
	if params["ms"] != 250 || params["text"] != "done" {
		t.Fatalf("the wait is %#v", params)
	}
	params = stepParams(t, "open", `{"action":"open","url":"https://example.com/x"}`)
	if params["url"] != "https://example.com/x" {
		t.Fatalf("the open is %#v", params)
	}
	params = stepParams(t, "new-tab", `{"action":"new-tab","url":"https://example.com/y"}`)
	if params["tabId"] != "__new_tab__" {
		t.Fatalf("the new tab is %#v", params["tabId"])
	}
}

func TestBuildStepParamsRefusesWhatTheActionNeeds(t *testing.T) {
	cases := []struct {
		action string
		raw    string
		want   string
	}{
		{"fill", `{"action":"fill"}`, "webview_browser action 'fill' requires 'uid'"},
		{"type", `{"action":"type"}`, "webview_browser action 'type' requires non-empty 'text'"},
		{"press", `{"action":"press"}`, "webview_browser action 'press' requires non-empty 'key'"},
		{"select_option", `{"action":"select_option","value":"v"}`, "webview_browser action 'select_option' requires 'uid'"},
		{"select_option", `{"action":"select_option","uid":"e-1"}`, "webview_browser action 'select_option' requires non-empty 'value'"},
		{"wait", `{"action":"wait","ms":40000}`, "webview_browser action 'wait' requires 'ms' between 0 and 30000"},
		{"scroll", `{"action":"scroll","direction":"sideways"}`, "invalid scroll direction \"sideways\": must be one of 'up', 'down', 'left', 'right'"},
		{"click", `{"action":"click"}`, "webview_browser action \"click\" requires either 'uid' or both normalized coordinates 'x' and 'y' in [0, 1000]"},
		{"click", `{"action":"click","x":500}`, "webview_browser action \"click\" requires either 'uid' or both normalized coordinates 'x' and 'y' in [0, 1000]"},
		{"click", `{"action":"click","x":5000,"y":10}`, "webview_browser action \"click\" requires either 'uid' or both normalized coordinates 'x' and 'y' in [0, 1000]"},
		{"open", `{"action":"open"}`, "URL is required for 'open' / 'navigate'"},
		{"navigate", `{"action":"navigate","url":"ftp://example.com"}`, "disallowed URL scheme \"ftp\" in \"ftp://example.com\": only http://, https://, and about:blank URLs are permitted"},
		{"open", `{"action":"open","url":"http://169.254.169.254/latest/meta-data"}`, "disallowed cloud metadata URL \"http://169.254.169.254/latest/meta-data\""},
		{"open", `{"action":"open","url":"http://169.254.1.2/"}`, "disallowed link-local address in URL \"http://169.254.1.2/\""},
		{"open", `{"action":"open","url":"https:///x"}`, "invalid URL \"https:///x\": host is required"},
		{"fly", `{"action":"fly"}`, "unsupported webview_browser action \"fly\""},
	}
	for _, tc := range cases {
		var a args
		if err := json.Unmarshal([]byte(tc.raw), &a); err != nil {
			t.Fatalf("the arguments: %v", err)
		}
		_, err := buildStepParams(&a, tc.action, defaultParamsOptions())
		if err == nil {
			t.Errorf("%s: the action was accepted", tc.action)
			continue
		}
		if err.Error() != tc.want {
			t.Errorf("%s: the error is %q, want %q", tc.action, err.Error(), tc.want)
		}
	}
}

func TestValidateHTTPOrHTTPSURLAcceptsAboutBlank(t *testing.T) {
	if err := validateHTTPOrHTTPSURL("about:blank"); err != nil {
		t.Fatalf("about:blank: %v", err)
	}
	if err := validateHTTPOrHTTPSURL("  https://example.com "); err != nil {
		t.Fatalf("the trimmed URL: %v", err)
	}
}

func TestToolDeclarationShape(t *testing.T) {
	decl := ToolDeclaration()
	if decl.Name != "webview_browser" {
		t.Fatalf("the name is %q", decl.Name)
	}
	if len(decl.Description) != 611 {
		t.Fatalf("the description is %d bytes, want 611", len(decl.Description))
	}
	if !strings.HasPrefix(decl.Description, "Interact with the built-in webview browser") {
		t.Fatalf("the description is %q", decl.Description)
	}
}

// declarationSchema is the reference's own schema, decoded.
type declarationSchema struct {
	Type       string `json:"type"`
	Properties map[string]struct {
		Type  string   `json:"type"`
		Enum  []string `json:"enum"`
		Items *struct {
			Type       string `json:"type"`
			Properties map[string]struct {
				Type string   `json:"type"`
				Enum []string `json:"enum"`
			} `json:"properties"`
			Required []string `json:"required"`
		} `json:"items"`
	} `json:"properties"`
	Required []string `json:"required"`
}

// decodedDeclaration decodes the reference's declaration schema.
func decodedDeclaration(t *testing.T) declarationSchema {
	t.Helper()
	var schema declarationSchema
	if err := json.Unmarshal(ToolDeclaration().Parameters, &schema); err != nil {
		t.Fatalf("the schema: %v", err)
	}
	return schema
}

func TestToolDeclarationSchema(t *testing.T) {
	schema := decodedDeclaration(t)
	if schema.Type != "OBJECT" {
		t.Fatalf("the schema type is %q", schema.Type)
	}
	if len(schema.Required) != 1 || schema.Required[0] != "action" {
		t.Fatalf("the required list is %#v", schema.Required)
	}
	if len(schema.Properties) != 13 {
		t.Fatalf("the schema has %d properties, want 13", len(schema.Properties))
	}
	if got := len(schema.Properties["action"].Enum); got != 20 {
		t.Fatalf("the action enum has %d values, want 20", got)
	}
	if schema.Properties["x"].Type != "NUMBER" || schema.Properties["y"].Type != "NUMBER" {
		t.Fatalf("the coordinate types are %q/%q", schema.Properties["x"].Type, schema.Properties["y"].Type)
	}
	if schema.Properties["ms"].Type != "INTEGER" {
		t.Fatalf("the ms type is %q", schema.Properties["ms"].Type)
	}
}

func TestToolDeclarationStepSchema(t *testing.T) {
	steps := decodedDeclaration(t).Properties["steps"]
	if steps.Type != "ARRAY" || steps.Items == nil {
		t.Fatalf("the steps property is %#v", steps)
	}
	if len(steps.Items.Properties) != 12 {
		t.Fatalf("the step schema has %d properties, want 12", len(steps.Items.Properties))
	}
	if got := len(steps.Items.Properties["action"].Enum); got != 19 {
		t.Fatalf("the step action enum has %d values, want 19", got)
	}
}
