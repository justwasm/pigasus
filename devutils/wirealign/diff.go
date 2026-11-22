package main

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
)

// CompareOptions specifies how strictly two wire payloads should be compared.
type CompareOptions struct {
	Normalize            bool // Apply ephemeral jitter masking before diffing
	NormalizeOpts        NormalizeOptions
	AllowSkillMismatch   bool // Ignore differences in local installed plugin skills
	AllowWorkspaceDiff   bool // Ignore differences in local workspace directory strings
	AllowRuleContentDiff bool // Ignore content diff inside <user_rules> if rule label/order matches
	AllowExtraTools      bool // Allow plugin/dynamic tools (e.g. swarm, run_workflow)
	StrictByteIdentity   bool // Require exact byte identity on normalized prompt
}

// DefaultCompareOptions returns recommended parity comparison options.
func DefaultCompareOptions() CompareOptions {
	return CompareOptions{
		Normalize:            true,
		NormalizeOpts:        DefaultNormalizeOptions(),
		AllowSkillMismatch:   true,
		AllowWorkspaceDiff:   true,
		AllowRuleContentDiff: true,
		AllowExtraTools:      true,
		StrictByteIdentity:   false,
	}
}

// DiagnosticSeverity indicates whether an issue is fatal, a warning, or informational.
type DiagnosticSeverity string

const (
	SeverityError   DiagnosticSeverity = "ERROR"
	SeverityWarning DiagnosticSeverity = "WARN"
	SeverityInfo    DiagnosticSeverity = "INFO"
)

// Diagnostic represents an alignment observation or discrepancy.
type Diagnostic struct {
	Code     string             `json:"code"`
	Severity DiagnosticSeverity `json:"severity"`
	Message  string             `json:"message"`
	Detail   string             `json:"detail,omitempty"`
}

// SectionDiff describes differences within one prompt section.
type SectionDiff struct {
	Tag        string `json:"tag"`
	Matched    bool   `json:"matched"`
	RefBytes   int    `json:"refBytes"`
	TgtBytes   int    `json:"tgtBytes"`
	RefSnippet string `json:"refSnippet,omitempty"`
	TgtSnippet string `json:"tgtSnippet,omitempty"`
}

// PromptDiff summarizes the prompt alignment.
type PromptDiff struct {
	Matched         bool          `json:"matched"`
	OrderMatched    bool          `json:"orderMatched"`
	RefSections     []string      `json:"refSections"`
	TgtSections     []string      `json:"tgtSections"`
	MissingSections []string      `json:"missingSections,omitempty"`
	ExtraSections   []string      `json:"extraSections,omitempty"`
	SectionDiffs    []SectionDiff `json:"sectionDiffs"`
}

// ToolDiff describes differences in tool declarations.
type ToolDiff struct {
	Name         string   `json:"name"`
	Matched      bool     `json:"matched"`
	DescMatched  bool     `json:"descMatched"`
	ParamMatched bool     `json:"paramMatched"`
	ParamDiffs   []string `json:"paramDiffs,omitempty"`
}

// ToolsDiff summarizes the tools surface alignment.
type ToolsDiff struct {
	Matched      bool       `json:"matched"`
	OrderMatched bool       `json:"orderMatched"`
	RefCount     int        `json:"refCount"`
	TgtCount     int        `json:"tgtCount"`
	MissingTools []string   `json:"missingTools,omitempty"`
	ExtraTools   []string   `json:"extraTools,omitempty"`
	ToolDiffs    []ToolDiff `json:"toolDiffs"`
}

// WireDiffResult is the comprehensive comparison report between reference (AGY) and target (Pigasus).
type WireDiffResult struct {
	Passed        bool         `json:"passed"`
	TotalChecks   int          `json:"totalChecks"`
	PassedChecks  int          `json:"passedChecks"`
	FailedChecks  int          `json:"failedChecks"`
	WarningsCount int          `json:"warningsCount"`
	PromptDiff    *PromptDiff  `json:"promptDiff"`
	ToolsDiff     *ToolsDiff   `json:"toolsDiff"`
	Diagnostics   []Diagnostic `json:"diagnostics"`
}

