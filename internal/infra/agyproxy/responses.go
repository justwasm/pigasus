package agyproxy

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/justwasm/pigasus/geminicode"
	accountrouting "github.com/justwasm/pigasus/internal/features/accounts/routing"
)

// ---- OpenAI Responses API ---------------------------------------------------

type responsesReq struct {
	Model              string          `json:"model"`
	Instructions       string          `json:"instructions,omitempty"`
	Input              json.RawMessage `json:"input"`
	Tools              []responseTool  `json:"tools,omitempty"`
	Stream             bool            `json:"stream,omitempty"`
	Temperature        *float64        `json:"temperature,omitempty"`
	MaxOutputTokens    int             `json:"max_output_tokens,omitempty"`
	ParallelToolCalls  *bool           `json:"parallel_tool_calls,omitempty"`
	PreviousResponseID string          `json:"previous_response_id,omitempty"`
	Reasoning          *struct {
		Effort string `json:"effort"`
	} `json:"reasoning,omitempty"`
	ReasoningEffort string `json:"reasoning_effort,omitempty"`
}

type responseTool struct {
	Type        string          `json:"type"` // "function", "custom", "namespace", or "tool_search"
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
	Tools       []responseTool  `json:"tools,omitempty"`
	Format      *struct {
		Type       string `json:"type"`
		Syntax     string `json:"syntax,omitempty"`
		Definition string `json:"definition,omitempty"`
	} `json:"format,omitempty"`
	Strict *bool `json:"strict,omitempty"`
}

type responseItem struct {
	Type      string         `json:"type,omitempty"`
	Role      string         `json:"role,omitempty"`
	Content   any            `json:"content,omitempty"`
	ID        string         `json:"id,omitempty"`
	CallID    string         `json:"call_id,omitempty"`
	Name      string         `json:"name,omitempty"`
	Arguments any            `json:"arguments,omitempty"`
	Input     string         `json:"input,omitempty"`
	Output    any            `json:"output,omitempty"`
	Status    string         `json:"status,omitempty"`
	Tools     []responseTool `json:"tools,omitempty"`
	Reasoning *struct {
		Effort string `json:"effort"`
	} `json:"reasoning,omitempty"`
}

// customSchema is the arguments schema a freeform tool is offered as taking.
var customSchema = json.RawMessage(`{"type":"object","properties":{"input":{"type":"string","description":"The tool's free-form input, the whole of it, exactly as it is to be run."}},"required":["input"],"additionalProperties":false}`)

func customDescription(t responseTool) string {
	d := strings.TrimSpace(t.Description)
	if d != "" {
		d += "\n\n"
	}
	d += "Put the tool's whole free-form input, as it is to be run, in the `input` string; it is passed on as written, so it is not itself JSON."
	if f := t.Format; f != nil && f.Type == "grammar" && strings.TrimSpace(f.Definition) != "" {
		syntax := f.Syntax
		if syntax == "" {
			syntax = "the"
		}
		d += "\n\nThe input follows this " + syntax + " grammar:\n" + strings.TrimSpace(f.Definition)
	}
	return d
}

func customInput(args string) string {
	var a struct {
		Input *string `json:"input"`
	}
	if json.Unmarshal([]byte(args), &a) == nil && a.Input != nil {
		return *a.Input
	}
	var str string
	if json.Unmarshal([]byte(args), &str) == nil {
		return str
	}
	return args
}

func searchFoundTools(tools []responseTool) string {
	var names []string
	for _, t := range tools {
		if t.Name != "" {
			names = append(names, t.Name)
		}
	}
	if len(names) == 0 {
		return "Found no tools matching search."
	}
	return "Found tools: " + strings.Join(names, ", ")
}

func stringifyOutput(out any) string {
	switch v := out.(type) {
	case string:
		return v
	case nil:
		return ""
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprintf("%v", v)
		}
		return string(b)
	}
}

func parseResponseItems(raw json.RawMessage) ([]responseItem, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return nil, nil
	}
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, err
		}
		return []responseItem{{Type: "message", Role: "user", Content: s}}, nil
	}
	if raw[0] == '[' {
		var items []responseItem
		if err := json.Unmarshal(raw, &items); err != nil {
			return nil, err
		}
		return items, nil
	}
	return nil, fmt.Errorf("input is neither string nor array")
}

