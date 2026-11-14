package storybook

import (
	tea "charm.land/bubbletea/v2"
	sb "github.com/justwasm/storybook"
)

// InstanceAdapter adapts an internal/ui/storybook.StoryInstance to sb.Instance.
type InstanceAdapter struct {
	inner StoryInstance
}

// NewInstanceAdapter constructs an adapter wrapping StoryInstance.
func NewInstanceAdapter(inner StoryInstance) *InstanceAdapter {
	return &InstanceAdapter{inner: inner}
}

// View implements sb.Instance.
func (a *InstanceAdapter) View() string {
	if a.inner == nil {
		return ""
	}
	return a.inner.View()
}

// Update implements sb.Instance.
func (a *InstanceAdapter) Update(msg tea.Msg) (sb.Instance, tea.Cmd) {
	if a.inner == nil {
		return a, nil
	}
	if key, ok := msg.(tea.KeyPressMsg); ok {
		cmd, _ := a.inner.HandleKey(key)
		return a, cmd
	}
	return a, nil
}

// SetSize implements sb.Resizable.
func (a *InstanceAdapter) SetSize(width, height int) {
	if a.inner != nil {
		a.inner.SetSize(width, height)
	}
}

// SetState implements sb.Stateful.
func (a *InstanceAdapter) SetState(stateIndex int) {
	if a.inner != nil {
		a.inner.SetState(stateIndex)
	}
}

// HandleKey implements sb.InteractiveHandler.
func (a *InstanceAdapter) HandleKey(key tea.KeyMsg) (tea.Cmd, bool) {
	if a.inner != nil {
		return a.inner.HandleKey(key)
	}
	return nil, false
}

// HandleMouse implements sb.MouseHandler.
func (a *InstanceAdapter) HandleMouse(ev tea.MouseMsg) (tea.Cmd, bool) {
	if a.inner == nil {
		return nil, false
	}
	if h, ok := a.inner.(interface {
		HandleMouse(tea.MouseMsg) (tea.Cmd, bool)
	}); ok {
		return h.HandleMouse(ev)
	}
	return nil, false
}

// Init implements sb.Initializer.
func (a *InstanceAdapter) Init() tea.Cmd {
	if a.inner == nil {
		return nil
	}
	if h, ok := a.inner.(interface {
		Init() tea.Cmd
	}); ok {
		return h.Init()
	}
	return nil
}

// SyncCatalogToSubstrate exports all stories in the catalog to github.com/justwasm/storybook.
func SyncCatalogToSubstrate(c *Catalog) {
	for _, s := range c.stories {
		st := s
		var states []sb.State
		for _, sState := range st.States {
			states = append(states, sb.State{
				ID:          sState.ID,
				Name:        sState.Name,
				Description: sState.Description,
			})
		}

		level := sb.AtomicLevel(st.AtomicLevelOrDefault())

		sb.Register(sb.Story{
			ID:          st.ID,
			Name:        st.Name,
			Level:       level,
			Category:    sb.Category(st.Category),
			Description: st.Description,
			UpstreamRef: st.UpstreamSym,
			Doc:         st.Description,
			States:      states,
			Factory: func() sb.Instance {
				return NewInstanceAdapter(st.Factory())
			},
		})
	}
}
