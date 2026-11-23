package tui

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/app/agentturn"
	"github.com/justwasm/pigasus/internal/app/battlemode"
	"github.com/justwasm/pigasus/internal/app/subagenttools"
	"github.com/justwasm/pigasus/internal/app/tools/surface"
	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/core/confhome"
	"github.com/justwasm/pigasus/internal/ui/views/battlepanel"
	"github.com/justwasm/pigasus/internal/ui/views/modelargpopup"
)

// BattleModel owns the Battle Mode session manager and interactive comparison overlay.
type BattleModel struct {
	Manager *battlemode.Manager
	Panel   *battlepanel.Panel
	cancel  context.CancelFunc
}

func (m *RootModel) ensureBattlePanel() *battlepanel.Panel {
	if m.agents.battle.Panel != nil {
		return m.agents.battle.Panel
	}
	m.agents.battle.Panel = battlepanel.NewPanel(battlepanel.Options{
		OnPickWinner: func(armIndex int) {
			if m.agents.battle.Manager == nil {
				return
			}
			res, err := m.agents.battle.Manager.PickWinner(context.Background(), armIndex)
			if err != nil {
				m.agents.battle.Panel.SetStatusNotice("Pick winner failed: " + err.Error())
				return
			}
			m.agents.battle.Panel.SetSnapshot(m.agents.battle.Manager.Snapshot())
			msg := fmt.Sprintf("Applied winner arm #%d (%s)", armIndex+1, res.WinnerModel)
			if len(res.AppliedFiles) > 0 {
				msg += fmt.Sprintf(" — %d file(s) updated", len(res.AppliedFiles))
			}
			m.agents.battle.Panel.SetStatusNotice(msg)
			m.slashRespond("/battle", msg)
		},
		OnEliminate: func(armIndex int) {
			if m.agents.battle.Manager == nil {
				return
			}
			if err := m.agents.battle.Manager.EliminateArm(armIndex); err != nil {
				m.agents.battle.Panel.SetStatusNotice("Eliminate failed: " + err.Error())
				return
			}
			m.agents.battle.Panel.SetSnapshot(m.agents.battle.Manager.Snapshot())
			m.agents.battle.Panel.SetStatusNotice(fmt.Sprintf("Eliminated arm #%d", armIndex+1))
		},
		OnClose: func() tea.Cmd {
			if m.agents.battle.Panel != nil {
				m.agents.battle.Panel.Close()
			}
			return nil
		},
	})
	return m.agents.battle.Panel
}

func (m *RootModel) slashBattle(args string) (bool, tea.Cmd) {
	trimmed := strings.TrimSpace(args)
	if trimmed == "" || trimmed == "board" {
		if m.agents.battle.Manager == nil || m.agents.battle.Manager.Session() == nil {
			m.slashRespond("/battle", "Usage: /battle <modelA> <modelB> <task>  (or /battle board | /battle cleanup)")
			return true, nil
		}
		panel := m.ensureBattlePanel()
		panel.SetSnapshot(m.agents.battle.Manager.Snapshot())
		panel.Toggle()
		return true, nil
	}

	if trimmed == "cleanup" {
		if m.agents.battle.cancel != nil {
			m.agents.battle.cancel()
			m.agents.battle.cancel = nil
		}
		if m.agents.battle.Manager != nil {
			_ = m.agents.battle.Manager.Cleanup()
		}
		if m.agents.battle.Panel != nil {
			m.agents.battle.Panel.Close()
		}
		m.slashRespond("/battle cleanup", "Battle Mode workspaces cleaned up.")
		return true, nil
	}

	fields := strings.Fields(trimmed)
	if len(fields) < 3 {
		m.slashRespond("/battle", "Usage: /battle <modelA> <modelB> <task>")
		return true, nil
	}

	modelA := resolveBattleModel(fields[0])
	modelB := resolveBattleModel(fields[1])
	task := strings.TrimSpace(strings.TrimPrefix(trimmed, fields[0]))
	task = strings.TrimSpace(strings.TrimPrefix(task, fields[1]))
	if task == "" {
		m.slashRespond("/battle", "Usage: /battle <modelA> <modelB> <task>")
		return true, nil
	}

	workspace := m.session.cwd
	if workspace == "" {
		workspace, _ = os.Getwd()
	}

	if m.agents.battle.cancel != nil {
		m.agents.battle.cancel()
		m.agents.battle.cancel = nil
	}
	if m.agents.battle.Manager == nil {
		m.agents.battle.Manager = battlemode.NewManager()
	}

	panel := m.ensureBattlePanel()
	m.agents.battle.Manager.OnUpdate(func(snap battlemode.SessionSnapshot) {
		if m.turn.prog != nil {
			m.turn.prog.Send(battlepanel.MsgSnapshot{Snapshot: snap})
		}
	})

	arms := []battlemode.ArmSpec{
		{Label: modelA, Model: modelA},
		{Label: modelB, Model: modelB},
	}
	if err := m.agents.battle.Manager.PrepareWorkspaces(context.Background(), workspace, arms); err != nil {
		m.slashRespond("/battle", "Failed to prepare Battle Mode workspaces: "+err.Error())
		return true, nil
	}

	snap := m.agents.battle.Manager.Snapshot()
	snap.Task = task
	snap.Running = true
	panel.SetSnapshot(snap)
	panel.Open()

	ctx, cancel := context.WithCancel(context.Background())
	m.agents.battle.cancel = cancel

	mgr := m.agents.battle.Manager
	runner := m.battleArmRunner()
	go func() {
		_ = mgr.Run(ctx, task, runner)
		if m.turn.prog != nil {
			m.turn.prog.Send(battlepanel.MsgSnapshot{Snapshot: mgr.Snapshot()})
		}
	}()

	return true, armBattleTick()
}

