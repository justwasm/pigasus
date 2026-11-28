// fixtures_test.go — the two view fixtures the surface's tests share.
//
// The fixtures live with the views they describe, not with one test file:
// several of the surface's tests need a request and a conversation, and a
// second copy would be a second answer to "what does a Wire view look
// like".
package debugpanel

// wireFixture is a request with one section of each kind, three tools and
// three messages, so every column of every group is exercised.
func wireFixture() WireState {
	return WireState{
		Model:  "Gemini 3.8 Flash (High)",
		Engine: "pigasus",
		Note:   "host-side sizes",
		Sections: []WireSection{
			{Name: "identity", Bytes: 900, Tokens: 300},
			{Name: "guidelines", Bytes: 3000, Tokens: 1000},
			{Name: "skills", Bytes: 1200, Tokens: 400},
		},
		Tools: []WireTool{
			{Name: "run_command", Bytes: 2100, Tokens: 700},
			{Name: "read_file", Bytes: 600, Tokens: 200},
			{Name: "grep_search", Bytes: 1200, Tokens: 400},
		},
		Messages: []WireMessage{
			{Index: 1, Role: "user", Kind: "envelope", Bytes: 400, Tokens: 139, Preview: "<USER_REQUEST> do the thing"},
			{Index: 2, Role: "model", Kind: "tool_call", Bytes: 200, Tokens: 72},
			{Index: 3, Role: "tool", Kind: "response", Bytes: 8000, Tokens: 2673, Preview: "line of output"},
		},
		Width: 120,
	}
}

// trajFixture is a short conversation with one of every step kind,
// including a checkpoint and a step the checkpoint folded away.
func trajFixture() TrajectoryState {
	return TrajectoryState{
		Note:  "the store's own steps",
		Width: 120,
		Steps: []TrajStep{
			{Index: 1, Kind: TrajUser, Summary: "fix the build", Detail: []string{"fix the build"}},
			{Index: 2, Kind: TrajThought, Summary: "the test target is stale", Folded: true},
			{Index: 3, Kind: TrajTool, Summary: "Ran go build ./...", Tokens: 42,
				Detail: []string{"args:", "  {\"CommandLine\":\"go build ./...\"}", "result:", "ok"}},
			{Index: 4, Kind: TrajCheckpoint, Summary: "same_model · steps 1–3",
				Detail: []string{"summary:", "  the build was fixed"}},
			{Index: 5, Kind: TrajReply, Summary: "the build is fixed", Detail: []string{"the build is fixed"}},
			{Index: 6, Kind: TrajTool, Summary: "Read main.go", Cold: true, ColdNote: "2.8 KB on disk"},
			{Index: 7, Kind: TrajError, Summary: "the turn failed", Detail: []string{"the turn failed"}},
		},
		Turns:       2,
		Checkpoints: 1,
		ColdChunks:  1,
	}
}
