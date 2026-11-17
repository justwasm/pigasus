package agentspanel

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/justwasm/pigasus/internal/core/confhome"
	"github.com/justwasm/pigasus/internal/ui/tuitools"
)

// TrajectoryStep represents a single turn or action within a subagent's execution trajectory.
type TrajectoryStep struct {
	Kind     string // "thought", "tool", "message"
	Title    string // e.g. "Thought for 5s, 313 tokens"
	ToolName string // e.g. "Bash", "WebSearch", "SendMessage"
	RawName  string // e.g. "run_command"
	Args     string // e.g. "curl -s ..."
	Preview  string // 1-line preview for collapsed thought
	Text     string // full text for thought or message
	Output   string // output for tool
	Err      bool   // whether tool execution failed
}

func firstLine(s string) string {
	for _, l := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(l); t != "" {
			return t
		}
	}
	return ""
}

// FormatToolName standardizes tool names to match AGY display convention.
func FormatToolName(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "run_command", "bash", "sh", "exec":
		return "Bash"
	case "search_web", "websearch", "web_search":
		return "WebSearch"
	case "send_message", "message":
		return "SendMessage"
	case "view_file", "read_file", "read":
		return "ReadFile"
	case "write_file", "write_to_file", "write":
		return "WriteFile"
	case "edit_file", "replace_file_content", "edit":
		return "EditFile"
	case "ask_question":
		return "AskQuestion"
	case "schedule":
		return "Schedule"
	default:
		raw = strings.TrimSpace(raw)
		if len(raw) > 0 {
			return strings.ToUpper(raw[:1]) + raw[1:]
		}
		return raw
	}
}

// formatToolArgs extracts clean arguments from raw tool JSON input.
func formatToolArgs(toolName string, raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		var s string
		if err2 := json.Unmarshal(raw, &s); err2 == nil {
			_ = json.Unmarshal([]byte(s), &m)
		}
		if len(m) == 0 {
			str := string(raw)
			str = strings.Trim(str, "\"")
			str = strings.ReplaceAll(str, `\"`, `"`)
			return str
		}
	}
	cleanVal := func(v any) string {
		if v == nil {
			return ""
		}
		str := fmt.Sprint(v)
		str = strings.TrimSpace(str)
		if strings.HasPrefix(str, "\"") && strings.HasSuffix(str, "\"") && len(str) >= 2 {
			var unquoted string
			if err := json.Unmarshal([]byte(str), &unquoted); err == nil {
				str = unquoted
			} else {
				str = str[1 : len(str)-1]
			}
		}
		str = strings.ReplaceAll(str, `\"`, `"`)
		if idx := strings.Index(str, "\n"); idx != -1 {
			str = strings.TrimSpace(str[:idx])
			if !strings.HasSuffix(str, "...") {
				str += " ..."
			}
		}
		if len([]rune(str)) > 80 {
			runes := []rune(str)
			str = string(runes[:77]) + "..."
		}
		return str
	}

	switch strings.ToLower(toolName) {
	case "run_command", "bash":
		for _, k := range []string{"CommandLine", "command_line", "command", "cmd"} {
			if v, ok := m[k]; ok {
				return cleanVal(v)
			}
		}
	case "search_web", "websearch", "web_search":
		for _, k := range []string{"query", "Query", "q"} {
			if v, ok := m[k]; ok {
				return cleanVal(v)
			}
		}
	case "send_message", "message":
		recip := cleanVal(m["Recipient"])
		if recip == "" {
			recip = cleanVal(m["recipient"])
		}
		msg := cleanVal(m["Message"])
		if msg == "" {
			msg = cleanVal(m["message"])
		}
		fl := firstLine(msg)
		if len(fl) > 60 {
			fl = fl[:57] + "..."
		}
		if recip != "" && fl != "" {
			return recip + ": " + fl
		}
		if fl != "" {
			return fl
		}
		if recip != "" {
			return recip
		}
	case "read_file", "view_file", "write_file", "edit_file":
		for _, k := range []string{"Path", "path", "AbsolutePath", "TargetFile", "file"} {
			if v, ok := m[k]; ok {
				return cleanVal(v)
			}
		}
	case "manage_task":
		action := cleanVal(m["action"])
		if action == "" {
			action = cleanVal(m["Action"])
		}
		id := cleanVal(m["id"])
		if id == "" {
			id = cleanVal(m["TaskId"])
		}
		if action != "" && id != "" {
			return action + " " + id
		}
		if action != "" {
			return action
		}
		if id != "" {
			return id
		}
	case "read_url_content":
		for _, k := range []string{"Url", "url", "URL"} {
			if v, ok := m[k]; ok {
				return cleanVal(v)
			}
		}
	}
	for _, v := range m {
		if s := cleanVal(v); s != "" && len(s) < 100 {
			return s
		}
	}
	return ""
}

