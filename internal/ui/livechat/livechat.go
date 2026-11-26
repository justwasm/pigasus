package livechat

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/app/tools/web"
	"github.com/justwasm/pigasus/internal/features/models/catalog"
	"github.com/justwasm/pigasus/internal/infra/livevoice"
	"github.com/justwasm/pigasus/internal/voice"
)

// performWebSearch is the pluggable web search function, defaulting to web.PerformWebSearch.
var performWebSearch = web.PerformWebSearch

// playVoicePreview is the pluggable recorded-sample player, defaulting to
// voice.PlayVoicePreview (which shells out to afplay / paplay / ffplay).
var playVoicePreview = voice.PlayVoicePreview

// ANSI Styling Helpers
const (
	ansiReset   = "\x1b[0m"
	ansiBold    = "\x1b[1m"
	ansiDim     = "\x1b[2m"
	ansiCyan    = "\x1b[36m"
	ansiGreen   = "\x1b[32m"
	ansiYellow  = "\x1b[33m"
	ansiRed     = "\x1b[31m"
	ansiMagenta = "\x1b[35m"
	ansiBlue    = "\x1b[34m"
	ansiGray    = "\x1b[38;5;244m"
)

func liveChatSystemPrompt(ws string) string {
	return fmt.Sprintf(`You are LiveChat, an intelligent voice coding assistant running in workspace: %s.
Key rules:
1. Keep spoken responses short, natural, friendly, and spoken-friendly (1-2 sentences). Do not use markdown tables or raw code blocks in speech.
2. You have real executable tools:
%s
3. NEVER fake running a command or hallucinate web content. Always call the corresponding tool to obtain real ground-truth output.
4. When you receive a [PROACTIVE_EVENT: ...] message, acknowledge it out loud concisely.
5. When a tool or Thinker finishes, summarize the real execution result out loud to the user naturally.`, ws, voice.DirectToolPromptLines())
}

