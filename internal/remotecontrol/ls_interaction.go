// Package remotecontrol — ls_interaction.go: the Web UI's transport
// for the interactive tools.
//
// The reference's agent blocks a tool call that needs the user until
// the user answers, and the Web UI renders the pending question as a
// card on the conversation.
//
// 【一手】 2026-10-26, from the IDE web bundle (internal/rcfrontend/
// assets/main.js):
//
//   - the card's data source is the agent state's trajectory slice
//     (`k?.trajectorySlice?.stepsInSlice`), filtered to the steps
//     whose status is WAITING (`v8b`);
//   - the question is the WAITING step whose step oneof is
//     "askQuestion" (`x8b`), and the questions come from
//     `step.value.questions`;
//   - the permission card reads the same slice, but the permission
//     comes off the step's `requestedInteraction` (`w8b`), and the
//     tool's name off the step's metadata (`metadata.toolCall.name`);
//   - the answer carries the trajectory id, the step index and the
//     responses (`QPb`), and the frontend sends it as
//     `HandleCascadeUserInteraction`.
//
// So the round trip has three parts: the tool asks (the host), the
// host publishes the waiting step (the card appears), and the answer
// RPC resolves the pending interaction (the turn continues).
package remotecontrol

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/justwasm/pigasus/internal/interaction"
)

// lsInteractionHost is the Web UI's transport for the interactive
// tools.
//
// One host serves one turn: the trajectory belongs to a conversation
// and the pending-interaction registry belongs to a cascade.
type lsInteractionHost struct {
	cascade *Cascade
	traj    *lsTrajectory
	sink    EventSink
}

// Ask publishes the waiting step and blocks until the user answered.
//
// The step goes onto the trajectory first, so the card appears before
// the tool is parked; the pending interaction is registered next, so
// the answer that comes back finds the entry to resolve.
func (h *lsInteractionHost) Ask(ctx context.Context, req interaction.Request) (interaction.Answer, error) {
	if h == nil || h.traj == nil {
		return interaction.Answer{}, errors.New("remotecontrol: the interaction has no trajectory")
	}
	step := lsInteractionStep(req)
	index := h.traj.Append(step)
	intr, err := h.register(req, index)
	if err != nil {
		return interaction.Answer{}, err
	}
	ans, cue, err := h.wait(ctx, intr, req)
	if err != nil {
		return interaction.Answer{}, err
	}
	h.complete(index, req, cue, ans)
	return ans, nil
}

