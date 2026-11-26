package agentturn

import (
	"context"

	"github.com/justwasm/pigasus/actor"
	"github.com/justwasm/pigasus/agentlink"
	"github.com/justwasm/pigasus/internal/core/backend"
)

func runMediaFrame(ctx context.Context, runner interface {
	RunWithMedia(context.Context, backend.ToolCall) (backend.ToolOutput, error)
}, call backend.ToolCall, emit func(agentlink.Frame) bool, steps *[]agentlink.Step) (actor.ToolResult, error) {
	emit(agentlink.Frame{Kind: agentlink.KindTool, Tool: &agentlink.Tool{Phase: agentlink.ToolStart, Name: call.Name, Args: string(call.Arguments)}})
	out, err := runner.RunWithMedia(ctx, call)
	text := out.Text
	if err != nil && text == "" {
		text = err.Error()
	}
	emit(agentlink.Frame{Kind: agentlink.KindTool, Tool: &agentlink.Tool{Phase: agentlink.ToolEnd, Name: call.Name, Args: string(call.Arguments), Result: text, Err: err != nil}})
	if steps != nil {
		*steps = append(*steps, agentlink.Step{Name: call.Name, Args: string(call.Arguments), Result: text, Err: err != nil})
	}
	if err != nil {
		return actor.ToolResult{Error: err.Error()}, err
	}
	res := actor.ToolResult{Output: out.Text}
	for _, image := range out.Images {
		res.Images = append(res.Images, actor.ImagePart{MIMEType: image.MIMEType, Data: append([]byte(nil), image.Data...)})
	}
	return res, nil
}
