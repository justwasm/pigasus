// tui_cumulative.go — the cumulative prompt's implicit-trajectory messages.
//
// 【反编译】 agy 1.4.7: the reference's cumulative-prompt handler
// (`google3/third_party/jetski/prompt/cumulative_prompt_handler/cumulative_prompt_handler`,
// init 0x10225a6f0) converts the implicit trajectory — the `!` commands the
// user ran outside the conversation — into chat messages and carries them
// ahead of the current turn. The messages stay out of the conversation: the
// reference records a `!` command in the implicit trajectory only (see
// tui_implicit.go), and the conversation store holds no record of it.
//
// The clone therefore keeps the converted messages in their own field and
// prepends them to the turn's message list, rather than appending them to
// m.session — the conversation store would then carry a `!` command the
// reference never wrote there.
//
// The converted messages carry no <USER_REQUEST> envelope: the reference
// wraps only what the user typed, and these messages are the reference's own
// rendering of a step.
package cliapp

import (
	"github.com/justwasm/pigasus/internal/backend"
	"github.com/justwasm/pigasus/internal/convstore"
	"github.com/justwasm/pigasus/internal/cumulativeprompt"
	"github.com/justwasm/pigasus/internal/implicit"
)

// collectImplicitMessages converts the implicit trajectory's steps that no
// prompt has carried yet.
//
// The read is best-effort: the trajectory lives on disk, and a failure to
// read it must not stop the turn — the reference's own handler reports the
// failure and carries on without the trajectory.
func (m *runMode) collectImplicitMessages() {
	steps, err := implicit.Open(convstore.StoreDir()).Steps(implicit.WorkspaceURI(m.cwd))
	if err != nil {
		return
	}
	if len(steps) < m.implicitCursor {
		// The store only appends, so a shorter trajectory means the file was
		// replaced. Start over rather than carry a stale cursor.
		m.implicitMsgs = nil
		m.implicitCursor = 0
	}
	if len(steps) == m.implicitCursor {
		return
	}
	for _, cm := range cumulativeprompt.Convert(steps, cumulativeprompt.Options{From: m.implicitCursor}) {
		m.implicitMsgs = append(m.implicitMsgs, backend.Message{
			Role:    cm.Role,
			Content: cm.Content,
		})
	}
	m.implicitCursor = len(steps)
}
