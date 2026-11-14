package storybook

import (
	"bytes"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/ui/views/announce"
	"github.com/justwasm/pigasus/internal/ui/views/narration"
	"github.com/justwasm/pigasus/internal/ui/views/programstatus"
	"github.com/justwasm/pigasus/internal/ui/views/seasonal"
	"github.com/justwasm/pigasus/internal/ui/views/statusrow"
	"github.com/justwasm/pigasus/internal/ui/views/voice"
)

// RegisterStatusExtendedStories registers additional status, voice, and system overlay stories.
func RegisterStatusExtendedStories(c *Catalog) {
	// 1. VoiceBanner
	c.Register(Story{
		ID:          "status-voice-banner",
		Name:        "VoiceBanner (Dictation Overlay)",
		Category:    CategoryStatus,
		Description: "Top voice dictation banner overlay with animated recording pulse and action hints.",
		UpstreamSym: "jetski/cli/model.VoiceBanner",
		States: []State{
			{ID: "connecting", Name: "Connecting Phase", Description: "Connecting to real-time speech transcription"},
			{ID: "recording", Name: "Active Recording", Description: "Pulsing red recording dot with mm:ss duration counter"},
			{ID: "finishing", Name: "Finishing Up", Description: "Processing speech and confirming final transcription"},
		},
		Factory: func() StoryInstance {
			inst := &voiceBannerStory{width: 80, height: 24}
			inst.SetState(1)
			return inst
		},
	})

	// 2. SeasonalBanner
	c.Register(Story{
		ID:          "status-seasonal-banner",
		Name:        "SeasonalBanner (Themed ASCII Header)",
		Category:    CategoryStatus,
		Description: "Themed ASCII art startup banner with seasonal palette, greeting line, and account badges.",
		UpstreamSym: "jetski/cli/seasonal.Banner",
		States: []State{
			{ID: "halloween-pumpkin", Name: "Halloween Pumpkin", Description: "Spooky season 24-bit orange pumpkin art and greeting"},
			{ID: "halloween-moon", Name: "Halloween Moon", Description: "Eerie moon and nighttime clouds art scene"},
			{ID: "halloween-ghost", Name: "Halloween Ghost", Description: "Floating spooky ghost ASCII/Truecolor art"},
			{ID: "halloween-witch-hat", Name: "Halloween Witch's Hat", Description: "Classic witch hat with glowing purple/orange buckle"},
			{ID: "halloween-bat", Name: "Halloween Bat", Description: "Nocturnal flying bat with orange Halloween palette"},
		},
		Factory: func() StoryInstance {
			inst := &seasonalBannerStory{width: 80, height: 24}
			inst.SetState(0)
			return inst
		},
	})

	// 3. AnnouncementCard
	c.Register(Story{
		ID:          "status-announcement",
		Name:        "AnnouncementCard (System Notices)",
		Category:    CategoryStatus,
		Description: "Top-level announcement notification card with borders, icon, and dismiss hint.",
		UpstreamSym: "jetski/cli/notice.Card",
		States: []State{
			{ID: "update-notice", Name: "Update Available", Description: "New version release notification card"},
			{ID: "feature-notice", Name: "Feature Preview", Description: "Announcement for experimental capability"},
			{ID: "service-notice", Name: "Service Quota Notice", Description: "Service rollover and quota reset notice"},
		},
		Factory: func() StoryInstance {
			inst := &announcementCardStory{width: 80, height: 24}
			inst.SetState(0)
			return inst
		},
	})

	// 4. StatusRow (Single-line Compact Bar)
	c.Register(Story{
		ID:          "status-statusrow",
		Name:        "StatusRow (Compact One-Line Segment Bar)",
		Category:    CategoryStatus,
		Description: "Single-line status bar underneath prompt with dynamic hint segments and model/effort indicators.",
		UpstreamSym: "jetski/cli.StatusRow",
		States: []State{
			{ID: "idle", Name: "Idle Shortcuts Hint", Description: "? for shortcuts hint with model and tasks"},
			{ID: "streaming", Name: "Streaming State", Description: "Esc to cancel hint during generation"},
			{ID: "compacting", Name: "Context Compaction", Description: "Compacting context progress with duration"},
			{ID: "exit-confirm", Name: "Exit Confirmation", Description: "Press Ctrl+C again to exit warning"},
		},
		Factory: func() StoryInstance {
			inst := &statusRowStory{width: 80, height: 24}
			inst.SetState(0)
			return inst
		},
	})

	// 5. Narration
	c.Register(Story{
		ID:          "status-narration",
		Name:        "Narration Feedback (/narration)",
		Category:    CategoryStatus,
		Description: "Voice/audio narration introduction and feedback toggle message display.",
		UpstreamSym: "jetski/cli.Narration",
		States: []State{
			{ID: "intro-text", Name: "Intro Narration Text", Description: "Verbatim or auto-selected introductory narration"},
			{ID: "disabled-notice", Name: "Narration Disabled", Description: "Disabled feedback notice"},
			{ID: "already-disabled", Name: "Already Disabled", Description: "Already disabled notice"},
		},
		Factory: func() StoryInstance {
			inst := &narrationStory{width: 80, height: 24}
			inst.SetState(0)
			return inst
		},
	})

	// 6. ProgramStatus (OSC 7501)
	c.Register(Story{
		ID:          "status-program-status",
		Name:        "ProgramStatus (OSC 7501 Terminal Protocol)",
		Category:    CategoryStatus,
		Description: "Ghostty / WezTerm / Kitty OSC 7501 status badges (Working, Blocked, Done, Error).",
		UpstreamSym: "jetski/cli.ProgramStatus",
		States: []State{
			{ID: "busy-working", Name: "Agent Busy / Working", Description: "Active turn execution badge"},
			{ID: "blocked-permission", Name: "Blocked / Permission", Description: "Awaiting user approval / confirmation badge"},
			{ID: "idle-done", Name: "Idle / Done", Description: "Normal completed terminal status"},
		},
		Factory: func() StoryInstance {
			inst := &programStatusStory{width: 80, height: 24}
			inst.SetState(0)
			return inst
		},
	})
}