// register mints the pending interaction the answer RPC resolves.
//
// The registry is the cascade's: the answer names the trajectory and
// the step, and the step index is what the pending entry carries.
func (h *lsInteractionHost) register(req interaction.Request, index int) (*PendingInteraction, error) {
	kind, title, prompt, opts := lsInteractionPrompt(req)
	intr, err := h.cascade.AddInteraction(kind, title, prompt, opts, false, time.Time{}, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	intr.StepIndex = index
	intr.TrajectoryID = h.traj.Snapshot().TrajectoryID
	// The legacy dashboard listens for the notification; the Web UI
	// reads the step. Both get the request.
	if h.sink != nil {
		h.sink.OnUserInteraction(h.cascade, intr)
	}
	return intr, nil
}

// wait blocks until the answer arrives, the interaction expires, or
// the turn is cancelled.
//
// It returns the decoded answer and the answer message it came from:
// the message is what the completed step carries.
//
// An expired interaction is a cancelled one: the channel is closed
// rather than written to, and the user never answered.
func (h *lsInteractionHost) wait(ctx context.Context, intr *PendingInteraction, req interaction.Request) (interaction.Answer, lsCascadeUserInteraction, error) {
	select {
	case res, ok := <-intr.ResultCh:
		if !ok {
			return interaction.Answer{Cancelled: true}, lsCascadeUserInteraction{}, nil
		}
		return lsAnswerFromResponse(res.Response, req)
	case <-ctx.Done():
		return interaction.Answer{}, lsCascadeUserInteraction{}, ctx.Err()
	}
}

// complete closes the waiting step: the step is done, and it carries
// the exchange — the request and the response — the reference keeps in
// `completedInteractions`.
//
// 【一手】 the bundle's answer renderer reads the previous answers off
// the completed interactions (`b[c].response?.interaction`), so the
// exchange has to survive on the step.
//
// The step is copied rather than mutated: the trajectory's steps are
// shared with the readers that serialize them, and a reader must
// never see a half-written step.
func (h *lsInteractionHost) complete(index int, req interaction.Request, cue lsCascadeUserInteraction, ans interaction.Answer) {
	snap := h.traj.Snapshot()
	if index < 0 || index >= len(snap.Steps) {
		return
	}
	step := lsCopyStep(snap.Steps[index])
	step["status"] = lsStepStatusDone
	step["completedInteractions"] = []any{
		map[string]any{
			"request":  lsRequestedInteraction(req),
			"response": lsCompletedResponse(cue, snap.TrajectoryID, index),
		},
	}
	h.traj.Update(index, step)
}

// lsCopyStep copies a step and its metadata one level down.
//
// The nested values are shared: the copy only has to be safe against
// the fields the clone replaces.
func lsCopyStep(step map[string]any) map[string]any {
	out := make(map[string]any, len(step))
	for k, v := range step {
		out[k] = v
	}
	if meta, ok := step["metadata"].(map[string]any); ok {
		metaCopy := make(map[string]any, len(meta))
		for k, v := range meta {
			metaCopy[k] = v
		}
		out["metadata"] = metaCopy
	}
	return out
}

// lsInteractionStep builds the waiting step for one interaction.
//
// A question is a step of its own type (CORTEX_STEP_TYPE_ASK_QUESTION)
// carrying the questions; a permission is a generic tool call whose
// request rides in `requestedInteraction`.
func lsInteractionStep(req interaction.Request) map[string]any {
	if req.Kind == interaction.KindPermission {
		return lsPermissionStep(req)
	}
	return lsAskQuestionStep(req)
}

// lsAskQuestionStep builds the step the Web UI's questionnaire card
// reads.
//
// 【一手】 the card finds the question on the WAITING step whose step
// oneof is "askQuestion" and reads `step.value.questions`
// (x8b / u8b in the bundle).
func lsAskQuestionStep(req interaction.Request) map[string]any {
	return map[string]any{
		"type":   lsStepTypeAskQuestion,
		"status": lsStepStatusWaiting,
		"askQuestion": map[string]any{
			"questions": lsAskQuestionEntries(req.Questions),
		},
		"requestedInteraction": map[string]any{
			"askQuestion": map[string]any{
				"questions": lsAskQuestionEntries(req.Questions),
			},
		},
		"metadata": lsInteractionMetadata(req),
	}
}

// lsAskQuestionEntries renders the questions as the proto-JSON form of
// exa.cortex_pb.AskQuestionEntry.
//
// The clone's ask_question carries the options as plain strings; the
// IDE's own shape gives every option an id, and the answer names the
// picked options by id. The clone numbers them.
//
// 【一手】 the bundle's option row keys on `option.id` and the answer
// renderer filters the options by `selectedOptionIds.includes(id)`.
func lsAskQuestionEntries(questions []interaction.Question) []any {
	out := make([]any, 0, len(questions))
	for _, q := range questions {
		entry := map[string]any{"question": q.Prompt}
		if len(q.Options) > 0 {
			opts := make([]any, 0, len(q.Options))
			for i, text := range q.Options {
				opts = append(opts, map[string]any{
					"id":   strconv.Itoa(i + 1),
					"text": text,
				})
			}
			entry["options"] = opts
		}
		out = append(out, entry)
	}
	return out
}

// lsPermissionStep builds the waiting step for a permission request.
//
// 【一手】 the permission card reads the request off the step's
// `requestedInteraction` (`requestedInteraction.interaction.case ===
// "permission"`) and the tool's name off the step's metadata
// (`metadata.toolCall.name`).
func lsPermissionStep(req interaction.Request) map[string]any {
	spec := map[string]any{
		"resource": map[string]any{
			"action": req.Action,
			"target": req.Target,
		},
	}
	if req.Reason != "" {
		spec["reason"] = req.Reason
	}
	return map[string]any{
		"type":   lsStepTypeGeneric,
		"status": lsStepStatusWaiting,
		"generic": map[string]any{
			"args": map[string]any{},
		},
		"requestedInteraction": map[string]any{
			"permission": spec,
		},
		"metadata": lsInteractionMetadata(req),
	}
}

// lsInteractionMetadata builds the step's metadata.
//
// The trajectory stamps the step's own position onto the metadata
// (lsStampStepInfo); the tool call rides along so the Web UI can name
// the tool that is waiting.
func lsInteractionMetadata(req interaction.Request) map[string]any {
	return map[string]any{
		"source": lsStepSourceModel,
		"toolCall": map[string]any{
			"id":            req.ToolName,
			"name":          req.ToolName,
			"argumentsJson": req.ArgsJSON,
		},
	}
}

// lsRequestedInteraction renders the request half of a completed
// interaction: the proto-JSON form of exa.cortex_pb.RequestedInteraction.
func lsRequestedInteraction(req interaction.Request) map[string]any {
	if req.Kind == interaction.KindPermission {
		return map[string]any{
			"permission": map[string]any{
				"resource": map[string]any{
					"action": req.Action,
					"target": req.Target,
				},
			},
		}
	}
	return map[string]any{
		"askQuestion": map[string]any{
			"questions": lsAskQuestionEntries(req.Questions),
		},
	}
}

// lsCompletedResponse renders the response half of a completed
// interaction: the proto-JSON form of
// exa.cortex_pb.CascadeUserInteraction.
//
// The oneof member sits at the top level, as proto-JSON spells it, and
// the trajectory id and the step index name the step the answer
// belongs to.
func lsCompletedResponse(cue lsCascadeUserInteraction, trajectoryID string, stepIndex int) map[string]any {
	out := map[string]any{
		"trajectoryId": trajectoryID,
		"stepIndex":    stepIndex,
	}
	if cue.Permission != nil {
		block := map[string]any{}
		if cue.Permission.Allow {
			block["allow"] = true
		}
		if cue.Permission.Scope != 0 {
			block["scope"] = cue.Permission.Scope
		}
		out["permission"] = block
		return out
	}
	if cue.AskQuestion != nil {
		block := map[string]any{
			"responses": lsResponseEntries(cue.AskQuestion.Responses),
		}
		// proto-JSON leaves a default field out. 【一手】 the
		// reference's own answer payload carries no `cancelled`
		// when the user did not dismiss the card.
		if cue.AskQuestion.Cancelled {
			block["cancelled"] = true
		}
		out["askQuestion"] = block
	}
	return out
}

// lsResponseEntries renders the answered questions as the proto-JSON
// form of exa.cortex_pb.AskQuestionEntry.
//
// The answer carries the question back with the picked option ids and
// the write-in text, so the entries are already the right shape.
//
// proto-JSON leaves a default field out, and the reference's own
// answer carries none of them: an unanswered question is just the
// question and its options.
func lsResponseEntries(responses []lsAskQuestionResponse) []any {
	out := make([]any, 0, len(responses))
	for _, entry := range responses {
		answers := map[string]any{"question": entry.Question}
		if len(entry.SelectedOptionIDs) > 0 {
			answers["selectedOptionIds"] = entry.SelectedOptionIDs
		}
		if entry.WriteInResponse != "" {
			answers["writeInResponse"] = entry.WriteInResponse
		}
		if entry.Skipped {
			answers["skipped"] = true
		}
		if len(entry.Options) > 0 {
			options := make([]any, 0, len(entry.Options))
			for _, opt := range entry.Options {
				options = append(options, map[string]any{"id": opt.ID, "text": opt.Text})
			}
			answers["options"] = options
		}
		out = append(out, answers)
	}
	return out
}

// lsInteractionPrompt renders the request as the legacy
// cascade/user_interaction_required notification's fields.
//
// The notification is the legacy dashboard's surface; the Web UI reads
// the step. Both are served from the same request.
func lsInteractionPrompt(req interaction.Request) (kind, title, prompt string, opts []InteractionOption) {
	if req.Kind == interaction.KindPermission {
		title = req.ToolName
		prompt = req.Action
		if req.Target != "" {
			prompt = req.Action + " on " + req.Target
		}
		return InteractionKindPermission, title, prompt, nil
	}
	title = req.ToolName
	var texts []string
	for _, q := range req.Questions {
		texts = append(texts, q.Prompt)
		for _, opt := range q.Options {
			opts = append(opts, InteractionOption{Label: opt, Value: opt})
		}
	}
	return InteractionKindQuestion, title, joinInteractionTexts(texts), opts
}

// joinInteractionTexts joins the question texts with a newline.
func joinInteractionTexts(texts []string) string {
	out := ""
	for i, text := range texts {
		if i > 0 {
			out += "\n"
		}
		out += text
	}
	return out
}

// lsTrajectoryCarrier is the sink capability that carries the
// trajectory a turn publishes onto.
type lsTrajectoryCarrier interface {
	Trajectory() *lsTrajectory
}

// lsTrajectoryOf returns the trajectory the sink publishes onto, or
// nil when the sink does not carry one.
func lsTrajectoryOf(sink EventSink) *lsTrajectory {
	carrier, ok := sink.(lsTrajectoryCarrier)
	if !ok {
		return nil
	}
	return carrier.Trajectory()
}

// Trajectory returns the trajectory the sink publishes onto.
func (s *lsTrajectorySink) Trajectory() *lsTrajectory { return s.traj }

// Trajectory returns the trajectory the wrapped sink publishes onto.
func (s *lsThoughtSink) Trajectory() *lsTrajectory {
	return lsTrajectoryOf(s.inner)
}

// lsInstallInteractionHost puts the conversation's interaction host on
// the turn's context.
//
// The host is what the interactive tools ask: it publishes the waiting
// step the Web UI's card reads and blocks until the user answered.
//
// A turn with no trajectory — the legacy JSON-RPC surface, whose sink
// is not the trajectory sink — gets no host, and the tools keep their
// headless behaviour.
func lsInstallInteractionHost(ctx context.Context, c *Cascade, sink EventSink) context.Context {
	traj := lsTrajectoryOf(sink)
	if traj == nil {
		return ctx
	}
	return interaction.WithHost(ctx, &lsInteractionHost{cascade: c, traj: traj, sink: sink})
}
