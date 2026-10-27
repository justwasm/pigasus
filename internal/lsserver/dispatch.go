// Package lsserver — dispatch.go: the language server's dispatcher.
//
// The clone already speaks the `LanguageServerService` — the Web UI's
// own RPCs live in internal/remotecontrol. The language server adds
// two things on top of it:
//
//   - the answers the reference derives from its own flags
//     (`GetServerConfiguration` names the app data dir, the config
//     dir, the subclient type and the workspace limit);
//   - the mocked catalogue `-use_mocked_data=true` asks for.
//
// The answers that name the reference's own values — the instance id,
// the scratch directory, the default project directory — are in
// answers.go.
//
// The wrapper embeds the transport's Dispatcher interface, so every
// method it does not answer is forwarded without naming it.
package lsserver

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/google/uuid"

	"github.com/justwasm/pigasus/remotetransport"
)

// Dispatcher is the language server's dispatcher.
type Dispatcher struct {
	remotetransport.Dispatcher

	ctx AnswerContext
}

// NewDispatcher wraps inner with the language server's own answers.
//
// mocked selects the reference's mocked catalogue; the flag-derived
// answers are always installed.
func NewDispatcher(inner remotetransport.Dispatcher, flags *Flags, layout Layout, mocked bool) *Dispatcher {
	return &Dispatcher{
		Dispatcher: inner,
		ctx: AnswerContext{
			Flags:      flags,
			Layout:     layout,
			InstanceID: uuid.NewString(),
			Mocked:     mocked,
		},
	}
}

// DispatchDirect answers a unary RPC.
//
// A method the inner dispatcher does not know is answered with the
// default instance of its response message (coverage.go): the
// reference answers every method of the service, and a workbench
// that gets "Method not found" logs an error for every call it makes
// while the page loads.
func (d *Dispatcher) DispatchDirect(ctx context.Context, method, payloadText string) (string, error) {
	if answer, ok := d.answer(method); ok {
		return answer, nil
	}
	answer, err := d.Dispatcher.DispatchDirect(ctx, method, payloadText)
	if err != nil && isMethodNotFound(err) {
		if fallback, ok := fallbackAnswer(method); ok {
			return fallback, nil
		}
	}
	if err == nil && strings.TrimSpace(answer) == "" {
		// A dispatcher that answers with nothing answers with the
		// default instance of the response message — the reference
		// answers those methods with `{}`.
		if fallback, ok := fallbackAnswer(method); ok {
			return fallback, nil
		}
	}
	return answer, err
}

// DispatchStream forwards a server-streaming RPC to the inner
// dispatcher.
//
// The wrapper cannot inherit the inner dispatcher's optional
// streaming capability by embedding the interface, so it is asserted
// at call time and forwarded when it is there.
func (d *Dispatcher) DispatchStream(ctx context.Context, method, payloadText string, emit func(json.RawMessage) error) (bool, error) {
	streamer, ok := d.Dispatcher.(remotetransport.StreamDispatcher)
	if !ok {
		return false, nil
	}
	return streamer.DispatchStream(ctx, method, payloadText, emit)
}

// answer returns the language server's own answer for a method.
//
// The reference's own answers come first — the reference's shape is
// what a byte-for-byte comparison is against. The mocked catalogue
// follows; the clone's own service answers everything else.
func (d *Dispatcher) answer(method string) (string, bool) {
	switch method {
	case "GetServerConfiguration":
		return d.ctx.serverConfiguration(), true
	case "GetStatus":
		return `{}`, true
	case "GetCapabilities":
		return `{"supportsHookResultProtoBytes":true}`, true
	case "GetRemoteControlInfo":
		return d.ctx.remoteControlInfo(), true
	case "GetLocalUserInfo":
		return d.ctx.localUserInfo(), true
	case "GetDefaultProjectDir":
		return d.ctx.defaultProjectDir(), true
	case "GetStandaloneDir":
		return d.ctx.standaloneDir(), true
	case "GetMendelFlags":
		// The reference's own server serves an empty set to every
		// identity but the hub's: the flags it reads are the ones
		// the IDE's hub product turns on.
		if d.ctx.Flags.SubclientType != "hub" {
			return `{"experimentConfig":{}}`, true
		}
	case "GetUserSettings":
		// The reference does not implement the method in any mode —
		// it answers the Connect-RPC "unimplemented" error, with a
		// fresh tracing id per call.
		return mockedUnimplemented(), true
	case "GetAllSkills":
		// The workbench asks for the skills on every load and logs
		// "skills_fetch_failed" when the answer is an error.
		return d.skillsAnswer(), true
	case "GetWorkspaceInfos":
		// The workbench opens the home directory and the config home
		// from this answer.
		return d.workspaceInfosAnswer(), true
	case "GetStaticExperimentStatus":
		// The clone runs on the user's own configuration; there is no
		// experiment server behind it.
		return d.staticExperimentsAnswer(), true
	}
	return d.mockedAnswer(method)
}

// mockedAnswer is the mocked catalogue's own answers.
//
// The catalogue only exists when the clone runs with
// `-use_mocked_data=true` — the mode the reference's own standalone
// mode uses, and the mode the byte-for-byte comparison is taken in.
func (d *Dispatcher) mockedAnswer(method string) (string, bool) {
	if !d.ctx.Mocked {
		return "", false
	}
	switch method {
	case "GetUserStatus":
		return mockedUserStatus(), true
	case "GetCascadeModelConfigData":
		return mockedCascadeModelConfigData(), true
	case "GetAvailableModels":
		return `{"response":{}}`, true
	case "GetCascadeModelConfigs":
		return `{}`, true
	case "GetAuthStatus":
		return `{"authResult":{"hasValidAuth":true}}`, true
	}
	return "", false
}

// ServerConfiguration is the flag-derived configuration the web UI's
// page reads. It is exported for the tests that compare the clone's
// answer with the reference's.
func ServerConfiguration(f *Flags) string {
	ctx := AnswerContext{Flags: f}
	return ctx.serverConfiguration()
}