// CompareWire compares two wire payloads and produces an exhaustive diff report.
func CompareWire(ref, tgt *WirePayload, opts CompareOptions) *WireDiffResult {
	res := &WireDiffResult{
		Passed: true,
	}

	workRef := ref
	workTgt := tgt

	if opts.Normalize {
		workRef = NormalizePayload(ref, opts.NormalizeOpts)
		workTgt = NormalizePayload(tgt, opts.NormalizeOpts)
	}

	refPrompt := DeconstructPrompt(workRef.SystemInstruction)
	tgtPrompt := DeconstructPrompt(workTgt.SystemInstruction)

	refTools := DeconstructTools(workRef.Tools)
	tgtTools := DeconstructTools(workTgt.Tools)

	// 1. Compare Prompt Sections
	pDiff := comparePrompts(refPrompt, tgtPrompt, opts, res)
	res.PromptDiff = pDiff

	// 2. Compare Tool Declarations
	tDiff := compareTools(refTools, tgtTools, opts, res)
	res.ToolsDiff = tDiff

	// 3. High-Leverage Semantic Diagnostics
	checkSemanticParity(refPrompt, tgtPrompt, refTools, tgtTools, res)

	// 4. Conversation Contents (if reference defines contents)
	if len(workRef.Contents) > 0 {
		compareContents(workRef.Contents, workTgt.Contents, res)
	}

	if res.FailedChecks > 0 {
		res.Passed = false
	}

	return res
}

func compareContents(ref, tgt []ContentTurn, res *WireDiffResult) {
	res.TotalChecks++
	if len(ref) != len(tgt) {
		res.FailedChecks++
		res.Diagnostics = append(res.Diagnostics, Diagnostic{
			Code:     "DIAG_CONTENTS_COUNT",
			Severity: SeverityError,
			Message:  fmt.Sprintf("Contents turn count mismatch: ref=%d, tgt=%d", len(ref), len(tgt)),
		})
		return
	}
	for i := range ref {
		if ref[i].Role != tgt[i].Role {
			res.FailedChecks++
			res.Diagnostics = append(res.Diagnostics, Diagnostic{
				Code:     "DIAG_CONTENTS_ROLE",
				Severity: SeverityError,
				Message:  fmt.Sprintf("Contents turn[%d] role mismatch: ref=%s, tgt=%s", i, ref[i].Role, tgt[i].Role),
			})
			return
		}
	}
	res.PassedChecks++
}

func comparePrompts(ref, tgt *PromptDeconstruction, opts CompareOptions, res *WireDiffResult) *PromptDiff {
	pd := &PromptDiff{
		Matched:      true,
		RefSections:  ref.SectionOrder,
		TgtSections:  tgt.SectionOrder,
		OrderMatched: reflect.DeepEqual(ref.SectionOrder, tgt.SectionOrder),
	}

	res.TotalChecks++
	if pd.OrderMatched {
		res.PassedChecks++
	} else {
		res.FailedChecks++
		res.Diagnostics = append(res.Diagnostics, Diagnostic{
			Code:     "DIAG_SECTION_ORDER",
			Severity: SeverityError,
			Message:  "Prompt section order differs between reference and target",
			Detail:   fmt.Sprintf("Ref order: %v\nTgt order: %v", ref.SectionOrder, tgt.SectionOrder),
		})
	}

	refMap := ref.SectionMap
	tgtMap := tgt.SectionMap

	for tag := range refMap {
		if _, exists := tgtMap[tag]; !exists {
			pd.MissingSections = append(pd.MissingSections, tag)
		}
	}
	for tag := range tgtMap {
		if _, exists := refMap[tag]; !exists {
			pd.ExtraSections = append(pd.ExtraSections, tag)
		}
	}

	for _, tag := range ref.SectionOrder {
		refSec := refMap[tag]
		tgtSec, hasTgt := tgtMap[tag]

		sd := SectionDiff{
			Tag:      tag,
			RefBytes: len(refSec.Content),
		}

		if !hasTgt {
			sd.Matched = false
			pd.Matched = false
			pd.SectionDiffs = append(pd.SectionDiffs, sd)
			continue
		}

		sd.TgtBytes = len(tgtSec.Content)
		cRef := refSec.Content
		cTgt := tgtSec.Content

		// If local skill mismatch, workspace string, or rule content diff is allowed:
		if (tag == "skills" && opts.AllowSkillMismatch) ||
			(tag == "user_information" && opts.AllowWorkspaceDiff) ||
			(tag == "user_rules" && opts.AllowRuleContentDiff) {
			sd.Matched = true
		} else {
			sd.Matched = (cRef == cTgt)
		}

		if !sd.Matched {
			pd.Matched = false
			sd.RefSnippet = snippet(cRef, 80)
			sd.TgtSnippet = snippet(cTgt, 80)
		}

		pd.SectionDiffs = append(pd.SectionDiffs, sd)
	}

	return pd
}

