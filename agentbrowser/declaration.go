package agentbrowser

import "encoding/json"

// ToolName is the reference's browser tool name.
//
// 【一手】 2026-10-27, the reference's language server ships exactly one
// browser tool; its name is `webview_browser`. The 23 legacy browser
// names the catalogue advertises are a different surface.
const ToolName = "webview_browser"

// Description is the reference's tool description, byte for byte.
//
// 【一手】 2026-10-27, `tools.(*WebviewBrowserTool).Description` returns
// the 611 bytes at 0x102fbddbe of the reference's language_server.
const Description = "Interact with the built-in webview browser using screenshot-first perception and a 3-Tier Auxiliary Interactive DOM HUD. " +
	"Supported actions: 'open', 'navigate', 'snapshot', 'click', 'fill', 'type', 'press', 'hover', 'scroll', 'select_option', 'get_state', 'wait', 'back', 'forward', 'reload', and 'batch'. " +
	"Prefer targeting interactive elements by 'uid' from the HUD when available; otherwise use normalized viewport coordinates (x, y in [0, 1000]). " +
	"After each call, the latest PNG screenshot and 3-Tier Auxiliary Interactive DOM HUD are automatically injected as an ephemeral perception message on the next turn."

// Declaration is the reference's tool declaration.
type Declaration struct {
	// Name is the tool's name as the model knows it.
	Name string
	// Description is the tool's description.
	Description string
	// Parameters is the parameter schema in the reference's wire form.
	Parameters json.RawMessage
}

// ToolDeclaration returns the reference's `webview_browser` declaration.
//
// The parameter schema is the reference's own: the property names are
// the `json` tags of `tools.webviewBrowserArgs`, the types are the
// Go types (a *float64 is a NUMBER, a *int is an INTEGER), the
// descriptions are the `jsonschema_description` tags, and the enum and
// the bounds are the `jsonschema` tags.
//
// 【反编译】 2026-10-27, the field tags of `tools.webviewBrowserArgs`
// (0x107dcf120) and of `tools.webviewBrowserBatchStep`: the batch step
// carries the same parameters minus the step list, and its action enum
// is the tool's enum without "batch".
func ToolDeclaration() Declaration {
	return Declaration{
		Name:        ToolName,
		Description: Description,
		Parameters:  declarationParameters,
	}
}

// declarationParameters is the reference's parameter schema.
//
// The rendering follows the reference's own wire form: the member names
// are protojson's lowerCamelCase spellings, the properties are sorted
// by name, and the int64 bounds carry as strings.
var declarationParameters = json.RawMessage(`{
  "type": "OBJECT",
  "properties": {
    "action": {
      "type": "STRING",
      "description": "Browser action to execute in the built-in webview browser.",
      "enum": [
        "open",
        "navigate",
        "snapshot",
        "click",
        "fill",
        "type",
        "press",
        "hover",
        "scroll",
        "select_option",
        "get_state",
        "wait",
        "back",
        "forward",
        "reload",
        "list-tabs",
        "new-tab",
        "activate-tab",
        "close-tab",
        "batch"
      ]
    },
    "amount": {
      "type": "NUMBER",
      "description": "Scroll distance in pixels for 'scroll' (default 400)."
    },
    "direction": {
      "type": "STRING",
      "description": "Scroll direction for 'scroll' ('up', 'down', 'left', 'right').",
      "enum": [
        "up",
        "down",
        "left",
        "right"
      ]
    },
    "key": {
      "type": "STRING",
      "description": "Keyboard key name for 'press' (e.g., 'Enter', 'Tab', 'Escape', 'ArrowDown')."
    },
    "ms": {
      "type": "INTEGER",
      "description": "Wait duration in milliseconds for 'wait'.",
      "minimum": "0",
      "maximum": "30000"
    },
    "steps": {
      "type": "ARRAY",
      "description": "Ordered list of browser actions to execute sequentially when action='batch' (stops on first error or URL change).",
      "items": {
        "type": "OBJECT",
        "properties": {
          "action": {
            "type": "STRING",
            "description": "Browser action to execute in this batch step.",
            "enum": [
              "open",
              "navigate",
              "snapshot",
              "click",
              "fill",
              "type",
              "press",
              "hover",
              "scroll",
              "select_option",
              "get_state",
              "wait",
              "back",
              "forward",
              "reload",
              "list-tabs",
              "new-tab",
              "activate-tab",
              "close-tab"
            ]
          },
          "amount": {
            "type": "NUMBER",
            "description": "Scroll distance in pixels for 'scroll' (default 400)."
          },
          "direction": {
            "type": "STRING",
            "description": "Scroll direction for 'scroll' ('up', 'down', 'left', 'right').",
            "enum": [
              "up",
              "down",
              "left",
              "right"
            ]
          },
          "key": {
            "type": "STRING",
            "description": "Keyboard key name for 'press' (e.g., 'Enter', 'Tab', 'Escape', 'ArrowDown')."
          },
          "ms": {
            "type": "INTEGER",
            "description": "Wait duration in milliseconds for 'wait'.",
            "minimum": "0",
            "maximum": "30000"
          },
          "tab_id": {
            "type": "STRING",
            "description": "Target agent browser tab ID (defaults to 'agent-tab-1')."
          },
          "text": {
            "type": "STRING",
            "description": "Text to enter for 'fill' or 'type', or text to wait for in 'wait'."
          },
          "uid": {
            "type": "STRING",
            "description": "Interactive DOM element UID from the Auxiliary Interactive DOM HUD (preferred for click, fill, hover, scroll, select_option)."
          },
          "url": {
            "type": "STRING",
            "description": "Target http://, https://, or about:blank URL for 'open', 'navigate', or 'new-tab'."
          },
          "value": {
            "type": "STRING",
            "description": "Option value or visible label to select for 'select_option'."
          },
          "x": {
            "type": "NUMBER",
            "description": "Normalized horizontal viewport coordinate in [0, 1000] when clicking/hovering/scrolling by coordinates.",
            "minimum": "0",
            "maximum": "1000"
          },
          "y": {
            "type": "NUMBER",
            "description": "Normalized vertical viewport coordinate in [0, 1000] when clicking/hovering/scrolling by coordinates.",
            "minimum": "0",
            "maximum": "1000"
          }
        },
        "required": [
          "action"
        ]
      }
    },
    "tab_id": {
      "type": "STRING",
      "description": "Target agent browser tab ID (defaults to 'agent-tab-1')."
    },
    "text": {
      "type": "STRING",
      "description": "Text to enter for 'fill' or 'type', or text to wait for in 'wait'."
    },
    "uid": {
      "type": "STRING",
      "description": "Interactive DOM element UID from the Auxiliary Interactive DOM HUD (preferred for click, fill, hover, scroll, select_option)."
    },
    "url": {
      "type": "STRING",
      "description": "Target http://, https://, or about:blank URL for 'open', 'navigate', or 'new-tab'."
    },
    "value": {
      "type": "STRING",
      "description": "Option value or visible label to select for 'select_option'."
    },
    "x": {
      "type": "NUMBER",
      "description": "Normalized horizontal viewport coordinate in [0, 1000] when clicking/hovering/scrolling by coordinates.",
      "minimum": "0",
      "maximum": "1000"
    },
    "y": {
      "type": "NUMBER",
      "description": "Normalized vertical viewport coordinate in [0, 1000] when clicking/hovering/scrolling by coordinates.",
      "minimum": "0",
      "maximum": "1000"
    }
  },
  "required": [
    "action"
  ]
}`)
