// Package remotecontrol — ls_interaction_answer.go: decoding the
// answer the Web UI sends back.
//
// 【一手】 2026-10-26, from the IDE web bundle (main.js): the answer
// is an exa.cortex_pb.CascadeUserInteraction — the trajectory id, the
// step index, and the oneof member the user answered. On the wire
// (proto-JSON) the oneof member sits at the top level:
//
//	{"trajectoryId": "...", "stepIndex": 3,
//	 "askQuestion": {"responses": [...], "cancelled": false}}
//
// The responses carry the question back with the picked option ids and
// the write-in text, and the bundle's own renderer turns them into
// "Tea, Coffee" / "Something else (write-in)" / "Skipped" / "No answer
// provided".
//
// The option ids are the clone's own: the clone numbers a question's
// options from one, and the answer names the picked ones by that
// number. So the request — which the host kept — is the authority for
// what an id means.
package remotecontrol

import (
	"bytes"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/justwasm/pigasus/internal/interaction"
)

// lsAskQuestionResponse is one answered question: the proto-JSON form
// of exa.cortex_pb.AskQuestionEntry.
type lsAskQuestionResponse struct {
	Question          string     `json:"question"`
	Options           []lsOption `json:"options"`
	SelectedOptionIDs []string   `json:"selectedOptionIds"`
	WriteInResponse   string     `json:"writeInResponse"`
	Skipped           bool       `json:"skipped"`
}

// lsOption is one option of a question: the proto-JSON form of
// exa.cortex_pb.AskQuestionOption.
type lsOption struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

// lsPermissionAnswer is the permission verdict: the proto-JSON form of
// exa.cortex_pb.PermissionInteraction.
type lsPermissionAnswer struct {
	Allow bool `json:"allow"`
	Scope int  `json:"scope"`
}

// lsCascadeUserInteraction is the IDE's answer message: the proto-JSON
// form of exa.cortex_pb.CascadeUserInteraction.
//
// The oneof member is at the top level, as proto-JSON spells it. The
// Interaction field is the shape the bundle builds in memory (the
// oneof wrapped under its own name), and it is accepted as well.
type lsCascadeUserInteraction struct {
	TrajectoryID string              `json:"trajectoryId"`
	StepIndex    int                 `json:"stepIndex"`
	AskQuestion  *lsAskQuestionBlock `json:"askQuestion"`
	Permission   *lsPermissionAnswer `json:"permission"`
	Interaction  *struct {
		AskQuestion *lsAskQuestionBlock `json:"askQuestion"`
		Permission  *lsPermissionAnswer `json:"permission"`
	} `json:"interaction"`
}

// lsAskQuestionBlock is the answered questions: the proto-JSON form of
// exa.cortex_pb.AskQuestionInteraction.
type lsAskQuestionBlock struct {
	Responses []lsAskQuestionResponse `json:"responses"`
	Cancelled bool                    `json:"cancelled"`
}

// lsAnswerFromResponse turns the answer the RPC carried into the
// tool-facing answer.
//
// Three shapes are accepted:
//
//   - the IDE's CascadeUserInteraction (canonical proto-JSON),
//   - the same message with the oneof wrapped in `interaction` — the
//     shape the bundle builds in memory,
//   - a bare JSON string, which is what the legacy dashboard's answer
//     payload is.
//
// The answer message comes back with the decoded answer, because the
// completed step carries it.
func lsAnswerFromResponse(payload json.RawMessage, req interaction.Request) (interaction.Answer, lsCascadeUserInteraction, error) {
	trimmed := bytes.TrimSpace(payload)
	if len(trimmed) == 0 {
		return interaction.Answer{}, lsCascadeUserInteraction{}, errors.New("remotecontrol: the interaction answer is empty")
	}
	if trimmed[0] == '"' {
		var text string
		if err := json.Unmarshal(trimmed, &text); err == nil {
			return interaction.Answer{Text: text}, lsCascadeUserInteraction{}, nil
		}
	}
	cue, err := lsDecodeCascadeUserInteraction(trimmed)
	if err != nil {
		return interaction.Answer{}, cue, err
	}
	if !cue.hasInteraction() {
		return interaction.Answer{Text: string(trimmed)}, cue, nil
	}
	return cue.answer(req), cue, nil
}

// lsDecodeCascadeUserInteraction decodes the answer and folds the
// wrapped oneof into the top level when the message carries it there.
func lsDecodeCascadeUserInteraction(payload []byte) (lsCascadeUserInteraction, error) {
	var cue lsCascadeUserInteraction
	if err := json.Unmarshal(payload, &cue); err != nil {
		return cue, err
	}
	if !cue.hasInteraction() && cue.Interaction != nil {
		cue.AskQuestion = cue.Interaction.AskQuestion
		cue.Permission = cue.Interaction.Permission
	}
	return cue, nil
}

// hasInteraction reports whether the message carries an answer at all.
func (c lsCascadeUserInteraction) hasInteraction() bool {
	return c.AskQuestion != nil || c.Permission != nil
}

// answer turns the decoded message into the tool-facing answer.
func (c lsCascadeUserInteraction) answer(req interaction.Request) interaction.Answer {
	if c.Permission != nil {
		return interaction.Answer{Allowed: c.Permission.Allow}
	}
	if c.AskQuestion == nil {
		return interaction.Answer{}
	}
	if c.AskQuestion.Cancelled {
		return interaction.Answer{Cancelled: true}
	}
	return interaction.Answer{Text: lsQuestionAnswerText(c.AskQuestion.Responses, req.Questions)}
}

// lsQuestionAnswerText renders the answers of every question.
//
// The questions are joined with a newline, the way the clone's own
// subagent-question path joins them.
func lsQuestionAnswerText(responses []lsAskQuestionResponse, questions []interaction.Question) string {
	parts := make([]string, 0, len(responses))
	for i, entry := range responses {
		var question interaction.Question
		if i < len(questions) {
			question = questions[i]
		}
		parts = append(parts, lsResponseText(entry, question))
	}
	return strings.Join(parts, "\n")
}

// lsResponseText renders one answer.
//
// 【一手】 the bundle's own answer renderer (uGb) renders one entry as
// "Skipped" when it was skipped, as the picked options' texts joined
// with ", " plus "<write-in> (write-in)", and as "No answer provided"
// when nothing was picked.
//
// The picked options are resolved through the request's own options:
// the clone numbered them, so the id is the option's number.
func lsResponseText(entry lsAskQuestionResponse, question interaction.Question) string {
	if entry.Skipped {
		return "Skipped"
	}
	parts := make([]string, 0, len(question.Options))
	for i, text := range question.Options {
		if lsContainsID(entry.SelectedOptionIDs, strconv.Itoa(i+1)) {
			parts = append(parts, text)
		}
	}
	if entry.WriteInResponse != "" {
		parts = append(parts, entry.WriteInResponse+" (write-in)")
	}
	if len(parts) == 0 {
		return "No answer provided"
	}
	return strings.Join(parts, ", ")
}

// lsContainsID reports whether ids holds id.
func lsContainsID(ids []string, id string) bool {
	for _, candidate := range ids {
		if candidate == id {
			return true
		}
	}
	return false
}
