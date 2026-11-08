package commands

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/app/agentturn"
)

// EngineHost defines host operations needed for turn-engine slash commands.
type EngineHost interface {
	TurnEngine() any
	SlashRespond(cmd, msg string, extra ...string)
}

// RegisterEngineCommands binds engine-level actions (e.g. /steer) to the dispatcher.
func RegisterEngineCommands(d *Dispatcher, h EngineHost) {
	d.Register("steer", func(args string) (bool, tea.Cmd) {
		instruction := strings.TrimSpace(args)
		if instruction == "" {
			h.SlashRespond("/steer", "Usage: /steer <instruction>")
			return true, nil
		}

		eng := h.TurnEngine()
		if eng == nil {
			h.SlashRespond("/steer", "No active engine available for steering")
			return true, nil
		}

		// 1. Direct Steerer interface
		if s, ok := eng.(agentturn.Steerer); ok {
			if err := s.Steer(instruction); err != nil {
				h.SlashRespond("/steer", fmt.Sprintf("Steer error: %v", err))
				return true, nil
			}
			h.SlashRespond("/steer", fmt.Sprintf("Steering guidance queued: %q", instruction))
			return true, nil
		}

		// 2. ActionHandler interface
		if ah, ok := eng.(agentturn.ActionHandler); ok {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			res, err := ah.ExecuteAction(ctx, "steer", instruction)
			if err != nil {
				h.SlashRespond("/steer", fmt.Sprintf("Steer error: %v", err))
				return true, nil
			}
			msg := res.Message
			if msg == "" {
				msg = fmt.Sprintf("Steering guidance queued: %q", instruction)
			}
			h.SlashRespond("/steer", msg)
			return true, nil
		}

		h.SlashRespond("/steer", "Active engine does not support mid-turn steering")
		return true, nil
	})
}
