package main

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"time"

	sp "charm.land/bubbles/v2/spinner"
	tas "charm.land/bubbles/v2/textarea"
	"charm.land/lipgloss/v2"
	"github.com/justwasm/pigasus/internal/auth"
	"github.com/justwasm/pigasus/internal/backend"
	"github.com/justwasm/pigasus/internal/hooks"
	"github.com/justwasm/pigasus/internal/keybind"
	"github.com/justwasm/pigasus/internal/subagentquestions"
	"github.com/justwasm/pigasus/internal/tools"
)

// newConversationID returns an RFC-4122-style 8-4-4-4-12 hex UUID
// for the current session.
func newConversationID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		ts := uint64(time.Now().UnixNano())
		for i := 0; i < 16; i++ {
			b[i] = byte(ts >> (uint(i) * 4))
		}
	}
	hex := hex.EncodeToString(b[:])
	return hex[0:8] + "-" + hex[8:12] + "-" + hex[12:16] + "-" + hex[16:20] + "-" + hex[20:32]
}

func resolveRunModeDefaults(b backend.Client, email, model, tier, cwd string) (string, string, string, string) {
	if email == "" {
		email = auth.EffectiveUserEmail()
	}
	if email == "" {
		email = "navigaid@gmail.com"
	}
	if b != nil && model == "" {
		if ab, ok := b.(agwBackend); ok {
			if isMiniMax(ab.AgwConfig().ExternalModel) {
				model = "MiniMax M3"
			}
		}
	}
	if isMiniMax(model) {
		model = "MiniMax M3"
	}
	if model == "" {
		model = "Gemini 3.8 Flash (High)"
	}
	if tier == "" {
		tier = "Antigravity Starter Quota"
	}
	if cwd == "" {
		if c, err := os.Getwd(); err == nil {
			cwd = c
		} else {
			cwd = "/"
		}
	}
	return email, model, tier, cwd
}

func newRunModeBase(b backend.Client, email, model, tier, cwd string, altScreen bool) *runMode {
	return &runMode{
		email:                   email,
		model:                   model,
		statusModel:             stripTier(model),
		tier:                    tier,
		cwd:                     cwd,
		backend:                 b,
		altScreen:               altScreen,
		bashRowIdx:              -1,
		taskReg:                 tools.SharedTaskRegistry(),
		taskSubagentManager:     tools.SharedSubagentManager(),
		subAgentQuestions:       subagentquestions.NewManager(false),
		subAgentQuestionsActive: subagentquestions.ParentKey,
		conversationID:          newConversationID(),
		autoScroll:              true,
		rowsCacheDirty:          true,
		pinnedPromptRowIdx:      -1,
		streamingSearch:         true,
		replyStart:              -1,
		liveReplyIdx:            -1,
		activeBtwIndex:          -1,
		atRouter:                DefaultRouter,
		hooks:                   hooks.NewManager(),
		narrationStart:          time.Now(),
		wireSettingsPending:     true,
	}
}

func initRunModeInput(m *runMode) {
	m.input = tas.New()
	m.input.SetVirtualCursor(false)
	m.input.Prompt = inputPrompt
	m.input.SetPromptFunc(2, func(info tas.PromptInfo) string {
		if info.LineNumber == 0 {
			if m.bashMode {
				return "! "
			}
			return "> "
		}
		return "  "
	})
	m.input.Placeholder = ""
	m.input.ShowLineNumbers = false
	m.input.SetWidth(inputWidth)
	m.input.SetHeight(1)
	m.input.MaxHeight = 24
	m.input.CharLimit = 0
	m.input.EndOfBufferCharacter = ' '
	m.input.DynamicHeight = true
	m.input.MinHeight = 1

	promptBlueStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#268BD2"))
	placeholderStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#657B83"))
	st := m.input.Styles()
	st.Focused.Base = lipgloss.NewStyle()
	st.Focused.CursorLine = lipgloss.NewStyle()
	st.Focused.Text = lipgloss.NewStyle()
	st.Focused.Prompt = promptBlueStyle
	st.Focused.Placeholder = placeholderStyle
	st.Focused.EndOfBuffer = lipgloss.NewStyle()
	st.Blurred.Base = lipgloss.NewStyle()
	st.Blurred.Text = lipgloss.NewStyle()
	st.Blurred.Prompt = promptBlueStyle
	st.Blurred.Placeholder = placeholderStyle
	st.Blurred.EndOfBuffer = lipgloss.NewStyle()
	m.input.SetStyles(st)
	m.input.Focus()
	if initScheme := auth.SettingsString("colorScheme"); initScheme != "" {
		m.applyThemeLive(initScheme)
	}
}

func NewRunMode(b backend.Client, email, model, tier, cwd string, altScreen bool) *runMode {
	pickerHidden := pickerHiddenCommands()
	email, model, tier, cwd = resolveRunModeDefaults(b, email, model, tier, cwd)
	m := newRunModeBase(b, email, model, tier, cwd, altScreen)

	if kb, err := keybind.Load(keybind.DefaultPath()); err == nil {
		m.keybindings = kb
	}
	m.startTasksSubscription()
	initRunModeInput(m)

	m.spinner = sp.New(sp.WithSpinner(sp.Dot))
	if len(pickerHidden) > 0 {
		m.picker.SetHiddenCommands(pickerHidden...)
	}
	return m
}
