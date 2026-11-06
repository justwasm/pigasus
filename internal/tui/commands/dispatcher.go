package commands

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/app/agentturn"
	"github.com/justwasm/pigasus/internal/core/slashparse"
	"github.com/justwasm/pigasus/internal/ui/slashcommands"
)

// Host provides the capabilities required by the slash command dispatcher.
type Host interface {
	SlashScopeContext() slashcommands.ScopeContext
	SlashRespond(cmd, msg string, extra ...string)
	SendAsPrompt(token, prompt string) tea.Cmd
	EngineSlashCommands() []slashcommands.Command
	TurnEngine() any
	LookupSkillPrompt(name, args string) (string, bool)
}

// ChainedHost provides optional support for handling multiple stacked leading slash commands.
type ChainedHost interface {
	HandleChainedSlashCommands(commands []string, task string) (bool, tea.Cmd)
}

// Handler is a function executing a specific slash command.
type Handler func(args string) (bool, tea.Cmd)

// ChainedHandler executes a sequence of stacked slash commands with a task.
type ChainedHandler func(commands []string, task string) (bool, tea.Cmd)

// Dispatcher coordinates slash command routing and execution.
type Dispatcher struct {
	host           Host
	handlers       map[string]Handler
	chainedHandler ChainedHandler
}

// NewDispatcher creates a new command Dispatcher for the given host.
func NewDispatcher(host Host) *Dispatcher {
	return &Dispatcher{
		host:     host,
		handlers: make(map[string]Handler),
	}
}

// Register binds a command name to a handler.
func (d *Dispatcher) Register(name string, h Handler) {
	d.handlers[name] = h
}

// RegisterChained binds a handler for multiple stacked slash commands.
func (d *Dispatcher) RegisterChained(h ChainedHandler) {
	d.chainedHandler = h
}

// Dispatch processes a raw buffer input. If it is a slash command, it routes and executes it.
func (d *Dispatcher) Dispatch(buf string) (bool, tea.Cmd) {
	if len(buf) == 0 || buf[0] != '/' {
		return false, nil
	}

	cmds, task := slashparse.ParseLeadingCommands(buf)
	if len(cmds) > 1 {
		if d.chainedHandler != nil {
			if handled, cmd := d.chainedHandler(cmds, task); handled {
				return true, cmd
			}
		}
		if ch, ok := d.host.(ChainedHost); ok {
			if handled, cmd := ch.HandleChainedSlashCommands(cmds, task); handled {
				return true, cmd
			}
		}
	}

	cmd, args := slashparse.Split(buf)

	name := cmd
	if canonical, ok := slashcommands.Canonical(cmd); ok {
		name = canonical
	}

	if cmdInfo, found := slashcommands.Find(name); found {
		sctx := d.host.SlashScopeContext()
		if !slashcommands.IsAvailable(cmdInfo, sctx) {
			d.host.SlashRespond("/"+name, fmt.Sprintf("%s is not supported by the %s backend", "/"+name, sctx.EngineName))
			return true, nil
		}
	}

	if h, ok := d.handlers[name]; ok {
		return h(args)
	}

	if cmd == "agent" {
		if h, ok := d.handlers["agent"]; ok {
			return h(args)
		}
	}

	// Skills lookup
	if prompt, ok := d.host.LookupSkillPrompt(cmd, args); ok {
		sctx := d.host.SlashScopeContext()
		if sctx.EngineName != "pigasus" && sctx.EngineName != "agy" {
			d.host.SlashRespond("/"+cmd, fmt.Sprintf("/%s is a Pigasus skill, not supported by the %s backend", cmd, sctx.EngineName))
			return true, nil
		}
		token := "/" + cmd
		if args != "" {
			token += " " + args
		}
		return true, d.host.SendAsPrompt(token, prompt)
	}

	// Dynamic agent slash commands
	for _, dyn := range d.host.EngineSlashCommands() {
		cleanDyn := strings.TrimPrefix(dyn.Name, "/")
		if strings.EqualFold(cleanDyn, cmd) {
			token := "/" + cmd
			if args != "" {
				token += " " + args
			}
			eng := d.host.TurnEngine()
			if handler, ok := eng.(agentturn.ActionHandler); ok {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				res, err := handler.ExecuteAction(ctx, cmd, args)
				cancel()
				if err == nil && res.Success && res.Message != "" {
					d.host.SlashRespond(token, res.Message)
					return true, nil
				}
			}
			return true, d.host.SendAsPrompt(token, token)
		}
	}

	// Non-blocking pass-through for active agent backends
	if eng := d.host.TurnEngine(); eng != nil {
		token := "/" + cmd
		if args != "" {
			token += " " + args
		}
		return true, d.host.SendAsPrompt(token, token)
	}

	return false, nil
}
