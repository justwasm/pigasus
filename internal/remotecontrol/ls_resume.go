// Package remotecontrol — ls_resume.go: continuing a conversation the
// daemon has no cascade for.
//
// The Web UI's composer sends a message into a conversation it opened
// from the sidebar. The frontend never calls StartCascade for that:
// its StartCascade call belongs to the *new* conversation flow (the
// frontend mints the cascade id there and hands it over), while
// continuing an existing conversation goes straight to
// SendUserCascadeMessage with the conversation's own id.
//
// The clone's cascade registry lives in memory, so a conversation a
// previous process wrote — or one the reference's own CLI wrote — is
// unknown to it. The reference's language server resolves it from the
// conversation store; so does the clone.
package remotecontrol

import (
	"strings"

	"github.com/justwasm/pigasus/internal/convstore"
)

// lsResumeCascade materialises the cascade for a conversation the Web
// UI continues but the daemon has no registry entry for.
//
// 【一手】 2026-10-26: the reference's own language server answers a
// message sent into a conversation it did not start. The conversation
// file under ~/.gemini/antigravity-cli/conversations/ is the state,
// and the reference appends the new turn to it — the file of the
// conversation the reference continued grew from three steps to five,
// and the earlier steps were still there.
//
// The reference's own flag for the same thing is
// exa.cortex_pb.CascadeConfig's `useDbTrajectory` — "load the
// trajectory from the db".
//
// The visible trajectory starts empty, exactly as it does in the
// reference: the Web UI shows the new turn only, although the file
// keeps the earlier turns.
func (d *Dispatcher) lsResumeCascade(id string, model string) (*Cascade, bool) {
	if d == nil || d.cascades == nil || id == "" {
		return nil, false
	}
	if !convstore.HasConversation(id) {
		return nil, false
	}
	opts := CascadeOpts{DispatchDefault: d.cfg.MessageBackend}
	if d.cfg.Session != nil {
		opts.Model = d.cfg.Session.Model
		opts.BackendLabel = d.cfg.Session.Backend
		opts.Workspace = d.cfg.Session.Workspace
	}
	if model != "" {
		opts.Model = model
	}
	opts.ID = id
	c, err := NewCascade(opts)
	if err != nil {
		return nil, false
	}
	if err := d.cascades.Register(c); err != nil {
		return nil, false
	}
	// A continued conversation starts a fresh trajectory: the Web UI
	// resets its step slice when the trajectory id changes, and the
	// reference shows only the new turn — the file keeps the rest.
	//
	// The fresh id keeps the reference's shape ("traj-" plus an id):
	// the reference's trajectory id is derived from an id, so a
	// continued conversation's is derived from the new one.
	if fresh, err := convstore.NewUUIDv4(); err == nil {
		d.trajectories.Restart(id, lsTrajectoryID(fresh))
	}
	return c, true
}

// lsCascadeConfig is the slice of exa.cortex_pb.CascadeConfig the
// clone reads: the model the Web UI's picker chose.
//
// The picker sends the model twice — as a name and as the numeric
// Model value — and the resumed conversation has no StartCascade to
// carry it, so the cascade config of the message request is the only
// place the choice survives.
type lsCascadeConfig struct {
	PlannerConfig struct {
		ModelName      string `json:"modelName"`
		RequestedModel struct {
			Model modelValue `json:"model"`
		} `json:"requestedModel"`
	} `json:"plannerConfig"`
	// UseDbTrajectory is the reference's own "the trajectory is in the
	// db" flag. The Web UI sets it when it continues a conversation.
	UseDbTrajectory bool `json:"useDbTrajectory"`
}

// lsCascadeConfigModel resolves the model the Web UI's picker chose
// from a request's cascade config.
//
// An empty result means "the host's default model" — the same
// fallback the StartCascade path uses when the picker sends nothing.
func (d *Dispatcher) lsCascadeConfigModel(cfg lsCascadeConfig) string {
	if name := strings.TrimSpace(cfg.PlannerConfig.ModelName); name != "" {
		return name
	}
	return d.modelIDForRequestedModel(int(cfg.PlannerConfig.RequestedModel.Model))
}