// handleResponses processes POST /v1/responses.
func (s *Server) handleResponses(w http.ResponseWriter, r *http.Request, body []byte) {
	setCORSHeaders(w)
	if r.Method != http.MethodPost {
		writeOpenAIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST required for /v1/responses")
		return
	}

	var req responsesReq
	if err := json.Unmarshal(body, &req); err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "invalid_json", "invalid JSON: "+err.Error())
		return
	}

	var allItems []json.RawMessage
	if req.PreviousResponseID != "" {
		if prior, ok := globalPrevStore.get(req.PreviousResponseID); ok {
			allItems = append(allItems, prior...)
		} else {
			writeOpenAIError(w, http.StatusBadRequest, "previous_response_not_found", fmt.Sprintf("Previous response with id '%s' not found.", req.PreviousResponseID))
			return
		}
	}

	items, err := parseResponseItems(req.Input)
	if err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "invalid_input", "invalid input: "+err.Error())
		return
	}

	// Track input items for previous_response_id caching
	if len(req.Input) > 0 {
		var rawList []json.RawMessage
		if req.Input[0] == '[' {
			_ = json.Unmarshal(req.Input, &rawList)
		} else if req.Input[0] == '"' {
			rawList = []json.RawMessage{req.Input}
		}
		allItems = append(allItems, rawList...)
	}

	canonicalModel := normalizeModelName(req.Model)
	family := accountrouting.ModelFamily(canonicalModel)

	var systemPromptParts []string
	if strings.TrimSpace(req.Instructions) != "" {
		systemPromptParts = append(systemPromptParts, req.Instructions)
	}

	var turnMessages []geminicode.TurnMessage
	customTools := make(map[string]bool)

	for _, it := range items {
		role := strings.ToLower(strings.TrimSpace(it.Role))
		itemType := strings.ToLower(strings.TrimSpace(it.Type))

		switch {
		case itemType == "message" || (itemType == "" && role != ""):
			text, images := parseOpenAIContent(it.Content)
			if role == "system" || role == "developer" {
				if text != "" {
					systemPromptParts = append(systemPromptParts, text)
				}
				continue
			}
			if role == "" {
				role = "user"
			}
			if len(turnMessages) > 0 && turnMessages[len(turnMessages)-1].Role == "user" && role == "user" {
				last := &turnMessages[len(turnMessages)-1]
				if text != "" {
					if last.Content != "" {
						last.Content += "\n\n"
					}
					last.Content += text
				}
				last.Images = append(last.Images, images...)
			} else {
				turnMessages = append(turnMessages, geminicode.TurnMessage{
					Role:    role,
					Content: text,
					Images:  images,
				})
			}
		case itemType == "agent_message":
			text, images := parseOpenAIContent(it.Content)
			if len(turnMessages) > 0 && turnMessages[len(turnMessages)-1].Role == "user" {
				last := &turnMessages[len(turnMessages)-1]
				if text != "" {
					if last.Content != "" {
						last.Content += "\n\n"
					}
					last.Content += text
				}
				last.Images = append(last.Images, images...)
			} else {
				turnMessages = append(turnMessages, geminicode.TurnMessage{
					Role:    "user",
					Content: text,
					Images:  images,
				})
			}
		case itemType == "function_call":
			callID := it.CallID
			if callID == "" {
				callID = it.ID
			}
			argsStr := stringifyOutput(it.Arguments)
			turnMessages = append(turnMessages, geminicode.TurnMessage{
				Role: "assistant",
				ToolCalls: []geminicode.TurnToolCall{
					{
						ID:        callID,
						Name:      it.Name,
						Arguments: json.RawMessage(argsStr),
					},
				},
			})
		case itemType == "custom_tool_call":
			callID := it.CallID
			if callID == "" {
				callID = it.ID
			}
			inputArgs, _ := json.Marshal(map[string]string{"input": it.Input})
			turnMessages = append(turnMessages, geminicode.TurnMessage{
				Role: "assistant",
				ToolCalls: []geminicode.TurnToolCall{
					{
						ID:        callID,
						Name:      it.Name,
						Arguments: inputArgs,
					},
				},
			})
		case itemType == "function_call_output" || itemType == "custom_tool_call_output":
			callID := it.CallID
			if callID == "" {
				callID = it.ID
			}
			turnMessages = append(turnMessages, geminicode.TurnMessage{
				Role:       "tool",
				ToolCallID: callID,
				Content:    stringifyOutput(it.Output),
			})
		case itemType == "tool_search_call":
			callID := it.CallID
			if callID == "" {
				callID = it.ID
			}
			argsStr := stringifyOutput(it.Arguments)
			turnMessages = append(turnMessages, geminicode.TurnMessage{
				Role: "assistant",
				ToolCalls: []geminicode.TurnToolCall{
					{
						ID:        callID,
						Name:      "tool_search",
						Arguments: json.RawMessage(argsStr),
					},
				},
			})
		case itemType == "tool_search_output":
			callID := it.CallID
			if callID == "" {
				callID = it.ID
			}
			turnMessages = append(turnMessages, geminicode.TurnMessage{
				Role:       "tool",
				ToolCallID: callID,
				Content:    searchFoundTools(it.Tools),
			})
		}
	}

	var turnTools []geminicode.TurnTool
	seenTools := make(map[string]bool)
	var addTool func(t responseTool)
	addTool = func(t responseTool) {
		if t.Type == "namespace" || len(t.Tools) > 0 {
			for _, sub := range t.Tools {
				addTool(sub)
			}
			return
		}
		name := strings.TrimSpace(t.Name)
		if name == "" {
			// Skip tools with no name (e.g. web_search or empty stubs)
			return
		}
		if seenTools[name] {
			return
		}
		seenTools[name] = true

		switch t.Type {
		case "custom":
			customTools[name] = true
			turnTools = append(turnTools, geminicode.TurnTool{
				Name:        name,
				Description: customDescription(t),
				Parameters:  customSchema,
			})
		case "tool_search":
			turnTools = append(turnTools, geminicode.TurnTool{
				Name:        name,
				Description: t.Description,
				Parameters:  t.Parameters,
			})
		default: // "function" or empty
			turnTools = append(turnTools, geminicode.TurnTool{
				Name:        name,
				Description: t.Description,
				Parameters:  t.Parameters,
			})
		}
	}
	for _, t := range req.Tools {
		addTool(t)
	}

	temp := 0.0
	if req.Temperature != nil {
		temp = *req.Temperature
	}
	maxTokens := req.MaxOutputTokens
	if maxTokens <= 0 {
		maxTokens = 8192
	}

	thinkingLevel := "high"
	if req.Reasoning != nil && req.Reasoning.Effort != "" {
		thinkingLevel = req.Reasoning.Effort
	} else if req.ReasoningEffort != "" {
		thinkingLevel = req.ReasoningEffort
	}
	switch strings.ToLower(thinkingLevel) {
	case "low", "medium", "high", "none":
		thinkingLevel = strings.ToLower(thinkingLevel)
	default:
		thinkingLevel = "high"
	}

	turn := geminicode.Turn{
		Model:           canonicalModel,
		Project:         geminicode.DefaultProject,
		SystemPrompt:    antigravityCloak(strings.Join(systemPromptParts, "\n\n")),
		Messages:        turnMessages,
		Tools:           turnTools,
		Temperature:     temp,
		MaxOutputTokens: maxTokens,
		ThinkingLevel:   thinkingLevel,
		SessionID:       strconv.FormatInt(geminicode.NewSessionID(), 10),
		RequestID:       geminicode.NewRequestID(),
	}

	ctx := r.Context()
	resp, currentEmail, err := s.executeBridgeRequest(ctx, turn, r.Header.Get("Authorization"))
	recordAccount(w, currentEmail)
	if err != nil {
		accTag := ""
		if currentEmail != "" {
			accTag = " [" + currentEmail + "]"
		}
		s.logf("[responses]%s upstream error: %v", accTag, err)
		writeOpenAIError(w, http.StatusBadGateway, "upstream_error", formatClientErrorMessage(currentEmail, err))
		return
	}
	defer resp.Body.Close()

	respID := "resp_" + newUUIDv4()
	if req.Stream {
		s.streamResponses(w, r, resp, canonicalModel, respID, customTools, allItems, currentEmail, family)
		return
	}
	s.unaryResponses(w, resp, canonicalModel, respID, customTools, allItems, currentEmail)
}

