// remote_agent_interaction_test.go — the wire shapes of the remote
// user seam: the WAITING step's multi-select flag and the answer's
// per-question option ids.
package agentturn

import "testing"

// TestWaitingInteractionDecodesMultiSelect pins that the remote agent's
// proto-JSON isMultiSelect reaches RemoteQuestion, in both the
// requestedInteraction and top-level askQuestion shapes.
func TestWaitingInteractionDecodesMultiSelect(t *testing.T) {
	for _, key := range []string{"isMultiSelect", "is_multi_select"} {
		t.Run(key, func(t *testing.T) {
			step := map[string]any{
				"status": 9,
				"metadata": map[string]any{
					"sourceTrajectoryStepInfo": map[string]any{
						"trajectoryId": "traj-1",
						"stepIndex":    0,
					},
				},
				"askQuestion": map[string]any{"questions": []any{
					map[string]any{
						"question": "Pick tools",
						"options": []any{
							map[string]any{"id": "1", "text": "Git"},
							map[string]any{"id": "2", "text": "Docker"},
						},
						key: true,
					},
				}},
				"requestedInteraction": map[string]any{"askQuestion": map[string]any{"questions": []any{
					map[string]any{
						"question": "Pick tools",
						"options": []any{
							map[string]any{"id": "1", "text": "Git"},
							map[string]any{"id": "2", "text": "Docker"},
						},
						key: true,
					},
				}}},
			}

			req, ok := waitingInteraction(step, "cascade-1", 0)
			if !ok {
				t.Fatal("WAITING step was not decoded")
			}
			if len(req.Questions) != 1 {
				t.Fatalf("questions = %+v", req.Questions)
			}
			if !req.Questions[0].IsMultiSelect {
				t.Errorf("IsMultiSelect = false, want true")
			}
		})
	}
}

// TestInteractionPayloadCarriesEverySelectedOption pins that a
// multi-select answer reaches the wire with all of its option ids.
func TestInteractionPayloadCarriesEverySelectedOption(t *testing.T) {
	req := RemoteInteraction{
		Kind:         RemoteInteractionQuestion,
		TrajectoryID: "traj-1",
		StepIndex:    3,
	}
	answer := RemoteInteractionAnswer{Answers: []RemoteQuestionAnswer{{
		Prompt:            "Pick tools",
		SelectedOptionIDs: []string{"1", "2"},
		WriteInResponse:   "and make",
	}}}

	payload := interactionPayload(req, answer)
	ask := payload["askQuestion"].(map[string]any)
	responses := ask["responses"].([]any)
	entry := responses[0].(map[string]any)
	selected := entry["selectedOptionIds"].([]string)
	if len(selected) != 2 || selected[0] != "1" || selected[1] != "2" {
		t.Errorf("selectedOptionIds = %v, want [1 2]", selected)
	}
	if entry["writeInResponse"] != "and make" {
		t.Errorf("writeInResponse = %v, want and make", entry["writeInResponse"])
	}
}