// TranscriptFileStat returns the path, mtime, and size of the transcript file on disk for subID, if any.
func TranscriptFileStat(subID string) (path string, mtime time.Time, size int64) {
	if subID == "" {
		return "", time.Time{}, 0
	}
	candidates := []string{
		filepath.Join(confhome.Antigravity(), "subagents", subID+".jsonl"),
		filepath.Join(confhome.Antigravity(), "brain", subID, ".system_generated", "logs", "transcript.jsonl"),
		filepath.Join(confhome.Antigravity(), "brain", subID, ".system_generated", "logs", "transcript_full.jsonl"),
	}
	if home := os.Getenv("HOME"); home != "" {
		candidates = append(candidates,
			filepath.Join(home, ".gemini", "antigravity-cli", "subagents", subID+".jsonl"),
			filepath.Join(home, ".gemini", "antigravity-cli", "brain", subID, ".system_generated", "logs", "transcript.jsonl"),
		)
	}
	for _, p := range candidates {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p, fi.ModTime(), fi.Size()
		}
	}
	return "", time.Time{}, 0
}

type transcriptMemoEntry struct {
	mtime time.Time
	size  int64
	steps []TrajectoryStep
	total int
}

var (
	transcriptMemoMu sync.Mutex
	transcriptMemo   = map[string]transcriptMemoEntry{}
)

// InvalidateTranscriptMemo clears the in-memory transcript cache.
func InvalidateTranscriptMemo() {
	transcriptMemoMu.Lock()
	transcriptMemo = map[string]transcriptMemoEntry{}
	transcriptMemoMu.Unlock()
}

