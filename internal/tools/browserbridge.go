package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/justwasm/pigasus/browserbridge"
)

// The Open Claude in Chrome bridge.
//
// What these tools are
// --------------------
// The Open Claude in Chrome project (the sibling checkout
// ../open-claude-in-chrome) drives the browser the *user already has
// open*: the extension runs inside their Chrome, holds their tabs and
// their logins. The clone's own CDP tools cannot do that — they drive a
// Chrome the clone starts, with a profile of its own.
//
// So the browser agent gets a second surface: the same agent, but its
// tools ask the bridge instead of the clone's Chrome. The bridge speaks
// newline-delimited JSON over TCP (browserbridge); each tool
// here is one bridge call, with the project's own tool name and the
// project's own argument shape.
//
// The names and the argument shapes are the project's — they are the
// protocol's vocabulary, and the extension dispatches on them. The
// descriptions are the clone's own prose: the project is PolyForm
// Noncommercial, so none of its text is copied here.
//
// Where the surface lives
// ----------------------
// These tools are *not* in the advertised catalogue (internal/tools/
// catalog.go): the catalogue is the reference's own list of tool names,
// and its sixty names stay as they are. The bridge surface belongs to the
// browser agent alone, and it is offered only when the browser policy
// says the agent drives the user's browser (internal/browsersubagent).

// bridgeTool is one Open Claude in Chrome tool.
type bridgeTool struct {
	// name is the name the model calls.
	name string
	// desc is the tool's description, in the clone's own prose.
	desc string
	// schema is the tool's JSON Schema.
	schema json.RawMessage
	// upstream is the tool's name on the bridge.
	upstream string
}

func (t *bridgeTool) Name() string            { return t.name }
func (t *bridgeTool) Schema() json.RawMessage { return t.schema }
func (t *bridgeTool) Description() string     { return t.desc }

// Run calls the tool on the bridge and renders its answer.
func (t *bridgeTool) Run(ctx context.Context, args json.RawMessage) (any, error) {
	client, err := browserbridge.GetClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", t.name, err)
	}
	res, err := client.Call(ctx, t.upstream, args)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", t.name, err)
	}
	return renderBridgeResult(res), nil
}

// renderBridgeResult turns the bridge's answer into the text the model
// reads.
//
// The project's tools answer with an MCP-shaped content array — a list of
// typed parts, of which the text ones carry the answer. Anything else
// (an unknown shape, a bare string) is handed over as it arrived: the
// model can read JSON, and guessing would lose information.
func renderBridgeResult(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var envelope struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &envelope); err == nil && len(envelope.Content) > 0 {
		var parts []string
		for _, part := range envelope.Content {
			if part.Text == "" {
				continue
			}
			parts = append(parts, part.Text)
		}
		if len(parts) > 0 {
			return strings.Join(parts, "\n")
		}
	}
	return string(raw)
}

// BridgeToolNames are the bridge tools the browser agent may call.
//
// The list is the browser half of the project's own tool set: the tab
// trio, the page readers, the input tools and the window tools. The
// project's recorder, plan and shortcut tools are left out — they belong
// to a Claude Code session, not to a browsing task.
var BridgeToolNames = []string{
	"tabs_context_mcp",
	"tabs_create_mcp",
	"tabs_close_mcp",
	"navigate",
	"computer",
	"find",
	"form_input",
	"get_page_text",
	"read_page",
	"javascript_tool",
	"read_console_messages",
	"read_network_requests",
	"resize_window",
	"upload_image",
	"file_upload",
	"set_tab_focus",
	"switch_browser",
}

// BridgeCatalog builds the bridge tool registry.
//
// It is a registry of its own: the advertised catalogue (Catalog) keeps
// the reference's sixty names, and the bridge surface is the browser
// agent's, not the conversation's.
func BridgeCatalog() *Registry {
	reg := NewRegistry()
	for _, t := range bridgeTools() {
		reg.Register(t)
	}
	return reg
}

