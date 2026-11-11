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
	// Fast path: error frames always carry an "error", "status", or "code" key.
	// Normal candidate stream frames do not, so skip JSON unmarshaling entirely.
	if !strings.Contains(payload, "\"error\"") && !strings.Contains(payload, "\"status\"") && !strings.Contains(payload, "\"code\"") {
		return nil
	}
	var f struct {
		Code     int    `json:"code"`
		Message  string `json:"message"`
		Status   string `json:"status"`
		Error    any    `json:"error"`
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

// dualFrame decodes either a CloudCode-wrapped frame (response member)
// or a bare Gemini GenerateContentResponse in a single JSON unmarshal pass.
type dualFrame struct {
	Response      *GenerateContentResponse `json:"response"`
	Candidates    []Candidate              `json:"candidates"`
	UsageMetadata *UsageMetadata           `json:"usageMetadata,omitempty"`
	ModelVersion  string                   `json:"modelVersion,omitempty"`
	ResponseID    string                   `json:"responseId,omitempty"`
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
	var d dualFrame
	if err := json.Unmarshal([]byte(payload), &d); err == nil {
		if d.Response != nil {
			return *d.Response, true
		}
		if len(d.Candidates) > 0 || (d.UsageMetadata != nil && d.UsageMetadata.TotalTokenCount > 0) {
			return GenerateContentResponse{
				Candidates:    d.Candidates,
				UsageMetadata: d.UsageMetadata,
				ModelVersion:  d.ModelVersion,
				ResponseID:    d.ResponseID,
			}, true
		}
	}
	return GenerateContentResponse{}, false
}
