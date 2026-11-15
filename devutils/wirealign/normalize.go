package main

import (
	"regexp"
)

// NormalizeOptions controls which dynamic/ephemeral jitters are masked during comparison.
type NormalizeOptions struct {
	MaskUUIDs      bool // Replaces UUIDs with {{UUID}}
	MaskTimestamps bool // Replaces ISO timestamps and Created/Completed At lines
	MaskWorkspaces bool // Replaces workspace/CWD paths with {{WORKSPACE}}
	MaskHomeDir    bool // Replaces /Users/xxx or /home/xxx with {{HOME}}
	MaskTaskIDs    bool // Replaces task-12345 with {{TASK_ID}}
}

// DefaultNormalizeOptions returns the recommended normalization settings for parity checks.
func DefaultNormalizeOptions() NormalizeOptions {
	return NormalizeOptions{
		MaskUUIDs:      true,
		MaskTimestamps: true,
		MaskWorkspaces: true,
		MaskHomeDir:    true,
		MaskTaskIDs:    true,
	}
}

var (
	uuidRe     = regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)
	isoTimeRe  = regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})`)
	stepTimeRe = regexp.MustCompile(`(Created At|Completed At): \d{4}-\d{2}-\d{2}T[^\n]+`)
	homeDirRe  = regexp.MustCompile(`/(?:Users|home)/[a-zA-Z0-9._-]+`)
	tmpWsRe    = regexp.MustCompile(`/(?:tmp|var/folders/[^\s]+)/(?:bench_agy|wirealign_ws_[a-zA-Z0-9]+|tuialign_ws_[a-zA-Z0-9]+|[0-9a-f-]+)`)
	taskIDRe   = regexp.MustCompile(`task-[0-9a-zA-Z]+`)
)

// NormalizeText replaces ephemeral runtime jitters in a text string according to the options.
func NormalizeText(text string, opts NormalizeOptions) string {
	res := text

	if opts.MaskTimestamps {
		res = stepTimeRe.ReplaceAllString(res, "$1: {{TIMESTAMP}}")
		res = isoTimeRe.ReplaceAllString(res, "{{TIMESTAMP}}")
	}

	if opts.MaskWorkspaces {
		res = tmpWsRe.ReplaceAllString(res, "{{WORKSPACE}}")
	}

	if opts.MaskHomeDir {
		res = homeDirRe.ReplaceAllString(res, "{{HOME}}")
	}

	if opts.MaskUUIDs {
		res = uuidRe.ReplaceAllString(res, "{{UUID}}")
	}

	if opts.MaskTaskIDs {
		res = taskIDRe.ReplaceAllString(res, "{{TASK_ID}}")
	}

	return res
}

// NormalizePayload creates a normalized copy of a WirePayload with ephemeral strings masked.
func NormalizePayload(p *WirePayload, opts NormalizeOptions) *WirePayload {
	if p == nil {
		return nil
	}

	norm := &WirePayload{
		Model:             p.Model,
		Project:           p.Project,
		RequestType:       p.RequestType,
		UserAgent:         p.UserAgent,
		RequestID:         "{{REQUEST_ID}}",
		SessionID:         "{{SESSION_ID}}",
		SystemInstruction: NormalizeText(p.SystemInstruction, opts),
		GenerationConfig:  p.GenerationConfig,
	}

	// Normalize Tools
	for _, t := range p.Tools {
		normDecl := FunctionDecl{
			Name:        t.Name,
			Description: NormalizeText(t.Description, opts),
			Parameters:  normalizeMap(t.Parameters, opts),
		}
		norm.Tools = append(norm.Tools, normDecl)
	}

	// Normalize Contents
	for _, c := range p.Contents {
		normTurn := ContentTurn{
			Role: c.Role,
		}
		for _, part := range c.Parts {
			normPart := ContentPart{
				Text:             NormalizeText(part.Text, opts),
				Thought:          part.Thought,
				ThoughtSignature: "{{THOUGHT_SIG}}",
			}
			if part.FunctionCall != nil {
				normPart.FunctionCall = &WireFuncCall{
					ID:   "{{CALL_ID}}",
					Name: part.FunctionCall.Name,
					Args: normalizeMap(part.FunctionCall.Args, opts),
				}
			}
			if part.FunctionResponse != nil {
				normPart.FunctionResponse = &WireFuncResp{
					ID:       "{{CALL_ID}}",
					Name:     part.FunctionResponse.Name,
					Response: normalizeValue(part.FunctionResponse.Response, opts),
				}
			}
			normTurn.Parts = append(normTurn.Parts, normPart)
		}
		norm.Contents = append(norm.Contents, normTurn)
	}

	return norm
}

func normalizeMap(m map[string]any, opts NormalizeOptions) map[string]any {
	if m == nil {
		return nil
	}
	res := make(map[string]any)
	for k, v := range m {
		res[k] = normalizeValue(v, opts)
	}
	return res
}

func normalizeValue(v any, opts NormalizeOptions) any {
	switch val := v.(type) {
	case string:
		return NormalizeText(val, opts)
	case map[string]any:
		return normalizeMap(val, opts)
	case []any:
		arr := make([]any, len(val))
		for i, el := range val {
			arr[i] = normalizeValue(el, opts)
		}
		return arr
	default:
		return val
	}
}