func compareTools(ref, tgt *ToolsDeconstruction, opts CompareOptions, res *WireDiffResult) *ToolsDiff {
	td := &ToolsDiff{
		Matched:      true,
		RefCount:     ref.Count,
		TgtCount:     tgt.Count,
		OrderMatched: reflect.DeepEqual(ref.ToolNames, tgt.ToolNames),
	}

	res.TotalChecks++
	if ref.Count == tgt.Count && td.OrderMatched {
		res.PassedChecks++
	} else if opts.AllowExtraTools {
		res.PassedChecks++
		res.WarningsCount++
		res.Diagnostics = append(res.Diagnostics, Diagnostic{
			Code:     "DIAG_TOOL_COUNT",
			Severity: SeverityInfo,
			Message:  fmt.Sprintf("Tool count differs (ref: %d, tgt: %d; extra dynamic tools permitted)", ref.Count, tgt.Count),
			Detail:   fmt.Sprintf("Ref tools: %v\nTgt tools: %v", ref.ToolNames, tgt.ToolNames),
		})
	} else {
		res.FailedChecks++
		res.Diagnostics = append(res.Diagnostics, Diagnostic{
			Code:     "DIAG_TOOL_COUNT",
			Severity: SeverityError,
			Message:  fmt.Sprintf("Tool declarations count/order mismatch: ref has %d, target has %d", ref.Count, tgt.Count),
			Detail:   fmt.Sprintf("Ref tools: %v\nTgt tools: %v", ref.ToolNames, tgt.ToolNames),
		})
	}

	for name, refTool := range ref.ToolMap {
		tgtTool, exists := tgt.ToolMap[name]
		diff := ToolDiff{
			Name: name,
		}

		if !exists {
			td.MissingTools = append(td.MissingTools, name)
			if !opts.AllowExtraTools {
				diff.Matched = false
				td.Matched = false
			}
			td.ToolDiffs = append(td.ToolDiffs, diff)
			continue
		}

		diff.DescMatched = (refTool.Description == tgtTool.Description)
		diff.ParamMatched = reflect.DeepEqual(refTool.Parameters, tgtTool.Parameters)

		if !diff.ParamMatched {
			diff.ParamDiffs = diffParamSchemas(refTool.Parameters, tgtTool.Parameters)
		}

		diff.Matched = diff.DescMatched && diff.ParamMatched
		if !diff.Matched {
			td.Matched = false
		}

		td.ToolDiffs = append(td.ToolDiffs, diff)
	}

	for name := range tgt.ToolMap {
		if _, exists := ref.ToolMap[name]; !exists {
			td.ExtraTools = append(td.ExtraTools, name)
		}
	}

	return td
}