func resolveBattleModel(arg string) string {
	if entry, ok := modelargpopup.Resolve(arg); ok && entry.ID != "" {
		return entry.ID
	}
	return arg
}

func (m *RootModel) battleArmRunner() battlemode.ArmRunner {
	return func(ctx context.Context, arm battlemode.ArmSpec, workspace string, task string) (battlemode.ArmOutput, error) {
		var be backend.Client
		if m.turn.backendFor != nil {
			be = m.turn.backendFor(arm.Model)
		}
		if be == nil {
			be = m.turn.backend
		}
		if be == nil {
			return battlemode.ArmOutput{}, fmt.Errorf("no backend available for model %q", arm.Model)
		}

		provider := agentturn.NewBackendProviderAdapter(be, arm.Model)
		reg := surface.RuntimeCatalog(workspace)
		subDeps := subagenttools.Deps{
			Provider:          provider,
			Backend:           be,
			Model:             arm.Model,
			Effort:            m.currentEffortName(),
			CWD:               workspace,
			AgentDir:          confhome.Antigravity(),
			SystemPrompt:      m.systemPrompt,
			Catalogue:         allCatalogActorTools(reg),
			PermissionGateFor: m.subagentGateFactory,
		}
		res, err := subagenttools.Invoke(ctx, subagenttools.Invocation{
			Prompt:   task,
			Model:    arm.Model,
			Worktree: workspace,
			TypeName: "self",
			Role:     "Battle Arm " + arm.DisplayName(),
		}, subDeps)
		if err != nil {
			return battlemode.ArmOutput{}, err
		}
		return battlemode.ArmOutput{
			Response: res.Text,
		}, nil
	}
}

func armBattleTick() tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(time.Time) tea.Msg {
		return battlepanel.MsgTick{}
	})
}

func (m *RootModel) updateBattleMsg(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case battlepanel.MsgSnapshot:
		panel := m.ensureBattlePanel()
		panel.SetSnapshot(msg.Snapshot)
		return m, nil, true
	case battlepanel.MsgTick:
		if m.agents.battle.Panel != nil && m.agents.battle.Panel.Active() {
			_, cmd := m.agents.battle.Panel.Update(msg)
			if m.agents.battle.Manager != nil && m.agents.battle.Manager.Snapshot().Running {
				return m, tea.Batch(cmd, armBattleTick()), true
			}
			return m, cmd, true
		}
		return m, nil, true
	case battlepanel.MsgDismissed:
		if m.agents.battle.Panel != nil {
			m.agents.battle.Panel.Close()
		}
		return m, nil, true
	}
	return m, nil, false
}