// LoadTrajectoryFromTranscript loads trajectory steps and total steps from disk.
func LoadTrajectoryFromTranscript(subID string) ([]TrajectoryStep, int) {
	if subID == "" {
		return nil, 0
	}
	p, mtime, size := TranscriptFileStat(subID)
	if p == "" {
		return nil, 0
	}

	transcriptMemoMu.Lock()
	if entry, ok := transcriptMemo[p]; ok && entry.mtime.Equal(mtime) && entry.size == size {
		steps := make([]TrajectoryStep, len(entry.steps))
		copy(steps, entry.steps)
		total := entry.total
		transcriptMemoMu.Unlock()
		return steps, total
	}
	transcriptMemoMu.Unlock()

	f, err := os.Open(p)
	if err != nil {
		return nil, 0
	}
	defer f.Close()

	type rawRecord struct {
		StepIndex    int    `json:"step_index"`
		Source       string `json:"source"`
		Type         string `json:"type"`
		Status       string `json:"status"`
		Thinking     string `json:"thinking"`
		Content      string `json:"content"`
		CreatedAt    string `json:"created_at"`
		OutputTokens int    `json:"output_tokens"`
		ToolCalls    []struct {
			Name string          `json:"name"`
			Args json.RawMessage `json:"args"`
		} `json:"tool_calls"`
	}

	type subagentRecord struct {
		Index   int    `json:"index"`
		Role    string `json:"role"`
		Message struct {
			Role       string `json:"role"`
			Content    string `json:"content"`
			ToolName   string `json:"tool_name"`
			ToolCallID string `json:"tool_call_id"`
		} `json:"message"`
		CreatedAt string `json:"created_at"`
	}

	var rawLines [][]byte
	scanner := bufio.NewScanner(f)
	buf := make([]byte, 1024*1024)
	scanner.Buffer(buf, 10*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		cp := make([]byte, len(line))
		copy(cp, line)
		rawLines = append(rawLines, cp)
	}

	if len(rawLines) == 0 {
		return nil, 0
	}

	// Detect if file is Format B (subagents/<subID>.jsonl with message wrapper)
	isFormatB := false
	for _, l := range rawLines {
		if bytes.Contains(l, []byte(`"tool_name"`)) ||
			bytes.Contains(l, []byte(`"toolResult"`)) ||
			(bytes.Contains(l, []byte(`"subagent"`)) && bytes.Contains(l, []byte(`"message"`))) {
			isFormatB = true
			break
		}
	}

	if isFormatB {
		var subRecs []subagentRecord
		for _, l := range rawLines {
			var sr subagentRecord
			if err := json.Unmarshal(l, &sr); err == nil {
				subRecs = append(subRecs, sr)
			}
		}
		toolResults := make(map[string]string)
		var orderedResults []string
		for _, r := range subRecs {
			role := r.Role
			if role == "" {
				role = r.Message.Role
			}
			if role == "toolResult" {
				if r.Message.ToolCallID != "" {
					toolResults[r.Message.ToolCallID] = r.Message.Content
				}
				orderedResults = append(orderedResults, r.Message.Content)
			}
		}

		var steps []TrajectoryStep
		resultIdx := 0
		for recIdx, r := range subRecs {
			role := r.Role
			if role == "" {
				role = r.Message.Role
			}
			if role != "assistant" {
				continue
			}
			if r.Message.ToolName != "" {
				toolName := FormatToolName(r.Message.ToolName)
				rawName := r.Message.ToolName
				argsStr := formatToolArgs(rawName, json.RawMessage(r.Message.Content))
				output := ""
				if r.Message.ToolCallID != "" && toolResults[r.Message.ToolCallID] != "" {
					output = toolResults[r.Message.ToolCallID]
				} else if resultIdx < len(orderedResults) {
					output = orderedResults[resultIdx]
					resultIdx++
				}
				errState := false
				if strings.Contains(output, "Command exited with code 1") ||
					strings.Contains(output, "exit status 1") ||
					strings.Contains(output, "Encountered error") ||
					strings.Contains(output, "Error:") {
					errState = true
				}
				steps = append(steps, TrajectoryStep{
					Kind:     "tool",
					ToolName: toolName,
					RawName:  rawName,
					Args:     argsStr,
					Output:   output,
					Err:      errState,
				})
			} else if r.Message.Content != "" {
				text := strings.TrimSpace(r.Message.Content)
				if !strings.HasPrefix(text, "<SYSTEM_MESSAGE>") && !strings.HasPrefix(text, "The following is a <SYSTEM_MESSAGE>") && !strings.Contains(text, "<SYSTEM_MESSAGE>") && recIdx < len(subRecs)-1 {
					steps = append(steps, TrajectoryStep{
						Kind: "message",
						Text: r.Message.Content,
					})
				}
			}
		}
		if len(steps) == 0 {
			for _, r := range subRecs {
				if (r.Role == "assistant" || r.Message.Role == "assistant") && r.Message.Content != "" {
					text := strings.TrimSpace(r.Message.Content)
					if !strings.HasPrefix(text, "<SYSTEM_MESSAGE>") && !strings.HasPrefix(text, "The following is a <SYSTEM_MESSAGE>") && !strings.Contains(text, "<SYSTEM_MESSAGE>") {
						steps = append(steps, TrajectoryStep{
							Kind: "message",
							Text: r.Message.Content,
						})
						break
					}
				}
			}
		}
		transcriptMemoMu.Lock()
		transcriptMemo[p] = transcriptMemoEntry{
			mtime: mtime,
			size:  size,
			steps: steps,
			total: len(subRecs),
		}
		transcriptMemoMu.Unlock()
		outSteps := make([]TrajectoryStep, len(steps))
		copy(outSteps, steps)
		return outSteps, len(subRecs)
	}

	// Format A: Brain transcript
	var records []rawRecord
	for _, l := range rawLines {
		var r rawRecord
		if err := json.Unmarshal(l, &r); err == nil {
			records = append(records, r)
		}
	}

	var steps []TrajectoryStep
	totalSteps := len(records)
	var prevTime time.Time

	for i := 0; i < len(records); i++ {
		r := records[i]
		curTime, _ := time.Parse(time.RFC3339Nano, r.CreatedAt)
		if curTime.IsZero() {
			curTime, _ = time.Parse(time.RFC3339, r.CreatedAt)
		}

		if r.Thinking != "" {
			dur := 5 * time.Second
			if !prevTime.IsZero() && !curTime.IsZero() && curTime.After(prevTime) {
				dur = curTime.Sub(prevTime)
				if dur < time.Second {
					dur = time.Second
				}
			}
			tokens := r.OutputTokens
			if tokens <= 0 {
				tokens = len(strings.Fields(r.Thinking))
			}
			preview := firstLine(r.Thinking)
			steps = append(steps, TrajectoryStep{
				Kind:    "thought",
				Title:   fmt.Sprintf("Thought for %ds, %d tokens", int(dur.Seconds()), tokens),
				Preview: preview,
				Text:    r.Thinking,
			})
		}

		if len(r.ToolCalls) > 0 {
			for tcIdx, tc := range r.ToolCalls {
				toolName := FormatToolName(tc.Name)
				argsStr := formatToolArgs(tc.Name, tc.Args)
				output := ""
				errState := false
				// Look ahead for tool output in matching GENERIC record
				targetIdx := i + 1 + tcIdx
				if targetIdx < len(records) && records[targetIdx].Type == "GENERIC" {
					output = records[targetIdx].Content
					if records[targetIdx].Status == "ERROR" ||
						strings.Contains(output, "Command exited with code 1") ||
						strings.Contains(output, "exit status 1") ||
						strings.Contains(output, "Error:") {
						errState = true
					}
				}
				steps = append(steps, TrajectoryStep{
					Kind:     "tool",
					ToolName: toolName,
					RawName:  tc.Name,
					Args:     argsStr,
					Output:   output,
					Err:      errState,
				})
			}
		} else if r.Type == "PLANNER_RESPONSE" && r.Content != "" {
			steps = append(steps, TrajectoryStep{
				Kind: "message",
				Text: r.Content,
			})
		}

		if !curTime.IsZero() {
			prevTime = curTime
		}
	}

	transcriptMemoMu.Lock()
	transcriptMemo[p] = transcriptMemoEntry{
		mtime: mtime,
		size:  size,
		steps: steps,
		total: totalSteps,
	}
	transcriptMemoMu.Unlock()
	outSteps := make([]TrajectoryStep, len(steps))
	copy(outSteps, steps)
	return outSteps, totalSteps
}