func Run(args []string) error {
	fs := flag.NewFlagSet("livechat", flag.ContinueOnError)
	webFlag := fs.Bool("web", false, "Start embedded LiveChat Web UI server")
	webShortFlag := fs.Bool("w", false, "Alias for --web")
	hostFlag := fs.String("host", "127.0.0.1", "HTTP server host to bind for Web UI (e.g. 127.0.0.1, localhost, 0.0.0.0)")
	portFlag := fs.Int("port", 8090, "HTTP server port to listen on for Web UI")
	noBrowserFlag := fs.Bool("no-browser", false, "Do not automatically open browser on startup in web mode")
	proxyFlag := fs.String("proxy", "", "Optional Pigasus proxy URL (e.g. http://127.0.0.1:8085)")
	webDirFlag := fs.String("web-dir", "", "Custom directory for web assets (overrides embedded assets for local development)")

	apiKeyFlag := fs.String("api-key", "", "Gemini API Key (defaults to credentials in ~/.openagy/config.json or $GEMINI_API_KEY)")
	modelFlag := fs.String("model", "models/gemini-3.8-live", "Gemini Live model name")
	voiceFlag := fs.String("voice", "Fola", "Voice persona (Antigravity: Fola, Arlo, Cleo, Finn, Gero, Lora, Neno, Olin, Tari; Gemini: Aoede, Puck, Kore, Fenrir, Charon, Zephyr, Leda, Orus)")
	workspaceFlag := fs.String("workspace", ".", "Workspace directory to monitor for changes")
	mockFlag := fs.Bool("mock", false, "Use Mock Live Backend for offline testing")
	duplexFlag := fs.Bool("duplex", false, "Enable full duplex mode (headphones only)")
	debugWireFlag := fs.Bool("debug-wire", false, "Show real-time WebSocket and audio frame diagnostics")
	reconnectFlag := fs.Bool("reconnect", true, "Enable automatic reconnection on network disruption")
	maxRetriesFlag := fs.Int("max-retries", 5, "Maximum reconnection attempts on network disruption")
	cooldownFlag := fs.Duration("cooldown", 20*time.Second, "Proactive alert minimum cooldown")
	resumeFlag := fs.String("resume", "", "Resume a saved LiveChat session by ID, prefix, index (e.g. 1), or 'latest'")
	listSessionsFlag := fs.Bool("list-sessions", false, "List all saved LiveChat sessions and exit")
	noPreviewFlag := fs.Bool("no-voice-preview", false, "Do not play a recorded persona clip when the voice switches")
	altBackendFlag := fs.String("alt-backend", "", "Alternative Live API backend endpoint (or env $OPENAGY_VOICE_ALT_BACKEND / $OPENAGY_VOICE_URL)")
	endpointFlag := fs.String("endpoint", "", "Override voice WebSocket endpoint (alias for --alt-backend)")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	absWs, err := filepath.Abs(*workspaceFlag)
	if err != nil {
		absWs = "."
	}

	if *webFlag || *webShortFlag {
		if err := runWebServerWithHost(*hostFlag, *portFlag, *proxyFlag, *noBrowserFlag, *webDirFlag, absWs); err != nil {
			return fmt.Errorf("livechat web server error: %w", err)
		}
		return nil
	}

	if *listSessionsFlag {
		summaries, err := voice.ListLiveSessions(absWs)
		if err != nil {
			return fmt.Errorf("failed to list sessions: %w", err)
		}
		if len(summaries) == 0 {
			summaries, _ = voice.ListLiveSessions()
		}
		if len(summaries) == 0 {
			fmt.Println("No saved LiveChat sessions found.")
			return nil
		}
		fmt.Println("Saved LiveChat Sessions:")
		for i, s := range summaries {
			fmt.Printf("  #%d [%s] · %s · %d turns · %q (%s)\n",
				i+1, s.ID, s.UpdatedAt.Format("2006-01-02 15:04"), s.TurnCount, s.Title, s.Workspace)
		}
		return nil
	}

	var initialSession *voice.LiveSessionData
	if *resumeFlag != "" {
		resumed, err := voice.FindLiveSession(*resumeFlag, absWs)
		if err != nil {
			return fmt.Errorf("failed to resume session %q: %w", *resumeFlag, err)
		}
		initialSession = resumed
		fmt.Printf("%sResuming LiveChat session [%s] (%d turns)...%s\n", ansiGreen, resumed.ID, len(resumed.Turns), ansiReset)
	}

	// 1. Resolve Credentials via shared livevoice pool & cascade
	// (Gemini API key -> OAuth account pool with failover -> alt-backend -> custom endpoint)
	altBackend := strings.TrimSpace(*altBackendFlag)
	if altBackend == "" {
		altBackend = strings.TrimSpace(*endpointFlag)
	}
	if altBackend == "" {
		altBackend = strings.TrimSpace(os.Getenv("OPENAGY_VOICE_ALT_BACKEND"))
	}
	if altBackend == "" {
		altBackend = strings.TrimSpace(os.Getenv("PIGASUS_VOICE_ALT_BACKEND"))
	}
	if altBackend == "" {
		altBackend = strings.TrimSpace(os.Getenv("OPENAGY_VOICE_URL"))
	}

	liveBackend, err := livevoice.ResolveBackend(livevoice.Options{
		Mock:        *mockFlag,
		APIKey:      *apiKeyFlag,
		ForceAPIKey: strings.TrimSpace(*apiKeyFlag) != "" || os.Getenv("OPENAGY_VOICE_FORCE_API_KEY") != "",
		AltBackend:  altBackend,
		ForceAlt:    os.Getenv("OPENAGY_VOICE_FORCE_ALT_BACKEND") != "" || *altBackendFlag != "" || *endpointFlag != "",
	})
	if err != nil {
		fmt.Printf("%s⚠️  No active Google session or Gemini API Key found.%s\n", ansiRed+ansiBold, ansiReset)
		fmt.Println("To connect to Google Voice Live:")
		fmt.Println("  1. Sign in with Google: pigasus login")
		fmt.Println("  2. Or add \"gemini_api_key\" to ~/.openagy/config.json")
		fmt.Println("  3. Or run with Pigasus proxy: OPENAGY_VOICE_ALT_BACKEND=http://127.0.0.1:<port> livechat")
		fmt.Println("  4. Or run with --mock for an interactive offline simulation")
		return err
	}

	// 2. Configure System Prompt for Proactive Voice Agent with Real Tools
	sysPrompt := liveChatSystemPrompt(absWs)

	selectedVoice := *voiceFlag
	if initialSession != nil && initialSession.Voice != "" {
		selectedVoice = initialSession.Voice
	}

	cfg := voice.LiveSessionConfig{
		Voice:        voice.MapVoiceName(selectedVoice),
		Model:        *modelFlag,
		SystemPrompt: sysPrompt,
		SampleRate:   16000,
		DirectTools:  true,
	}

	bridge := &liveChatHandlerBridge{
		workspace: absWs,
	}
	thinkerModel, _ := catalog.ResolveEffectiveModel("")
	thinkerActor, thinkerBridge, thinkerErr := newLiveChatThinker(absWs, thinkerModel, *mockFlag, bridge.thinkerBridgeOptions()...)
	if thinkerErr == nil && thinkerBridge != nil {
		bridge.thinkerBridge = thinkerBridge
	}
	if thinkerActor != nil {
		defer thinkerActor.Close()
	}

	sess := voice.NewNativeLiveSession(liveBackend, bridge)
	sess.SetDuplex(*duplexFlag)
	sess.SetReconnectConfig(voice.ReconnectConfig{
		Enabled:        *reconnectFlag,
		MaxAttempts:    *maxRetriesFlag,
		InitialBackoff: 500 * time.Millisecond,
		MaxBackoff:     8 * time.Second,
		Continuous:     true,
	})
	bridge.sess = sess

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := sess.Start(ctx, cfg); err != nil {
		return fmt.Errorf("failed to start Live session: %w", err)
	}
	defer sess.HangUp()

	watcher := NewProactiveWatcher(absWs, sess, *cooldownFlag)
	watcher.Start()
	defer watcher.Stop()

	// Proactive Welcome Nudge
	go func() {
		time.Sleep(1 * time.Second)
		if initialSession != nil {
			_ = sess.SendText(fmt.Sprintf("[PROACTIVE_EVENT: LiveChat session [%s] resumed with %d previous turns. Concisely welcome back the user in one sentence and say you are ready to continue where we left off.]", initialSession.ID, len(initialSession.Turns)))
		} else {
			_ = sess.SendText("[PROACTIVE_EVENT: Agent session established. Introduce yourself concisely in one spoken sentence: 'Hi, I'm LiveChat, your proactive voice coding assistant. I'm actively monitoring your workspace and ready to help.']")
		}
	}()

	// 3. Launch Bubbletea TUI with Rich Composer, AltScreen and Mouse Wheel Support
	tuiModel := newLiveChatModel(sess, watcher, absWs, selectedVoice, *modelFlag, *debugWireFlag)
	tuiModel.voicePreview = !*noPreviewFlag
	if initialSession != nil {
		tuiModel.LoadSession(initialSession)
		_ = sess.RehydrateResumeContext()
	}
	defer tuiModel.saveSession()

	prog := tea.NewProgram(tuiModel)
	bridge.prog = prog
	watcher.SetProgram(prog)

	if _, err := prog.Run(); err != nil {
		return fmt.Errorf("error running TUI: %w", err)
	}
	return nil
}
