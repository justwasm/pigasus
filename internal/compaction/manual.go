package compaction

import (
	"context"
	"errors"

	piagent "github.com/MichaelKinsy/PiG/agent"
)

// HandleManualResult updates doneCh or sends an initial error msg if err != nil.
func HandleManualResult(err error, customInstructions string, doneCh chan ManualDoneMsg) {
	if err == nil {
		return
	}
	select {
	case got := <-doneCh:
		if got.Err == nil {
			got.Err = err
			select {
			case doneCh <- got:
			default:
			}
		}
	default:
		doneCh <- ManualDoneMsg{Err: err, CustomInstructions: customInstructions}
	}
}

// RunManualTask runs a compaction on compactor and notifies onDone with the ManualDoneMsg.
func RunManualTask(
	ctx context.Context,
	compactor ManualCompactor,
	customInstructions string,
	onDone func(ManualDoneMsg),
) {
	var (
		doneCh      = make(chan ManualDoneMsg, 1)
		unsubscribe func()
	)
	unsubscribe = compactor.SubscribeManual(func(ev piagent.AgentEvent) {
		switch e := ev.(type) {
		case piagent.CompactionEndEvent:
			msg := ManualDoneMsg{
				TokensBefore:       e.TokensBefore,
				TokensAfter:        e.EstimatedTokensAfter,
				Reason:             e.Reason,
				Aborted:            e.Aborted,
				CustomInstructions: customInstructions,
			}
			if e.ErrorMessage != "" {
				msg.Err = errors.New(e.ErrorMessage)
			}
			select {
			case doneCh <- msg:
			default:
			}
			if unsubscribe != nil {
				unsubscribe()
			}
		}
	})
	defer unsubscribe()

	err := compactor.Compact(ctx, customInstructions)
	HandleManualResult(err, customInstructions, doneCh)

	var msg ManualDoneMsg
	select {
	case msg = <-doneCh:
	case <-ctx.Done():
		msg = ManualDoneMsg{Err: ctx.Err()}
	default:
		msg = ManualDoneMsg{}
	}
	if onDone != nil {
		onDone(msg)
	}
}