// ---------------------------------------------------------------- VoiceBanner Story

type voiceBannerStory struct {
	state  int
	width  int
	height int
}

func (s *voiceBannerStory) SetState(idx int) {
	s.state = idx
}

func (s *voiceBannerStory) SetSize(w, h int) {
	s.width, s.height = w, h
}

func (s *voiceBannerStory) View() string {
	w := s.width
	if w <= 0 {
		w = 80
	}
	switch s.state {
	case 0: // connecting
		return voice.FormatBanner(voice.PhaseConnecting, 0, w)
	case 2: // finishing
		return voice.FormatBanner(voice.PhaseFinishing, 4*time.Second, w)
	default: // recording
		return voice.FormatBanner(voice.PhaseRecording, 23*time.Second, w)
	}
}

func (s *voiceBannerStory) HandleKey(key tea.KeyMsg) (tea.Cmd, bool) {
	return nil, false
}

// ---------------------------------------------------------------- SeasonalBanner Story

type seasonalBannerStory struct {
	state  int
	width  int
	height int
}

func (s *seasonalBannerStory) SetState(idx int) {
	s.state = idx
}

func (s *seasonalBannerStory) SetSize(w, h int) {
	s.width, s.height = w, h
}

func (s *seasonalBannerStory) View() string {
	ev := seasonal.Halloween(2026)
	st := seasonal.State{
		Email:   "gear@example.com",
		Tier:    "Google AI Pro",
		Model:   "Gemini 3.8 Flash (High)",
		Cwd:     "~/GitHub/openagy",
		CLIName: "Pigasus CLI",
		Version: "1.5.5",
	}
	oldArt, oldGreet := seasonal.SetBannerPicks(s.state, s.state%4)
	defer seasonal.SetBannerPicks(oldArt, oldGreet)

	var buf bytes.Buffer
	seasonal.RenderBanner(&buf, ev, st)
	return buf.String()
}

func (s *seasonalBannerStory) HandleKey(key tea.KeyMsg) (tea.Cmd, bool) {
	return nil, false
}

// ---------------------------------------------------------------- AnnouncementCard Story

type announcementCardStory struct {
	state  int
	width  int
	height int
}

func (s *announcementCardStory) SetState(idx int) {
	s.state = idx
}

func (s *announcementCardStory) SetSize(w, h int) {
	s.width, s.height = w, h
}

