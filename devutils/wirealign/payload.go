package main

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// WirePayload represents a decoded CloudCode/Gemini API request payload.
type WirePayload struct {
	Model             string         `json:"model,omitempty"`
	Project           string         `json:"project,omitempty"`
	RequestType       string         `json:"requestType,omitempty"`
	UserAgent         string         `json:"userAgent,omitempty"`
	RequestID         string         `json:"requestId,omitempty"`
	SessionID         string         `json:"sessionId,omitempty"`
	SystemInstruction string         `json:"systemInstruction,omitempty"`
	Tools             []FunctionDecl `json:"tools,omitempty"`
	Contents          []ContentTurn  `json:"contents,omitempty"`
	GenerationConfig  map[string]any `json:"generationConfig,omitempty"`
	RawRequest        map[string]any `json:"rawRequest,omitempty"`
}

// FunctionDecl represents a model-facing function declaration.
type FunctionDecl struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

// ContentTurn represents one conversation message turn.
type ContentTurn struct {
	Role  string        `json:"role"`
	Parts []ContentPart `json:"parts,omitempty"`
}

// ContentPart represents one part of a message turn.
type ContentPart struct {
	Text             string        `json:"text,omitempty"`
	Thought          bool          `json:"thought,omitempty"`
	ThoughtSignature string        `json:"thoughtSignature,omitempty"`
	FunctionCall     *WireFuncCall `json:"functionCall,omitempty"`
	FunctionResponse *WireFuncResp `json:"functionResponse,omitempty"`
}

// WireFuncCall is a model-requested tool invocation.
type WireFuncCall struct {
	ID   string         `json:"id,omitempty"`
	Name string         `json:"name"`
	Args map[string]any `json:"args,omitempty"`
}

// WireFuncResp is a tool response sent back to the model.
type WireFuncResp struct {
	ID       string `json:"id,omitempty"`
	Name     string `json:"name"`
	Response any    `json:"response"`
}

// PromptSection represents an XML-tagged prompt section (e.g. <identity>...</identity>).
type PromptSection struct {
	Tag       string `json:"tag"`
	Header    string `json:"header"` // e.g. "<identity>"
	Footer    string `json:"footer"` // e.g. "</identity>"
	Content   string `json:"content"`
	Raw       string `json:"raw"`
	StartByte int    `json:"startByte"`
	EndByte   int    `json:"endByte"`
}

// PromptDeconstruction breaks down a complete system prompt into its structural XML sections
// and extracts high-leverage semantic properties for alignment verification.
type PromptDeconstruction struct {
	FullPrompt   string                   `json:"fullPrompt"`
	Sections     []PromptSection          `json:"sections"`
	SectionOrder []string                 `json:"sectionOrder"`
	SectionMap   map[string]PromptSection `json:"sectionMap"`

	// Extracted semantic properties:
	IdentityRole       string   `json:"identityRole"`    // "agentic AI assistant" vs "agentic AI coding assistant"
	IsGenericPrompt    bool     `json:"isGenericPrompt"` // generic-system-prompt: true
	IsSandboxed        bool     `json:"isSandboxed"`     // true if <terminal_sandbox> is present
	SubagentsList      []string `json:"subagentsList"`   // names under Available subagents:
	SkillsCount        int      `json:"skillsCount"`     // number of declared skills
	SkillNames         []string `json:"skillNames"`      // list of skill names
	UserRulesCount     int      `json:"userRulesCount"`  // number of <RULE[...]> entries
	UserRuleLabels     []string `json:"userRuleLabels"`  // list of rule labels/paths
	UserInfoWording    string   `json:"userInfoWording"` // "Files relating to..." or "Code relating to..."
	UserInfoOS         string   `json:"userInfoOS"`      // e.g. "mac", "linux"
	UserInfoWorkspaces []string `json:"userInfoWorkspaces"`
	UserInfoCWD        string   `json:"userInfoCWD"`
}

// ToolsDeconstruction breaks down the tools array for deep parameter schema inspection.
type ToolsDeconstruction struct {
	Count     int                     `json:"count"`
	ToolNames []string                `json:"toolNames"`
	ToolMap   map[string]FunctionDecl `json:"toolMap"`

	// Diagnostics
	HasRunCommand          bool `json:"hasRunCommand"`
	RunCommandHasBypass    bool `json:"runCommandHasBypass"`
	InvokeSubagentAppended bool `json:"invokeSubagentAppended"` // true if "Built-in subagents:" leaked into description
}

