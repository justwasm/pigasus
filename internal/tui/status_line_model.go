package tui

// status_line_model.go — the status-line sub-model.
//
// StatusLineModel owns the bottom status row's configuration and the two
// cached settings blocks that drive it: the built-in `statusLine` block and
// the `title` block, plus the external scripts' state and the window-title
// store. RootModel still assembles the payload (it needs the session's
// model, cwd and token counts) and paints the row.
//
// Upstream: google3/third_party/jetski/cli/model/model.StatusLineModel.

import (
	"strings"
	"sync"

	"github.com/justwasm/pigasus/internal/core/confhome"
	"github.com/justwasm/pigasus/internal/core/logts"
	"github.com/justwasm/pigasus/internal/ui/views/statusline"
)

// StatusLineModel owns the status row's configuration and external state.
type StatusLineModel struct {
	// visible is "on"/"off"; title is the window-title setting.
	visible string
	title   string

	// external mirrors cfg.Active() for quick reads.
	external bool
	command  string
	cfg      statusline.Config

	// titleCfg is the cached `title` block, read the same way.
	titleCfg     statusline.Config
	titleCommand string

	// titleStore holds the window title the title script produced. The
	// script runs on the tick goroutine and the renderer reads the title
	// on the model's goroutine, so the value needs its own lock.
	titleStore *statusline.TitleStore
	storeMu    sync.Mutex

	// externalState is the cached rendered output / tick counter.
	externalState *statusline.State
}

// Reload re-reads the statusLine and title blocks from settings.
func (s *StatusLineModel) Reload() {
	if cfg, ok := statusline.LoadConfig(); ok {
		s.cfg = cfg
		s.command = cfg.Command
		s.external = cfg.Active()
	} else {
		s.cfg = statusline.Config{}
		s.command = ""
		s.external = false
	}
	if cfg, ok := statusline.LoadTitleConfig(); ok {
		s.titleCfg = cfg
		s.titleCommand = cfg.Command
		if cfg.Active() {
			s.title = "on"
		}
	} else {
		s.titleCfg = statusline.Config{}
		s.titleCommand = ""
	}
}

// ScriptActive reports whether an external status-line script is enabled.
func (s *StatusLineModel) ScriptActive() bool {
	return s.cfg.Active() || s.external
}

// Enabled reports whether the status row is drawn.
func (s *StatusLineModel) Enabled() bool { return s.visible != "off" }

// Config returns the cached statusLine block.
func (s *StatusLineModel) Config() statusline.Config { return s.cfg }

// TitleConfig returns the cached title block.
func (s *StatusLineModel) TitleConfig() statusline.Config { return s.titleCfg }

// ExternalState returns the cached output of the external status script.
func (s *StatusLineModel) ExternalState() *statusline.State {
	if s == nil {
		return nil
	}
	return s.externalState
}

// EnsureExternalState returns the external state, creating it on first use.
func (s *StatusLineModel) EnsureExternalState() *statusline.State {
	if s == nil {
		return nil
	}
	if s.externalState == nil {
		s.externalState = &statusline.State{}
	}
	return s.externalState
}

// TitleStore returns the window-title store, creating it on first use.
func (s *StatusLineModel) TitleStore() *statusline.TitleStore {
	if s == nil {
		return nil
	}
	s.storeMu.Lock()
	defer s.storeMu.Unlock()
	if s.titleStore == nil {
		s.titleStore = &statusline.TitleStore{}
	}
	return s.titleStore
}

// ExternalRenderOK reports whether the external status script has a usable
// rendered value to show.
func (s *StatusLineModel) ExternalRenderOK() bool {
	if s == nil || !s.Enabled() || !s.ScriptActive() {
		return false
	}
	state := s.ExternalState()
	if state == nil {
		return false
	}
	out, _ := state.Current()
	return strings.TrimSpace(out) != ""
}

// AreaState assembles the status row's rendering facts.
func (s *StatusLineModel) AreaState(builtin string) statusline.AreaState {
	return statusline.AreaState{
		Builtin: builtin,
		Active:  s.ScriptActive(),
		Config:  s.cfg,
		State:   s.ExternalState(),
		LogPath: confhome.CLILogPath(logts.FormatLogTS()),
	}
}
