// Package remotecontrol — ls_trajectory.go: the per-conversation
// trajectory the Antigravity Web UI replays, and the proto-JSON step
// builders that fill it.
//
// The Web UI is the reference's own embedded frontend (the IDE's
// `language_server -web_bundle_path` bundle, recovered into
// internal/rcfrontend/assets/main.js). It talks to the IDE's
// LanguageServerService and renders a conversation from the
// `StreamAgentStateUpdates` server stream: every frame carries an
// `exa.jetski_cortex_pb.AgentStateUpdate` whose
// `mainTrajectoryUpdate.stepsUpdate` holds the step list.
//
// The shapes below are the IDE's own protos, recovered from the
// FileDescriptorProto set embedded in the frontend bundle
// (out/ide-language-server/extract_ls_descriptor.py):
//
//   - exa.jetski_cortex_pb.AgentStateUpdate
//   - exa.jetski_cortex_pb.TrajectoryUpdate / StepsUpdate / Slice
//   - gemini_coder.Step (the trajectory step)
//
// Wire shape (proto-JSON):
//
//	{
//	  "update": {
//	    "conversationId": "<uuid>",
//	    "trajectoryId":   "<uuid>",
//	    "status":         2,                  // CascadeRunStatus.RUNNING
//	    "mainTrajectoryUpdate": {
//	      "stepsUpdate": {
//	        "indices":     [0, 1],
//	        "steps":       [ <gemini_coder.Step>, … ],
//	        "totalLength": 2,
//	        "pageBounds":  {"startIndex": 0, "endIndexExclusive": 2}
//	      },
//	      "lastStepType": 15                  // CortexStepType.PLANNER_RESPONSE
//	    }
//	  }
//	}
//
// The client parses unknown fields leniently (its Connect transport
// passes ignoreUnknownFields: true), so a response may carry extra
// keys — but a *known* key with the wrong JSON type still fails.
package remotecontrol

import (
	"context"
	"encoding/base64"
	"sync"
	"time"

	"github.com/justwasm/pigasus/internal/convstore"
)

// Cortex step-type values (exa.cortex_pb.CortexStepType) the Web UI's
// trajectory renderer keys off.
const (
	lsStepTypeUserInput       = 14
	lsStepTypePlannerResponse = 15
	lsStepTypeGeneric         = 132
	// lsStepTypeAskQuestion is CORTEX_STEP_TYPE_ASK_QUESTION — the
	// step the Web UI's questionnaire card reads.
	lsStepTypeAskQuestion = 138
)

// Step statuses (exa.cortex_pb.CortexStepStatus).
const (
	lsStepStatusDone = 3
	// lsStepStatusQueued is CORTEX_STEP_STATUS_QUEUED — the status of
	// a message the server is holding.
	lsStepStatusQueued = 11
	// lsStepStatusWaiting is CORTEX_STEP_STATUS_WAITING — the status
	// of a step that is blocked on the user.
	//
	// 【一手】 2026-10-26, from the IDE web bundle (main.js): the
	// interaction card's step filter is `status === WAITING`
	// (`v8b`), so a step that waits for the user and says so in any
	// other status is invisible to the card.
	lsStepStatusWaiting = 9
)

// Cascade run statuses (exa.cortex_pb.CascadeRunStatus).
const (
	lsRunStatusIdle    = 1
	lsRunStatusRunning = 2
)

// Step sources (exa.cortex_pb.CortexStepSource).
const (
	lsStepSourceModel        = 2
	lsStepSourceUserExplicit = 4
)