func checkSemanticParity(refPrompt, tgtPrompt *PromptDeconstruction, refTools, tgtTools *ToolsDeconstruction, res *WireDiffResult) {
	// 1. Identity generic prompt check
	res.TotalChecks++
	if refPrompt.IdentityRole != "" && refPrompt.IdentityRole == tgtPrompt.IdentityRole {
		res.PassedChecks++
	} else if refPrompt.IdentityRole != "" {
		res.FailedChecks++
		res.Diagnostics = append(res.Diagnostics, Diagnostic{
			Code:     "DIAG_IDENTITY_DRIFT",
			Severity: SeverityError,
			Message:  fmt.Sprintf("Identity role mismatch: want %q, got %q", refPrompt.IdentityRole, tgtPrompt.IdentityRole),
			Detail:   fmt.Sprintf("generic-system-prompt state: ref=%v, tgt=%v", refPrompt.IsGenericPrompt, tgtPrompt.IsGenericPrompt),
		})
	}

	// 2. Sandbox parity check
	res.TotalChecks++
	if refPrompt.IsSandboxed == tgtPrompt.IsSandboxed && refTools.RunCommandHasBypass == tgtTools.RunCommandHasBypass {
		res.PassedChecks++
	} else {
		res.FailedChecks++
		res.Diagnostics = append(res.Diagnostics, Diagnostic{
			Code:     "DIAG_SANDBOX_MISMATCH",
			Severity: SeverityError,
			Message:  "Sandbox state mismatch between reference and target",
			Detail: fmt.Sprintf("Prompt <terminal_sandbox>: ref=%v, tgt=%v\nrun_command BypassSandbox: ref=%v, tgt=%v",
				refPrompt.IsSandboxed, tgtPrompt.IsSandboxed, refTools.RunCommandHasBypass, tgtTools.RunCommandHasBypass),
		})
	}

	// 3. invoke_subagent description check
	res.TotalChecks++
	if !tgtTools.InvokeSubagentAppended {
		res.PassedChecks++
	} else {
		res.FailedChecks++
		res.Diagnostics = append(res.Diagnostics, Diagnostic{
			Code:     "DIAG_INVOKE_SUBAGENT_DESCRIPTION",
			Severity: SeverityError,
			Message:  "Target invoke_subagent tool description contains unwanted appended built-in list",
			Detail:   "Expected clean reference description without 'Built-in subagents:' list",
		})
	}

	// 4. Subagents list check
	res.TotalChecks++
	if reflect.DeepEqual(refPrompt.SubagentsList, tgtPrompt.SubagentsList) {
		res.PassedChecks++
	} else {
		res.WarningsCount++
		res.Diagnostics = append(res.Diagnostics, Diagnostic{
			Code:     "DIAG_SUBAGENTS_LIST",
			Severity: SeverityWarning,
			Message:  fmt.Sprintf("Prompt subagents list differs: ref=%v, tgt=%v", refPrompt.SubagentsList, tgtPrompt.SubagentsList),
		})
	}

	// 5. User information wording check
	res.TotalChecks++
	if refPrompt.UserInfoWording == tgtPrompt.UserInfoWording {
		res.PassedChecks++
	} else if refPrompt.UserInfoWording != "" {
		res.FailedChecks++
		res.Diagnostics = append(res.Diagnostics, Diagnostic{
			Code:     "DIAG_USER_INFO_WORDING",
			Severity: SeverityError,
			Message:  fmt.Sprintf("User information wording mismatch: want %q, got %q", refPrompt.UserInfoWording, tgtPrompt.UserInfoWording),
			Detail:   "In AGY 1.5.9 with generic-system-prompt, wording must be 'Files relating to...', not 'Code relating to...'",
		})
	}
}

func snippet(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.TrimSpace(s)
	if len(s) > maxLen {
		return s[:maxLen] + "..."
	}
	return s
}

func diffParamSchemas(p1, p2 map[string]any) []string {
	var diffs []string
	b1, _ := json.Marshal(p1)
	b2, _ := json.Marshal(p2)
	if string(b1) != string(b2) {
		diffs = append(diffs, fmt.Sprintf("schema json differs: ref=%s, tgt=%s", string(b1), string(b2)))
	}
	return diffs
}
