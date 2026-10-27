// proto.go — the wire bridge between the typed hook model in
// lifecyclehooks.go and the generated protobuf bindings.
//
// Everything here is a mechanical translation between the two shapes:
//
//	lifecyclehooks.Request  ←  pb.CallHookRequest
//	lifecyclehooks.Response →  pb.CallHookResponse
//
// The translations are deliberately one-directional: the inbound direction
// carries every field the harness sends, while the outbound direction
// carries only the fields the harness reads for that particular hook type.
// The mapping table below is the SDK's own
// (`google/antigravity/connections/local/hook_router.py`, `_handle_*`):
//
//	ON_SESSION_START → EmptyResult
//	ON_SESSION_END   → EmptyResult
//	PRE_TURN         → PreTurnResult{decision, reason}
//	POST_TURN        → EmptyResult
//	PRE_TOOL         → PreToolResult{decision, reason, modified_args}
//	POST_TOOL        → EmptyResult
//	ON_TOOL_ERROR    → OnToolErrorResult{custom_error_message} | EmptyResult
//	ON_COMPACTION    → EmptyResult
//	STOP             → StopResult{decision, reason}
//
// A failed hook produces the wire's `error_message` field instead of any
// result payload — again matching the SDK, whose `_handle` wrapper catches
// every exception and sets `resp.error_message = f"Hook failed: {e!r}"`.
//
// # A note on the generated API level
//
// The bindings were generated straight from the wheel's descriptor with
// protoc-gen-go v1.36.11. The `antigravity.localharness` and
// `genai.skills` protos are edition-2023 (proto3) files, for which
// protoc-gen-go emits the *opaque* API: fields are reached through setters
// and oneof members through `SetX`. The `genai` protos are edition-2022
// (proto2) files and keep the open API. Both are used below; the mixing is
// deliberate — the descriptor is reproduced as-is rather than re-authored,
// so the wire contract cannot drift from the wheel's.
package lifecyclehooks

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/justwasm/pigasus/localharness/pb/genai"
	pb "github.com/justwasm/pigasus/localharness/pb/localharness"
)

// TypeFromProto converts the wire enum to a hook type. The wire enum and
// the local Type share their numbering, so this is a checked cast.
func TypeFromProto(h pb.LifecycleHook) Type {
	t := Type(h)
	if !t.Valid() {
		return TypeUnspecified
	}
	return t
}

// RequestFromProto converts an inbound CallHookRequest into the typed
// Request. A nil request yields the zero Request.
//
// The payload pointer that matches the request's type is populated; the
// others stay nil. A request whose oneof payload does not match its type
// (a malformed or newer harness) yields a Request with no payload set — the
// handler must cope, and the manager never panics.
func RequestFromProto(req *pb.CallHookRequest) Request {
	if req == nil {
		return Request{}
	}
	out := Request{
		Type:      TypeFromProto(req.GetType()),
		Name:      req.GetName(),
		RequestID: req.GetRequestId(),
		ClientID:  req.GetClientId(),
	}
	switch out.Type {
	case TypePreTurn:
		if a := req.GetPreTurnArgs(); a != nil {
			out.PreTurn = &PreTurnArgs{
				UserInput:    RenderUserInput(a.GetUserInput()),
				TrajectoryID: a.GetTrajectoryId(),
			}
		}
	case TypePostTurn:
		if a := req.GetPostTurnArgs(); a != nil {
			out.PostTurn = &PostTurnArgs{
				ResponseText: a.GetResponseText(),
				TrajectoryID: a.GetTrajectoryId(),
			}
		}
	case TypePreTool:
		if a := req.GetPreToolArgs(); a != nil {
			out.PreTool = &PreToolArgs{
				ToolName:      a.GetToolName(),
				ArgumentsJSON: a.GetArgumentsJson(),
				ServerName:    a.GetServerName(),
				CallID:        a.GetCallId(),
				TrajectoryID:  a.GetTrajectoryId(),
				StepIndex:     int(a.GetStepIndex()),
			}
		}
	case TypePostTool:
		if a := req.GetPostToolArgs(); a != nil {
			out.PostTool = &PostToolArgs{
				ToolName:     a.GetToolName(),
				Result:       a.GetResult(),
				Error:        a.GetError(),
				ServerName:   a.GetServerName(),
				CallID:       a.GetCallId(),
				TrajectoryID: a.GetTrajectoryId(),
				StepIndex:    int(a.GetStepIndex()),
			}
		}
	case TypeOnToolError:
		if a := req.GetOnToolErrorArgs(); a != nil {
			out.OnToolError = &OnToolErrorArgs{
				ToolName:     a.GetToolName(),
				ErrorMessage: a.GetErrorMessage(),
				ServerName:   a.GetServerName(),
				CallID:       a.GetCallId(),
				TrajectoryID: a.GetTrajectoryId(),
				StepIndex:    int(a.GetStepIndex()),
			}
		}
	case TypeOnCompaction:
		if a := req.GetOnCompactionArgs(); a != nil {
			out.OnCompaction = &OnCompactionArgs{
				TrajectoryID: a.GetTrajectoryId(),
				StepIndex:    int(a.GetStepIndex()),
				Summary:      a.GetSummary(),
			}
		}
	case TypeStop:
		if a := req.GetStopArgs(); a != nil {
			out.Stop = &StopArgs{
				ResponseText:      a.GetResponseText(),
				TrajectoryID:      a.GetTrajectoryId(),
				ContinuationCount: int(a.GetContinuationCount()),
				StopReason:        a.GetStopReason().String(),
				ErrorMessage:      a.GetErrorMessage(),
			}
		}
	}
	return out
}