// lsTrajectory is one conversation's step list. The Web UI keeps one
// trajectory per conversation; the clone keys it by the cascade id,
// which is also the conversation id the Web UI mints (the frontend
// sends the same uuid as `cascadeId` and `conversationId`).
//
// The steps are the proto-JSON form of gemini_coder.Step. They are
// immutable once appended, so readers may hold the slice without
// copying the maps.
type lsTrajectory struct {
	mu             sync.Mutex
	conversationID string
	trajectoryID   string
	steps          []map[string]any
	lastStepType   int
	running        bool
	version        int
	changed        chan struct{}
	// createdAt is when the trajectory started, updatedAt when it
	// last changed. The conversation methods report both.
	createdAt time.Time
	updatedAt time.Time
	// initStateID is the trajectory's initialisation state id — the
	// reference's own uuid, which the trajectory's metadata carries.
	initStateID string

	// The delivery queue (see ls_queue.go). A conversation runs one
	// turn at a time: `running` is true while a turn is in flight,
	// and `queue` holds the messages that are waiting for it.
	queue []lsQueuedMessage
	// turnCancel is the cancel func of the turn in flight. A message
	// that interrupts the turn fires it.
	turnCancel context.CancelFunc
	// interrupted marks the turn in flight as interrupted, so its
	// error is not reported as a failure.
	interrupted bool
	// pendingSeq numbers the pending messages, so each one gets an
	// id of its own.
	pendingSeq int
}

// lsSnapshot is an immutable view of a trajectory at one version.
type lsSnapshot struct {
	ConversationID string
	TrajectoryID   string
	Steps          []map[string]any
	LastStepType   int
	Running        bool
	// Pending is the queue: the messages the conversation is holding.
	Pending []lsQueuedMessage
	Version int
	// CreatedAt is when the trajectory started, UpdatedAt when it
	// last changed.
	CreatedAt time.Time
	UpdatedAt time.Time
	// InitStateID is the trajectory's initialisation state id.
	InitStateID string
}

// newLSTrajectory returns a fresh trajectory for conversationID. The
// trajectory id is derived from the conversation id and stays stable
// for the conversation's life — the Web UI resets its step slice
// whenever the trajectory id changes.
//
// 【一手】 2026-10-27: the reference's trajectory id is the cascade id
// with the prefix "traj-" (see lsTrajectoryID).
func newLSTrajectory(conversationID string) *lsTrajectory {
	now := time.Now().UTC()
	return &lsTrajectory{
		conversationID: conversationID,
		trajectoryID:   lsTrajectoryID(conversationID),
		changed:        make(chan struct{}),
		createdAt:      now,
		updatedAt:      now,
		initStateID:    lsNewStateID(),
	}
}

// lsNewStateID mints an initialisation state id.
//
// 【一手】 2026-10-27: the reference's user trajectory carries an
// "initializationStateId" in its metadata — a uuid of its own.
func lsNewStateID() string {
	if id, err := convstore.NewUUIDv4(); err == nil {
		return id
	}
	return ""
}