// ParseFallbackSteps parses synthetic/in-memory steps when no transcript JSONL file is available on disk.
func ParseFallbackSteps(stepsRaw []string, output string, stepOutputs ...string) []TrajectoryStep {
	var steps []TrajectoryStep
	for i, raw := range stepsRaw {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		var stepOutput string
		if i < len(stepOutputs) {
			stepOutput = stepOutputs[i]
		}
		errState := false
		if strings.Contains(stepOutput, "Command exited with code 1") ||
			strings.Contains(stepOutput, "exit status 1") ||
			strings.Contains(stepOutput, "Error:") ||
			strings.Contains(stepOutput, "Encountered error") {
			errState = true
		}
		if strings.HasPrefix(raw, "Thought") {
			title := raw
			if idx := strings.Index(raw, "\n"); idx != -1 {
				title = strings.TrimSpace(raw[:idx])
			}
			steps = append(steps, TrajectoryStep{
				Kind:    "thought",
				Title:   title,
				Preview: firstLine(raw),
				Text:    raw,
			})
			continue
		}
		if idx := strings.Index(raw, ": "); idx != -1 {
			prefix := raw[:idx]
			args := strings.TrimSpace(raw[idx+2:])
			toolName := FormatToolName(prefix)
			steps = append(steps, TrajectoryStep{
				Kind:     "tool",
				ToolName: toolName,
				RawName:  prefix,
				Args:     args,
				Output:   stepOutput,
				Err:      errState,
			})
			continue
		}
		if strings.Contains(raw, "(") && strings.HasSuffix(raw, ")") {
			idx := strings.Index(raw, "(")
			prefix := raw[:idx]
			toolName := FormatToolName(prefix)
			args := raw[idx+1 : len(raw)-1]
			steps = append(steps, TrajectoryStep{
				Kind:     "tool",
				ToolName: toolName,
				RawName:  prefix,
				Args:     args,
				Output:   stepOutput,
				Err:      errState,
			})
			continue
		}
		toolName := FormatToolName(raw)
		steps = append(steps, TrajectoryStep{
			Kind:     "tool",
			ToolName: toolName,
			RawName:  raw,
			Output:   stepOutput,
			Err:      errState,
		})
	}
	if out := strings.TrimSpace(output); out != "" {
		hasMsg := false
		for _, s := range steps {
			if s.Kind == "message" && strings.TrimSpace(s.Text) == out {
				hasMsg = true
				break
			}
		}
		if !hasMsg {
			steps = append(steps, TrajectoryStep{
				Kind: "message",
				Text: out,
			})
		}
	}
	return steps
}