// bridgeTools describes the bridge surface: the project's tool names and
// argument shapes, the clone's own descriptions.
func bridgeTools() []Tool {
	// tabIDProp is the JSON fragment for the tab argument, shared by every
	// tool that works on one tab.
	const tabIDProp = `"tabId":{"type":"integer","description":"The tab to work on. Ask tabs_context_mcp for the tabs that exist; a tab outside the extension's group is not reachable."}`

	return []Tool{
		&bridgeTool{
			name:     "tabs_context_mcp",
			upstream: "tabs_context_mcp",
			desc:     "List the browser tabs this session may use, with their ids, titles and URLs. Call it first — every other tool needs a tab id, and the ids only exist in this answer. With createIfEmpty it also opens a tab (and a window) when the session has none.",
			schema: json.RawMessage(`{
				"type":"object",
				"properties":{
					"createIfEmpty":{"type":"boolean","description":"Open a tab and a window for this session when it has none."}
				}
			}`),
		},
		&bridgeTool{
			name:     "tabs_create_mcp",
			upstream: "tabs_create_mcp",
			desc:     "Open a new empty tab for this session. The new tab joins the session's tab group.",
			schema:   json.RawMessage(`{"type":"object","properties":{}}`),
		},
		&bridgeTool{
			name:     "tabs_close_mcp",
			upstream: "tabs_close_mcp",
			desc:     "Close tabs of this session. Pass tabId for one tab or tabIds for several. A closed tab is gone — navigating it to about:blank is not a close.",
			schema: json.RawMessage(`{
				"type":"object",
				"properties":{
					"tabId":{"type":"integer","description":"The tab to close."},
					"tabIds":{"type":"array","items":{"type":"integer"},"description":"Several tabs to close at once. Use either tabId or tabIds, not both."}
				}
			}`),
		},
		&bridgeTool{
			name:     "navigate",
			upstream: "navigate",
			desc:     "Point a tab at a URL and wait for the page to load. The URL may be written with or without a protocol (https:// is assumed), and \"forward\" or \"back\" move through the browser's history instead of loading a page.",
			schema: json.RawMessage(fmt.Sprintf(`{
				"type":"object",
				"properties":{
					"url":{"type":"string","description":"The URL to load — with or without a protocol, or \"forward\" / \"back\" to move through the browser's history."},
					%s
				},
				"required":["url","tabId"]
			}`, tabIDProp)),
		},
		&bridgeTool{
			name:     "computer",
			upstream: "computer",
			desc:     "Drive the mouse and the keyboard in the browser, and take screenshots. The action decides what the rest of the arguments mean: left_click and right_click need coordinate; type needs text; key needs text (a key name); scroll needs coordinate and scroll_direction; left_click_drag needs start_coordinate and coordinate; zoom needs region; wait needs duration; screenshot needs nothing; hover needs coordinate; scroll_to needs ref; triple_click and double_click need coordinate.",
			schema: json.RawMessage(fmt.Sprintf(`{
				"type":"object",
				"properties":{
					"action":{"type":"string","enum":["left_click","right_click","double_click","triple_click","type","screenshot","wait","scroll","key","left_click_drag","zoom","scroll_to","hover"],"description":"What to do in the browser."},
					%s,
					"coordinate":{"type":"array","items":{"type":"integer"},"description":"The point to act on, as [x, y]."},
					"start_coordinate":{"type":"array","items":{"type":"integer"},"description":"Where a drag starts, as [x, y]."},
					"region":{"type":"array","items":{"type":"integer"},"description":"The rectangle to capture, as [x, y, width, height]."},
					"duration":{"type":"number","description":"How long to wait, in seconds."},
					"modifiers":{"type":"string","description":"Modifier keys held down, e.g. \"shift\" or \"ctrl+shift\"."},
					"ref":{"type":"string","description":"An element reference from read_page or find."},
					"repeat":{"type":"integer","description":"How many times to repeat the action."},
					"scroll_direction":{"type":"string","enum":["up","down","left","right"],"description":"Which way to scroll."},
					"scroll_amount":{"type":"number","description":"How far to scroll, in pixels."},
					"text":{"type":"string","description":"The text to type, or the key to press."},
					"save_to_disk":{"type":"boolean","description":"Keep the screenshot on disk as well."}
				},
				"required":["action"]
			}`, tabIDProp)),
		},
		&bridgeTool{
			name:     "find",
			upstream: "find",
			desc:     "Find elements on the page, by what they are (\"the search box\", \"the login button\") or by their text. The answer carries element references that read_page, form_input and the mouse tools take.",
			schema: json.RawMessage(fmt.Sprintf(`{
				"type":"object",
				"properties":{
					"query":{"type":"string","description":"What to look for, in words."},
					%s
				},
				"required":["query","tabId"]
			}`, tabIDProp)),
		},
		&bridgeTool{
			name:     "form_input",
			upstream: "form_input",
			desc:     "Fill a form field, by the element reference read_page or find gave you. Use a boolean for a checkbox, the option's value or text for a select, and a string otherwise.",
			schema: json.RawMessage(fmt.Sprintf(`{
				"type":"object",
				"properties":{
					"ref":{"type":"string","description":"The element reference of the field."},
					"value":{"description":"The value to set.","oneOf":[{"type":"string"},{"type":"boolean"},{"type":"number"}]},
					%s
				},
				"required":["ref","value","tabId"]
			}`, tabIDProp)),
		},
		&bridgeTool{
			name:     "get_page_text",
			upstream: "get_page_text",
			desc:     "Read the page as plain text, the article's text first when there is one. The cheapest way to know what a page says.",
			schema: json.RawMessage(fmt.Sprintf(`{
				"type":"object",
				"properties":{%s},
				"required":["tabId"]
			}`, tabIDProp)),
		},
		&bridgeTool{
			name:     "read_page",
			upstream: "read_page",
			desc:     "Read the page as a tree of elements, with a reference for each. Use it when you need to know what the page offers — the buttons, the links, the fields — and not merely what it says. filter \"interactive\" keeps only what can be clicked or typed into; depth and ref_id narrow the tree when the answer is too large.",
			schema: json.RawMessage(fmt.Sprintf(`{
				"type":"object",
				"properties":{
					%s,
					"filter":{"type":"string","enum":["interactive","all"],"description":"Which elements to keep. \"all\" (the default) keeps every element, visible or not."},
					"depth":{"type":"integer","description":"How deep to walk the tree."},
					"ref_id":{"type":"string","description":"Read only this element and its children."},
					"max_chars":{"type":"integer","description":"How many characters the answer may hold."}
				},
				"required":["tabId"]
			}`, tabIDProp)),
		},
		&bridgeTool{
			name:     "javascript_tool",
			upstream: "javascript_tool",
			desc:     "Run JavaScript in the page and answer with the value of the last expression. Write the expression itself — no return statement. The page's own variables and the DOM are in reach.",
			schema: json.RawMessage(fmt.Sprintf(`{
				"type":"object",
				"properties":{
					"action":{"type":"string","const":"javascript_exec","description":"Must be javascript_exec."},
					"text":{"type":"string","description":"The JavaScript to run."},
					%s
				},
				"required":["action","text","tabId"]
			}`, tabIDProp)),
		},
		&bridgeTool{
			name:     "read_console_messages",
			upstream: "read_console_messages",
			desc:     "Read what the page wrote to the console. pattern keeps only the lines that match it; onlyErrors keeps the errors alone; clear empties the buffer afterwards.",
			schema: json.RawMessage(fmt.Sprintf(`{
				"type":"object",
				"properties":{
					%s,
					"pattern":{"type":"string","description":"Keep only the lines that match this pattern."},
					"limit":{"type":"integer","description":"How many messages to answer with."},
					"onlyErrors":{"type":"boolean","description":"Keep only the errors and the warnings."},
					"clear":{"type":"boolean","description":"Empty the buffer once the answer is taken."}
				},
				"required":["tabId"]
			}`, tabIDProp)),
		},
		&bridgeTool{
			name:     "read_network_requests",
			upstream: "read_network_requests",
			desc:     "Read the requests the page made. urlPattern keeps only the matching ones; clear empties the log afterwards.",
			schema: json.RawMessage(fmt.Sprintf(`{
				"type":"object",
				"properties":{
					%s,
					"urlPattern":{"type":"string","description":"Keep only the requests whose URL matches."},
					"limit":{"type":"integer","description":"How many requests to answer with."},
					"clear":{"type":"boolean","description":"Empty the log once the answer is taken."}
				},
				"required":["tabId"]
			}`, tabIDProp)),
		},
		&bridgeTool{
			name:     "resize_window",
			upstream: "resize_window",
			desc:     "Resize the browser window, in pixels. Useful for a page that only lays itself out at another size.",
			schema: json.RawMessage(fmt.Sprintf(`{
				"type":"object",
				"properties":{
					"width":{"type":"integer","description":"The width in pixels."},
					"height":{"type":"integer","description":"The height in pixels."},
					%s
				},
				"required":["width","height","tabId"]
			}`, tabIDProp)),
		},
		&bridgeTool{
			name:     "upload_image",
			upstream: "upload_image",
			desc:     "Put a screenshot you took earlier into a file input on the page. The target must be an <input type=\"file\"> — often a hidden one.",
			schema: json.RawMessage(fmt.Sprintf(`{
				"type":"object",
				"properties":{
					"imageId":{"type":"string","description":"The screenshot to upload."},
					%s,
					"ref":{"type":"string","description":"The element reference of the file input."},
					"filename":{"type":"string","description":"The name to store the file under."}
				},
				"required":["imageId","tabId","ref"]
			}`, tabIDProp)),
		},
		&bridgeTool{
			name:     "file_upload",
			upstream: "file_upload",
			desc:     "Put files from this machine into a file input on the page. The paths are absolute and the files must exist already; keep the total under about 10 MB.",
			schema: json.RawMessage(fmt.Sprintf(`{
				"type":"object",
				"properties":{
					"paths":{"type":"array","items":{"type":"string"},"description":"The absolute paths of the files to upload."},
					"ref":{"type":"string","description":"The element reference of the file input."},
					%s
				},
				"required":["paths","ref","tabId"]
			}`, tabIDProp)),
		},
		&bridgeTool{
			name:     "set_tab_focus",
			upstream: "set_tab_focus",
			desc:     "Bring a tab to the user's attention: make it the selected tab, and with focus_window also raise its window over everything else. Use it when the user should be looking at the page — not for routine progress.",
			schema: json.RawMessage(fmt.Sprintf(`{
				"type":"object",
				"properties":{
					%s,
					"focus_window":{"type":"boolean","description":"Also raise the window over every other application."}
				},
				"required":["tabId"]
			}`, tabIDProp)),
		},
		&bridgeTool{
			name:     "switch_browser",
			upstream: "switch_browser",
			desc:     "Hand the browser over to another Chromium browser — Chrome, Brave, Edge — that has the extension enabled. One browser drives at a time; wait a few seconds and check with tabs_context_mcp which one is active now.",
			schema:   json.RawMessage(`{"type":"object","properties":{}}`),
		},
	}
}