// Append adds one step to the trajectory and wakes every waiter.
// The step's `type` becomes the trajectory's lastStepType — the Web
// UI reads it to decide whether the user's optimistic message has
// been superseded by the server's own user-input step.
//
// It returns the index the step landed on, so a caller that keeps
// publishing into the same step can find it again.
func (t *lsTrajectory) Append(step map[string]any) int {
	if t == nil || step == nil {
		return -1
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	lsStampStepInfo(step, t.conversationID, t.trajectoryID, len(t.steps))
	t.steps = append(t.steps, step)
	if v, ok := step["type"].(int); ok {
		t.lastStepType = v
	}
	t.mutateLocked()
	return len(t.steps) - 1
}

// lsStampStepInfo records where a step sits in the trajectory.
//
// 【一手】 2026-10-26, from the CLI web bundle (cli-1.4.7-main.js):
// the Web UI groups a conversation's steps into turns by the step index
// the metadata carries — `jV(metadata)` reads
// `metadata.sourceTrajectoryStepInfo.stepIndex` — and it keys each turn
// by "<trajectoryId>-<index>". A step whose metadata has no index
// lands in the unnamed "initial" bucket, and two such steps in one
// conversation produce two turns with the same key: the Web UI then
// renders one of them only, and the other turn is gone.
//
// The reference's own steps always carry the index, so the clone
// stamps it on every step it serves — the steps it builds for a live
// turn and the steps it reads back from the store.
func lsStampStepInfo(step map[string]any, cascadeID, trajectoryID string, index int) {
	if step == nil {
		return
	}
	meta, _ := step["metadata"].(map[string]any)
	if meta == nil {
		meta = map[string]any{}
		step["metadata"] = meta
	}
	info, _ := meta["sourceTrajectoryStepInfo"].(map[string]any)
	if info == nil {
		info = map[string]any{}
		meta["sourceTrajectoryStepInfo"] = info
	}
	if _, ok := info["cascadeId"]; !ok {
		info["cascadeId"] = cascadeID
	}
	if _, ok := info["trajectoryId"]; !ok {
		info["trajectoryId"] = trajectoryID
	}
	info["stepIndex"] = index
}

// Update replaces the step at index and wakes every waiter.
//
// The Web UI applies a steps update by index, so replacing the step
// in place is how a step grows: the answer's text lands in the same
// slot the first word landed in.
func (t *lsTrajectory) Update(index int, step map[string]any) {
	if t == nil || step == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if index < 0 || index >= len(t.steps) {
		return
	}
	t.steps[index] = step
	t.mutateLocked()
}

// Seed fills the trajectory with the steps the store holds for the
// conversation.
//
// The Web UI asks for a conversation's steps when it opens the
// conversation; the clone answers by loading them from the store and
// seeding the trajectory, so the state stream carries them and the
// conversation's history is on screen.
//
// A trajectory that already holds steps keeps them — the live steps
// are newer than the stored ones — and a running trajectory keeps
// them too: a turn in flight is what the user is looking at.
func (t *lsTrajectory) Seed(steps []map[string]any) {
	if t == nil || len(steps) == 0 {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.steps) > 0 || t.running {
		return
	}
	for i, step := range steps {
		lsStampStepInfo(step, t.conversationID, t.trajectoryID, i)
	}
	t.steps = append(t.steps, steps...)
	for _, step := range steps {
		if v, ok := step["type"].(int); ok {
			t.lastStepType = v
		}
	}
	t.mutateLocked()
}

// Restart clears the trajectory's steps and gives it a new id.
//
// The Web UI resets its step slice when the trajectory id changes, so
// a new id is how a conversation that is continued shows only the new
// turn. The reference behaves the same way: the conversation file
// keeps every turn, the view shows the new one.
//
// The trajectory is cleared in place — the state stream holds this
// pointer, so the store must not swap it for another one.
func (t *lsTrajectory) Restart(trajectoryID string) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.steps = nil
	t.lastStepType = 0
	t.running = false
	t.queue = nil
	t.interrupted = false
	t.trajectoryID = trajectoryID
	t.createdAt = time.Now().UTC()
	t.mutateLocked()
}

// mutateLocked bumps the version and replaces the change channel.
// The caller must hold t.mu.
func (t *lsTrajectory) mutateLocked() {
	t.version++
	t.updatedAt = time.Now().UTC()
	close(t.changed)
	t.changed = make(chan struct{})
}