func (s *Server) streamResponses(w http.ResponseWriter, r *http.Request, resp *http.Response, model, respID string, customTools map[string]bool, priorItems []json.RawMessage, currentEmail, family string) {
	ctx := context.Background()
	if r != nil {
		ctx = r.Context()
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	setCORSHeaders(w)
	w.WriteHeader(http.StatusOK)

	flusher, _ := w.(http.Flusher)
	now := time.Now().Unix()

	seq := 0
	sendEvent := func(typ string, data map[string]any) {
		data["type"] = typ
		data["sequence_number"] = seq
		seq++
		b, _ := json.Marshal(data)
		_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", typ, b)
		if flusher != nil {
			flusher.Flush()
		}
	}

	sendEvent("response.created", map[string]any{
		"response": map[string]any{
			"id":         respID,
			"object":     "response",
			"created_at": now,
			"status":     "in_progress",
			"model":      model,
			"output":     []any{},
		},
	})
	sendEvent("response.in_progress", map[string]any{
		"response": map[string]any{
			"id":         respID,
			"object":     "response",
			"created_at": now,
			"status":     "in_progress",
			"model":      model,
			"output":     []any{},
		},
	})

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	decoder := geminicode.NewDecoder(nil)

	linesChan := make(chan string, 32)
	go func() {
		defer close(linesChan)
		for scanner.Scan() {
			linesChan <- scanner.Text()
		}
	}()

	ticker := time.NewTicker(keepAliveInterval)
	defer ticker.Stop()

	outputIndex := -1
	inThinking := false
	thinkID := ""
	var thinkingText strings.Builder

	inMessage := false
	msgID := ""
	var messageText strings.Builder

	var outputItems []any
	var completedItems []json.RawMessage

	closeThinking := func() {
		if !inThinking {
			return
		}
		t := thinkingText.String()
		sendEvent("response.reasoning_summary_text.done", map[string]any{
			"item_id":       thinkID,
			"output_index":  outputIndex,
			"summary_index": 0,
			"text":          t,
		})
		part := map[string]any{"type": "summary_text", "text": t}
		sendEvent("response.reasoning_summary_part.done", map[string]any{
			"item_id":       thinkID,
			"output_index":  outputIndex,
			"summary_index": 0,
			"part":          part,
		})
		item := map[string]any{
			"id":      thinkID,
			"type":    "reasoning",
			"status":  "completed",
			"summary": []any{part},
		}
		sendEvent("response.output_item.done", map[string]any{
			"output_index": outputIndex,
			"item":         item,
		})
		outputItems = append(outputItems, item)
		raw, _ := json.Marshal(item)
		completedItems = append(completedItems, raw)
		inThinking = false
		thinkID = ""
		thinkingText.Reset()
	}

	closeMessage := func() {
		if !inMessage {
			return
		}
		t := messageText.String()
		sendEvent("response.output_text.done", map[string]any{
			"item_id":       msgID,
			"output_index":  outputIndex,
			"content_index": 0,
			"text":          t,
			"logprobs":      []any{},
		})
		part := map[string]any{"type": "output_text", "text": t, "annotations": []any{}, "logprobs": []any{}}
		sendEvent("response.content_part.done", map[string]any{
			"item_id":       msgID,
			"output_index":  outputIndex,
			"content_index": 0,
			"part":          part,
		})
		item := map[string]any{
			"id":      msgID,
			"type":    "message",
			"role":    "assistant",
			"status":  "completed",
			"content": []any{part},
		}
		sendEvent("response.output_item.done", map[string]any{
			"output_index": outputIndex,
			"item":         item,
		})
		outputItems = append(outputItems, item)
		raw, _ := json.Marshal(item)
		completedItems = append(completedItems, raw)
		inMessage = false
		msgID = ""
		messageText.Reset()
	}

	promptTokens, compTokens, totalTokens := 0, 0, 0

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_, _ = fmt.Fprint(w, ": keepalive\n\n")
			if flusher != nil {
				flusher.Flush()
			}
		case textLine, ok := <-linesChan:
			if !ok {
				goto StreamDone
			}
			chunks := decoder.Feed(textLine)
			for _, c := range chunks {
				if c.Error != nil {
					accTag := ""
					if currentEmail != "" {
						accTag = " [" + currentEmail + "]"
					}
					s.logf("[responses]%s in-stream error: %v", accTag, c.Error)
					if sErr, ok := c.Error.(*geminicode.StreamError); ok {
						if isAcc, isGlobal, reason := isStreamAccountError(sErr); isAcc {
							if isGlobal {
								s.router.Exclude(currentEmail)
							} else {
								s.router.ExcludeForModel(currentEmail, family)
							}
							s.logf("[responses] %s in-stream %s; excluded for future turns", currentEmail, reason)
						}
					}
					sendEvent("response.failed", map[string]any{
						"response": map[string]any{
							"id":     respID,
							"status": "failed",
							"error": map[string]any{
								"message": c.Error.Error(),
								"type":    "upstream_error",
							},
						},
					})
					continue
				}

				text := c.Text
				if text == "<think>" {
					closeMessage()
					inThinking = true
					outputIndex++
					thinkID = "rs_" + newUUIDv4()
					sendEvent("response.output_item.added", map[string]any{
						"output_index": outputIndex,
						"item": map[string]any{
							"id":      thinkID,
							"type":    "reasoning",
							"status":  "in_progress",
							"summary": []any{},
						},
					})
					sendEvent("response.reasoning_summary_part.added", map[string]any{
						"item_id":       thinkID,
						"output_index":  outputIndex,
						"summary_index": 0,
						"part":          map[string]any{"type": "summary_text", "text": ""},
					})
					continue
				}
				if text == "</think>" {
					closeThinking()
					continue
				}
				if strings.HasPrefix(text, "<think>") {
					closeMessage()
					inThinking = true
					outputIndex++
					thinkID = "rs_" + newUUIDv4()
					sendEvent("response.output_item.added", map[string]any{
						"output_index": outputIndex,
						"item": map[string]any{
							"id":      thinkID,
							"type":    "reasoning",
							"status":  "in_progress",
							"summary": []any{},
						},
					})
					sendEvent("response.reasoning_summary_part.added", map[string]any{
						"item_id":       thinkID,
						"output_index":  outputIndex,
						"summary_index": 0,
						"part":          map[string]any{"type": "summary_text", "text": ""},
					})
					text = strings.TrimPrefix(text, "<think>")
				}
				if strings.HasSuffix(text, "</think>") {
					cleanText := strings.TrimSuffix(text, "</think>")
					if cleanText != "" {
						thinkingText.WriteString(cleanText)
						sendEvent("response.reasoning_summary_text.delta", map[string]any{
							"item_id":       thinkID,
							"output_index":  outputIndex,
							"summary_index": 0,
							"delta":         cleanText,
						})
					}
					closeThinking()
					continue
				}

				if text != "" {
					if inThinking {
						thinkingText.WriteString(text)
						sendEvent("response.reasoning_summary_text.delta", map[string]any{
							"item_id":       thinkID,
							"output_index":  outputIndex,
							"summary_index": 0,
							"delta":         text,
						})
					} else {
						if !inMessage {
							inMessage = true
							outputIndex++
							msgID = "msg_" + newUUIDv4()
							sendEvent("response.output_item.added", map[string]any{
								"output_index": outputIndex,
								"item": map[string]any{
									"id":      msgID,
									"type":    "message",
									"role":    "assistant",
									"status":  "in_progress",
									"content": []any{},
								},
							})
							sendEvent("response.content_part.added", map[string]any{
								"item_id":       msgID,
								"output_index":  outputIndex,
								"content_index": 0,
								"part":          map[string]any{"type": "output_text", "text": "", "annotations": []any{}, "logprobs": []any{}},
							})
						}
						messageText.WriteString(text)
						sendEvent("response.output_text.delta", map[string]any{
							"item_id":       msgID,
							"output_index":  outputIndex,
							"content_index": 0,
							"delta":         text,
							"logprobs":      []any{},
						})
					}
				}

				if len(c.ToolCalls) > 0 {
					closeThinking()
					closeMessage()
					for _, tc := range c.ToolCalls {
						outputIndex++
						if customTools[tc.Name] {
							ctcID := "ctc_" + newUUIDv4()
							rawInput := customInput(string(tc.Arguments))
							sendEvent("response.output_item.added", map[string]any{
								"output_index": outputIndex,
								"item": map[string]any{
									"id":      ctcID,
									"type":    "custom_tool_call",
									"status":  "in_progress",
									"call_id": tc.ID,
									"name":    tc.Name,
									"input":   "",
								},
							})
							sendEvent("response.custom_tool_call_input.delta", map[string]any{
								"item_id":      ctcID,
								"output_index": outputIndex,
								"delta":        rawInput,
							})
							sendEvent("response.custom_tool_call_input.done", map[string]any{
								"item_id":      ctcID,
								"output_index": outputIndex,
								"input":        rawInput,
							})
							item := map[string]any{
								"id":      ctcID,
								"type":    "custom_tool_call",
								"status":  "completed",
								"call_id": tc.ID,
								"name":    tc.Name,
								"input":   rawInput,
							}
							sendEvent("response.output_item.done", map[string]any{
								"output_index": outputIndex,
								"item":         item,
							})
							outputItems = append(outputItems, item)
							raw, _ := json.Marshal(item)
							completedItems = append(completedItems, raw)
						} else {
							fcID := "fc_" + newUUIDv4()
							args := string(tc.Arguments)
							sendEvent("response.output_item.added", map[string]any{
								"output_index": outputIndex,
								"item": map[string]any{
									"id":        fcID,
									"type":      "function_call",
									"status":    "in_progress",
									"call_id":   tc.ID,
									"name":      tc.Name,
									"arguments": "",
								},
							})
							sendEvent("response.function_call_arguments.delta", map[string]any{
								"item_id":      fcID,
								"output_index": outputIndex,
								"call_id":      tc.ID,
								"delta":        args,
							})
							sendEvent("response.function_call_arguments.done", map[string]any{
								"item_id":      fcID,
								"output_index": outputIndex,
								"call_id":      tc.ID,
								"arguments":    args,
							})
							item := map[string]any{
								"id":        fcID,
								"type":      "function_call",
								"status":    "completed",
								"call_id":   tc.ID,
								"name":      tc.Name,
								"arguments": args,
							}
							sendEvent("response.output_item.done", map[string]any{
								"output_index": outputIndex,
								"item":         item,
							})
							outputItems = append(outputItems, item)
							raw, _ := json.Marshal(item)
							completedItems = append(completedItems, raw)
						}
					}
				}

				if c.Meta != nil {
					promptTokens = c.Meta.PromptTokens
					compTokens = c.Meta.CompletionTokens
					totalTokens = c.Meta.TotalTokens
				}
			}
		}
	}