// ParseWirePayload attempts to parse raw JSON from multiple shapes (wrapper envelope, requestBody, or raw request).
func ParseWirePayload(raw []byte) (*WirePayload, error) {
	var top map[string]any
	if err := json.Unmarshal(raw, &top); err != nil {
		return nil, fmt.Errorf("invalid JSON wire payload: %w", err)
	}

	payload := &WirePayload{
		RawRequest: top,
	}

	// Case 1: Proxy envelope with "requestBody"
	if rb, ok := top["requestBody"].(map[string]any); ok {
		top = rb
	}

	// Top-level envelope fields
	if m, ok := top["model"].(string); ok {
		payload.Model = m
	}
	if p, ok := top["project"].(string); ok {
		payload.Project = p
	}
	if rt, ok := top["requestType"].(string); ok {
		payload.RequestType = rt
	}
	if ua, ok := top["userAgent"].(string); ok {
		payload.UserAgent = ua
	}
	if reqID, ok := top["requestId"].(string); ok {
		payload.RequestID = reqID
	}

	// Inside "request" object (standard CloudCode wrapping) or at root
	reqMap := top
	if r, ok := top["request"].(map[string]any); ok {
		reqMap = r
	}

	if sid, ok := reqMap["sessionId"].(string); ok {
		payload.SessionID = sid
	}

	// 1. System Instruction
	if si, ok := reqMap["systemInstruction"].(map[string]any); ok {
		if parts, ok := si["parts"].([]any); ok {
			var sb strings.Builder
			for _, p := range parts {
				if pm, ok := p.(map[string]any); ok {
					if txt, ok := pm["text"].(string); ok {
						sb.WriteString(txt)
					}
				}
			}
			payload.SystemInstruction = sb.String()
		}
	}

	// 2. Tools
	if toolsArr, ok := reqMap["tools"].([]any); ok {
		for _, t := range toolsArr {
			if tm, ok := t.(map[string]any); ok {
				if fds, ok := tm["functionDeclarations"].([]any); ok {
					for _, fd := range fds {
						if fdm, ok := fd.(map[string]any); ok {
							decl := FunctionDecl{}
							if name, ok := fdm["name"].(string); ok {
								decl.Name = name
							}
							if desc, ok := fdm["description"].(string); ok {
								decl.Description = desc
							}
							if params, ok := fdm["parameters"].(map[string]any); ok {
								decl.Parameters = params
							}
							payload.Tools = append(payload.Tools, decl)
						}
					}
				}
			}
		}
	}

	// 3. Contents
	if contentsArr, ok := reqMap["contents"].([]any); ok {
		for _, c := range contentsArr {
			if cm, ok := c.(map[string]any); ok {
				turn := ContentTurn{}
				if role, ok := cm["role"].(string); ok {
					turn.Role = role
				}
				if parts, ok := cm["parts"].([]any); ok {
					for _, p := range parts {
						if pm, ok := p.(map[string]any); ok {
							part := ContentPart{}
							if txt, ok := pm["text"].(string); ok {
								part.Text = txt
							}
							if th, ok := pm["thought"].(bool); ok {
								part.Thought = th
							}
							if ts, ok := pm["thoughtSignature"].(string); ok {
								part.ThoughtSignature = ts
							}
							if fc, ok := pm["functionCall"].(map[string]any); ok {
								part.FunctionCall = &WireFuncCall{}
								if id, ok := fc["id"].(string); ok {
									part.FunctionCall.ID = id
								}
								if name, ok := fc["name"].(string); ok {
									part.FunctionCall.Name = name
								}
								if args, ok := fc["args"].(map[string]any); ok {
									part.FunctionCall.Args = args
								}
							}
							if fr, ok := pm["functionResponse"].(map[string]any); ok {
								part.FunctionResponse = &WireFuncResp{}
								if id, ok := fr["id"].(string); ok {
									part.FunctionResponse.ID = id
								}
								if name, ok := fr["name"].(string); ok {
									part.FunctionResponse.Name = name
								}
								part.FunctionResponse.Response = fr["response"]
							}
							turn.Parts = append(turn.Parts, part)
						}
					}
				}
				payload.Contents = append(payload.Contents, turn)
			}
		}
	}

	// 4. GenerationConfig
	if gc, ok := reqMap["generationConfig"].(map[string]any); ok {
		payload.GenerationConfig = gc
	}

	return payload, nil
}

var openTagRe = regexp.MustCompile(`(?s)<([a-zA-Z0-9_]+)>`)
var ruleTagRe = regexp.MustCompile(`(?s)<RULE\[(.*?)\]>\n(.*?)\n</RULE\[.*?\]>`)
var subagentsRe = regexp.MustCompile(`(?m)^- ([a-zA-Z0-9_-]+):`)
var skillsRe = regexp.MustCompile(`(?m)^- ([a-zA-Z0-9_-]+) \((/[^)]+)\):`)

