package geminicode

import (
	"encoding/json"
	"fmt"
	"strings"
)

// StreamError represents an in-stream error frame from Google CloudCode or Gemini endpoints.
type StreamError struct {
	Code    int    `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
	Status  string `json:"status,omitempty"`
}

func (e *StreamError) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Message != "" {
		if e.Code != 0 {
			return fmt.Sprintf("geminicode: stream error %d: %s", e.Code, e.Message)
		}
		return fmt.Sprintf("geminicode: stream error: %s", e.Message)
	}
	if e.Status != "" {
		return fmt.Sprintf("geminicode: stream error: %s", e.Status)
	}
	return "geminicode: stream error"
}

// ParseFrameError inspects an SSE data payload for in-stream error messages
// sent by Google CloudCode or Gemini endpoints. Returns non-nil when an error
// is found.
func ParseFrameError(payload string) *StreamError {
	payload = strings.TrimSpace(payload)
	if payload == "" || payload == "[DONE]" {
		return nil
	}
	var f struct {
		Code     int          `json:"code"`
		Message  string       `json:"message"`
		Status   string       `json:"status"`
		Error    any          `json:"error"`
		Response *struct {
			Error any `json:"error"`
		} `json:"response"`
	}
	if err := json.Unmarshal([]byte(payload), &f); err == nil {
		if sErr := extractStreamError(f.Error); sErr != nil {
			return sErr
		}
		if f.Response != nil {
			if sErr := extractStreamError(f.Response.Error); sErr != nil {
				return sErr
			}
		}
		if f.Code != 0 || f.Status != "" {
			return &StreamError{Code: f.Code, Message: f.Message, Status: f.Status}
		}
	}
	return nil
}

func extractStreamError(raw any) *StreamError {
	if raw == nil {
		return nil
	}
	switch v := raw.(type) {
	case string:
		if s := strings.TrimSpace(v); s != "" {
			return &StreamError{Message: s}
		}
	case map[string]any:
		var se StreamError
		if c, ok := v["code"].(float64); ok {
			se.Code = int(c)
		}
		if m, ok := v["message"].(string); ok {
			se.Message = m
		}
		if s, ok := v["status"].(string); ok {
			se.Status = s
		}
		if se.Code != 0 || se.Message != "" || se.Status != "" {
			return &se
		}
	}
	return nil
}

// frame is the wrapper every CloudCode SSE frame carries: the response
// under `response`, the trace id beside it.
type frame struct {
	Response json.RawMessage `json:"response"`
}

// ParseFrame decodes one SSE payload.
//
// It accepts both the wrapped frame the CloudCode endpoint sends
//
//	data: {"response":{"candidates":[…]},"traceId":"…"}
//
// and a bare inner response (some intermediaries strip the wrapper):
//
//	data: {"candidates":[…]}
//
// The second return reports whether the payload was a frame at all — a
// keep-alive, an error envelope or a stray line is not one.
func ParseFrame(payload string) (GenerateContentResponse, bool) {
	var wrapped frame
	if err := json.Unmarshal([]byte(payload), &wrapped); err == nil && len(wrapped.Response) > 0 {
		var out GenerateContentResponse
		if err := json.Unmarshal(wrapped.Response, &out); err == nil {
			return out, true
		}
	}
	var out GenerateContentResponse
	if err := json.Unmarshal([]byte(payload), &out); err == nil {
		if len(out.Candidates) > 0 {
			return out, true
		}
		if out.UsageMetadata != nil && out.UsageMetadata.TotalTokenCount > 0 {
			return out, true
		}
	}
	return GenerateContentResponse{}, false
}