// RenderTrajectoryStep renders a single TrajectoryStep into visual lines:
// - thoughts: collapsed shows "  ▸ Thought for Xs, Y tokens" + 1-line preview; expanded shows "  ▾ ..." + full indented text.
// - tools: collapsed shows "  ● ToolName(args)"; expanded shows header plus indented, syntax/format-cleaned output lines.
// - messages: indented response lines.
func RenderTrajectoryStep(step TrajectoryStep, width int, expanded bool) []string {
	var rows []string
	maxW := width - 6
	if maxW < 20 {
		maxW = 20
	}

	switch step.Kind {
	case "thought":
		if !expanded {
			rows = append(rows, "  \x1b[38;5;146m▸ "+step.Title+"\x1b[39m")
			prev := strings.TrimSpace(step.Preview)
			if prev != "" {
				truncated := ansi.Truncate(prev, maxW, "...")
				rows = append(rows, "    \x1b[38;5;146m"+truncated+"\x1b[39m")
			}
		} else {
			rows = append(rows, "  \x1b[38;5;146m▾ "+step.Title+"\x1b[39m")
			for _, line := range strings.Split(step.Text, "\n") {
				line = strings.TrimRight(line, "\r")
				if strings.TrimSpace(line) == "" {
					continue
				}
				for _, wl := range strings.Split(ansi.Wrap(line, maxW, ""), "\n") {
					rows = append(rows, "    \x1b[38;5;146m"+wl+"\x1b[39m")
				}
			}
		}
	case "tool":
		bullet := "\x1b[38;5;114m●\x1b[39m"
		if step.Err {
			bullet = "\x1b[38;5;196m●\x1b[39m"
		}
		toolName := "\x1b[1m\x1b[38;5;221m" + step.ToolName + "\x1b[0m"
		argStr := ""
		if step.Args != "" {
			argStr = "\x1b[38;5;146m(" + step.Args + ")\x1b[39m"
		}
		callHead := fmt.Sprintf("  %s %s%s", bullet, toolName, argStr)
		rows = append(rows, callHead)

		if expanded {
			rawName := step.RawName
			if rawName == "" {
				rawName = step.ToolName
			}
			detail := tuitools.StepDetailText(rawName, step.Output, false)
			detail = strings.TrimSpace(tuitools.NormalizeDetailNewlines(detail))
			if detail != "" {
				diffMode := tuitools.LooksLikeDiff(detail)
				wrapWidth := maxW - 4
				if wrapWidth < 20 {
					wrapWidth = 20
				}
				var wrappedLines []string
				for _, l := range strings.Split(detail, "\n") {
					l = strings.TrimRight(l, "\r")
					if diffMode {
						wrappedLines = append(wrappedLines, tuitools.WrapDiffLine(l, wrapWidth)...)
					} else {
						l = tuitools.ExpandTabs(l, 4)
						wrapped := ansi.Wrap(l, wrapWidth, "")
						wrappedLines = append(wrappedLines, strings.Split(wrapped, "\n")...)
					}
				}
				formatted := tuitools.FormatDetailLines(wrappedLines, diffMode)
				for _, fl := range formatted {
					rows = append(rows, fl)
				}
			}
		}
	case "message":
		for _, line := range strings.Split(step.Text, "\n") {
			line = strings.TrimRight(line, "\r")
			for _, wl := range strings.Split(ansi.Wrap(line, maxW, ""), "\n") {
				rows = append(rows, "    \x1b[38;2;232;234;237m"+wl+"\x1b[39m")
			}
		}
	}
	return rows
}
