package geminicode

import "encoding/json"

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