// BuildResponse renders a dispatch result for a given hook type. It is the
// single place where the local Decision type is narrowed back to the
// per-type wire enums.
//
// Narrowing rules, from the wire enums:
//
//	PreTurnResult.Decision  — ALLOW / DENY
//	PreToolResult.Decision  — ALLOW / DENY
//	StopResult.Decision     — ALLOW_STOP / CONTINUE
//
// A decision the wire cannot express is mapped to the blocking value for
// the deciding types (DENY) and to ALLOW_STOP for STOP, because a decision
// the harness cannot interpret blocks execution on its side
// ("Unrecognized pre-tool hook decision %q; blocked execution for safety.").
//
// When Response.Err is set the response carries `error_message` and no
// result payload, which is what the SDK does for a hook that raised.
func BuildResponse(t Type, resp Response) *pb.CallHookResponse {
	out := &pb.CallHookResponse{}
	out.SetRequestId(resp.RequestID)
	if resp.Err != nil {
		out.SetErrorMessage(fmt.Sprintf("Hook failed: %v", resp.Err))
		return out
	}

	switch t {
	case TypePreTurn:
		decision := pb.PreTurnResult_ALLOW
		if resp.Decision != DecisionAllow {
			decision = pb.PreTurnResult_DENY
		}
		result := &pb.PreTurnResult{}
		result.SetDecision(decision)
		result.SetReason(resp.Reason)
		out.SetPreTurnResult(result)
	case TypePreTool:
		decision := pb.PreToolResult_ALLOW
		if resp.Decision != DecisionAllow {
			decision = pb.PreToolResult_DENY
		}
		result := &pb.PreToolResult{}
		result.SetDecision(decision)
		result.SetReason(resp.Reason)
		if len(resp.ModifiedArgs) > 0 {
			result.SetModifiedArgs(MapToStruct(resp.ModifiedArgs))
		}
		out.SetPreToolResult(result)
	case TypeOnToolError:
		if resp.CustomErrorMessage != "" {
			result := &pb.OnToolErrorResult{}
			result.SetCustomErrorMessage(resp.CustomErrorMessage)
			out.SetOnToolErrorResult(result)
		} else {
			out.SetEmptyResult(&pb.EmptyResult{})
		}
	case TypeStop:
		decision := pb.StopResult_ALLOW_STOP
		if resp.Decision == DecisionContinue {
			decision = pb.StopResult_CONTINUE
		}
		result := &pb.StopResult{}
		result.SetDecision(decision)
		result.SetReason(resp.Reason)
		out.SetStopResult(result)
	default:
		out.SetEmptyResult(&pb.EmptyResult{})
	}
	return out
}

// RenderUserInput flattens the wire's UserInput message into the text a
// hook sees.
//
// The wire carries a list of parts, each of which is text, media or a slash
// command. The SDK converts the whole message into a `types.Content`
// (multi-modal); this package is text-oriented, so it concatenates the text
// parts and renders the non-text parts as a bracketed placeholder. That
// keeps a media-only prompt visible to a hook instead of silently empty.
func RenderUserInput(in *pb.UserInput) string {
	if in == nil {
		return ""
	}
	var b strings.Builder
	for _, part := range in.GetParts() {
		if part == nil {
			continue
		}
		switch {
		case part.GetText() != "":
			if b.Len() > 0 {
				b.WriteByte('\n')
			}
			b.WriteString(part.GetText())
		case part.GetMedia() != nil:
			if b.Len() > 0 {
				b.WriteByte('\n')
			}
			b.WriteString("[media: " + part.GetMedia().GetMimeType() + "]")
		case part.GetSlashCommand() != nil:
			if b.Len() > 0 {
				b.WriteByte('\n')
			}
			b.WriteString("[/" + part.GetSlashCommand().GetName() + "]")
		}
	}
	return b.String()
}