// TrajectoryID is the trajectory's own id.
//
// It is the conversation id with the reference's prefix (see
// lsTrajectoryID), unless a continued conversation replaced it.
func (t *lsTrajectory) TrajectoryID() string {
	if t == nil {
		return ""
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.trajectoryID
}

// Snapshot returns the trajectory's current state. The returned step
// slice is a fresh copy of the slice header's backing array, so a
// later Append cannot mutate a snapshot the caller is holding.
func (t *lsTrajectory) Snapshot() lsSnapshot {
	if t == nil {
		return lsSnapshot{}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	steps := make([]map[string]any, len(t.steps))
	copy(steps, t.steps)
	return lsSnapshot{
		ConversationID: t.conversationID,
		TrajectoryID:   t.trajectoryID,
		Steps:          steps,
		LastStepType:   t.lastStepType,
		Running:        t.running,
		Pending:        t.pendingLocked(),
		Version:        t.version,
		CreatedAt:      t.createdAt,
		UpdatedAt:      t.updatedAt,
		InitStateID:    t.initStateID,
	}
}

// Wait blocks until the trajectory's version moves past `since`, or
// until ctx is done. It returns the current version. A version that
// already moved past `since` returns immediately — the caller must
// re-snapshot to see the new state.
func (t *lsTrajectory) Wait(ctx context.Context, since int) (int, error) {
	if t == nil {
		return 0, context.Canceled
	}
	t.mu.Lock()
	if t.version != since {
		v := t.version
		t.mu.Unlock()
		return v, nil
	}
	ch := t.changed
	t.mu.Unlock()
	select {
	case <-ch:
	case <-ctx.Done():
		return 0, ctx.Err()
	}
	t.mu.Lock()
	v := t.version
	t.mu.Unlock()
	return v, nil
}

// lsAgentStateFrame renders one AgentStateUpdate frame for the
// trajectory snapshot. The frame is the `StreamAgentStateUpdates`
// response message: `{"update": {…}}`.
func lsAgentStateFrame(snap lsSnapshot) map[string]any {
	status := lsRunStatusIdle
	if snap.Running {
		status = lsRunStatusRunning
	}
	// The executor loop is what runs the turns. It is busy while a
	// turn is in flight and while a message is waiting for one — the
	// Web UI's pending predicate reads this field to decide whether
	// the message it sent is still on its way.
	executor := status
	if len(snap.Pending) > 0 {
		executor = lsRunStatusRunning
	}
	steps := snap.Steps
	if steps == nil {
		steps = []map[string]any{}
	}
	indices := make([]int, len(steps))
	for i := range steps {
		indices[i] = i
	}
	return map[string]any{
		"update": map[string]any{
			"conversationId":      snap.ConversationID,
			"trajectoryId":        snap.TrajectoryID,
			"status":              status,
			"executableStatus":    status,
			"executorLoopStatus":  executor,
			"fullyIdle":           !snap.Running && len(snap.Pending) == 0,
			"pendingAgentMessagesUpdate": lsPendingMessagesUpdate(snap.Pending),
			"mainTrajectoryUpdate": map[string]any{
				"stepsUpdate": map[string]any{
					"indices":     indices,
					"steps":       steps,
					"totalLength": len(steps),
					"pageBounds": map[string]any{
						"startIndex":        0,
						"endIndexExclusive": len(steps),
					},
				},
				"status":       status,
				"lastStepType": snap.LastStepType,
			},
		},
	}
}

// lsPendingMessagesUpdate renders the queued messages as the state
// frame's pendingAgentMessagesUpdate.
//
// The frontend applies the update as a slice update onto the list it
// already holds — the same shape the trajectory's steps update has —
// so the clone sends the whole list every time: the indices are the
// list's own positions and the total length is its size.
//
// 【反编译】 exa.jetski_cortex_pb.PendingAgentMessagesUpdate:
// indices=1, pending_agent_messages=2, total_length=3.
func lsPendingMessagesUpdate(pending []lsQueuedMessage) map[string]any {
	messages := make([]any, 0, len(pending))
	indices := make([]int, 0, len(pending))
	for i, msg := range pending {
		indices = append(indices, i)
		messages = append(messages, lsPendingAgentMessageJSON(msg))
	}
	return map[string]any{
		"indices":              indices,
		"pendingAgentMessages": messages,
		"totalLength":          len(messages),
	}
}

// lsPendingAgentMessageJSON renders one queued message as the
// proto-JSON form of exa.cortex_pb.AgentMessage.
//
// The payload is the step's bytes — the frontend decodes them with its
// own Step schema — and proto-JSON spells a bytes field as base64.
// The strategy is the number the frontend compares against its own enum
// (`e.deliveryStrategy===V.MessageDeliveryStrategy.WHEN_IDLE`).
//
// The payload is spelled the way proto-JSON spells a oneof: the
// member's own name at the top level. The frontend reads the oneof
// through its runtime's own shape (`e.payload.case==="stepPayload"`),
// but that shape is what the *decoded* message looks like — the wire
// carries the member name, and the Connect bridge re-encodes this JSON
// with protojson before it reaches the browser.
//
// 【反编译】 exa.cortex_pb.AgentMessage: id=1, content=4,
// step_payload=9, delivery_strategy=13; the oneof is `payload`.
func lsPendingAgentMessageJSON(msg lsQueuedMessage) map[string]any {
	return map[string]any{
		"id":               msg.ID,
		"stepPayload":      base64.StdEncoding.EncodeToString(msg.Step),
		"deliveryStrategy": int(msg.Strategy),
	}
}
