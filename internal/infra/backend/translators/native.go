package translators

import (
	_ "embed"
	"encoding/json"
	"fmt"

	"github.com/justwasm/pigasus/geminicode"
)

//go:embed model_tools_1.5.14.json
var nativeToolsJSON []byte

// GetPigasus1514Declarations returns the 12 native Pigasus 1.5.14 tool declarations.
// If sandboxed is false, the BypassSandbox property is stripped from run_command.
func GetPigasus1514Declarations(sandboxed bool) ([]geminicode.FunctionDeclaration, error) {
	var decls []geminicode.FunctionDeclaration
	if err := json.Unmarshal(nativeToolsJSON, &decls); err != nil {
		return nil, fmt.Errorf("failed to unmarshal native 1.5.14 tools: %w", err)
	}

	if !sandboxed {
		for i := range decls {
			if decls[i].Name == "run_command" {
				decls[i].Parameters = stripBypassSandbox(decls[i].Parameters)
			}
		}
	}

	return decls, nil
}

// stripBypassSandbox removes the BypassSandbox property from a schema map.
func stripBypassSandbox(params map[string]any) map[string]any {
	if params == nil {
		return nil
	}
	res := copyMap(params)
	if props, ok := res["properties"].(map[string]any); ok {
		propsCopy := copyMap(props)
		delete(propsCopy, "BypassSandbox")
		res["properties"] = propsCopy
	}
	if req, ok := res["required"].([]any); ok {
		var newReq []any
		for _, r := range req {
			if s, ok := r.(string); ok && s == "BypassSandbox" {
				continue
			}
			newReq = append(newReq, r)
		}
		res["required"] = newReq
	} else if reqStr, ok := res["required"].([]string); ok {
		var newReq []string
		for _, s := range reqStr {
			if s == "BypassSandbox" {
				continue
			}
			newReq = append(newReq, s)
		}
		res["required"] = newReq
	}
	return res
}

func copyMap(src map[string]any) map[string]any {
	if src == nil {
		return nil
	}
	dst := make(map[string]any, len(src))
	for k, v := range src {
		if m, ok := v.(map[string]any); ok {
			dst[k] = copyMap(m)
		} else if s, ok := v.([]any); ok {
			dst[k] = copySlice(s)
		} else {
			dst[k] = v
		}
	}
	return dst
}

func copySlice(src []any) []any {
	if src == nil {
		return nil
	}
	dst := make([]any, len(src))
	for i, v := range src {
		if m, ok := v.(map[string]any); ok {
			dst[i] = copyMap(m)
		} else if s, ok := v.([]any); ok {
			dst[i] = copySlice(s)
		} else {
			dst[i] = v
		}
	}
	return dst
}