StreamDone:
	closeThinking()
	closeMessage()

	finalResp := map[string]any{
		"id":         respID,
		"object":     "response",
		"created_at": now,
		"status":     "completed",
		"model":      model,
		"output":     outputItems,
		"usage": map[string]any{
			"input_tokens":  promptTokens,
			"output_tokens": compTokens,
			"total_tokens":  totalTokens,
		},
	}
	sendEvent("response.completed", map[string]any{
		"response": finalResp,
	})

	// Cache conversation for subsequent turns referencing previous_response_id
	var allCached []json.RawMessage
	allCached = append(allCached, priorItems...)
	allCached = append(allCached, completedItems...)
	globalPrevStore.put(respID, allCached)
}

func (s *Server) unaryResponses(w http.ResponseWriter, resp *http.Response, model, respID string, customTools map[string]bool, priorItems []json.RawMessage, currentEmail string) {
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	decoder := geminicode.NewDecoder(nil)

	now := time.Now().Unix()
	var outputItems []any
	var completedItems []json.RawMessage

	var thinkingText strings.Builder
	var messageText strings.Builder
	var lastStreamErr error
	inThinking := false
	promptTokens, compTokens, totalTokens := 0, 0, 0

	for scanner.Scan() {
		chunks := decoder.Feed(scanner.Text())
		for _, c := range chunks {
			if c.Error != nil {
				lastStreamErr = c.Error
				continue
			}
			text := c.Text
			if text == "<think>" {
				inThinking = true
				continue
			}
			if text == "</think>" {
				inThinking = false
				continue
			}
			if strings.HasPrefix(text, "<think>") {
				inThinking = true
				text = strings.TrimPrefix(text, "<think>")
			}
			if strings.HasSuffix(text, "</think>") {
				cleanText := strings.TrimSuffix(text, "</think>")
				thinkingText.WriteString(cleanText)
				inThinking = false
				continue
			}
			if text != "" {
				if inThinking {
					thinkingText.WriteString(text)
				} else {
					messageText.WriteString(text)
				}
			}
			if len(c.ToolCalls) > 0 {
				for _, tc := range c.ToolCalls {
					if customTools[tc.Name] {
						rawInput := customInput(string(tc.Arguments))
						item := map[string]any{
							"id":      "ctc_" + newUUIDv4(),
							"type":    "custom_tool_call",
							"status":  "completed",
							"call_id": tc.ID,
							"name":    tc.Name,
							"input":   rawInput,
						}
						outputItems = append(outputItems, item)
						raw, _ := json.Marshal(item)
						completedItems = append(completedItems, raw)
					} else {
						item := map[string]any{
							"id":        "fc_" + newUUIDv4(),
							"type":      "function_call",
							"status":    "completed",
							"call_id":   tc.ID,
							"name":      tc.Name,
							"arguments": string(tc.Arguments),
						}
						outputItems = append(outputItems, item)
						raw, _ := json.Marshal(item)
						completedItems = append(completedItems, raw)
					}
				}
			}
			if c.Meta != nil {
				promptTokens = c.Meta.PromptTokens
				compTokens = c.Meta.CompletionTokens
				totalTokens = c.Meta.TotalTokens
			}
		}
	}

	if lastStreamErr != nil && len(outputItems) == 0 && messageText.Len() == 0 {
		accTag := ""
		if currentEmail != "" {
			accTag = " [" + currentEmail + "]"
		}
		s.logf("[responses]%s upstream stream error: %v", accTag, lastStreamErr)
		writeOpenAIError(w, http.StatusBadGateway, "upstream_error", formatClientErrorMessage(currentEmail, lastStreamErr))
		return
	}

	if thinkingText.Len() > 0 {
		part := map[string]any{"type": "summary_text", "text": thinkingText.String()}
		item := map[string]any{
			"id":      "rs_" + newUUIDv4(),
			"type":    "reasoning",
			"status":  "completed",
			"summary": []any{part},
		}
		outputItems = append([]any{item}, outputItems...)
		raw, _ := json.Marshal(item)
		completedItems = append([]json.RawMessage{raw}, completedItems...)
	}

	if messageText.Len() > 0 {
		part := map[string]any{"type": "output_text", "text": messageText.String(), "annotations": []any{}, "logprobs": []any{}}
		item := map[string]any{
			"id":      "msg_" + newUUIDv4(),
			"type":    "message",
			"role":    "assistant",
			"status":  "completed",
			"content": []any{part},
		}
		outputItems = append(outputItems, item)
		raw, _ := json.Marshal(item)
		completedItems = append(completedItems, raw)
	}

	finalResp := map[string]any{
		"id":         respID,
		"object":     "response",
		"created_at": now,
		"status":     "completed",
		"model":      model,
		"output":     outputItems,
		"usage": map[string]any{
			"input_tokens":  promptTokens,
			"output_tokens": compTokens,
			"total_tokens":  totalTokens,
		},
	}

	var allCached []json.RawMessage
	allCached = append(allCached, priorItems...)
	allCached = append(allCached, completedItems...)
	globalPrevStore.put(respID, allCached)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(finalResp)
}