// MapToStruct converts a Go map into the wire's `genai.Struct`.
//
// The wire's Struct is a list of named fields (not a JSON object), and its
// Value is a oneof over null / number / string / bool / struct / list. The
// two content / function kinds have no Go-map equivalent and are dropped.
func MapToStruct(m map[string]any) *genai.Struct {
	if m == nil {
		return nil
	}
	out := &genai.Struct{}
	for _, key := range sortedKeys(m) {
		name := key
		out.Fields = append(out.Fields, &genai.Field{
			Name:  &name,
			Value: valueToProto(m[key]),
		})
	}
	return out
}

// StructToMap converts the wire's `genai.Struct` into a Go map. It is the
// inverse of MapToStruct for the value kinds a Go map can carry.
func StructToMap(s *genai.Struct) map[string]any {
	if s == nil {
		return nil
	}
	out := make(map[string]any, len(s.GetFields()))
	for _, f := range s.GetFields() {
		if f == nil {
			continue
		}
		out[f.GetName()] = valueFromProto(f.GetValue())
	}
	return out
}

// valueToProto maps a Go value onto the wire's Value oneof.
func valueToProto(v any) *genai.Value {
	switch t := v.(type) {
	case nil:
		return &genai.Value{Kind: &genai.Value_NullValue{}}
	case bool:
		return &genai.Value{Kind: &genai.Value_BoolValue{BoolValue: t}}
	case string:
		return &genai.Value{Kind: &genai.Value_StringValue{StringValue: t}}
	case float64:
		return &genai.Value{Kind: &genai.Value_NumberValue{NumberValue: t}}
	case float32:
		return &genai.Value{Kind: &genai.Value_NumberValue{NumberValue: float64(t)}}
	case int:
		return &genai.Value{Kind: &genai.Value_NumberValue{NumberValue: float64(t)}}
	case int32:
		return &genai.Value{Kind: &genai.Value_NumberValue{NumberValue: float64(t)}}
	case int64:
		return &genai.Value{Kind: &genai.Value_NumberValue{NumberValue: float64(t)}}
	case uint32:
		return &genai.Value{Kind: &genai.Value_NumberValue{NumberValue: float64(t)}}
	case uint64:
		return &genai.Value{Kind: &genai.Value_NumberValue{NumberValue: float64(t)}}
	case map[string]any:
		return &genai.Value{Kind: &genai.Value_StructValue{StructValue: MapToStruct(t)}}
	case []any:
		list := &genai.ListValue{}
		for _, item := range t {
			list.Values = append(list.Values, valueToProto(item))
		}
		return &genai.Value{Kind: &genai.Value_ListValue{ListValue: list}}
	}
	// Unknown kind: encode as its Go rendering so nothing is lost silently.
	return &genai.Value{Kind: &genai.Value_StringValue{StringValue: fmt.Sprint(v)}}
}

// valueFromProto maps the wire's Value oneof back onto a Go value.
func valueFromProto(v *genai.Value) any {
	if v == nil {
		return nil
	}
	switch k := v.GetKind().(type) {
	case *genai.Value_NullValue:
		return nil
	case *genai.Value_BoolValue:
		return k.BoolValue
	case *genai.Value_StringValue:
		return k.StringValue
	case *genai.Value_NumberValue:
		return k.NumberValue
	case *genai.Value_StructValue:
		return StructToMap(k.StructValue)
	case *genai.Value_ListValue:
		out := make([]any, 0, len(k.ListValue.GetValues()))
		for _, item := range k.ListValue.GetValues() {
			out = append(out, valueFromProto(item))
		}
		return out
	}
	return nil
}

// sortedKeys returns the map's keys in a deterministic order so the wire
// encoding of a Struct is stable.
func sortedKeys(m map[string]any) []string {
	return slices.Sorted(maps.Keys(m))
}

// FormatStepID renders the "trajectory/step" identifier the SDK builds for
// a tool call. The SDK's `make_step_id(trajectory_id, step_index)` is what
// ties a hook's tool call to the step the harness later reports.
func FormatStepID(trajectoryID string, stepIndex int) string {
	if trajectoryID == "" {
		return strconv.Itoa(stepIndex)
	}
	return trajectoryID + "/" + strconv.Itoa(stepIndex)
}