// DeconstructPrompt breaks down a complete prompt string into XML sections and semantic facts.
func DeconstructPrompt(promptText string) *PromptDeconstruction {
	d := &PromptDeconstruction{
		FullPrompt: promptText,
		SectionMap: make(map[string]PromptSection),
	}

	pos := 0
	for pos < len(promptText) {
		sub := promptText[pos:]
		loc := openTagRe.FindStringSubmatchIndex(sub)
		if loc == nil {
			break
		}
		openStart := pos + loc[0]
		openEnd := pos + loc[1]
		tag := sub[loc[2]:loc[3]]

		closeTag := "</" + tag + ">"
		closeIdx := strings.Index(promptText[openEnd:], closeTag)
		if closeIdx == -1 {
			pos = openEnd
			continue
		}

		contentStart := openEnd
		contentEnd := openEnd + closeIdx
		totalEnd := contentEnd + len(closeTag)

		sec := PromptSection{
			Tag:       tag,
			Header:    "<" + tag + ">",
			Footer:    closeTag,
			Content:   promptText[contentStart:contentEnd],
			Raw:       promptText[openStart:totalEnd],
			StartByte: openStart,
			EndByte:   totalEnd,
		}
		d.Sections = append(d.Sections, sec)
		d.SectionOrder = append(d.SectionOrder, tag)
		d.SectionMap[tag] = sec

		pos = totalEnd
	}

	// 1. Identity section inspection
	if idSec, ok := d.SectionMap["identity"]; ok {
		if strings.Contains(idSec.Content, "agentic AI assistant designed by the Google Deepmind team") {
			d.IdentityRole = "agentic AI assistant"
			d.IsGenericPrompt = true
		} else if strings.Contains(idSec.Content, "agentic AI coding assistant") {
			d.IdentityRole = "agentic AI coding assistant"
			d.IsGenericPrompt = false
		}
	}

	// 2. Sandbox section
	if _, ok := d.SectionMap["terminal_sandbox"]; ok {
		d.IsSandboxed = true
	}

	// 3. Subagents list
	if subSec, ok := d.SectionMap["subagents"]; ok {
		matches := subagentsRe.FindAllStringSubmatch(subSec.Content, -1)
		for _, m := range matches {
			d.SubagentsList = append(d.SubagentsList, m[1])
		}
	}

	// 4. Skills list
	if skSec, ok := d.SectionMap["skills"]; ok {
		matches := skillsRe.FindAllStringSubmatch(skSec.Content, -1)
		d.SkillsCount = len(matches)
		for _, m := range matches {
			d.SkillNames = append(d.SkillNames, m[1])
		}
	}

	// 5. User Rules list
	if ruleSec, ok := d.SectionMap["user_rules"]; ok {
		matches := ruleTagRe.FindAllStringSubmatch(ruleSec.Content, -1)
		d.UserRulesCount = len(matches)
		for _, m := range matches {
			d.UserRuleLabels = append(d.UserRuleLabels, m[1])
		}
	}

	// 6. User Information
	if uiSec, ok := d.SectionMap["user_information"]; ok {
		if strings.Contains(uiSec.Content, "Files relating to the user's requests") {
			d.UserInfoWording = "Files relating to..."
		} else if strings.Contains(uiSec.Content, "Code relating to the user's requests") {
			d.UserInfoWording = "Code relating to..."
		}

		if m := regexp.MustCompile(`The USER's OS version is ([a-zA-Z0-9]+)\.`).FindStringSubmatch(uiSec.Content); len(m) > 1 {
			d.UserInfoOS = m[1]
		}
		if m := regexp.MustCompile(`Command Working Directory: (.*?)\n`).FindStringSubmatch(uiSec.Content); len(m) > 1 {
			d.UserInfoCWD = m[1]
		}
		for _, line := range strings.Split(uiSec.Content, "\n") {
			if strings.HasPrefix(line, "- ") && strings.Contains(line, "/") {
				d.UserInfoWorkspaces = append(d.UserInfoWorkspaces, strings.TrimPrefix(line, "- "))
			}
		}
	}

	return d
}

// DeconstructTools breaks down the tools function declarations.
func DeconstructTools(tools []FunctionDecl) *ToolsDeconstruction {
	td := &ToolsDeconstruction{
		Count:   len(tools),
		ToolMap: make(map[string]FunctionDecl),
	}

	for _, t := range tools {
		td.ToolNames = append(td.ToolNames, t.Name)
		td.ToolMap[t.Name] = t

		if t.Name == "run_command" {
			td.HasRunCommand = true
			if props, ok := t.Parameters["properties"].(map[string]any); ok {
				if _, ok := props["BypassSandbox"]; ok {
					td.RunCommandHasBypass = true
				}
			}
		}

		if t.Name == "invoke_subagent" {
			if strings.Contains(t.Description, "Built-in subagents:") {
				td.InvokeSubagentAppended = true
			}
		}
	}

	return td
}