func (s *announcementCardStory) View() string {
	w := s.width
	if w <= 0 {
		w = 70
	}
	switch s.state {
	case 1: // feature-notice
		return announce.RenderBoxedCard(&announce.Card{
			Title: "Experimental Native Swarm Mode Enabled",
			Body:  "You can now run multiple autonomous agents collaborating in parallel using /swarm.",
			Hint:  "Esc to dismiss · Type /swarm to inspect",
		}, w)
	case 2: // service-notice
		return announce.RenderBoxedCard(&announce.Card{
			Title: "Daily quota resets at 00:00 UTC",
			Body:  "Antigravity Starter Quota and Pro tiers reset together every 24 hours.",
			Hint:  "Esc to dismiss · Send a message to hide",
		}, w)
	default: // update-notice
		return announce.RenderBoxedCard(&announce.Card{
			Title: "Antigravity CLI v1.5.5 is now available",
			Body:  "Features improved CAPTCHA defense guidelines, updated remote agent wire schemas, and bug fixes.",
			Hint:  "Esc to dismiss · Run agy update to upgrade",
		}, w)
	}
}

func (s *announcementCardStory) HandleKey(key tea.KeyMsg) (tea.Cmd, bool) {
	return nil, false
}

// ---------------------------------------------------------------- StatusRow Story

type statusRowStory struct {
	state  int
	width  int
	height int
}

func (s *statusRowStory) SetState(idx int) {
	s.state = idx
}

func (s *statusRowStory) SetSize(w, h int) {
	s.width, s.height = w, h
}

func (s *statusRowStory) View() string {
	w := s.width
	if w <= 0 {
		w = 80
	}
	base := statusrow.State{
		Width:          w,
		CanonicalWidth: 80,
		Model:          "Gemini 3.8 Flash (High)",
		StatusModel:    "Gemini 3.8 Flash",
		Mode:           "auto",
		EffortSuffix:   " · high",
		TaskSuffix:     " · 2 tasks",
		BufferEmpty:    true,
	}

	switch s.state {
	case 1: // streaming
		base.Streaming = true
	case 2: // compacting
		base.Compact = &statusrow.Compact{
			TokensBefore: 184000,
			TokensAfter:  1200000,
			Start:        time.Now().Add(-12 * time.Second),
		}
	case 3: // exit confirm
		base.ExitConfirm = true
	}
	return statusrow.Render(base)
}

func (s *statusRowStory) HandleKey(key tea.KeyMsg) (tea.Cmd, bool) {
	return nil, false
}

// ---------------------------------------------------------------- Narration Story

type narrationStory struct {
	state  int
	width  int
	height int
}

func (s *narrationStory) SetState(idx int) {
	s.state = idx
}

func (s *narrationStory) SetSize(w, h int) {
	s.width, s.height = w, h
}

func (s *narrationStory) View() string {
	switch s.state {
	case 1:
		text, echo, _, _ := narration.Feedback("off", time.Now(), false)
		if text != "" {
			return text
		}
		return echo
	case 2:
		text, echo, _, _ := narration.Feedback("off", time.Now(), true)
		if text != "" {
			return text
		}
		return echo
	default:
		return narration.SelectText("", time.Now())
	}
}

func (s *narrationStory) HandleKey(key tea.KeyMsg) (tea.Cmd, bool) {
	return nil, false
}

// ---------------------------------------------------------------- ProgramStatus Story

type programStatusStory struct {
	state  int
	width  int
	height int
}

func (s *programStatusStory) SetState(idx int) {
	s.state = idx
}

func (s *programStatusStory) SetSize(w, h int) {
	s.width, s.height = w, h
}

func (s *programStatusStory) View() string {
	inputs := programstatus.Inputs{}
	switch s.state {
	case 0: // busy
		inputs.IsAgentBusy = true
	case 1: // blocked
		inputs.PendingPermission = true
	case 2: // idle
		inputs.LastTurnOutcome = tea.ProgramStateDone
	}
	ps := programstatus.Resolve(inputs)
	if ps == nil {
		return "ProgramStatus Protocol: None / Disabled"
	}
	return fmt.Sprintf("OSC 7501 Terminal Tab Report:\n  App:     %s\n  State:   %v\n  Message: %s",
		programstatus.App, ps.State, ps.Message)
}

func (s *programStatusStory) HandleKey(key tea.KeyMsg) (tea.Cmd, bool) {
	return nil, false
}
