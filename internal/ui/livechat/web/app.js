/**
 * Pigasus Web Demo — Google OAuth & Multimodal LiveChat
 *
 * Supports DUAL-MODE operation:
 * 1. Pure Static Standalone Mode (Zero backend server required! Run from GitHub Pages, static CDN, or local file server).
 *    - In-browser Web Crypto API for PKCE verifier + S256 challenge generation.
 *    - Direct CORS token exchange against https://oauth2.googleapis.com/token.
 *    - In-browser JWT parsing for account identification.
 *    - Direct Gemini AI Studio REST SSE streaming & Native Multimodal Live WebSocket.
 * 2. Backend Bridge Mode (when paired with pigasus livechat --web or pigasus proxy).
 *    - Local CLI token auto-detection (~/.openagy / ~/.gemini).
 *    - Proxy route auto-configuration and CORS bypass.
 */

(function () {
  'use strict';

  // --- Canonical Antigravity Public OAuth Credentials ---
  // The Client ID is standard Google Cloud Code / Antigravity public client.
  // The Client Secret is served dynamically by the livechat server at runtime (/api/config).
  const CLI_CLIENT_ID = '1071006060591-tmhssin2h21lcre235vtolojh4g403ep.apps.googleusercontent.com';
  let dynamicClientSecret = '';
  const REDIRECT_URI = 'https://antigravity.google/oauth-callback';
  const PKCE_SCOPES = [
    'https://www.googleapis.com/auth/cloud-platform',
    'https://www.googleapis.com/auth/userinfo.email',
    'https://www.googleapis.com/auth/userinfo.profile',
    'https://www.googleapis.com/auth/cclog',
    'https://www.googleapis.com/auth/experimentsandconfigs',
    'https://www.googleapis.com/auth/aicode',
    'openid',
  ].join(' ');

  // --- Application State ---
  function loadStoredAccount() {
    try {
      const raw = localStorage.getItem('pigasus_account_doc');
      if (raw) return JSON.parse(raw);
    } catch (_) {}
    return null;
  }

  function deriveSessionTitle(turns) {
    if (!turns || turns.length === 0) return 'New Live Session';
    for (const t of turns) {
      if (t.role === 'user' && t.text) {
        const trimmed = t.text.trim();
        if (trimmed && !trimmed.startsWith('[')) {
          return trimmed.length > 36 ? trimmed.slice(0, 33) + '...' : trimmed;
        }
      }
    }
    return 'Live Session (' + new Date().toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }) + ')';
  }

  function loadStoredLiveTurns() {
    try {
      const raw = localStorage.getItem('pigasus_live_turns');
      if (raw) {
        const parsed = JSON.parse(raw);
        if (Array.isArray(parsed)) {
          return parsed.filter(
            (t) => t && t.text && t.text.trim() !== '...' && !t.text.includes('Voice Message')
          );
        }
      }
    } catch (_) {}
    return [];
  }

  function loadStoredLiveSessions() {
    try {
      const raw = localStorage.getItem('pigasus_live_sessions_v2');
      if (raw) {
        const parsed = JSON.parse(raw);
        if (Array.isArray(parsed) && parsed.length > 0) {
          return parsed;
        }
      }
    } catch (_) {}

    // Migration from single legacy session
    const legacyTurns = loadStoredLiveTurns();
    if (legacyTurns && legacyTurns.length > 0) {
      const legacySession = {
        id: localStorage.getItem('pigasus_live_session_id') || ('live_' + Date.now().toString(36)),
        title: deriveSessionTitle(legacyTurns),
        model: 'models/gemini-3.8-live',
        voice: 'Fola',
        createdAt: legacyTurns[0]?.timestamp || Date.now(),
        updatedAt: legacyTurns[legacyTurns.length - 1]?.timestamp || Date.now(),
        turns: legacyTurns,
      };
      saveStoredLiveSessions([legacySession]);
      return [legacySession];
    }

    const defaultSession = {
      id: 'live_' + Date.now().toString(36) + '_' + Math.random().toString(36).slice(2, 6),
      title: 'New Live Session',
      model: 'models/gemini-3.8-live',
      voice: 'Fola',
      createdAt: Date.now(),
      updatedAt: Date.now(),
      turns: [],
    };
    saveStoredLiveSessions([defaultSession]);
    return [defaultSession];
  }

  function saveStoredLiveSessions(sessions, syncRemote = true) {
    try {
      localStorage.setItem('pigasus_live_sessions_v2', JSON.stringify(sessions));
    } catch (_) {}

    if (syncRemote && state?.hasServer) {
      const active = sessions.find((s) => s.id === state.activeSessionId);
      if (active) {
        syncSessionToServer(active);
      }
    }
  }

  const sessionSyncQueue = new Map();

  async function syncSessionToServer(sess) {
    if (!state?.hasServer || !sess || !sess.id) return;
    const payload = {
      id: sess.id,
      title: sess.title,
      workspace: sess.workspace || '',
      model: sess.model || 'models/gemini-3.8-live',
      voice: sess.voice || 'Fola',
      created_at: new Date(sess.createdAt || Date.now()).toISOString(),
      updated_at: new Date(sess.updatedAt || Date.now()).toISOString(),
      turns: (sess.turns || []).map((t) => ({
        role: t.role === 'agent' ? 'Agent' : 'User',
        content: t.text || t.content || '',
        translation: t.translation || '',
        audio_id: t.audioId || t.audio_id || '',
        audio_sample_rate: t.audioSampleRate || t.audio_sample_rate || 0,
        timestamp: new Date(t.timestamp || Date.now()).toISOString(),
      })),
    };
    const prev = sessionSyncQueue.get(sess.id) || Promise.resolve();
    const next = prev
      .catch(() => {})
      .then(() =>
        fetch('/api/sessions', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(payload),
        })
      )
      .catch(() => {});
    sessionSyncQueue.set(sess.id, next);
    await next;
  }

  async function deleteSessionFromServer(id) {
    if (!state?.hasServer || !id) return;
    try {
      await fetch(`/api/sessions?id=${encodeURIComponent(id)}`, { method: 'DELETE' });
    } catch (_) {}
  }

  const initialSessions = loadStoredLiveSessions();
  let initialActiveId = localStorage.getItem('pigasus_active_session_id') || '';
  if (!initialSessions.some((s) => s.id === initialActiveId)) {
    initialActiveId = initialSessions[0].id;
    localStorage.setItem('pigasus_active_session_id', initialActiveId);
  }
  const currentActiveSession = initialSessions.find((s) => s.id === initialActiveId) || initialSessions[0];

  const storedAcc = loadStoredAccount();

  const state = {
    hasServer: false,
    authMode: localStorage.getItem('pigasus_auth_mode') || 'aggregate',
    oauthToken: (storedAcc?.token?.token?.access_token) || localStorage.getItem('pigasus_oauth_token') || '',
    oauthRefreshToken: (storedAcc?.token?.token?.refresh_token) || localStorage.getItem('pigasus_oauth_refresh_token') || '',
    oauthExpiry: (storedAcc?.token?.token?.expiry) || localStorage.getItem('pigasus_oauth_expiry') || '',
    oauthEmail: (storedAcc?.email) || localStorage.getItem('pigasus_oauth_email') || '',
    oauthPlanTier: (storedAcc?.plan_tier) || localStorage.getItem('pigasus_oauth_plan_tier') || 'Google AI Pro',
    oauthRawCode: (storedAcc?.raw_input) || localStorage.getItem('pigasus_oauth_raw_code') || '',
    apiKey: localStorage.getItem('pigasus_api_key') || '',
    proxyURL: localStorage.getItem('pigasus_proxy_url') || '',
    serverHasOAuth: false,
    serverHasApiKey: false,
    serverAggregateEnabled: false,
    serverPoolCount: 0,
    serverPoolHealthy: 0,
    activeSessionID: null,

    // Live Voice Session State
    liveWS: null,
    audioCtx: null,
    micAudioCtx: null,
    micStream: null,
    micProcessor: null,
    micAnalyser: null,
    micMuted: false,
    audioQueueTime: 0,
    activeAudioSources: [],
    analyser: null,
    animFrameId: null,

    // Audio Replay Storage (in-memory + IndexedDB persistent cache)
    currentTurnAudioChunks: [],
    currentUserTurnAudioChunks: [],
    turnAudioMap: new Map(), // turnId -> { sampleRate, pcmBuffer } | string[]
    activeReplaySource: null,
    activeReplayBtn: null,

    // Real-Time VAD & Audio Buffer State
    isUserSpeaking: false,
    vadSilenceTimer: null,

    // Duplex Mode & Audio Echo Guard
    isFullDuplex: true,
    agentSpeakingUntil: 0,
    activeAuditionAudio: null,
    activeAuditionSample: null,

    // Microphone Hardware & Device Selection
    selectedMicDeviceId: localStorage.getItem('pigasus_mic_device_id') || '',
    micSourceNode: null,
    micConstraints: {
      echoCancellation: localStorage.getItem('pigasus_mic_aec') !== 'false',
      noiseSuppression: localStorage.getItem('pigasus_mic_ns') !== 'false',
      autoGainControl: localStorage.getItem('pigasus_mic_agc') !== 'false',
    },
    micTest: {
      active: false,
      starting: false,
      audioCtx: null,
      stream: null,
      sourceNode: null,
      analyser: null,
      processor: null,
      animFrameId: null,
      recording: false,
      recordedChunks: [],
      recordTimer: null,
      playbackSource: null,
      sampleRate: 0,
    },

    // Multi-Session Management (aligning with ./cmd/livechat)
    sessions: initialSessions,
    activeSessionId: initialActiveId,
    liveSessionID: initialActiveId,
    liveTurns: currentActiveSession.turns ? [...currentActiveSession.turns] : [],
  };

  // --- DOM Elements ---
  const elems = {
    // Header & Global Nav
    navTabs: document.querySelectorAll('.nav-tab'),
    tabPanes: document.querySelectorAll('.tab-pane'),
    appStatus: document.getElementById('app-status'),
    barAuthMode: document.getElementById('bar-auth-mode'),
    barAuthIdentity: document.getElementById('bar-auth-identity'),
    barProxyRoute: document.getElementById('bar-proxy-route'),

    // Auth Tab
    cliAuthAutoBanner: document.getElementById('cli-auth-auto-banner'),
    oauthFallbackDetails: document.getElementById('oauth-fallback-details'),
    sidebarStatusDot: document.querySelector('.user-profile-button .status-indicator-dot'),
    btnStartOAuth: document.getElementById('btn-start-oauth'),
    btnLoadCLIToken: document.getElementById('btn-load-cli-token'),
    authUrlContainer: document.getElementById('auth-url-container'),
    authUrlInput: document.getElementById('auth-url-input'),
    btnCopyUrl: document.getElementById('btn-copy-url'),
    btnOpenUrl: document.getElementById('btn-open-url'),
    codeExchangeForm: document.getElementById('code-exchange-form'),
    authCodeInput: document.getElementById('auth-code-input'),
    btnExchangeCode: document.getElementById('btn-exchange-code'),
    authStatusMsg: document.getElementById('auth-status-msg'),
    oauthTokenDisplay: document.getElementById('oauth-token-display'),
    oauthRefreshTokenDisplay: document.getElementById('oauth-refresh-token-display'),
    oauthRawCodeDisplay: document.getElementById('oauth-raw-code-display'),
    tokenAccountEmail: document.getElementById('token-account-email'),
    tokenPlanTier: document.getElementById('token-plan-tier'),
    tokenExpiryVal: document.getElementById('token-expiry-val'),
    btnCopyToken: document.getElementById('btn-copy-token'),
    btnRefreshToken: document.getElementById('btn-refresh-token'),
    btnUseTokenChat: document.getElementById('btn-use-token-chat'),
    btnClearToken: document.getElementById('btn-clear-token'),
    apiKeyForm: document.getElementById('apikey-form'),
    geminiApiKey: document.getElementById('gemini-api-key'),
    btnUseKeyChat: document.getElementById('btn-use-key-chat'),

    // Chat Tab
    chatAuthSelect: document.getElementById('chat-auth-select'),
    chatModelSelect: document.getElementById('chat-model-select'),
    btnClearChat: document.getElementById('btn-clear-chat'),
    chatMessages: document.getElementById('chat-messages'),
    chatForm: document.getElementById('chat-form'),
    chatInput: document.getElementById('chat-input'),
    chatSendBtn: document.getElementById('chat-send-btn'),

    // Live Tab
    liveStatusPill: document.getElementById('live-status-pill'),
    liveDot: document.getElementById('live-dot'),
    liveStatusText: document.getElementById('live-status-text'),
    liveDuplexState: document.getElementById('live-duplex-state'),
    liveModelSelect: document.getElementById('live-model-select'),
    liveVoiceSelect: document.getElementById('live-voice-select'),
    voicePersonaGrid: document.getElementById('voice-persona-grid'),
    voicePersonaMeta: document.getElementById('voice-persona-meta'),
    auditionSamplesGroup: document.getElementById('audition-samples-group'),
    liveMicSelect: document.getElementById('live-mic-select'),
    micHotplugIndicator: document.getElementById('mic-hotplug-indicator'),
    liveAuthSelect: document.getElementById('live-auth-select'),
    liveSystemPrompt: document.getElementById('live-system-prompt'),
    liveDuplexToggle: document.getElementById('live-duplex-toggle'),
    liveProactiveGreeting: document.getElementById('live-proactive-greeting'),
    liveAutoReconnect: document.getElementById('live-auto-reconnect'),
    btnToggleLive: document.getElementById('btn-toggle-live'),
    btnToggleMic: document.getElementById('btn-toggle-mic'),
    waveformCanvas: document.getElementById('waveform-canvas'),
    audioStateIndicator: document.getElementById('audio-state-indicator'),
    micMeterFill: document.getElementById('mic-meter-fill'),
    micMeterVal: document.getElementById('mic-meter-val'),
    micStatusText: document.getElementById('mic-status-text'),
    liveTranscript: document.getElementById('live-transcript'),
    liveSessionSelect: document.getElementById('live-session-select'),
    btnNewLiveSession: document.getElementById('btn-new-live-session'),
    btnManageSessions: document.getElementById('btn-manage-sessions'),
    sessionCountBadge: document.getElementById('session-count-badge'),
    sessionModal: document.getElementById('session-modal'),
    btnCloseSessionModal: document.getElementById('btn-close-session-modal'),
    sessionSearchInput: document.getElementById('session-search-input'),
    btnModalNewSession: document.getElementById('btn-modal-new-session'),
    sessionListContainer: document.getElementById('session-list-container'),
    btnClearAllSessions: document.getElementById('btn-clear-all-sessions'),
    btnModalDone: document.getElementById('btn-modal-done'),
    modalSessionTotalBadge: document.getElementById('modal-session-total-badge'),
    liveTextForm: document.getElementById('live-text-form'),
    liveTextInput: document.getElementById('live-text-input'),
    btnLiveTextSend: document.getElementById('btn-live-text-send'),

    // Proxy Settings Tab
    proxyConfigForm: document.getElementById('proxy-config-form'),
    cfgProxyUrl: document.getElementById('cfg-proxy-url'),
    btnTestProxy: document.getElementById('btn-test-proxy'),
    proxyTestResult: document.getElementById('proxy-test-result'),

    // Microphone Test & Diagnostics Tab
    btnGotoMicTest: document.getElementById('btn-goto-mic-test'),
    micDiagSecure: document.getElementById('mic-diag-secure'),
    micDiagPermission: document.getElementById('mic-diag-permission'),
    micDiagSampleRate: document.getElementById('mic-diag-samplerate'),
    micDiagTrack: document.getElementById('mic-diag-track'),
    micTestDeviceSelect: document.getElementById('mic-test-device-select'),
    btnMicTestRefresh: document.getElementById('btn-mic-test-refresh'),
    micOptAec: document.getElementById('mic-opt-aec'),
    micOptNs: document.getElementById('mic-opt-ns'),
    micOptAgc: document.getElementById('mic-opt-agc'),
    micTestWaveform: document.getElementById('mic-test-waveform'),
    micTestMeterRole: document.getElementById('mic-test-meter-role'),
    micTestMeterFill: document.getElementById('mic-test-meter-fill'),
    micTestLevelVal: document.getElementById('mic-test-level-val'),
    micTestDbVal: document.getElementById('mic-test-db-val'),
    micTestSignalBadge: document.getElementById('mic-test-signal-badge'),
    btnMicTestToggle: document.getElementById('btn-mic-test-toggle'),
    btnMicTestRecord: document.getElementById('btn-mic-test-record'),
    btnMicTestTone: document.getElementById('btn-mic-test-tone'),
    micTestStatus: document.getElementById('mic-test-status'),
    micTestTrackDetails: document.getElementById('mic-test-track-details'),

    // Modern Sidebar & Layout
    chatSidebar: document.getElementById('chat-sidebar'),
    btnToggleSidebar: document.getElementById('btn-toggle-sidebar'),
    btnHeaderNewChat: document.getElementById('btn-header-new-chat'),
    btnCloseSidebar: document.getElementById('btn-close-sidebar'),
    sidebarOverlay: document.getElementById('sidebar-overlay'),
    sidebarBtnNewChat: document.getElementById('sidebar-btn-new-chat'),
    sidebarSessionSearch: document.getElementById('sidebar-session-search'),
    sidebarSessionList: document.getElementById('sidebar-session-list'),
    headerSessionTitle: document.getElementById('header-session-title'),
    btnHeaderRename: document.getElementById('btn-header-rename'),
    btnOpenSettings: document.getElementById('btn-open-settings'),
    btnOpenSettingsTop: document.getElementById('btn-open-settings-top'),
    settingsModal: document.getElementById('settings-modal'),
    btnCloseSettingsModal: document.getElementById('btn-close-settings-modal'),
    settingsNavItems: document.querySelectorAll('.settings-nav-item'),
    settingsPanes: document.querySelectorAll('.settings-pane'),
    liveVisualizerBar: document.getElementById('live-visualizer-bar'),
    btnScrollBottom: document.getElementById('btn-scroll-bottom'),
    btnThemeToggle: document.getElementById('btn-theme-toggle'),
    themeToggleIcon: document.getElementById('theme-toggle-icon'),
    composerModelSelect: document.getElementById('composer-model-select'),
    composerModelLabel: document.getElementById('composer-model-label'),
    composerDuplexDot: document.getElementById('composer-duplex-dot'),
    btnToggleBargein: document.getElementById('btn-toggle-bargein'),
    bargeinBadgeIcon: document.getElementById('bargein-badge-icon'),
    bargeinBadgeText: document.getElementById('bargein-badge-text'),
    bargeinStatePill: document.getElementById('bargein-state-pill'),
    btnComposerVoice: document.getElementById('btn-composer-voice'),
    composerVoiceLabel: document.getElementById('composer-voice-label'),
    composerAudioPopover: document.getElementById('composer-audio-popover'),
    popoverOptAec: document.getElementById('popover-opt-aec'),
    popoverOptNs: document.getElementById('popover-opt-ns'),
    popoverOptAgc: document.getElementById('popover-opt-agc'),
    popoverAecBadge: document.getElementById('popover-aec-badge'),
    btnPopoverMicTest: document.getElementById('btn-popover-mic-test'),
    composerToolbar: document.querySelector('.composer-toolbar-bottom'),
  };

  let serverDetectPromise = null;

  // --- Initialization ---
  async function init() {
    setupThemeManager();
    setupFunctionalToolbar();
    setupTabNavigation();
    setupAuthListeners();
    setupChatListeners();
    setupLiveListeners();
    setupAuditionListeners();
    setupMicrophoneManager();
    setupMicTestSection();
    setupProxySettingsListeners();
    setupModernSidebar();
    setupModernSettingsModal();
    setupComposerAutoResize();
    setupScrollToBottom();
    updateSessionSelector();
    updateSessionCountBadge();
    setupSessionListeners();
    syncUIWithState();
    renderStoredTranscript();
    if (elems.btnLiveTextSend) {
      elems.btnLiveTextSend.disabled = false;
    }

    // Resume suspended Web Audio contexts when returning to tab on Android Chrome
    document.addEventListener('visibilitychange', () => {
      if (document.visibilityState === 'visible') {
        if (state.audioCtx && state.audioCtx.state === 'suspended') {
          state.audioCtx.resume().catch(() => {});
        }
        if (state.micAudioCtx && state.micAudioCtx.state === 'suspended') {
          state.micAudioCtx.resume().catch(() => {});
        }
      }
    });

    // Check if running alongside backend server (pigasus livechat --web) or as pure static website
    serverDetectPromise = detectServerCapabilities();
    await serverDetectPromise;
  }

  // --- Server Capabilities & Mode Detection ---
  async function detectServerCapabilities() {
    for (let attempt = 0; attempt < 2; attempt++) {
      try {
        const controller = new AbortController();
        const timer = setTimeout(() => controller.abort(), 4000);

        const resp = await fetch('/api/config', { signal: controller.signal });
        clearTimeout(timer);

        if (resp.ok) {
          state.hasServer = true;
          const cfg = await resp.json();
          state.serverHasOAuth = !!cfg.has_oauth_token;
          state.serverHasApiKey = !!cfg.has_api_key;
          state.serverAggregateEnabled = !!cfg.aggregate_enabled;
          state.serverPoolCount = Number(cfg.pool_account_count || 0);
          state.serverPoolHealthy = Number(cfg.pool_healthy_count || 0);
          if (cfg.client_secret) {
            dynamicClientSecret = cfg.client_secret;
          }
          if (cfg.default_proxy && !localStorage.getItem('pigasus_proxy_url')) {
            state.proxyURL = cfg.default_proxy;
            if (elems.cfgProxyUrl) elems.cfgProxyUrl.value = cfg.default_proxy;
          }
          const savedMode = localStorage.getItem('pigasus_auth_mode');
          const explicitMode = localStorage.getItem('pigasus_auth_mode_explicit') === '1';
          if (state.serverAggregateEnabled && (!savedMode || (!explicitMode && savedMode === 'oauth'))) {
            state.authMode = 'aggregate';
            localStorage.setItem('pigasus_auth_mode', 'aggregate');
          }
          if (cfg.has_oauth_token && localStorage.getItem('pigasus_oauth_manual_clear') !== '1') {
            const isCliSynced = !state.oauthToken ||
              (state.oauthRawCode && (state.oauthRawCode.startsWith('Auto-loaded') || state.oauthRawCode.startsWith('Loaded from local Pigasus')));
            const expMs = state.oauthExpiry ? new Date(state.oauthExpiry).getTime() : 0;
            const isExpiringOrExpired = !expMs || Number.isNaN(expMs) || expMs <= Date.now() + 120000;
            const missingEmail = !state.oauthEmail && !!cfg.oauth_email;

            if (isCliSynced || isExpiringOrExpired || missingEmail) {
              try {
                const tokRes = await fetch('/api/auth/existing');
                if (tokRes.ok) {
                  const tokData = await tokRes.json();
                  if (tokData.available && tokData.access_token) {
                    const keepCurrentMode =
                      state.authMode === 'aggregate' ||
                      (savedMode === 'api_key' && (!!state.apiKey || !!cfg.has_api_key)) ||
                      (savedMode === 'proxy' && !!state.proxyURL);
                    saveOAuthAccount(
                      {
                        access_token: tokData.access_token,
                        refresh_token: tokData.refresh_token,
                        expiry: tokData.expiry,
                        email: tokData.email || cfg.oauth_email || '',
                        plan_tier: tokData.plan_tier || 'Google AI Pro (CLI)',
                        raw_input: 'Auto-loaded from local Pigasus configuration',
                      },
                      { preserveAuthMode: keepCurrentMode }
                    );
                  }
                }
              } catch (_) {}
            }
          }
          elems.appStatus.textContent = 'Server Mode';
          elems.appStatus.className = 'badge badge-server';
          elems.btnLoadCLIToken.classList.remove('hidden');
          syncUIWithState();
          await syncSessionsFromServer();
          return;
        }
      } catch (_) {
        // Retry once on transient startup delay
      }
    }

    state.hasServer = false;
    if (state.authMode === 'aggregate') {
      state.authMode = state.apiKey && !state.oauthToken ? 'api_key' : 'oauth';
    }
    elems.appStatus.textContent = 'Static Standalone';
    elems.appStatus.className = 'badge badge-static';
    elems.btnLoadCLIToken.title = 'CLI auto-detection requires pigasus livechat server; paste your token or use Google OAuth login directly.';
    syncUIWithState();
  }

  async function syncSessionsFromServer() {
    if (!state.hasServer) return;
    try {
      const resp = await fetch('/api/sessions');
      if (!resp.ok) return;
      const serverSummaries = await resp.json();
      if (Array.isArray(serverSummaries) && serverSummaries.length > 0) {
        const localMap = new Map(state.sessions.map((s) => [s.id, s]));
        for (const sum of serverSummaries) {
          const existing = localMap.get(sum.id);
          if (existing) {
            existing.title = sum.title || existing.title;
            existing.workspace = sum.workspace || existing.workspace;
            existing.model = sum.model || existing.model;
            existing.voice = sum.voice || existing.voice;
            existing.updatedAt = sum.updated_at ? new Date(sum.updated_at).getTime() : existing.updatedAt;
          } else {
            localMap.set(sum.id, {
              id: sum.id,
              title: sum.title || 'Live Session',
              workspace: sum.workspace || '',
              model: sum.model || 'models/gemini-3.8-live',
              voice: sum.voice || 'Fola',
              createdAt: sum.created_at ? new Date(sum.created_at).getTime() : Date.now(),
              updatedAt: sum.updated_at ? new Date(sum.updated_at).getTime() : Date.now(),
              turns: [],
            });
          }
        }
        state.sessions = Array.from(localMap.values()).sort((a, b) => (b.updatedAt || 0) - (a.updatedAt || 0));
        saveStoredLiveSessions(state.sessions, false);
        updateSessionSelector();
        updateSessionCountBadge();
        renderSidebarSessionList();

        const active = state.sessions.find((s) => s.id === state.activeSessionId);
        if (active && (!active.turns || active.turns.length === 0) && serverSummaries.some((s) => s.id === active.id)) {
          await loadFullSessionFromServer(active.id);
        }
      }
    } catch (err) {
      console.warn('[sessions] server sync error:', err);
    }
  }

  async function loadFullSessionFromServer(id) {
    if (!state.hasServer || !id) return;
    try {
      const resp = await fetch(`/api/sessions?id=${encodeURIComponent(id)}`);
      if (!resp.ok) return;
      const full = await resp.json();
      const sess = state.sessions.find((s) => s.id === id);
      if (sess && full) {
        sess.title = full.title || sess.title;
        sess.model = full.model || sess.model;
        sess.voice = full.voice || sess.voice;
        sess.workspace = full.workspace || sess.workspace;
        if (Array.isArray(full.turns)) {
          sess.turns = full.turns.map((t, idx) => ({
            id: 'turn_' + idx,
            role: (t.role || t.Role || 'user').toLowerCase(),
            text: t.content || t.Content || t.text || '',
            translation: t.translation || t.Translation || '',
            audioId: t.audio_id || t.AudioID || '',
            audioSampleRate: t.audio_sample_rate || t.AudioSampleRate || 0,
            timestamp: t.timestamp ? new Date(t.timestamp).getTime() : Date.now(),
          }));
        }
        saveStoredLiveSessions(state.sessions, false);
        if (state.activeSessionId === id) {
          state.liveTurns = [...sess.turns];
          renderStoredTranscript();
        }
      }
    } catch (_) {}
  }

  // --- Theme Management (Light / Dark / System Auto) ---
  const THEME_STORAGE_KEY = 'pigasus_theme';

  function getSystemTheme() {
    return window.matchMedia && window.matchMedia('(prefers-color-scheme: light)').matches ? 'light' : 'dark';
  }

  function applyTheme(themeChoice) {
    const effective = themeChoice === 'system' ? getSystemTheme() : themeChoice;
    document.documentElement.setAttribute('data-theme', effective);
    document.documentElement.style.colorScheme = effective === 'light' ? 'light' : 'dark';
    const themeMeta = document.querySelector('meta[name="theme-color"]');
    if (themeMeta) {
      themeMeta.setAttribute('content', effective === 'light' ? '#f8fafc' : '#0a0d14');
    }

    if (elems.themeToggleIcon) {
      elems.themeToggleIcon.textContent = effective === 'light' ? '☀️' : '🌙';
    }
    if (elems.btnThemeToggle) {
      elems.btnThemeToggle.title = effective === 'light' ? 'Switch to Dark Mode' : 'Switch to Light Mode';
      elems.btnThemeToggle.setAttribute('aria-label', effective === 'light' ? 'Switch to Dark Mode' : 'Switch to Light Mode');
    }

    // Update settings choices if present
    document.querySelectorAll('.btn-theme-choice').forEach((btn) => {
      btn.classList.toggle('active', btn.dataset.theme === themeChoice);
    });
  }

  function setTheme(themeChoice) {
    localStorage.setItem(THEME_STORAGE_KEY, themeChoice);
    applyTheme(themeChoice);
  }

  function setupThemeManager() {
    const savedTheme = localStorage.getItem(THEME_STORAGE_KEY) || 'dark';
    applyTheme(savedTheme);

    // Watch OS color scheme changes if in 'system' mode
    if (window.matchMedia) {
      window.matchMedia('(prefers-color-scheme: light)').addEventListener('change', () => {
        if (localStorage.getItem(THEME_STORAGE_KEY) === 'system') {
          applyTheme('system');
        }
      });
    }

    elems.btnThemeToggle?.addEventListener('click', (e) => {
      e.stopPropagation();
      const currentEffective = document.documentElement.getAttribute('data-theme') || 'dark';
      const nextTheme = currentEffective === 'light' ? 'dark' : 'light';
      setTheme(nextTheme);
    });

    document.querySelectorAll('.btn-theme-choice').forEach((btn) => {
      btn.addEventListener('click', (e) => {
        e.preventDefault();
        const choice = btn.dataset.theme;
        if (choice) {
          setTheme(choice);
        }
      });
    });
  }

  // --- Functional Composer Controls & Model Switching ---
  function formatShortModelName(modelVal) {
    if (!modelVal) return 'Gemini 3.8 Live';
    if (modelVal.includes('2.0-flash')) return 'Gemini 2.0 Flash';
    if (modelVal.includes('extended-thinking')) return 'Gemini 3.8 Thinking';
    return 'Gemini 3.8 Live';
  }

  function updateUnifiedTriggerTitle() {
    if (!elems.btnComposerVoice) return;
    const modelLabel = elems.composerModelLabel?.textContent || 'Gemini 3.8 Live';
    const voiceLabel = elems.composerVoiceLabel?.textContent || 'Fola';
    const duplexLabel = state.isFullDuplex ? 'Full Duplex' : 'Half Duplex';
    elems.btnComposerVoice.title = `${modelLabel} · ${voiceLabel} (${duplexLabel}) — Click to configure`;
  }

  function syncModelSelection(modelVal, syncRemote = true) {
    if (!modelVal) modelVal = 'models/gemini-3.8-live';

    if (elems.composerModelSelect && elems.composerModelSelect.value !== modelVal) {
      elems.composerModelSelect.value = modelVal;
    }
    if (elems.liveModelSelect && elems.liveModelSelect.value !== modelVal) {
      elems.liveModelSelect.value = modelVal;
    }

    // Update active session model
    const active = state.sessions.find((s) => s.id === state.activeSessionId);
    if (active && active.model !== modelVal) {
      active.model = modelVal;
      saveStoredLiveSessions(state.sessions, syncRemote);
    }
    localStorage.setItem('pigasus_live_model', modelVal);

    const shortName = formatShortModelName(modelVal);
    if (elems.composerModelLabel) {
      elems.composerModelLabel.textContent = shortName;
    }

    // Update Header Pill (if present)
    const isThinking = modelVal.includes('extended-thinking');
    if (elems.headerModelName) {
      elems.headerModelName.textContent = modelVal.includes('2.0-flash') ? 'Gemini 2.0 Flash' : 'Gemini 3.8 Live';
    }
    if (elems.headerThinkBadge) {
      elems.headerThinkBadge.classList.toggle('hidden', !isThinking);
    }
    if (elems.headerModelPill) {
      elems.headerModelPill.title = isThinking
        ? 'Extended Thinking Mode is active (Click to switch to Standard Low-Latency)'
        : 'Standard Low-Latency Mode is active (Click to switch to Extended Thinking)';
    }
    updateUnifiedTriggerTitle();
  }

  function updateBargeinUI(isDuplex) {
    if (elems.btnToggleBargein) {
      elems.btnToggleBargein.classList.toggle('active', isDuplex);
      elems.btnToggleBargein.classList.toggle('inactive', !isDuplex);
      elems.btnToggleBargein.setAttribute('aria-pressed', isDuplex ? 'true' : 'false');
    }
    if (elems.bargeinBadgeIcon) {
      elems.bargeinBadgeIcon.textContent = isDuplex ? '⚡' : '🔇';
    }
    if (elems.bargeinBadgeText) {
      elems.bargeinBadgeText.textContent = isDuplex ? 'Full Duplex · Instant Barge-in' : 'Half Duplex · Push-to-Talk';
    }
    if (elems.bargeinStatePill) {
      elems.bargeinStatePill.textContent = isDuplex ? 'ON' : 'HALF';
    }
    if (elems.composerDuplexDot) {
      elems.composerDuplexDot.classList.toggle('hidden', isDuplex);
    }
    if (elems.liveDuplexState) {
      elems.liveDuplexState.textContent = isDuplex ? 'Full Duplex' : 'Half Duplex';
    }
    if (elems.liveDuplexToggle) {
      elems.liveDuplexToggle.checked = isDuplex;
    }
    state.isFullDuplex = isDuplex;
    updateUnifiedTriggerTitle();
  }

  function updateVoiceLabel(voiceName) {
    if (!voiceName) voiceName = 'Fola';
    if (elems.composerVoiceLabel) {
      elems.composerVoiceLabel.textContent = voiceName;
    }
    if (elems.voicePersonaGrid) {
      const lower = voiceName.toLowerCase();
      elems.voicePersonaGrid.querySelectorAll('.persona-chip[data-voice]').forEach((chip) => {
        const isSelected = (chip.dataset.voice || '').toLowerCase() === lower;
        chip.setAttribute('aria-pressed', isSelected ? 'true' : 'false');
      });
    }
    const selectedOpt = elems.liveVoiceSelect?.options?.[elems.liveVoiceSelect.selectedIndex];
    if (elems.voicePersonaMeta) {
      elems.voicePersonaMeta.textContent = selectedOpt?.dataset?.meta || mapVoiceToGemini(voiceName);
    }
    if (elems.auditionSamplesGroup) {
      const hasPreview = Boolean(selectedOpt?.dataset?.preview);
      elems.auditionSamplesGroup.querySelectorAll('.btn-sample-play[data-sample]').forEach((btn) => {
        const idx = btn.dataset.sample || '1';
        btn.disabled = !hasPreview;
        btn.title = hasPreview
          ? `Play ${voiceName} sample ${idx} of 3`
          : `No recorded sample for ${voiceName}`;
      });
    }
    updateUnifiedTriggerTitle();
  }

  function setupToolbarRovingTabindex(toolbar) {
    if (!toolbar || 'focusGroup' in HTMLElement.prototype) return;
    const getItems = () =>
      Array.from(toolbar.querySelectorAll('button:not(:disabled), select:not(:disabled)')).filter(
        (el) => !el.classList.contains('hidden') && el.offsetParent !== null
      );
    const items = getItems();
    items.forEach((el, idx) => {
      el.tabIndex = idx === 0 ? 0 : -1;
    });
    toolbar.addEventListener('focusin', ({ target }) => {
      const list = getItems();
      if (list.includes(target)) {
        list.forEach((el) => {
          el.tabIndex = el === target ? 0 : -1;
        });
      }
    });
    toolbar.addEventListener('keydown', (e) => {
      const list = getItems();
      const idx = list.indexOf(document.activeElement);
      if (idx === -1) return;
      const rtl = getComputedStyle(toolbar).direction === 'rtl';
      let next = -1;
      if (e.key === 'ArrowRight') {
        next = (idx + (rtl ? -1 : 1) + list.length) % list.length;
      } else if (e.key === 'ArrowLeft') {
        next = (idx + (rtl ? 1 : -1) + list.length) % list.length;
      } else if (e.key === 'Home') {
        next = 0;
      } else if (e.key === 'End') {
        next = list.length - 1;
      }
      if (next !== -1) {
        e.preventDefault();
        list.forEach((el, i) => {
          el.tabIndex = i === next ? 0 : -1;
        });
        list[next].focus();
      }
    });
  }

  function setupFunctionalToolbar() {
    elems.composerModelSelect?.addEventListener('change', (e) => {
      syncModelSelection(e.target.value);
      const optText = e.target.options[e.target.selectedIndex]?.text || e.target.value;
      if (state.liveWS && state.liveWS.readyState === WebSocket.OPEN) {
        appendTranscript('system', `Switched model to ${optText}. Please reconnect to apply to active voice stream.`);
      } else {
        appendTranscript('system', `Active model set to: ${optText}`);
      }
    });

    elems.liveModelSelect?.addEventListener('change', (e) => {
      syncModelSelection(e.target.value);
    });

    elems.btnToggleBargein?.addEventListener('click', (e) => {
      e.stopPropagation();
      updateBargeinUI(!state.isFullDuplex);
      appendTranscript('system', state.isFullDuplex ? 'Instant Barge-in enabled (interrupt anytime).' : 'Half-Duplex enabled (push-to-talk mode).');
    });

    if (elems.composerAudioPopover && elems.btnComposerVoice) {
      const supportsPopover = Object.hasOwn(HTMLElement.prototype, 'popover');
      if (supportsPopover) {
        elems.composerAudioPopover.addEventListener('toggle', (e) => {
          elems.btnComposerVoice.setAttribute('aria-expanded', e.newState === 'open' ? 'true' : 'false');
        });
      } else {
        elems.btnComposerVoice.addEventListener('click', (e) => {
          e.stopPropagation();
          const isOpen = elems.composerAudioPopover.classList.toggle(':popover-open');
          elems.btnComposerVoice.setAttribute('aria-expanded', isOpen ? 'true' : 'false');
        });
        document.addEventListener('click', (e) => {
          if (
            elems.composerAudioPopover.classList.contains(':popover-open') &&
            !elems.composerAudioPopover.contains(e.target) &&
            !elems.btnComposerVoice.contains(e.target)
          ) {
            elems.composerAudioPopover.classList.remove(':popover-open');
            elems.btnComposerVoice.setAttribute('aria-expanded', 'false');
          }
        });
      }
    }

    elems.voicePersonaGrid?.addEventListener('click', (e) => {
      const chip = e.target.closest('.persona-chip[data-voice]');
      if (!chip || !elems.liveVoiceSelect) return;
      const voiceName = chip.dataset.voice;
      if (!voiceName) return;
      if (elems.liveVoiceSelect.value !== voiceName) {
        elems.liveVoiceSelect.value = voiceName;
        elems.liveVoiceSelect.dispatchEvent(new Event('change'));
      }
    });

    elems.liveVoiceSelect?.addEventListener('change', (e) => {
      stopAuditionPlayback();
      const voiceName = e.target.value;
      updateVoiceLabel(voiceName);
      localStorage.setItem('pigasus_live_voice', voiceName);
      const active = state.sessions.find((s) => s.id === state.activeSessionId);
      if (active && active.voice !== voiceName) {
        active.voice = voiceName;
        saveStoredLiveSessions(state.sessions, true);
      }
    });

    setupToolbarRovingTabindex(elems.composerToolbar);
  }

  // --- Tab Navigation ---
  function setupTabNavigation() {
    elems.navTabs.forEach((tab) => {
      tab.addEventListener('click', () => {
        const targetId = tab.dataset.tab;
        elems.navTabs.forEach((t) => t.classList.remove('active'));
        elems.tabPanes.forEach((p) => p.classList.remove('active'));

        tab.classList.add('active');
        const targetPane = document.getElementById(targetId);
        if (targetPane) {
          targetPane.classList.add('active');
        }
      });
    });
  }

  // --- UI State Sync ---
  function syncUIWithState() {
    // Top bar
    elems.barAuthMode.textContent =
      state.authMode === 'aggregate'
        ? 'Aggregate Pool'
        : state.authMode === 'oauth'
        ? 'Google OAuth'
        : state.authMode === 'api_key'
        ? 'Gemini API Key'
        : 'Pigasus Proxy';

    if (state.authMode === 'aggregate') {
      if (state.serverPoolCount > 0) {
        elems.barAuthIdentity.textContent = `Backend Auto-Routing (${state.serverPoolHealthy}/${state.serverPoolCount})`;
      } else {
        elems.barAuthIdentity.textContent = 'Backend Auto-Routing';
      }
    } else if (state.authMode === 'oauth' && state.oauthEmail) {
      elems.barAuthIdentity.textContent = state.oauthEmail;
    } else if (state.authMode === 'oauth' && state.oauthToken) {
      elems.barAuthIdentity.textContent = 'CLI OAuth Token (Active)';
    } else if (state.authMode === 'oauth' && state.hasServer && state.serverHasOAuth) {
      elems.barAuthIdentity.textContent = 'CLI OAuth (Server Auto)';
    } else if (state.authMode === 'api_key' && state.apiKey) {
      elems.barAuthIdentity.textContent = 'API Key (' + state.apiKey.slice(0, 6) + '...)';
    } else if (state.authMode === 'api_key' && state.hasServer && state.serverHasApiKey) {
      elems.barAuthIdentity.textContent = 'Server API Key';
    } else if (state.authMode === 'proxy') {
      elems.barAuthIdentity.textContent = state.proxyURL ? 'Proxy Mode' : 'Proxy (No URL)';
    } else {
      elems.barAuthIdentity.textContent = 'Not Signed In';
    }

    if (elems.sidebarStatusDot) {
      const hasActiveCreds =
        (state.authMode === 'aggregate' && state.hasServer && (state.serverAggregateEnabled || state.serverHasOAuth || state.serverHasApiKey)) ||
        (state.authMode === 'oauth' && (!!state.oauthToken || (state.hasServer && state.serverHasOAuth))) ||
        (state.authMode === 'api_key' && (!!state.apiKey || (state.hasServer && state.serverHasApiKey))) ||
        (state.authMode === 'proxy' && !!state.proxyURL);
      elems.sidebarStatusDot.classList.toggle('online', hasActiveCreds);
      elems.sidebarStatusDot.classList.toggle('offline', !hasActiveCreds);
    }

    elems.barProxyRoute.textContent = state.proxyURL || (state.hasServer ? 'Direct Server Bridge' : 'Direct Browser');

    // Token Card
    elems.oauthTokenDisplay.value = state.oauthToken || '';
    if (elems.oauthRefreshTokenDisplay) {
      elems.oauthRefreshTokenDisplay.value = state.oauthRefreshToken || '';
    }
    if (elems.oauthRawCodeDisplay) {
      elems.oauthRawCodeDisplay.value = state.oauthRawCode || '';
    }
    if (elems.tokenPlanTier) {
      elems.tokenPlanTier.textContent = state.oauthPlanTier || 'Consumer OAuth';
    }

    elems.tokenAccountEmail.textContent = state.oauthEmail || (state.oauthToken ? 'CLI Active Account' : 'None');

    if (elems.btnUseTokenChat) {
      const isOAuthActive = state.authMode === 'oauth' && (!!state.oauthToken || (state.hasServer && state.serverHasOAuth));
      elems.btnUseTokenChat.textContent = isOAuthActive ? '✓ Active in LiveChat' : 'Apply to LiveChat';
      elems.btnUseTokenChat.disabled = isOAuthActive;
    }

    if (elems.cliAuthAutoBanner) {
      if (state.authMode === 'aggregate' && state.hasServer) {
        const poolDesc = state.serverPoolCount > 0
          ? `${state.serverPoolHealthy}/${state.serverPoolCount} healthy pool accounts`
          : 'backend credentials';
        elems.cliAuthAutoBanner.textContent = `✅ Backend Aggregate Mode active (${poolDesc}, quota-aware auto-failover). Frontend requires no API key or manual account selection.`;
        elems.cliAuthAutoBanner.className = 'status-msg-modern success';
      } else if (state.oauthToken) {
        const who = state.oauthEmail || 'local CLI account';
        const isCli = state.oauthRawCode && (state.oauthRawCode.startsWith('Auto-loaded') || state.oauthRawCode.startsWith('Loaded from local Pigasus'));
        if (state.authMode === 'oauth') {
          elems.cliAuthAutoBanner.textContent = isCli
            ? `✅ Automatically loaded & applied CLI auth token (${who}). Ready for LiveChat.`
            : `✅ OAuth token active (${who}). Ready for LiveChat.`;
          elems.cliAuthAutoBanner.className = 'status-msg-modern success';
        } else {
          elems.cliAuthAutoBanner.textContent = `ℹ️ CLI OAuth token loaded (${who}), currently using ${state.authMode === 'api_key' ? 'Gemini API Key' : 'Pigasus Proxy'} mode.`;
          elems.cliAuthAutoBanner.className = 'status-msg-modern info';
        }
      } else if (state.hasServer && state.serverHasOAuth) {
        elems.cliAuthAutoBanner.textContent = '✅ Server has active CLI OAuth credentials ready. Click "Re-Sync CLI Token" to inspect or refresh.';
        elems.cliAuthAutoBanner.className = 'status-msg-modern success';
      } else if (state.hasServer) {
        elems.cliAuthAutoBanner.textContent = 'ℹ️ No active CLI token found in ~/.openagy or ~/.gemini. Run `pigasus login` or use Google OAuth 2.0 PKCE below.';
        elems.cliAuthAutoBanner.className = 'status-msg-modern info';
      } else {
        elems.cliAuthAutoBanner.textContent = 'ℹ️ Static Standalone mode: sign in with Google OAuth 2.0 PKCE below or configure a Gemini API Key.';
        elems.cliAuthAutoBanner.className = 'status-msg-modern info';
      }
    }

    if (elems.oauthFallbackDetails && !elems.oauthFallbackDetails.dataset.userToggled) {
      elems.oauthFallbackDetails.open = !state.oauthToken && !(state.hasServer && state.serverHasOAuth);
    }

    if (state.oauthExpiry) {
      const expDate = new Date(state.oauthExpiry);
      const diffMs = expDate.getTime() - Date.now();
      if (diffMs > 0) {
        const mins = Math.round(diffMs / 60000);
        elems.tokenExpiryVal.textContent = `Valid (~${mins}m left, expires ${expDate.toLocaleTimeString()})`;
        elems.tokenExpiryVal.style.color = '#34d399';
      } else {
        elems.tokenExpiryVal.textContent = `Expired (${expDate.toLocaleTimeString()}) - Click "Refresh Now"`;
        elems.tokenExpiryVal.style.color = '#ef4444';
      }
    } else {
      elems.tokenExpiryVal.textContent = '-';
      elems.tokenExpiryVal.style.color = 'inherit';
    }

    // API Key input
    elems.geminiApiKey.value = state.apiKey || '';

    // Proxy setting
    elems.cfgProxyUrl.value = state.proxyURL || '';

    // Select boxes sync
    elems.chatAuthSelect.value = state.authMode;
    elems.liveAuthSelect.value = state.authMode;

    const defaultAuthRadio = document.querySelector(`input[name="default_auth"][value="${state.authMode}"]`);
    if (defaultAuthRadio) {
      defaultAuthRadio.checked = true;
    }

    // Model & Functional Toolbar sync
    const active = state.sessions.find((s) => s.id === state.activeSessionId);
    const activeModel = active?.model || localStorage.getItem('pigasus_live_model') || 'models/gemini-3.8-live';
    syncModelSelection(activeModel, false);
    updateBargeinUI(state.isFullDuplex);
    const activeVoice = active?.voice || localStorage.getItem('pigasus_live_voice') || (elems.liveVoiceSelect ? elems.liveVoiceSelect.value : 'Fola');
    if (elems.liveVoiceSelect && activeVoice) {
      const hasOpt = Array.from(elems.liveVoiceSelect.options).some((o) => o.value === activeVoice);
      if (hasOpt) {
        elems.liveVoiceSelect.value = activeVoice;
      }
    }
    updateVoiceLabel(elems.liveVoiceSelect ? elems.liveVoiceSelect.value : activeVoice);
  }

  // --- Pure Client-Side Web Crypto PKCE Helpers ---
  function base64UrlEncode(buffer) {
    const bytes = new Uint8Array(buffer);
    let binary = '';
    for (let i = 0; i < bytes.byteLength; i++) {
      binary += String.fromCharCode(bytes[i]);
    }
    return window
      .btoa(binary)
      .replace(/\+/g, '-')
      .replace(/\//g, '_')
      .replace(/=+$/, '');
  }

  function sanitizeCode(raw) {
    raw = (raw || '').trim();
    if (raw.includes('code=')) {
      try {
        const u = new URL(raw);
        if (u.searchParams.get('code')) {
          return u.searchParams.get('code');
        }
      } catch (_) {}
      const idx = raw.indexOf('code=');
      let sub = raw.slice(idx + 5);
      const amp = sub.indexOf('&');
      if (amp !== -1) {
        sub = sub.slice(0, amp);
      }
      try {
        return decodeURIComponent(sub);
      } catch (_) {
        return sub;
      }
    }
    return raw;
  }

  async function generatePKCEClientSide() {
    if (!window.crypto || !window.crypto.subtle) {
      throw new Error('Web Crypto API requires HTTPS or localhost/127.0.0.1 in this browser.');
    }
    const verifierBytes = new Uint8Array(32);
    window.crypto.getRandomValues(verifierBytes);
    const verifier = base64UrlEncode(verifierBytes);

    const encoder = new TextEncoder();
    const data = encoder.encode(verifier);
    const digest = await window.crypto.subtle.digest('SHA-256', data);
    const challenge = base64UrlEncode(digest);

    const stateBytes = new Uint8Array(16);
    window.crypto.getRandomValues(stateBytes);
    const stateVal = base64UrlEncode(stateBytes);

    const params = new URLSearchParams({
      access_type: 'offline',
      client_id: CLI_CLIENT_ID,
      code_challenge: challenge,
      code_challenge_method: 'S256',
      prompt: 'consent',
      redirect_uri: REDIRECT_URI,
      response_type: 'code',
      scope: PKCE_SCOPES,
      state: stateVal,
    });

    const authURL = `https://accounts.google.com/o/oauth2/auth?${params.toString()}`;
    return { verifier, challenge, state: stateVal, authURL };
  }

  async function exchangeTokenClientSide(rawCode, verifier) {
    const code = sanitizeCode(rawCode);
    if (!code) throw new Error('Empty authorization code');
    if (!verifier) throw new Error('Missing PKCE verifier');

    const params = {
      client_id: CLI_CLIENT_ID,
      code: code,
      code_verifier: verifier,
      grant_type: 'authorization_code',
      redirect_uri: REDIRECT_URI,
    };
    if (dynamicClientSecret) {
      params.client_secret = dynamicClientSecret;
    }
    const body = new URLSearchParams(params);

    // Google OAuth2 Token endpoint natively supports CORS
    const resp = await fetch('https://oauth2.googleapis.com/token', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/x-www-form-urlencoded',
      },
      body: body.toString(),
    });

    const data = await resp.json();
    if (!resp.ok) {
      throw new Error(data.error_description || data.error || 'Token exchange failed');
    }

    let email = '';
    if (data.id_token) {
      try {
        const payloadBase64 = data.id_token.split('.')[1].replace(/-/g, '+').replace(/_/g, '/');
        const decoded = JSON.parse(decodeURIComponent(escape(window.atob(payloadBase64))));
        email = decoded.email || '';
      } catch (_) {}
    }

    return {
      access_token: data.access_token,
      refresh_token: data.refresh_token,
      token_type: data.token_type,
      expires_in: data.expires_in,
      expiry: new Date(Date.now() + (data.expires_in || 3600) * 1000).toISOString(),
      email: email,
    };
  }

  // --- Auth Flow Logic ---
  function setupAuthListeners() {
    // 1. Start OAuth PKCE
    elems.btnStartOAuth.addEventListener('click', async () => {
      showStatusMsg('Generating Google OAuth authorization URL...', 'info');

      // Attempt server endpoint first if available; fall back to client-side Web Crypto
      if (state.hasServer) {
        try {
          const res = await fetch('/api/auth/start', { method: 'POST' });
          if (res.ok) {
            const data = await res.json();
            state.activeSessionID = data.session_id;
            elems.authUrlInput.value = data.auth_url;
            elems.authUrlContainer.classList.remove('hidden');
            showStatusMsg('OAuth URL generated via server. Click "Open ↗" to authenticate.', 'success');
            return;
          }
        } catch (_) {}
      }

      // Pure Client-Side Static Generation
      try {
        const pkce = await generatePKCEClientSide();
        sessionStorage.setItem('pigasus_pkce_verifier', pkce.verifier);
        state.activeSessionID = null;

        elems.authUrlInput.value = pkce.authURL;
        elems.authUrlContainer.classList.remove('hidden');
        showStatusMsg('OAuth URL generated (Client-Side PKCE). Click "Open ↗" to authenticate.', 'success');
      } catch (err) {
        showStatusMsg('Failed to generate OAuth URL: ' + err.message, 'error');
      }
    });

    elems.oauthFallbackDetails?.addEventListener('toggle', () => {
      elems.oauthFallbackDetails.dataset.userToggled = '1';
    });

    // 2. Load Existing CLI Token
    elems.btnLoadCLIToken.addEventListener('click', async () => {
      if (!state.hasServer) {
        showStatusMsg('Local CLI token auto-detection requires pigasus livechat server. Please use Google OAuth 2.0 PKCE below.', 'info');
        return;
      }
      localStorage.removeItem('pigasus_oauth_manual_clear');
      showStatusMsg('Syncing local Pigasus CLI token (~/.openagy / ~/.gemini)...', 'info');
      try {
        const res = await fetch('/api/auth/existing');
        if (!res.ok) throw new Error('Failed to query local token');
        const data = await res.json();
        if (!data.available) {
          showStatusMsg('No active CLI token found. Use Google OAuth 2.0 PKCE below or run `pigasus login`.', 'error');
          return;
        }

        saveOAuthAccount({
          access_token: data.access_token,
          refresh_token: data.refresh_token,
          expiry: data.expiry,
          email: data.email,
          plan_tier: data.plan_tier || 'Google AI Pro (CLI)',
          raw_input: 'Loaded from local Pigasus configuration',
        }, { preserveAuthMode: state.authMode === 'aggregate' });
        showStatusMsg(`Synced CLI token for: ${data.email || 'CLI Active Account'}`, 'success');
      } catch (err) {
        showStatusMsg(err.message, 'error');
      }
    });

    // Copy Auth URL
    elems.btnCopyUrl.addEventListener('click', async () => {
      if (elems.authUrlInput.value) {
        await copyTextSafe(elems.authUrlInput.value);
        elems.btnCopyUrl.textContent = 'Copied!';
        setTimeout(() => (elems.btnCopyUrl.textContent = 'Copy'), 2000);
      }
    });

    // Open Auth URL
    elems.btnOpenUrl.addEventListener('click', () => {
      if (elems.authUrlInput.value) {
        window.open(elems.authUrlInput.value, '_blank', 'noopener,noreferrer');
      }
    });

    // 3. Code Exchange Form
    elems.codeExchangeForm.addEventListener('submit', async (e) => {
      e.preventDefault();
      const rawInput = elems.authCodeInput.value.trim();
      if (!rawInput) return;

      showStatusMsg('Exchanging authorization code for token...', 'info');
      elems.btnExchangeCode.disabled = true;

      try {
        let tokenResult = null;

        // Try server exchange if session active
        if (state.hasServer && state.activeSessionID) {
          try {
            const res = await fetch('/api/auth/exchange', {
              method: 'POST',
              headers: { 'Content-Type': 'application/json' },
              body: JSON.stringify({
                session_id: state.activeSessionID,
                code: rawInput,
              }),
            });
            if (res.ok) {
              tokenResult = await res.json();
            }
          } catch (_) {}
        }

        // Fall back to direct Client-Side CORS Exchange
        if (!tokenResult) {
          const storedVerifier = sessionStorage.getItem('pigasus_pkce_verifier');
          if (!storedVerifier) {
            throw new Error('No PKCE verifier found. Please click "Start Google OAuth Login" first.');
          }
          tokenResult = await exchangeTokenClientSide(rawInput, storedVerifier);
          sessionStorage.removeItem('pigasus_pkce_verifier');
        }

        saveOAuthAccount({
          access_token: tokenResult.access_token,
          refresh_token: tokenResult.refresh_token,
          expiry: tokenResult.expiry,
          email: tokenResult.email,
          raw_input: rawInput,
          plan_tier: 'Google AI Pro (OAuth)',
        });
        elems.authCodeInput.value = '';
        elems.authUrlContainer.classList.add('hidden');
        showStatusMsg(`Authentication successful! Logged in as ${tokenResult.email || 'Google User'}.`, 'success');
      } catch (err) {
        showStatusMsg('Exchange error: ' + err.message, 'error');
      } finally {
        elems.btnExchangeCode.disabled = false;
      }
    });

    // Token Card Actions
    elems.btnCopyToken.addEventListener('click', async () => {
      if (state.oauthToken) {
        await copyTextSafe(state.oauthToken);
        elems.btnCopyToken.textContent = 'Copied!';
        setTimeout(() => (elems.btnCopyToken.textContent = 'Copy Access Token'), 2000);
      }
    });

    if (elems.btnRefreshToken) {
      elems.btnRefreshToken.addEventListener('click', async () => {
        if (!state.oauthRefreshToken) {
          showStatusMsg('No refresh token available to renew credentials.', 'error');
          return;
        }
        showStatusMsg('Renewing OAuth access token...', 'info');
        elems.btnRefreshToken.disabled = true;
        try {
          await refreshOAuthToken();
          showStatusMsg('Access token successfully renewed!', 'success');
        } catch (err) {
          showStatusMsg('Refresh failed: ' + err.message, 'error');
        } finally {
          elems.btnRefreshToken.disabled = false;
        }
      });
    }

    elems.btnUseTokenChat.addEventListener('click', () => {
      if (!state.oauthToken) {
        showStatusMsg('No OAuth token available to apply.', 'error');
        return;
      }
      setAuthMode('oauth');
      showStatusMsg('Applied OAuth token as active credential.', 'success');
    });

    elems.btnClearToken.addEventListener('click', () => {
      localStorage.setItem('pigasus_oauth_manual_clear', '1');
      state.oauthToken = '';
      state.oauthRefreshToken = '';
      state.oauthExpiry = '';
      state.oauthEmail = '';
      state.oauthRawCode = '';
      localStorage.removeItem('pigasus_oauth_token');
      localStorage.removeItem('pigasus_oauth_refresh_token');
      localStorage.removeItem('pigasus_oauth_expiry');
      localStorage.removeItem('pigasus_oauth_email');
      localStorage.removeItem('pigasus_oauth_raw_code');
      localStorage.removeItem('pigasus_oauth_plan_tier');
      localStorage.removeItem('pigasus_account_doc');
      syncUIWithState();
      showStatusMsg('Cleared saved OAuth account and tokens.', 'info');
    });

    // API Key Form
    elems.apiKeyForm.addEventListener('submit', (e) => {
      e.preventDefault();
      const key = elems.geminiApiKey.value.trim();
      if (!key) return;
      state.apiKey = key;
      localStorage.setItem('pigasus_api_key', key);
      syncUIWithState();
      showStatusMsg('Saved Gemini API Key.', 'success');
    });

    elems.btnUseKeyChat.addEventListener('click', () => {
      const key = elems.geminiApiKey.value.trim();
      if (!key) {
        showStatusMsg('Please enter a Gemini API Key first.', 'error');
        return;
      }
      state.apiKey = key;
      localStorage.setItem('pigasus_api_key', key);
      setAuthMode('api_key');
      showStatusMsg('Set Google AI Studio API Key as active credential.', 'success');
    });
  }

  function saveOAuthAccount(acc, opts = {}) {
    if (!acc) return;
    localStorage.removeItem('pigasus_oauth_manual_clear');
    state.oauthToken = acc.access_token || '';
    if (acc.refresh_token) state.oauthRefreshToken = acc.refresh_token;
    state.oauthExpiry = acc.expiry || '';
    if (acc.email) state.oauthEmail = acc.email;
    if (acc.plan_tier) state.oauthPlanTier = acc.plan_tier;
    if (acc.raw_input) state.oauthRawCode = acc.raw_input;
    if (!opts.preserveAuthMode) {
      state.authMode = 'oauth';
      localStorage.setItem('pigasus_auth_mode', 'oauth');
    }

    localStorage.setItem('pigasus_oauth_token', state.oauthToken);
    localStorage.setItem('pigasus_oauth_refresh_token', state.oauthRefreshToken);
    localStorage.setItem('pigasus_oauth_expiry', state.oauthExpiry);
    localStorage.setItem('pigasus_oauth_email', state.oauthEmail);
    localStorage.setItem('pigasus_oauth_raw_code', state.oauthRawCode);
    localStorage.setItem('pigasus_oauth_plan_tier', state.oauthPlanTier);

    // accounts.json compatible persistent object
    const accountDoc = {
      email: state.oauthEmail,
      plan_tier: state.oauthPlanTier,
      raw_input: state.oauthRawCode,
      token: {
        token: {
          access_token: state.oauthToken,
          refresh_token: state.oauthRefreshToken,
          token_type: 'Bearer',
          expiry: state.oauthExpiry,
        },
        auth_method: 'consumer',
        id_token_email: state.oauthEmail,
      },
    };
    localStorage.setItem('pigasus_account_doc', JSON.stringify(accountDoc));

    syncUIWithState();
  }

  async function refreshOAuthToken() {
    const rt = state.oauthRefreshToken;
    if (!rt) {
      throw new Error('No refresh_token found. Please log in again.');
    }

    // 1. Try server refresh route
    if (state.hasServer) {
      try {
        const res = await fetch('/api/auth/refresh', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ refresh_token: rt }),
        });
        if (res.ok) {
          const data = await res.json();
          saveOAuthAccount({
            access_token: data.access_token,
            refresh_token: data.refresh_token || rt,
            expiry: data.expiry,
            email: data.email || state.oauthEmail,
            plan_tier: state.oauthPlanTier,
            raw_input: state.oauthRawCode,
          }, { preserveAuthMode: true });
          return data.access_token;
        }
      } catch (_) {}
    }

    // 2. Direct client-side refresh via Google OAuth2 CORS endpoint
    const params = {
      client_id: CLI_CLIENT_ID,
      grant_type: 'refresh_token',
      refresh_token: rt,
    };
    if (dynamicClientSecret) {
      params.client_secret = dynamicClientSecret;
    }
    const body = new URLSearchParams(params);

    const resp = await fetch('https://oauth2.googleapis.com/token', {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: body.toString(),
    });

    const data = await resp.json();
    if (!resp.ok) {
      throw new Error(data.error_description || data.error || 'Token refresh failed');
    }

    const expiryISO = new Date(Date.now() + (data.expires_in || 3600) * 1000).toISOString();
    saveOAuthAccount({
      access_token: data.access_token,
      refresh_token: data.refresh_token || rt,
      expiry: expiryISO,
      email: state.oauthEmail,
      plan_tier: state.oauthPlanTier,
      raw_input: state.oauthRawCode,
    }, { preserveAuthMode: true });
    return data.access_token;
  }

  async function ensureValidOAuthToken() {
    if (!state.oauthToken && state.oauthRefreshToken) {
      return await refreshOAuthToken();
    }
    if (!state.oauthToken) {
      return '';
    }
    if (state.oauthExpiry) {
      const expTime = new Date(state.oauthExpiry).getTime();
      const now = Date.now();
      // If expiring within 5 minutes and refresh token exists, auto-renew seamlessly
      if (expTime - now < 5 * 60 * 1000 && state.oauthRefreshToken) {
        console.log('[auth] Token near expiration, auto-refreshing...');
        try {
          return await refreshOAuthToken();
        } catch (err) {
          console.warn('[auth] Background refresh failed, continuing with current token:', err);
        }
      }
    }
    return state.oauthToken;
  }

  function setAuthMode(mode, opts = {}) {
    state.authMode = mode;
    localStorage.setItem('pigasus_auth_mode', mode);
    if (opts.explicit !== false) {
      localStorage.setItem('pigasus_auth_mode_explicit', '1');
    }
    syncUIWithState();
  }

  function showStatusMsg(text, type) {
    elems.authStatusMsg.textContent = text;
    elems.authStatusMsg.className = `status-msg ${type}`;
    elems.authStatusMsg.classList.remove('hidden');
  }

  // --- Web Text Chat Logic (Dual-Mode) ---
  function setupChatListeners() {
    elems.chatAuthSelect.addEventListener('change', (e) => {
      setAuthMode(e.target.value);
    });

    elems.btnClearChat.addEventListener('click', () => {
      elems.chatMessages.innerHTML = `
        <div class="chat-msg system">
          <div class="msg-bubble">Chat history cleared.</div>
        </div>`;
    });

    // Complies with Modern Web Guidance (ime-safe-enter-submit):
    // Enter submits message, Shift+Enter inserts newline.
    // Blocks submission when isComposing is true or keyCode is 229 (IME conversion).
    elems.chatInput.addEventListener('keydown', (event) => {
      if (event.key === 'Enter' && !event.shiftKey) {
        event.preventDefault();
        if (event.isComposing || event.keyCode === 229) {
          return;
        }
        elems.chatForm.requestSubmit();
      }
    });

    elems.chatForm.addEventListener('submit', async (e) => {
      e.preventDefault();
      const prompt = elems.chatInput.value.trim();
      if (!prompt) return;

      appendChatMessage('user', prompt);
      elems.chatInput.value = '';
      elems.chatSendBtn.disabled = true;

      const assistantBubble = appendChatMessage('assistant', '');
      assistantBubble.innerHTML = '<span class="chat-placeholder">Thinking...</span>';
      const renderer = createMessageRenderer(assistantBubble);

      try {
        if (serverDetectPromise) {
          await serverDetectPromise;
        }
        // If server mode is available, dispatch through backend bridge
        if (state.hasServer) {
          await executeServerChat(prompt, renderer);
        } else {
          // Pure client-side direct chat
          await executeDirectChat(prompt, renderer);
        }
      } catch (err) {
        renderer.setError(err.message);
      } finally {
        elems.chatSendBtn.disabled = false;
        elems.chatInput.focus();
      }
    });
  }

  function createMessageRenderer(container) {
    let hasReceivedFirstChunk = false;
    let inThinking = false;
    let thinkStartTime = 0;
    let thinkBlock = null;
    let thinkTitle = null;
    let thinkBadge = null;
    let thinkContent = null;
    let responseContent = null;
    let pendingBuffer = '';

    function ensureResponseContainer() {
      if (!responseContent) {
        responseContent = document.createElement('div');
        responseContent.className = 'response-content';
        container.appendChild(responseContent);
      }
      return responseContent;
    }

    function startThinkingBlock() {
      inThinking = true;
      thinkStartTime = Date.now();

      thinkBlock = document.createElement('details');
      thinkBlock.className = 'think-block thinking';
      thinkBlock.open = true; // Stay expanded while streaming thought

      const summary = document.createElement('summary');
      summary.className = 'think-header';

      const icon = document.createElement('span');
      icon.className = 'think-icon';
      icon.textContent = '💭';

      thinkTitle = document.createElement('span');
      thinkTitle.className = 'think-title';
      thinkTitle.textContent = 'Thinking...';

      thinkBadge = document.createElement('span');
      thinkBadge.className = 'think-badge';
      thinkBadge.textContent = 'Thinking';

      const chevron = document.createElement('span');
      chevron.className = 'think-chevron';
      chevron.textContent = '▶';

      summary.appendChild(icon);
      summary.appendChild(thinkTitle);
      summary.appendChild(thinkBadge);
      summary.appendChild(chevron);

      thinkContent = document.createElement('div');
      thinkContent.className = 'think-content';

      thinkBlock.appendChild(summary);
      thinkBlock.appendChild(thinkContent);

      if (responseContent) {
        container.insertBefore(thinkBlock, responseContent);
      } else {
        container.appendChild(thinkBlock);
      }
    }

    function finishThinkingBlock() {
      if (!inThinking && !thinkBlock) return;
      inThinking = false;

      if (thinkBlock) {
        thinkBlock.classList.remove('thinking');
        const elapsed = ((Date.now() - thinkStartTime) / 1000).toFixed(1);
        if (thinkTitle) {
          thinkTitle.textContent = `Thought for ${elapsed}s`;
        }
        if (thinkBadge) {
          thinkBadge.textContent = 'Completed';
        }
        // Auto-collapse after thinking finishes so answer takes focus
        thinkBlock.open = false;
      }
    }

    function appendToTarget(text, isThink) {
      if (!text) return;
      if (!hasReceivedFirstChunk) {
        hasReceivedFirstChunk = true;
        container.innerHTML = '';
      }

      if (isThink) {
        if (!thinkBlock) startThinkingBlock();
        thinkContent.textContent += text;
        thinkContent.scrollTop = thinkContent.scrollHeight;
      } else {
        const resp = ensureResponseContainer();
        resp.textContent += text;
      }
      if (elems.chatMessages) {
        elems.chatMessages.scrollTop = elems.chatMessages.scrollHeight;
      }
    }

    function processBuffer() {
      while (pendingBuffer.length > 0) {
        if (!inThinking) {
          const thinkOpenIdx = pendingBuffer.indexOf('<think>');
          if (thinkOpenIdx !== -1) {
            const before = pendingBuffer.slice(0, thinkOpenIdx);
            appendToTarget(before, false);
            startThinkingBlock();
            pendingBuffer = pendingBuffer.slice(thinkOpenIdx + 7);
            continue;
          }

          // Check for partial '<think>' prefix at end of buffer
          const targetTag = '<think>';
          let partialMatchLen = 0;
          for (let i = targetTag.length - 1; i >= 1; i--) {
            if (pendingBuffer.endsWith(targetTag.slice(0, i))) {
              partialMatchLen = i;
              break;
            }
          }

          if (partialMatchLen > 0) {
            const safeToFlush = pendingBuffer.slice(0, pendingBuffer.length - partialMatchLen);
            appendToTarget(safeToFlush, false);
            pendingBuffer = pendingBuffer.slice(pendingBuffer.length - partialMatchLen);
            break;
          } else {
            appendToTarget(pendingBuffer, false);
            pendingBuffer = '';
          }
        } else {
          const thinkCloseIdx = pendingBuffer.indexOf('</think>');
          if (thinkCloseIdx !== -1) {
            const before = pendingBuffer.slice(0, thinkCloseIdx);
            appendToTarget(before, true);
            finishThinkingBlock();
            pendingBuffer = pendingBuffer.slice(thinkCloseIdx + 8);
            pendingBuffer = pendingBuffer.replace(/^\n+/, '');
            continue;
          }

          // Check for partial '</think>' prefix at end of buffer
          const targetTag = '</think>';
          let partialMatchLen = 0;
          for (let i = targetTag.length - 1; i >= 1; i--) {
            if (pendingBuffer.endsWith(targetTag.slice(0, i))) {
              partialMatchLen = i;
              break;
            }
          }

          if (partialMatchLen > 0) {
            const safeToFlush = pendingBuffer.slice(0, pendingBuffer.length - partialMatchLen);
            appendToTarget(safeToFlush, true);
            pendingBuffer = pendingBuffer.slice(pendingBuffer.length - partialMatchLen);
            break;
          } else {
            appendToTarget(pendingBuffer, true);
            pendingBuffer = '';
          }
        }
      }
    }

    return {
      appendChunk(chunk) {
        if (!chunk) return;
        pendingBuffer += chunk;
        processBuffer();
      },
      finish() {
        if (pendingBuffer.length > 0) {
          appendToTarget(pendingBuffer, inThinking);
          pendingBuffer = '';
        }
        if (inThinking) {
          finishThinkingBlock();
        }
      },
      setError(errMsg) {
        container.innerHTML = '';
        const errDiv = document.createElement('div');
        errDiv.className = 'chat-error';
        errDiv.textContent = '❌ Error: ' + errMsg;
        errDiv.style.color = '#ef4444';
        container.appendChild(errDiv);
      }
    };
  }

  async function executeServerChat(prompt, renderer) {
    const payload = {
      auth_mode: state.authMode,
      model: elems.chatModelSelect.value,
      prompt: prompt,
      stream: true,
    };
    if (state.authMode === 'oauth') {
      payload.token = state.oauthToken;
    } else if (state.authMode === 'api_key') {
      payload.api_key = state.apiKey;
    } else if (state.authMode === 'proxy') {
      payload.proxy_url = state.proxyURL;
    }

    const res = await fetch('/api/chat', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload),
    });

    if (!res.ok) {
      const err = await res.json();
      throw new Error(err.error || 'Server error HTTP ' + res.status);
    }

    const reader = res.body.getReader();
    const decoder = new TextDecoder();
    let buffer = '';

    while (true) {
      const { done, value } = await reader.read();
      if (done) break;

      buffer += decoder.decode(value, { stream: true });
      const lines = buffer.split('\n');
      buffer = lines.pop();

      for (const line of lines) {
        if (line.startsWith('data: ')) {
          const dataStr = line.slice(6).trim();
          if (!dataStr) continue;
          try {
            const parsed = JSON.parse(dataStr);
            if (parsed.text) {
              renderer.appendChunk(parsed.text);
            }
          } catch (_) {}
        }
      }
    }
    renderer.finish();
  }

  async function executeDirectChat(prompt, renderer) {
    const model = elems.chatModelSelect.value || 'gemini-3.8-flash';

    // 1. Google AI Studio API Key (Direct browser CORS)
    if (state.authMode === 'api_key') {
      if (!state.apiKey) throw new Error('Missing Gemini API Key');
      const cleanModel = model.replace(/^models\//, '');
      const url = `https://generativelanguage.googleapis.com/v1beta/models/${cleanModel}:streamGenerateContent?alt=sse&key=${state.apiKey}`;

      const res = await fetch(url, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          contents: [{ role: 'user', parts: [{ text: prompt }] }],
        }),
      });

      if (!res.ok) {
        const errText = await res.text();
        throw new Error(`Gemini API HTTP ${res.status}: ${errText}`);
      }

      const reader = res.body.getReader();
      const decoder = new TextDecoder();
      let buffer = '';
      let inDirectThought = false;

      while (true) {
        const { done, value } = await reader.read();
        if (done) break;

        buffer += decoder.decode(value, { stream: true });
        const lines = buffer.split('\n');
        buffer = lines.pop();

        for (const line of lines) {
          if (line.startsWith('data: ')) {
            const dataStr = line.slice(6).trim();
            if (!dataStr) continue;
            try {
              const parsed = JSON.parse(dataStr);
              const parts = parsed.candidates?.[0]?.content?.parts;
              if (Array.isArray(parts)) {
                for (const part of parts) {
                  if (part.thought && !inDirectThought) {
                    inDirectThought = true;
                    renderer.appendChunk('<think>');
                  } else if (!part.thought && inDirectThought) {
                    inDirectThought = false;
                    renderer.appendChunk('</think>');
                  }
                  if (part.text) {
                    renderer.appendChunk(part.text);
                  }
                }
              }
            } catch (_) {}
          }
        }
      }
      if (inDirectThought) {
        renderer.appendChunk('</think>');
      }
      renderer.finish();
      return;
    }

    // 2. Local Pigasus Proxy Route (Static Mode)
    if (state.authMode === 'oauth') {
      if (!state.proxyURL) {
        throw new Error('OAuth chat requires the Pigasus LiveChat server (`pigasus livechat --web`) or an explicit Proxy URL in Settings.');
      }
    }
    if (state.authMode === 'proxy' || state.authMode === 'oauth') {
      const proxyBase = (state.proxyURL || '').trim();
      if (!proxyBase) {
        throw new Error('Proxy mode selected, but no Pigasus Proxy URL is configured in Settings.');
      }
      const targetURL = `${proxyBase.replace(/\/$/, '')}/v1internal:streamGenerateContent`;

      const headers = { 'Content-Type': 'application/json' };
      if (state.oauthToken) headers['Authorization'] = 'Bearer ' + state.oauthToken;
      if (state.apiKey) headers['x-goog-api-key'] = state.apiKey;

      const wireModel = (model === 'gemini-3.8-flash') ? 'gemini-3.8-flash-high' : model;
      const res = await fetch(targetURL + '?alt=sse', {
        method: 'POST',
        headers: headers,
        body: JSON.stringify({
          project: 'aicode-consumers',
          model: wireModel,
          requestType: 'agent',
          userAgent: 'antigravity',
          request: {
            contents: [
              {
                role: 'user',
                parts: [{ text: prompt }]
              }
            ],
            generationConfig: {
              thinkingConfig: {
                thinkingLevel: 'high'
              }
            }
          }
        }),
      });

      if (!res.ok) {
        const errText = await res.text();
        throw new Error(`Proxy HTTP ${res.status}: ${errText}`);
      }

      const reader = res.body.getReader();
      const decoder = new TextDecoder();
      let buffer = '';

      while (true) {
        const { done, value } = await reader.read();
        if (done) break;

        buffer += decoder.decode(value, { stream: true });
        const lines = buffer.split('\n');
        buffer = lines.pop();

        for (const line of lines) {
          let trimmed = line.trim();
          if (!trimmed) continue;
          if (trimmed.startsWith('data: ')) {
            trimmed = trimmed.slice(6).trim();
          }
          if (trimmed === '[DONE]') continue;
          try {
            const parsed = JSON.parse(trimmed);
            const candText = parsed.response?.candidates?.[0]?.content?.parts?.[0]?.text;
            if (candText) {
              renderer.appendChunk(candText);
            } else if (parsed.text) {
              renderer.appendChunk(parsed.text);
            }
          } catch (_) {}
        }
      }
      renderer.finish();
      return;
    }

    throw new Error('No valid authentication configured for static chat mode');
  }

  function appendChatMessage(role, text) {
    const msgDiv = document.createElement('div');
    msgDiv.className = `chat-msg ${role}`;
    const bubble = document.createElement('div');
    bubble.className = 'msg-bubble';
    bubble.textContent = text;
    msgDiv.appendChild(bubble);
    elems.chatMessages.appendChild(msgDiv);
    elems.chatMessages.scrollTop = elems.chatMessages.scrollHeight;
    return bubble;
  }

  // --- Multimodal Live Chat Logic (Dual-Mode & Full-Duplex) ---
  let reconnectAttempts = 0;
  const MAX_RECONNECT_ATTEMPTS = 5;
  let reconnectTimer = null;
  let intentionalDisconnect = false;

  let currentTurnSpeaker = null;
  let currentTurnBubble = null;
  let currentTurnEntry = null;

  function ensureBottomAnchor() {
    if (!elems.liveTranscript) return null;
    let anchor = document.getElementById('chat-bottom-anchor');
    if (!anchor) {
      anchor = document.createElement('div');
      anchor.id = 'chat-bottom-anchor';
      anchor.className = 'chat-bottom-anchor';
      anchor.setAttribute('aria-hidden', 'true');
    }
    elems.liveTranscript.appendChild(anchor);
    return anchor;
  }

  function scrollTranscriptToBottom(smooth = false) {
    if (!elems.liveTranscript) return;
    requestAnimationFrame(() => {
      const anchor = ensureBottomAnchor();
      try {
        elems.liveTranscript.scrollTo({
          top: elems.liveTranscript.scrollHeight + 1000,
          behavior: smooth ? 'smooth' : 'auto',
        });
      } catch (_) {
        elems.liveTranscript.scrollTop = elems.liveTranscript.scrollHeight + 1000;
      }
      if (anchor && typeof anchor.scrollIntoView === 'function') {
        try {
          anchor.scrollIntoView({ behavior: smooth ? 'smooth' : 'auto', block: 'end' });
        } catch (_) {}
      }
    });
  }

  function getActiveLiveSession() {
    return state.sessions.find((s) => s.id === state.activeSessionId) || state.sessions[0];
  }

  function saveActiveSessionTurns() {
    const active = getActiveLiveSession();
    if (!active) return;
    active.turns = [...state.liveTurns];
    active.updatedAt = Date.now();
    if (!active.title || active.title === 'New Live Session') {
      active.title = deriveSessionTitle(active.turns);
    }
    saveStoredLiveSessions(state.sessions);
    try {
      localStorage.setItem('pigasus_live_turns', JSON.stringify(active.turns));
    } catch (_) {}
    updateSessionSelector();
    updateSessionCountBadge();
  }

  function updateSessionSelector() {
    const active = state.sessions.find((s) => s.id === state.activeSessionId) || state.sessions[0];
    if (elems.headerSessionTitle && active) {
      elems.headerSessionTitle.textContent = active.title || 'New Live Session';
    }
    if (elems.liveSessionSelect) {
      elems.liveSessionSelect.innerHTML = '';
      state.sessions.forEach((sess) => {
        const opt = document.createElement('option');
        opt.value = sess.id;
        const count = sess.turns ? sess.turns.length : 0;
        const title = sess.title || 'Untitled Session';
        opt.textContent = `${title} (${count} turns)`;
        if (sess.id === state.activeSessionId) {
          opt.selected = true;
        }
        elems.liveSessionSelect.appendChild(opt);
      });
    }
    renderSidebarSessionList(elems.sidebarSessionSearch ? elems.sidebarSessionSearch.value : '');
  }

  function updateSessionCountBadge() {
    const total = state.sessions ? state.sessions.length : 1;
    if (elems.sessionCountBadge) {
      elems.sessionCountBadge.textContent = total;
    }
    if (elems.modalSessionTotalBadge) {
      elems.modalSessionTotalBadge.textContent = total === 1 ? '1 session' : `${total} sessions`;
    }
  }

  function switchLiveSession(sessionId, rehydrate = true) {
    finalizeCurrentTurn();
    const target = state.sessions.find((s) => s.id === sessionId);
    if (!target) return;

    state.activeSessionId = sessionId;
    state.liveSessionID = sessionId;
    localStorage.setItem('pigasus_active_session_id', sessionId);
    localStorage.setItem('pigasus_live_session_id', sessionId);
    state.liveTurns = target.turns ? [...target.turns] : [];
    try {
      localStorage.setItem('pigasus_live_turns', JSON.stringify(state.liveTurns));
    } catch (_) {}

    renderStoredTranscript();
    updateSessionSelector();
    updateSessionCountBadge();
    scrollTranscriptToBottom(true);

    if ((!target.turns || target.turns.length === 0) && state.hasServer) {
      loadFullSessionFromServer(sessionId);
    }

    if (target.model) {
      syncModelSelection(target.model);
    }
    if (target.voice && elems.liveVoiceSelect) {
      elems.liveVoiceSelect.value = target.voice;
      updateVoiceLabel(target.voice);
    }

    // Rehydrate model memory across session switch if live connection is active
    if (rehydrate && state.liveWS && state.liveWS.readyState === WebSocket.OPEN && state.liveTurns.length > 0) {
      appendTranscript('system', `Switched session to "${target.title}". Synchronizing session context with agent...`);
      const historyTranscript = state.liveTurns
        .map((t) => `${t.role === 'user' ? 'User' : 'Agent'}: ${t.text}`)
        .join('\n');
      const rehydratePrompt = `[SYSTEM NOTE: User switched to session "${target.title}". Below is the previous conversation history. Please maintain seamless continuity:\n${historyTranscript}\n]`;
      sendLiveClientText(rehydratePrompt);
    }
  }

  function createNewLiveSession() {
    finalizeCurrentTurn();
    const newSess = {
      id: 'live_' + Date.now().toString(36) + '_' + Math.random().toString(36).slice(2, 6),
      title: 'New Live Session',
      model: elems.liveModelSelect ? elems.liveModelSelect.value : 'models/gemini-3.8-live',
      voice: elems.liveVoiceSelect ? elems.liveVoiceSelect.value : 'Fola',
      createdAt: Date.now(),
      updatedAt: Date.now(),
      turns: [],
    };
    state.sessions.unshift(newSess);
    saveStoredLiveSessions(state.sessions);
    switchLiveSession(newSess.id, false);

    elems.liveTranscript.innerHTML = renderWelcomeHero();
    bindPromptChips();
    appendTranscript('system', 'New LiveChat session initialized.');
    renderSessionModalList(elems.sessionSearchInput ? elems.sessionSearchInput.value : '');
    scrollTranscriptToBottom(true);
  }

  function deleteLiveSession(sessionId) {
    if (state.sessions.length <= 1) {
      const s = state.sessions[0];
      (s.turns || []).forEach((t) => {
        if (t.audioId) state.turnAudioMap.delete(t.audioId);
      });
      deleteSessionAudioFromCache(s.id);
      s.title = 'New Live Session';
      s.turns = [];
      s.updatedAt = Date.now();
      saveStoredLiveSessions(state.sessions);
      switchLiveSession(s.id, false);
      elems.liveTranscript.innerHTML = renderWelcomeHero();
      bindPromptChips();
      renderSessionModalList();
      return;
    }

    const idx = state.sessions.findIndex((s) => s.id === sessionId);
    if (idx === -1) return;

    (state.sessions[idx].turns || []).forEach((t) => {
      if (t.audioId) state.turnAudioMap.delete(t.audioId);
    });
    deleteSessionAudioFromCache(sessionId);
    state.sessions.splice(idx, 1);
    saveStoredLiveSessions(state.sessions);
    deleteSessionFromServer(sessionId);

    if (state.activeSessionId === sessionId) {
      switchLiveSession(state.sessions[0].id, true);
    } else {
      updateSessionSelector();
      updateSessionCountBadge();
    }
    renderSessionModalList(elems.sessionSearchInput ? elems.sessionSearchInput.value : '');
  }

  function renameLiveSession(sessionId, newTitle) {
    const s = state.sessions.find((sess) => sess.id === sessionId);
    if (!s) return;
    const clean = newTitle.trim();
    if (!clean) return;
    s.title = clean;
    s.updatedAt = Date.now();
    saveStoredLiveSessions(state.sessions);
    updateSessionSelector();
    renderSessionModalList(elems.sessionSearchInput ? elems.sessionSearchInput.value : '');
  }

  function clearAllLiveSessions() {
    if (!confirm('Are you sure you want to delete ALL saved LiveChat sessions? This cannot be undone.')) {
      return;
    }
    finalizeCurrentTurn();
    const freshSession = {
      id: 'live_' + Date.now().toString(36),
      title: 'New Live Session',
      model: 'models/gemini-3.8-live',
      voice: 'Fola',
      createdAt: Date.now(),
      updatedAt: Date.now(),
      turns: [],
    };
    state.sessions = [freshSession];
    state.activeSessionId = freshSession.id;
    state.liveTurns = [];
    state.turnAudioMap.clear();
    clearAllAudioFromCache();
    saveStoredLiveSessions(state.sessions);
    localStorage.setItem('pigasus_active_session_id', freshSession.id);
    localStorage.removeItem('pigasus_live_turns');
    elems.liveTranscript.innerHTML = renderWelcomeHero();
    bindPromptChips();
    updateSessionSelector();
    updateSessionCountBadge();
    renderSessionModalList();
    appendTranscript('system', 'All previous LiveChat sessions cleared.');
    scrollTranscriptToBottom(true);
  }

  function renderSessionModalList(filter = '') {
    if (!elems.sessionListContainer) return;
    elems.sessionListContainer.innerHTML = '';

    const cleanFilter = filter.toLowerCase().trim();
    const filtered = state.sessions.filter((s) => {
      if (!cleanFilter) return true;
      if (s.title && s.title.toLowerCase().includes(cleanFilter)) return true;
      if (s.turns && s.turns.some((t) => t.text && t.text.toLowerCase().includes(cleanFilter))) return true;
      return false;
    });

    if (filtered.length === 0) {
      elems.sessionListContainer.innerHTML = `
        <div style="text-align: center; color: var(--text-muted); padding: 2rem 1rem; font-size: 0.875rem;">
          No matching sessions found.
        </div>`;
      return;
    }

    filtered.forEach((sess) => {
      const isActive = sess.id === state.activeSessionId;
      const card = document.createElement('div');
      card.className = `session-item-card ${isActive ? 'active' : ''}`;

      const dateStr = new Date(sess.updatedAt || sess.createdAt).toLocaleString(undefined, {
        month: 'numeric',
        day: 'numeric',
        hour: '2-digit',
        minute: '2-digit',
      });
      const turnCount = sess.turns ? sess.turns.length : 0;

      card.innerHTML = `
        <div class="session-item-main">
          <div class="session-item-header">
            <span class="session-item-title" title="${escapeHTML(sess.title)}">${escapeHTML(sess.title)}</span>
            ${isActive ? '<span class="session-active-indicator">Active</span>' : ''}
          </div>
          <div class="session-item-meta">
            <span>📅 ${dateStr}</span>
            <span>💬 ${turnCount} turns</span>
            <span>🎙️ ${escapeHTML(sess.voice || 'Fola')}</span>
          </div>
        </div>
        <div class="session-item-actions">
          ${!isActive ? `<button type="button" class="btn btn-sm btn-outline btn-session-switch" data-id="${sess.id}">Open</button>` : ''}
          <button type="button" class="btn btn-sm btn-outline btn-session-rename" data-id="${sess.id}" title="Rename Session">✏️</button>
          <button type="button" class="btn btn-sm btn-danger-outline btn-session-delete" data-id="${sess.id}" title="Delete Session">🗑️</button>
        </div>
      `;

      card.querySelector('.btn-session-switch')?.addEventListener('click', () => {
        switchLiveSession(sess.id, true);
        renderSessionModalList(filter);
      });

      card.querySelector('.btn-session-rename')?.addEventListener('click', () => {
        const newTitle = prompt('Rename session to:', sess.title);
        if (newTitle) {
          renameLiveSession(sess.id, newTitle);
        }
      });

      card.querySelector('.btn-session-delete')?.addEventListener('click', () => {
        if (confirm(`Delete session "${sess.title}"?`)) {
          deleteLiveSession(sess.id);
        }
      });

      elems.sessionListContainer.appendChild(card);
    });
  }

  function openSessionModal() {
    if (!elems.sessionModal) return;
    renderSessionModalList(elems.sessionSearchInput ? elems.sessionSearchInput.value : '');
    elems.sessionModal.classList.remove('hidden');
    elems.sessionSearchInput?.focus();
  }

  function closeSessionModal() {
    if (!elems.sessionModal) return;
    elems.sessionModal.classList.add('hidden');
  }

  function setupSessionListeners() {
    elems.liveSessionSelect?.addEventListener('change', (e) => {
      switchLiveSession(e.target.value, true);
    });

    elems.btnManageSessions?.addEventListener('click', openSessionModal);
    elems.btnCloseSessionModal?.addEventListener('click', closeSessionModal);
    elems.btnModalDone?.addEventListener('click', closeSessionModal);
    elems.btnModalNewSession?.addEventListener('click', () => {
      createNewLiveSession();
      closeSessionModal();
    });
    elems.btnClearAllSessions?.addEventListener('click', clearAllLiveSessions);

    elems.sessionModal?.addEventListener('click', (e) => {
      if (e.target === elems.sessionModal) {
        closeSessionModal();
      }
    });

    document.addEventListener('keydown', (e) => {
      if (e.key === 'Escape' && elems.sessionModal && !elems.sessionModal.classList.contains('hidden')) {
        closeSessionModal();
      }
    });

    elems.sessionSearchInput?.addEventListener('input', (e) => {
      renderSessionModalList(e.target.value);
    });
  }

  // --- Modern UI & Welcome Hero Elements ---
  function renderWelcomeHero() {
    return `
      <div class="welcome-hero-container">
        <div class="hero-sparkle-icon">
          <svg width="48" height="48" viewBox="0 0 24 24" fill="none" stroke="url(#hero-gemini-grad)" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
            <defs>
              <linearGradient id="hero-gemini-grad" x1="0%" y1="0%" x2="100%" y2="100%">
                <stop offset="0%" stop-color="#38bdf8" />
                <stop offset="50%" stop-color="#818cf8" />
                <stop offset="100%" stop-color="#34d399" />
              </linearGradient>
            </defs>
            <path d="M12 2v4M12 18v4M4.93 4.93l2.83 2.83M16.24 16.24l2.83 2.83M2 12h4M18 12h4M4.93 19.07l2.83-2.83M16.24 7.76l2.83-2.83"></path>
          </svg>
        </div>
        <h2 class="hero-title">How can I help you today?</h2>
        <p class="hero-subtitle">
          Pigasus Multimodal LiveChat · Real-time bidirectional voice & text streaming with seamless multi-language switching.
        </p>

        <div class="prompt-starter-grid">
          <button type="button" class="prompt-chip" data-prompt="你好！请问你能用中文和我聊天吗？">
            <span class="chip-icon">🌏</span>
            <span class="chip-text">你好！请问你能用中文和我聊天吗？</span>
          </button>
          <button type="button" class="prompt-chip" data-prompt="Let's practice an English conversation for a tech job interview.">
            <span class="chip-icon">💼</span>
            <span class="chip-text">Practice English tech job interview</span>
          </button>
          <button type="button" class="prompt-chip" data-prompt="What is the difference between WebSocket live streaming and standard text generation?">
            <span class="chip-icon">⚡</span>
            <span class="chip-text">Explain live duplex barge-in audio streaming</span>
          </button>
          <button type="button" class="prompt-chip" data-prompt="Write a Python script that calculates Fibonacci numbers efficiently.">
            <span class="chip-icon">💻</span>
            <span class="chip-text">Write a Python Fibonacci algorithm</span>
          </button>
        </div>
      </div>
    `;
  }

  function bindPromptChips() {
    if (!elems.liveTranscript) return;
    elems.liveTranscript.querySelectorAll('.prompt-chip').forEach((chip) => {
      chip.addEventListener('click', () => {
        const prompt = chip.dataset.prompt;
        if (!prompt) return;
        if (elems.liveTextInput) {
          elems.liveTextInput.value = prompt;
          elems.liveTextForm?.requestSubmit();
        }
      });
    });
  }

  function renderSidebarSessionList(filter = '') {
    if (!elems.sidebarSessionList) return;
    elems.sidebarSessionList.innerHTML = '';

    const cleanFilter = filter.toLowerCase().trim();
    const filtered = state.sessions.filter((s) => {
      if (!cleanFilter) return true;
      if (s.title && s.title.toLowerCase().includes(cleanFilter)) return true;
      if (s.turns && s.turns.some((t) => t.text && t.text.toLowerCase().includes(cleanFilter))) return true;
      return false;
    });

    if (filtered.length === 0) {
      elems.sidebarSessionList.innerHTML = `
        <div style="text-align: center; color: var(--text-muted); padding: 1.5rem 0.5rem; font-size: 0.75rem;">
          No conversations found
        </div>`;
      return;
    }

    filtered.forEach((sess) => {
      const isActive = sess.id === state.activeSessionId;
      const item = document.createElement('div');
      item.className = `sidebar-session-item ${isActive ? 'active' : ''}`;
      item.dataset.id = sess.id;

      item.innerHTML = `
        <div class="sidebar-session-item-main" title="${escapeHTML(sess.title)}">
          <span class="session-icon">💬</span>
          <span class="session-title-text">${escapeHTML(sess.title)}</span>
        </div>
        <div class="session-item-hover-actions">
          <button type="button" class="btn-mini-action btn-rename" title="Rename conversation">✏️</button>
          <button type="button" class="btn-mini-action danger btn-delete" title="Delete conversation">🗑️</button>
        </div>
      `;

      item.querySelector('.sidebar-session-item-main')?.addEventListener('click', () => {
        switchLiveSession(sess.id, true);
        if (window.innerWidth <= 860) {
          elems.chatSidebar?.classList.remove('open');
          elems.sidebarOverlay?.classList.remove('active');
        }
      });

      item.querySelector('.btn-rename')?.addEventListener('click', (e) => {
        e.stopPropagation();
        const newTitle = prompt('Rename conversation:', sess.title);
        if (newTitle) {
          renameLiveSession(sess.id, newTitle);
        }
      });

      item.querySelector('.btn-delete')?.addEventListener('click', (e) => {
        e.stopPropagation();
        if (confirm(`Delete conversation "${sess.title}"?`)) {
          deleteLiveSession(sess.id);
        }
      });

      elems.sidebarSessionList.appendChild(item);
    });
  }

  function setupModernSidebar() {
    function isMobile() {
      return window.innerWidth <= 860;
    }

    function openMobileSidebar() {
      elems.chatSidebar?.classList.add('open');
      elems.sidebarOverlay?.classList.add('active');
    }

    function closeMobileSidebar() {
      elems.chatSidebar?.classList.remove('open');
      elems.sidebarOverlay?.classList.remove('active');
    }

    function toggleSidebar() {
      if (isMobile()) {
        if (elems.chatSidebar?.classList.contains('open')) {
          closeMobileSidebar();
        } else {
          openMobileSidebar();
        }
      } else {
        elems.chatSidebar?.classList.toggle('collapsed');
      }
    }

    function collapseOrCloseSidebar() {
      if (isMobile()) {
        closeMobileSidebar();
      } else {
        elems.chatSidebar?.classList.add('collapsed');
      }
    }

    elems.btnToggleSidebar?.addEventListener('click', (e) => {
      e.stopPropagation();
      toggleSidebar();
    });

    elems.btnCloseSidebar?.addEventListener('click', (e) => {
      e.stopPropagation();
      collapseOrCloseSidebar();
    });

    elems.sidebarOverlay?.addEventListener('click', closeMobileSidebar);

    elems.sidebarBtnNewChat?.addEventListener('click', () => {
      createNewLiveSession();
      if (isMobile()) {
        closeMobileSidebar();
      }
    });

    elems.btnHeaderNewChat?.addEventListener('click', () => {
      createNewLiveSession();
    });

    elems.sidebarSessionSearch?.addEventListener('input', (e) => {
      renderSidebarSessionList(e.target.value);
    });

    // Keyboard shortcut Cmd/Ctrl + B to toggle sidebar
    document.addEventListener('keydown', (e) => {
      if ((e.metaKey || e.ctrlKey) && (e.key === 'b' || e.key === 'B')) {
        e.preventDefault();
        toggleSidebar();
      }
    });

    // Keyboard shortcut Cmd/Ctrl + N for new chat
    document.addEventListener('keydown', (e) => {
      if ((e.metaKey || e.ctrlKey) && (e.key === 'n' || e.key === 'N')) {
        e.preventDefault();
        createNewLiveSession();
      }
    });

    // Header session title rename
    function promptRenameActiveSession() {
      const active = state.sessions.find((s) => s.id === state.activeSessionId);
      if (!active) return;
      const newTitle = prompt('Rename conversation:', active.title);
      if (newTitle) {
        renameLiveSession(active.id, newTitle);
      }
    }

    elems.headerSessionTitle?.addEventListener('click', promptRenameActiveSession);
    elems.btnHeaderRename?.addEventListener('click', promptRenameActiveSession);
  }

  function setupModernSettingsModal() {
    function openSettings(targetTabId = 'st-auth-tab') {
      if (!elems.settingsModal) return;
      elems.settingsModal.classList.remove('hidden');
      switchSettingsTab(targetTabId);
      renderSessionModalList(elems.sessionSearchInput ? elems.sessionSearchInput.value : '');
    }

    function closeSettings() {
      if (!elems.settingsModal) return;
      stopMicTest();
      elems.settingsModal.classList.add('hidden');
    }

    function switchSettingsTab(targetTabId) {
      elems.settingsNavItems?.forEach((item) => {
        item.classList.toggle('active', item.dataset.tab === targetTabId);
      });
      elems.settingsPanes?.forEach((pane) => {
        pane.classList.toggle('active', pane.id === targetTabId);
      });
      if (targetTabId === 'st-mic-tab') {
        refreshMicDiagnostics();
        updateMicrophoneList();
      }
    }

    elems.btnOpenSettings?.addEventListener('click', () => openSettings('st-auth-tab'));
    elems.btnOpenSettingsTop?.addEventListener('click', () => openSettings('st-voice-tab'));
    elems.btnGotoMicTest?.addEventListener('click', () => switchSettingsTab('st-mic-tab'));
    elems.btnPopoverMicTest?.addEventListener('click', () => {
      if (elems.composerAudioPopover) {
        if (typeof elems.composerAudioPopover.hidePopover === 'function') {
          try { elems.composerAudioPopover.hidePopover(); } catch (_) {}
        } else {
          elems.composerAudioPopover.classList.remove(':popover-open');
        }
      }
      openSettings('st-mic-tab');
    });
    elems.btnCloseSettingsModal?.addEventListener('click', closeSettings);
    elems.btnModalDone?.addEventListener('click', closeSettings);

    elems.settingsNavItems?.forEach((item) => {
      item.addEventListener('click', () => {
        switchSettingsTab(item.dataset.tab);
      });
    });

    elems.settingsModal?.addEventListener('click', (e) => {
      if (e.target === elems.settingsModal) {
        closeSettings();
      }
    });

    document.addEventListener('keydown', (e) => {
      if (e.key === 'Escape' && elems.settingsModal && !elems.settingsModal.classList.contains('hidden')) {
        closeSettings();
      }
    });
  }

  function setupComposerAutoResize() {
    if (!elems.liveTextInput) return;
    elems.liveTextInput.addEventListener('input', () => {
      elems.liveTextInput.style.height = 'auto';
      elems.liveTextInput.style.height = Math.min(elems.liveTextInput.scrollHeight, 180) + 'px';
    });
  }

  function setupScrollToBottom() {
    if (!elems.liveTranscript || !elems.btnScrollBottom) return;

    elems.liveTranscript.addEventListener('scroll', () => {
      const distFromBottom = elems.liveTranscript.scrollHeight - elems.liveTranscript.scrollTop - elems.liveTranscript.clientHeight;
      if (distFromBottom > 220) {
        elems.btnScrollBottom.classList.remove('hidden');
      } else {
        elems.btnScrollBottom.classList.add('hidden');
      }
    });

    elems.btnScrollBottom.addEventListener('click', () => {
      scrollTranscriptToBottom(true);
    });
  }

  function recordLiveTurn(role, text) {
    if (!text) return null;
    const trimmed = text.trim();
    if (
      !trimmed ||
      trimmed === '...' ||
      trimmed.includes('Voice Message') ||
      trimmed.startsWith('[SYSTEM NOTE:') ||
      trimmed.startsWith('[PROACTIVE_EVENT:')
    ) {
      return null;
    }

    // Avoid duplicate entries
    const last = state.liveTurns[state.liveTurns.length - 1];
    if (last && last.role === role && last.text === trimmed) return last;

    const turnObj = { role, text: trimmed, timestamp: Date.now() };
    state.liveTurns.push(turnObj);
    if (state.liveTurns.length > 50) {
      const evicted = state.liveTurns.slice(0, state.liveTurns.length - 50);
      state.liveTurns = state.liveTurns.slice(-50);
      evicted.forEach((t) => {
        if (t.audioId) {
          state.turnAudioMap.delete(t.audioId);
          deleteTurnAudioFromCache(t.audioId);
        }
      });
    }
    saveActiveSessionTurns();
    scrollTranscriptToBottom(true);
    return turnObj;
  }

  function stripThinkTags(text) {
    if (!text) return '';
    return text
      .replace(/<think>[\s\S]*?<\/think>/g, '')
      .replace(/<think>[\s\S]*$/g, '')
      .trim();
  }

  function extractTranslatableText(bubble) {
    if (!bubble) return '';
    const raw = bubble.dataset.rawText || bubble.querySelector('.text-content')?.textContent || '';
    return stripThinkTags(raw);
  }

  async function translateTextWithGeminiFlash(sourceText, onProgress) {
    const prompt = `Translate the following message into natural, accurate English. Output ONLY the English translation without explanations, quotes, or extra commentary:\n\n${sourceText}`;
    let accumulated = '';

    if (state.hasServer) {
      const payload = {
        auth_mode: state.authMode,
        model: 'gemini-3.8-flash',
        thinking_level: 'low',
        prompt,
        stream: true,
      };
      if (state.authMode === 'oauth') {
        payload.token = state.oauthToken;
      } else if (state.authMode === 'api_key') {
        payload.api_key = state.apiKey;
      } else if (state.authMode === 'proxy') {
        payload.proxy_url = state.proxyURL;
      }
      const res = await fetch('/api/chat', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
      });
      if (!res.ok) {
        let errMsg = `HTTP ${res.status}`;
        try {
          const err = await res.json();
          if (err?.error) errMsg = err.error;
        } catch (_) {}
        throw new Error(errMsg);
      }
      const reader = res.body.getReader();
      const decoder = new TextDecoder();
      let buffer = '';
      while (true) {
        const { done, value } = await reader.read();
        if (done) break;
        buffer += decoder.decode(value, { stream: true });
        const lines = buffer.split('\n');
        buffer = lines.pop();
        for (const line of lines) {
          if (!line.startsWith('data: ')) continue;
          const dataStr = line.slice(6).trim();
          if (!dataStr) continue;
          try {
            const parsed = JSON.parse(dataStr);
            if (parsed.text) {
              accumulated += parsed.text;
              const clean = stripThinkTags(accumulated);
              if (clean && onProgress) onProgress(clean);
            }
          } catch (_) {}
        }
      }
      return stripThinkTags(accumulated);
    }

    // Static fallback (no Go server)
    if (state.authMode === 'api_key') {
      if (!state.apiKey) throw new Error('Missing Gemini API Key');
      const url = `https://generativelanguage.googleapis.com/v1beta/models/gemini-2.5-flash:streamGenerateContent?alt=sse&key=${state.apiKey}`;
      const res = await fetch(url, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ contents: [{ role: 'user', parts: [{ text: prompt }] }] }),
      });
      if (!res.ok) throw new Error(`Gemini API HTTP ${res.status}`);
      const reader = res.body.getReader();
      const decoder = new TextDecoder();
      let buffer = '';
      while (true) {
        const { done, value } = await reader.read();
        if (done) break;
        buffer += decoder.decode(value, { stream: true });
        const lines = buffer.split('\n');
        buffer = lines.pop();
        for (const line of lines) {
          if (!line.startsWith('data: ')) continue;
          try {
            const parsed = JSON.parse(line.slice(6).trim());
            const parts = parsed.candidates?.[0]?.content?.parts;
            if (Array.isArray(parts)) {
              for (const part of parts) {
                if (!part.thought && part.text) {
                  accumulated += part.text;
                  if (onProgress) onProgress(accumulated.trim());
                }
              }
            }
          } catch (_) {}
        }
      }
      return accumulated.trim();
    }

    throw new Error('Translation requires Pigasus server (`pigasus livechat --web`) or a Gemini API Key.');
  }

  function renderBubbleTranslation(bubble, text, { loading = false, error = false } = {}) {
    if (!bubble) return null;
    let wrap = bubble.querySelector('.msg-translation-wrap');
    if (!wrap) {
      wrap = document.createElement('div');
      wrap.className = 'msg-translation-wrap';
      const hr = document.createElement('hr');
      hr.className = 'msg-translation-divider';
      const header = document.createElement('div');
      header.className = 'msg-translation-header';
      header.textContent = 'English · Gemini 3.8 Flash';
      const body = document.createElement('div');
      body.className = 'msg-translation-text';
      wrap.appendChild(hr);
      wrap.appendChild(header);
      wrap.appendChild(body);

      const bar = bubble.querySelector('.msg-audio-bar');
      if (bar) {
        bubble.insertBefore(wrap, bar);
      } else {
        bubble.appendChild(wrap);
      }
    }
    wrap.classList.remove('hidden');
    const textEl = wrap.querySelector('.msg-translation-text');
    if (textEl) {
      textEl.classList.toggle('error', Boolean(error));
      textEl.textContent = loading && !text ? 'Translating...' : text;
    }
    return wrap;
  }

  function persistTurnTranslation(bubble, turnRef, translation) {
    if (!translation) return;
    if (turnRef) {
      turnRef.translation = translation;
      saveActiveSessionTurns();
      return;
    }
    const raw = (bubble?.dataset?.rawText || '').trim();
    if (!raw) return;
    for (let i = state.liveTurns.length - 1; i >= 0; i--) {
      if (state.liveTurns[i].text === raw) {
        state.liveTurns[i].translation = translation;
        saveActiveSessionTurns();
        break;
      }
    }
  }

  function ensureBubbleActionBar(bubble, turnRef = null) {
    if (!bubble) return null;
    let bar = bubble.querySelector('.msg-audio-bar');
    if (!bar) {
      bar = document.createElement('div');
      bar.className = 'msg-audio-bar';
      bubble.appendChild(bar);
    }
    if (!bar.querySelector('.btn-translate-msg')) {
      const btn = document.createElement('button');
      btn.type = 'button';
      btn.className = 'btn-translate-msg';
      btn.title = 'Translate to English (Gemini 3.8 Flash)';
      btn.setAttribute('aria-label', 'Translate to English');
      btn.setAttribute('aria-pressed', 'false');
      btn.innerHTML = `
        <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
          <path d="m5 8 6 6"></path>
          <path d="m4 14 6-6 2-3"></path>
          <path d="M2 5h12"></path>
          <path d="M7 2h1"></path>
          <path d="m22 22-5-10-5 10"></path>
          <path d="M14 18h6"></path>
        </svg>
        <span>EN</span>
      `;
      btn.addEventListener('click', async (e) => {
        e.stopPropagation();
        const existingWrap = bubble.querySelector('.msg-translation-wrap');
        const existingTextEl = existingWrap?.querySelector('.msg-translation-text');
        if (existingWrap && existingTextEl && !existingTextEl.classList.contains('error') && bubble.dataset.translated === 'true') {
          const nowHidden = existingWrap.classList.toggle('hidden');
          btn.setAttribute('aria-pressed', nowHidden ? 'false' : 'true');
          return;
        }

        const sourceText = extractTranslatableText(bubble);
        if (!sourceText) return;

        btn.disabled = true;
        const labelSpan = btn.querySelector('span');
        if (labelSpan) labelSpan.textContent = '...';
        renderBubbleTranslation(bubble, '', { loading: true });

        try {
          const translated = await translateTextWithGeminiFlash(sourceText, (partial) => {
            renderBubbleTranslation(bubble, partial);
          });
          const finalText = translated || '(Empty translation)';
          renderBubbleTranslation(bubble, finalText);
          bubble.dataset.translated = 'true';
          btn.setAttribute('aria-pressed', 'true');
          persistTurnTranslation(bubble, turnRef, finalText);
        } catch (err) {
          renderBubbleTranslation(bubble, `Translation failed: ${err.message}`, { error: true });
          bubble.dataset.translated = 'false';
          btn.setAttribute('aria-pressed', 'false');
        } finally {
          btn.disabled = false;
          if (labelSpan) labelSpan.textContent = 'EN';
        }
      });
      bar.appendChild(btn);
    }
    return bar;
  }

  // --- Persistent Frontend Audio Cache (IndexedDB + In-Memory Map) ---
  const AUDIO_CACHE_DB_NAME = 'pigasus_live_audio_v1';
  const AUDIO_CACHE_STORE = 'turn_audio';
  let audioCacheDBPromise = null;

  function openAudioCacheDB() {
    if (typeof indexedDB === 'undefined') return Promise.resolve(null);
    if (audioCacheDBPromise) return audioCacheDBPromise;
    audioCacheDBPromise = new Promise((resolve) => {
      try {
        const req = indexedDB.open(AUDIO_CACHE_DB_NAME, 1);
        req.onupgradeneeded = (e) => {
          const db = e.target.result;
          if (!db.objectStoreNames.contains(AUDIO_CACHE_STORE)) {
            const store = db.createObjectStore(AUDIO_CACHE_STORE, { keyPath: 'id' });
            store.createIndex('sessionId', 'sessionId', { unique: false });
          }
        };
        req.onsuccess = () => resolve(req.result);
        req.onerror = () => {
          audioCacheDBPromise = null;
          resolve(null);
        };
      } catch (_) {
        audioCacheDBPromise = null;
        resolve(null);
      }
    });
    return audioCacheDBPromise;
  }

  function mergeBase64PCMChunks(chunks) {
    if (!Array.isArray(chunks) || chunks.length === 0) return new ArrayBuffer(0);
    const buffers = chunks.map((c) => new Uint8Array(base64ToArrayBuffer(c)));
    let totalBytes = 0;
    for (const b of buffers) totalBytes += b.byteLength;
    const merged = new Uint8Array(totalBytes);
    let offset = 0;
    for (const b of buffers) {
      merged.set(b, offset);
      offset += b.byteLength;
    }
    return merged.buffer;
  }

  function saveTurnAudioToCache(audioId, sessionId, chunks, sampleRate) {
    if (!audioId || !Array.isArray(chunks) || chunks.length === 0) return;
    const pcmBuffer = mergeBase64PCMChunks(chunks);
    if (!pcmBuffer || pcmBuffer.byteLength < 2) return;

    state.turnAudioMap.set(audioId, { sampleRate, pcmBuffer });

    openAudioCacheDB().then((db) => {
      if (!db) return;
      try {
        const tx = db.transaction(AUDIO_CACHE_STORE, 'readwrite');
        tx.objectStore(AUDIO_CACHE_STORE).put({
          id: audioId,
          sessionId: sessionId || '',
          sampleRate,
          pcmBuffer,
          createdAt: Date.now(),
        });
      } catch (err) {
        console.warn('[audio-cache] IndexedDB write failed:', err);
      }
    });
  }

  async function loadTurnAudioFromCache(audioId) {
    if (!audioId) return null;
    const mem = state.turnAudioMap.get(audioId);
    if (mem) {
      if (Array.isArray(mem)) {
        const pcmBuffer = mergeBase64PCMChunks(mem);
        const sampleRate = audioId.startsWith('user') ? 16000 : 24000;
        const entry = { sampleRate, pcmBuffer };
        state.turnAudioMap.set(audioId, entry);
        return entry;
      }
      return mem;
    }

    const db = await openAudioCacheDB();
    if (!db) return null;
    return new Promise((resolve) => {
      try {
        const tx = db.transaction(AUDIO_CACHE_STORE, 'readonly');
        const req = tx.objectStore(AUDIO_CACHE_STORE).get(audioId);
        req.onsuccess = () => {
          const rec = req.result;
          if (rec && rec.pcmBuffer) {
            const entry = {
              sampleRate: rec.sampleRate || (audioId.startsWith('user') ? 16000 : 24000),
              pcmBuffer: rec.pcmBuffer,
            };
            state.turnAudioMap.set(audioId, entry);
            resolve(entry);
          } else {
            resolve(null);
          }
        };
        req.onerror = () => resolve(null);
      } catch (_) {
        resolve(null);
      }
    });
  }

  async function hasTurnAudioInCache(audioId) {
    if (!audioId) return false;
    if (state.turnAudioMap.has(audioId)) return true;
    const db = await openAudioCacheDB();
    if (!db) return false;
    return new Promise((resolve) => {
      try {
        const tx = db.transaction(AUDIO_CACHE_STORE, 'readonly');
        const req = tx.objectStore(AUDIO_CACHE_STORE).getKey(audioId);
        req.onsuccess = () => resolve(Boolean(req.result));
        req.onerror = () => resolve(false);
      } catch (_) {
        resolve(false);
      }
    });
  }

  function deleteTurnAudioFromCache(audioId) {
    if (!audioId) return;
    openAudioCacheDB().then((db) => {
      if (!db) return;
      try {
        const tx = db.transaction(AUDIO_CACHE_STORE, 'readwrite');
        tx.objectStore(AUDIO_CACHE_STORE).delete(audioId);
      } catch (_) {}
    });
  }

  function deleteSessionAudioFromCache(sessionId) {
    if (!sessionId) return;
    openAudioCacheDB().then((db) => {
      if (!db) return;
      try {
        const tx = db.transaction(AUDIO_CACHE_STORE, 'readwrite');
        const store = tx.objectStore(AUDIO_CACHE_STORE);
        const idx = store.index('sessionId');
        const req = idx.openCursor(IDBKeyRange.only(sessionId));
        req.onsuccess = (e) => {
          const cursor = e.target.result;
          if (cursor) {
            cursor.delete();
            cursor.continue();
          }
        };
      } catch (_) {}
    });
  }

  function clearAllAudioFromCache() {
    openAudioCacheDB().then((db) => {
      if (!db) return;
      try {
        const tx = db.transaction(AUDIO_CACHE_STORE, 'readwrite');
        tx.objectStore(AUDIO_CACHE_STORE).clear();
      } catch (_) {}
    });
  }

  function attachReplayButton(bar, turnId, sampleRate, isUser) {
    if (!bar || !turnId || bar.querySelector('.btn-replay-audio')) return;
    const btn = document.createElement('button');
    btn.type = 'button';
    btn.className = 'btn-replay-audio';
    btn.dataset.turnId = turnId;
    btn.title = isUser ? 'Play Voice' : 'Play Audio';
    btn.setAttribute('aria-label', isUser ? 'Play Voice' : 'Play Audio');
    btn.innerHTML = '<span class="audio-btn-icon">🔊</span>';
    btn.addEventListener('click', (e) => {
      e.stopPropagation();
      replayTurnAudio(turnId, btn, sampleRate);
    });
    bar.insertBefore(btn, bar.firstChild);
  }

  function renderStoredTranscript() {
    if (!elems.liveTranscript) return;
    elems.liveTranscript.innerHTML = '';

    if (!state.liveTurns || state.liveTurns.length === 0) {
      elems.liveTranscript.innerHTML = renderWelcomeHero();
      bindPromptChips();
      return;
    }

    for (const turn of state.liveTurns) {
      const entry = document.createElement('div');
      entry.className = `transcript-entry ${turn.role}`;

      const spk = document.createElement('span');
      spk.className = 'speaker';
      spk.textContent = turn.role === 'user' ? 'You' : 'Live Agent';

      const body = document.createElement('div');
      body.className = 'text';
      body.dataset.rawText = turn.text;

      const textSpan = document.createElement('div');
      textSpan.className = 'text-content';

      if (turn.text.includes('<think>') && turn.text.includes('</think>')) {
        const match = turn.text.match(/<think>([\s\S]*?)<\/think>([\s\S]*)/);
        if (match) {
          textSpan.innerHTML = `
            <details class="think-block">
              <summary class="think-header">
                <span class="think-icon">💭</span>
                <span class="think-title">Thinking Process</span>
                <span class="think-badge">Thought</span>
                <span class="think-chevron">▶</span>
              </summary>
              <div class="think-content">${escapeHTML(match[1].trim())}</div>
            </details>
            <div class="response-content">${escapeHTML(match[2].trim())}</div>
          `;
        } else {
          textSpan.textContent = turn.text;
        }
      } else {
        textSpan.textContent = turn.text;
      }

      body.appendChild(textSpan);
      const bar = ensureBubbleActionBar(body, turn);
      if (turn.audioId && bar) {
        const isUser = turn.role === 'user';
        const sr = turn.audioSampleRate || (isUser ? 16000 : 24000);
        if (state.turnAudioMap.has(turn.audioId)) {
          attachReplayButton(bar, turn.audioId, sr, isUser);
        } else {
          hasTurnAudioInCache(turn.audioId).then((exists) => {
            if (exists && bar.isConnected) {
              attachReplayButton(bar, turn.audioId, sr, isUser);
            }
          });
        }
      }
      if (turn.translation) {
        renderBubbleTranslation(body, turn.translation);
        body.dataset.translated = 'true';
        const tBtn = body.querySelector('.btn-translate-msg');
        if (tBtn) tBtn.setAttribute('aria-pressed', 'true');
      }
      entry.appendChild(spk);
      entry.appendChild(body);
      elems.liveTranscript.appendChild(entry);
    }
    scrollTranscriptToBottom(true);
  }

  function finalizeCurrentTurn() {
    if (currentTurnEntry && currentTurnEntry.classList.contains('interim')) {
      currentTurnEntry.classList.remove('interim');
    }

    if (currentTurnSpeaker === 'agent' && currentTurnBubble) {
      const fullText = currentTurnBubble.dataset.rawText || currentTurnBubble.querySelector('.text-content')?.textContent || currentTurnBubble.textContent;
      const turnObj = fullText ? recordLiveTurn('agent', fullText) : null;
      const bar = ensureBubbleActionBar(currentTurnBubble, turnObj);

      // Attach audio replay button if agent audio chunks were received
      if (state.currentTurnAudioChunks.length > 0) {
        const turnId = 'turn_' + Date.now() + '_' + Math.random().toString(36).slice(2, 6);
        const chunks = [...state.currentTurnAudioChunks];
        state.currentTurnAudioChunks = [];
        saveTurnAudioToCache(turnId, state.activeSessionId, chunks, 24000);
        if (turnObj) {
          turnObj.audioId = turnId;
          turnObj.audioSampleRate = 24000;
          saveActiveSessionTurns();
        }
        attachReplayButton(bar, turnId, 24000, false);
      }
    }

    if (currentTurnSpeaker === 'user' && currentTurnBubble) {
      const fullText = (currentTurnBubble.dataset.rawText || currentTurnBubble.querySelector('.text-content')?.textContent || currentTurnBubble.textContent || '').trim();
      if (!fullText || fullText === '...') {
        // Discard phantom or blank interim bubble immediately
        if (currentTurnEntry && currentTurnEntry.parentNode) {
          currentTurnEntry.remove();
        }
        state.currentUserTurnAudioChunks = [];
      } else {
        const turnObj = recordLiveTurn('user', fullText);
        const bar = ensureBubbleActionBar(currentTurnBubble, turnObj);

        // Attach audio replay button to valid user turn
        if (state.currentUserTurnAudioChunks.length > 0) {
          const userTurnId = 'user_turn_' + Date.now() + '_' + Math.random().toString(36).slice(2, 6);
          const chunks = [...state.currentUserTurnAudioChunks];
          state.currentUserTurnAudioChunks = [];
          saveTurnAudioToCache(userTurnId, state.activeSessionId, chunks, 16000);
          if (turnObj) {
            turnObj.audioId = userTurnId;
            turnObj.audioSampleRate = 16000;
            saveActiveSessionTurns();
          }
          attachReplayButton(bar, userTurnId, 16000, true);
        }
      }
    }

    currentTurnSpeaker = null;
    currentTurnBubble = null;
    currentTurnEntry = null;
  }

  let replayRequestSeq = 0;

  async function replayTurnAudio(turnId, btn, sampleRate = 24000) {
    const reqSeq = ++replayRequestSeq;
    if (state.activeReplaySource) {
      try {
        state.activeReplaySource.stop();
      } catch (_) {}
      state.activeReplaySource = null;
      if (state.activeReplayBtn) {
        const isUserBtn = state.activeReplayBtn.dataset.turnId?.startsWith('user');
        state.activeReplayBtn.innerHTML = '<span class="audio-btn-icon">🔊</span>';
        state.activeReplayBtn.title = isUserBtn ? 'Play Voice' : 'Play Audio';
        state.activeReplayBtn.setAttribute('aria-label', isUserBtn ? 'Play Voice' : 'Play Audio');
        state.activeReplayBtn.classList.remove('playing');
      }
      if (state.activeReplayBtn === btn) {
        state.activeReplayBtn = null;
        return;
      }
    }

    const cached = await loadTurnAudioFromCache(turnId);
    if (reqSeq !== replayRequestSeq) return;
    if (!cached || !cached.pcmBuffer || cached.pcmBuffer.byteLength < 2) return;

    try {
      const AudioCtx = window.AudioContext || window.webkitAudioContext;
      if (!state.audioCtx || state.audioCtx.state === 'closed') {
        state.audioCtx = new AudioCtx();
      }
      if (state.audioCtx.state === 'suspended') {
        await state.audioCtx.resume();
      }
      if (reqSeq !== replayRequestSeq) return;

      const sampleCount = Math.floor(cached.pcmBuffer.byteLength / 2);
      if (sampleCount === 0) return;
      const pcm16 = new Int16Array(cached.pcmBuffer, 0, sampleCount);
      const float32 = new Float32Array(sampleCount);
      for (let i = 0; i < sampleCount; i++) {
        float32[i] = pcm16[i] / 32768.0;
      }

      const effectiveRate = cached.sampleRate || sampleRate;
      const audioBuffer = state.audioCtx.createBuffer(1, float32.length, effectiveRate);
      audioBuffer.getChannelData(0).set(float32);

      const source = state.audioCtx.createBufferSource();
      source.buffer = audioBuffer;

      if (state.analyser) {
        source.connect(state.analyser);
        state.analyser.connect(state.audioCtx.destination);
      } else {
        source.connect(state.audioCtx.destination);
      }

      source.start(0);
      state.activeReplaySource = source;
      state.activeReplayBtn = btn;
      btn.innerHTML = '<span class="audio-btn-icon">⏹</span>';
      btn.title = 'Stop Audio';
      btn.setAttribute('aria-label', 'Stop Audio');
      btn.classList.add('playing');

      source.onended = () => {
        if (state.activeReplayBtn === btn) {
          const isUser = turnId.startsWith('user');
          btn.innerHTML = '<span class="audio-btn-icon">🔊</span>';
          btn.title = isUser ? 'Play Voice' : 'Play Audio';
          btn.setAttribute('aria-label', isUser ? 'Play Voice' : 'Play Audio');
          btn.classList.remove('playing');
          state.activeReplaySource = null;
          state.activeReplayBtn = null;
        }
      };
    } catch (err) {
      console.warn('[replay] Audio playback error:', err);
    }
  }

  function setupLiveListeners() {
    if (elems.liveSystemPrompt) {
      const savedPrompt = localStorage.getItem('pigasus_live_prompt');
      const defaultMultilingualPrompt = 'You are LiveChat, an intelligent multilingual voice assistant. Always respond in the exact same language the user speaks to you in (respond in Chinese if the user speaks Chinese, respond in English if the user speaks English). Keep spoken answers natural, friendly, and concise in 1-2 sentences.';
      if (savedPrompt && !savedPrompt.includes('Speak concisely, friendly, and naturally in 1-2 spoken sentences.')) {
        elems.liveSystemPrompt.value = savedPrompt;
      } else {
        elems.liveSystemPrompt.value = defaultMultilingualPrompt;
      }
      elems.liveSystemPrompt.addEventListener('input', () => {
        localStorage.setItem('pigasus_live_prompt', elems.liveSystemPrompt.value);
      });
    }

    if (elems.liveDuplexToggle) {
      state.isFullDuplex = elems.liveDuplexToggle.checked;
      elems.liveDuplexToggle.addEventListener('change', (e) => {
        state.isFullDuplex = e.target.checked;
        if (state.liveWS) {
          updateLiveUI(state.activeAudioSources.length > 0 ? 'speaking' : 'ready', state.activeAudioSources.length > 0 ? 'Agent Speaking' : 'Listening & Ready');
        }
      });
    }

    elems.btnToggleLive.addEventListener('click', () => {
      if (state.liveWS) {
        intentionalDisconnect = true;
        clearTimeout(reconnectTimer);
        disconnectLiveSession();
      } else {
        intentionalDisconnect = false;
        reconnectAttempts = 0;
        clearTimeout(reconnectTimer);
        connectLiveSession();
      }
    });

    elems.btnToggleMic.addEventListener('click', () => {
      state.micMuted = !state.micMuted;
      if (state.micStream) {
        state.micStream.getAudioTracks().forEach((track) => {
          track.enabled = !state.micMuted;
        });
      }
      if (state.micMuted) {
        if (state.vadSilenceTimer) {
          clearTimeout(state.vadSilenceTimer);
          state.vadSilenceTimer = null;
        }
        if (state.isUserSpeaking) {
          finalizeCurrentTurn();
          state.isUserSpeaking = false;
        }
        if (state.liveWS && state.liveWS.readyState === WebSocket.OPEN) {
          state.liveWS.send(JSON.stringify({ realtimeInput: { audioStreamEnd: true } }));
        }
        setUnifiedLiveStatus('muted', 'muted', 'Muted', 'Microphone muted');
      } else if (state.liveWS && state.liveWS.readyState === WebSocket.OPEN) {
        const isAgent = state.activeAudioSources.length > 0;
        setUnifiedLiveStatus(
          isAgent ? 'agent' : 'listening',
          isAgent ? 'speaking' : 'connected',
          isAgent ? 'Agent' : 'Listening',
          isAgent ? 'Agent Speaking' : 'Listening & Ready'
        );
      }
      elems.btnToggleMic.textContent = state.micMuted ? '🔇' : '🎤';
      elems.btnToggleMic.title = state.micMuted ? 'Unmute Microphone' : 'Mute Microphone';
      elems.btnToggleMic.setAttribute('aria-pressed', state.micMuted ? 'true' : 'false');
      elems.btnToggleMic.classList.toggle('btn-danger', state.micMuted);
    });

    elems.liveTextForm.addEventListener('submit', (e) => {
      e.preventDefault();
      const text = elems.liveTextInput.value.trim();
      if (!text) return;

      // Handle slash commands in web composer
      if (text.startsWith('/search ') || text.startsWith('/web ') || text.startsWith('/google ')) {
        const query = text.slice(text.indexOf(' ') + 1).trim();
        elems.liveTextInput.value = '';
        elems.liveTextInput.style.height = 'auto';
        if (query) {
          handleWebSearchCommand(query);
        }
        return;
      }

      if (text.startsWith('/voice ') || text.startsWith('/speaker ')) {
        const vname = text.slice(text.indexOf(' ') + 1).trim();
        elems.liveTextInput.value = '';
        elems.liveTextInput.style.height = 'auto';
        if (vname) {
          handleVoiceCommand(vname);
        }
        return;
      }

      if (text.startsWith('/model ') || text.startsWith('/mdl ')) {
        const mname = text.slice(text.indexOf(' ') + 1).trim();
        elems.liveTextInput.value = '';
        elems.liveTextInput.style.height = 'auto';
        if (mname) {
          handleModelCommand(mname);
        }
        return;
      }

      if (!state.liveWS || state.liveWS.readyState !== WebSocket.OPEN) {
        appendTranscript('system', 'Connecting to LiveChat session...');
        connectLiveSession().then(() => {
          setTimeout(() => {
            if (state.liveWS && state.liveWS.readyState === WebSocket.OPEN) {
              finalizeCurrentTurn();
              sendLiveClientText(text);
              appendOrUpdateLiveTurn('user', text, false);
              finalizeCurrentTurn();
            }
          }, 600);
        });
        elems.liveTextInput.value = '';
        elems.liveTextInput.style.height = 'auto';
        scrollTranscriptToBottom(true);
        return;
      }

      finalizeCurrentTurn();
      sendLiveClientText(text);
      appendOrUpdateLiveTurn('user', text, false);
      finalizeCurrentTurn();
      elems.liveTextInput.value = '';
      elems.liveTextInput.style.height = 'auto';
      scrollTranscriptToBottom(true);
    });

    elems.liveTextInput.addEventListener('keydown', (e) => {
      if (e.key === 'Enter' && !e.shiftKey) {
        if (e.isComposing || e.keyCode === 229) return;
        e.preventDefault();
        elems.liveTextForm.requestSubmit();
      }
    });
  }

  async function handleWebSearchCommand(query) {
    finalizeCurrentTurn();
    appendTranscript('user', `🔍 /search ${query}`);
    appendTranscript('system', `Searching the web for: "${query}"...`);

    try {
      const resp = await fetch(`/api/search?q=${encodeURIComponent(query)}`);
      if (!resp.ok) {
        throw new Error(`HTTP ${resp.status}`);
      }
      const data = await resp.json();
      if (data.error) {
        appendTranscript('system', `⚠️ Web search failed: ${data.error}`);
        return;
      }

      const snippet = data.result || 'No search results found.';
      appendTranscript('system', `🌐 Web Search Results:\n${snippet}`);

      // Feed results to the active Live agent voice session
      if (state.liveWS && state.liveWS.readyState === WebSocket.OPEN) {
        const agentPrompt = `[SYSTEM NOTE: Web search results for "${query}":\n${snippet}\nAcknowledge and summarize for the user.]`;
        sendLiveClientText(agentPrompt);
      }
    } catch (err) {
      appendTranscript('system', `⚠️ Web search error: ${err.message}`);
    }
  }

  function handleVoiceCommand(name) {
    if (!elems.liveVoiceSelect) return;
    const lower = name.toLowerCase();
    for (let i = 0; i < elems.liveVoiceSelect.options.length; i++) {
      const opt = elems.liveVoiceSelect.options[i];
      if (opt.value.toLowerCase() === lower || opt.text.toLowerCase().includes(lower)) {
        elems.liveVoiceSelect.selectedIndex = i;
        elems.liveVoiceSelect.dispatchEvent(new Event('change'));
        appendTranscript('system', `Switched voice persona to ${opt.value}.`);
        return;
      }
    }
    appendTranscript('system', `Unknown voice persona "${name}". Available: Fola, Arlo, Cleo, Finn, Gero, Lora, Neno, Olin, Tari.`);
  }

  function handleModelCommand(name) {
    const lower = name.toLowerCase();
    if (lower === 'think on' || lower === 'thinking on' || lower === 'extended') {
      if (elems.btnToggleThinking) {
        elems.btnToggleThinking.classList.add('active');
        elems.btnToggleThinking.title = 'Extended Thinking: Active (2048 budget)';
        const txt = elems.btnToggleThinking.querySelector('.thinking-chip-text');
        if (txt) txt.textContent = 'Thinking ON';
        state.extendedThinking = true;
        appendTranscript('system', 'Extended Thinking enabled (2048 budget).');
      }
      return;
    }
    if (lower === 'think off' || lower === 'thinking off') {
      if (elems.btnToggleThinking) {
        elems.btnToggleThinking.classList.remove('active');
        elems.btnToggleThinking.title = 'Extended Thinking: Off';
        const txt = elems.btnToggleThinking.querySelector('.thinking-chip-text');
        if (txt) txt.textContent = 'Thinking OFF';
        state.extendedThinking = false;
        appendTranscript('system', 'Extended Thinking disabled.');
      }
      return;
    }
    if (elems.liveModelSelect) {
      for (let i = 0; i < elems.liveModelSelect.options.length; i++) {
        const opt = elems.liveModelSelect.options[i];
        if (opt.value.toLowerCase().includes(lower) || opt.text.toLowerCase().includes(lower)) {
          elems.liveModelSelect.selectedIndex = i;
          elems.liveModelSelect.dispatchEvent(new Event('change'));
          appendTranscript('system', `Switched Live model to ${opt.value}.`);
          return;
        }
      }
    }
    appendTranscript('system', `Model set to ${name}.`);
  }

  function resetAuditionSampleButtons() {
    if (!elems.auditionSamplesGroup) return;
    elems.auditionSamplesGroup.querySelectorAll('.btn-sample-play[data-sample]').forEach((btn) => {
      btn.classList.remove('playing');
      btn.setAttribute('aria-pressed', 'false');
      const icon = btn.querySelector('.sample-icon');
      if (icon) icon.textContent = '▶';
    });
  }

  function stopAuditionPlayback() {
    if (state.activeAuditionAudio) {
      try {
        state.activeAuditionAudio.pause();
      } catch (_) {}
      state.activeAuditionAudio = null;
    }
    state.activeAuditionSample = null;
    resetAuditionSampleButtons();
  }

  function setupAuditionListeners() {
    if (!elems.auditionSamplesGroup) return;

    elems.auditionSamplesGroup.addEventListener('click', (e) => {
      const btn = e.target.closest('.btn-sample-play[data-sample]');
      if (!btn || btn.disabled) return;

      const variant = btn.dataset.sample || '1';
      const wasSameSample = Boolean(state.activeAuditionAudio && state.activeAuditionSample === variant);
      stopAuditionPlayback();
      if (wasSameSample) {
        return;
      }

      const selectedOpt = elems.liveVoiceSelect?.options?.[elems.liveVoiceSelect.selectedIndex];
      const previewStem = selectedOpt?.dataset?.preview;
      if (!previewStem) {
        return;
      }

      const url = `previews/${previewStem}_${variant}.mp3`;
      const audio = new Audio(url);
      state.activeAuditionAudio = audio;
      state.activeAuditionSample = variant;
      btn.classList.add('playing');
      btn.setAttribute('aria-pressed', 'true');
      const icon = btn.querySelector('.sample-icon');
      if (icon) icon.textContent = '⏹';

      audio.play().catch((err) => {
        console.warn('Audition playback failed:', err);
        if (state.activeAuditionAudio === audio) {
          stopAuditionPlayback();
        }
      });

      audio.onended = () => {
        if (state.activeAuditionAudio === audio) {
          stopAuditionPlayback();
        }
      };
    });
  }

  // --- Microphone Device Management & Real-Time Hotplug Detection ---
  function setupMicrophoneManager() {
    if (!navigator.mediaDevices || !navigator.mediaDevices.enumerateDevices) {
      console.warn('[live] navigator.mediaDevices not supported');
      return;
    }

    // Initial enumeration
    updateMicrophoneList();

    // Hotplug Detection: devicechange fires when microphones / audio interfaces are plugged in or unplugged
    navigator.mediaDevices.addEventListener('devicechange', async () => {
      console.log('[live] Audio hardware devicechange event detected (hotplug)');
      flashHotplugBadge();
      const prevDeviceId = state.selectedMicDeviceId;
      const devices = await updateMicrophoneList();

      // Check if previously selected microphone is still connected
      const stillExists = devices.some((d) => d.deviceId === prevDeviceId);
      if (prevDeviceId && !stillExists) {
        appendTranscript('system', '⚠️ Selected microphone was unplugged. Falling back to default microphone.');
        await switchMicDevice('');
      } else if (state.micStream) {
        appendTranscript('system', '⚡ Audio device change detected (hotplug re-synchronized).');
      }
    });

    if (elems.liveMicSelect) {
      elems.liveMicSelect.addEventListener('change', async (e) => {
        const newDeviceId = e.target.value;
        await switchMicDevice(newDeviceId);
      });
    }
  }

  function getMicAudioConstraints(deviceId) {
    const audioConstraints = {
      channelCount: 1,
      echoCancellation: state.micConstraints.echoCancellation,
      noiseSuppression: state.micConstraints.noiseSuppression,
      autoGainControl: state.micConstraints.autoGainControl,
    };
    if (deviceId) {
      audioConstraints.deviceId = { exact: deviceId };
    }
    return audioConstraints;
  }

  function populateMicSelectElement(selectEl, audioInputs, currentVal) {
    if (!selectEl) return;
    selectEl.innerHTML = '';

    const defaultOpt = document.createElement('option');
    defaultOpt.value = '';
    defaultOpt.textContent = 'Default System Microphone';
    if (!currentVal) defaultOpt.selected = true;
    selectEl.appendChild(defaultOpt);

    let matchFound = false;
    audioInputs.forEach((d, idx) => {
      if (!d.deviceId) return;
      const opt = document.createElement('option');
      opt.value = d.deviceId;
      opt.textContent = d.label || `Microphone ${idx + 1} (${d.deviceId.slice(0, 8)}...)`;
      if (d.deviceId === currentVal) {
        opt.selected = true;
        matchFound = true;
      }
      selectEl.appendChild(opt);
    });

    if (currentVal && !matchFound) {
      selectEl.value = '';
    }
  }

  async function updateMicrophoneList() {
    if (!navigator.mediaDevices || !navigator.mediaDevices.enumerateDevices) return [];

    try {
      const allDevices = await navigator.mediaDevices.enumerateDevices();
      const audioInputs = allDevices.filter((d) => d.kind === 'audioinput');
      const currentVal = state.selectedMicDeviceId;

      populateMicSelectElement(elems.liveMicSelect, audioInputs, currentVal);
      populateMicSelectElement(elems.micTestDeviceSelect, audioInputs, currentVal);

      return audioInputs;
    } catch (err) {
      console.warn('[live] enumerateDevices error:', err);
      return [];
    }
  }

  function flashHotplugBadge() {
    if (!elems.micHotplugIndicator) return;
    elems.micHotplugIndicator.classList.add('hotplug-active');
    elems.micHotplugIndicator.textContent = '⚡ Hotplug Synced';
    setTimeout(() => {
      elems.micHotplugIndicator.classList.remove('hotplug-active');
      elems.micHotplugIndicator.textContent = '⚡ Auto-Detect';
    }, 2500);
  }

  function getMicDeviceLabel(deviceId) {
    if (!deviceId) return 'Default System Microphone';
    if (elems.liveMicSelect) {
      const opt = elems.liveMicSelect.querySelector(`option[value="${deviceId}"]`);
      if (opt && opt.textContent) return opt.textContent;
    }
    return 'Microphone (' + (deviceId ? deviceId.slice(0, 8) : '') + '...)';
  }

  async function switchMicDevice(deviceId, opts = {}) {
    state.selectedMicDeviceId = deviceId;
    if (deviceId) {
      localStorage.setItem('pigasus_mic_device_id', deviceId);
    } else {
      localStorage.removeItem('pigasus_mic_device_id');
    }
    if (elems.liveMicSelect && elems.liveMicSelect.value !== deviceId) {
      elems.liveMicSelect.value = deviceId;
    }
    if (elems.micTestDeviceSelect && elems.micTestDeviceSelect.value !== deviceId) {
      elems.micTestDeviceSelect.value = deviceId;
    }

    const label = getMicDeviceLabel(deviceId);

    // If currently capturing, seamlessly hot-swap the media stream without disconnecting LiveChat
    if (state.micStream && state.micAudioCtx && state.micAudioCtx.state !== 'closed') {
      try {
        const audioConstraints = getMicAudioConstraints(deviceId);

        let newStream;
        try {
          newStream = await navigator.mediaDevices.getUserMedia({
            audio: audioConstraints,
          });
        } catch (constraintErr) {
          if (deviceId && (constraintErr.name === 'OverconstrainedError' || constraintErr.name === 'NotFoundError')) {
            delete audioConstraints.deviceId;
            newStream = await navigator.mediaDevices.getUserMedia({
              audio: audioConstraints,
            });
          } else {
            throw constraintErr;
          }
        }

        if (!state.micAudioCtx || state.micAudioCtx.state === 'closed') {
          newStream.getTracks().forEach((t) => t.stop());
          return;
        }

        // Stop old stream tracks
        if (state.micStream) {
          state.micStream.getAudioTracks().forEach((t) => t.stop());
        }

        // Disconnect old source node
        if (state.micSourceNode) {
          state.micSourceNode.disconnect();
        }

        state.micStream = newStream;
        state.micSourceNode = state.micAudioCtx.createMediaStreamSource(newStream);

        if (state.micAnalyser) {
          state.micSourceNode.connect(state.micAnalyser);
        }

        if (state.micMuted) {
          state.micStream.getAudioTracks().forEach((t) => (t.enabled = false));
        }

        // Re-enumerate to ensure labels are updated if this was first permission grant
        await updateMicrophoneList();
        syncMicConstraintUI();

        if (!opts.silent) {
          appendTranscript('system', `🎙️ Switched microphone to: ${label}`);
        }
        if (elems.micStatusText && !state.micMuted) {
          elems.micStatusText.textContent = `Active (${label})`;
        }
      } catch (err) {
        console.warn('[live] Hot-swap microphone error:', err);
        appendTranscript('system', `Failed to switch microphone: ${err.message}`);
      }
    }
  }

  function syncMicConstraintUI() {
    const aec = !!state.micConstraints.echoCancellation;
    const ns = !!state.micConstraints.noiseSuppression;
    const agc = !!state.micConstraints.autoGainControl;

    if (elems.micOptAec) elems.micOptAec.checked = aec;
    if (elems.micOptNs) elems.micOptNs.checked = ns;
    if (elems.micOptAgc) elems.micOptAgc.checked = agc;
    if (elems.popoverOptAec) elems.popoverOptAec.checked = aec;
    if (elems.popoverOptNs) elems.popoverOptNs.checked = ns;
    if (elems.popoverOptAgc) elems.popoverOptAgc.checked = agc;

    if (elems.popoverAecBadge) {
      let effectiveAec = aec;
      const track = state.micStream?.getAudioTracks?.()[0];
      if (track && typeof track.getSettings === 'function') {
        const settings = track.getSettings();
        if (typeof settings.echoCancellation === 'boolean') {
          effectiveAec = settings.echoCancellation;
        }
      }
      elems.popoverAecBadge.textContent = effectiveAec ? 'AEC ON' : 'AEC OFF';
      elems.popoverAecBadge.classList.toggle('is-off', !effectiveAec);
    }
  }

  async function applyLiveMicConstraints() {
    if (!state.micStream || !state.micAudioCtx || state.micAudioCtx.state === 'closed') {
      syncMicConstraintUI();
      return;
    }
    const track = state.micStream.getAudioTracks?.()[0];
    if (track && typeof track.applyConstraints === 'function') {
      try {
        await track.applyConstraints(getMicAudioConstraints(state.selectedMicDeviceId));
        const settings = typeof track.getSettings === 'function' ? track.getSettings() : {};
        if (
          typeof settings.echoCancellation !== 'boolean' ||
          settings.echoCancellation === state.micConstraints.echoCancellation
        ) {
          syncMicConstraintUI();
          return;
        }
      } catch (_) {
        // Fall back to seamless stream hot-swap below
      }
    }
    await switchMicDevice(state.selectedMicDeviceId, { silent: true });
  }

  async function setMicConstraint(key, enabled) {
    const val = !!enabled;
    state.micConstraints[key] = val;
    const storageMap = {
      echoCancellation: 'pigasus_mic_aec',
      noiseSuppression: 'pigasus_mic_ns',
      autoGainControl: 'pigasus_mic_agc',
    };
    if (storageMap[key]) {
      localStorage.setItem(storageMap[key], String(val));
    }
    syncMicConstraintUI();

    if (state.micTest.active) {
      stopMicTest();
      startMicTest();
    }
    if (state.micStream && state.micAudioCtx && state.micAudioCtx.state !== 'closed') {
      await applyLiveMicConstraints();
      if (key === 'echoCancellation') {
        appendTranscript(
          'system',
          val
            ? '🔇 Browser Echo Cancellation (AEC) enabled.'
            : '🔊 Browser Echo Cancellation (AEC) disabled (headphones recommended).'
        );
      }
    }
  }

  function createAudioContext(preferredSampleRate) {
    const AudioCtx = window.AudioContext || window.webkitAudioContext;
    if (!AudioCtx) {
      const err = new Error('Web Audio API is not supported in this browser.');
      err.nonRetryable = true;
      throw err;
    }
    if (preferredSampleRate) {
      try {
        return new AudioCtx({ sampleRate: preferredSampleRate });
      } catch (_) {
        // Android Chrome / some mobile audio HALs reject custom sample rates; fall back to hardware rate
      }
    }
    return new AudioCtx();
  }

  function makeNonRetryableError(message) {
    const err = new Error(message);
    err.nonRetryable = true;
    return err;
  }

  async function connectLiveSession() {
    stopMicTest();
    updateLiveUI('connecting', 'Connecting...');

    try {
      // 1. Initialize & resume Web Audio Contexts synchronously inside user gesture (critical for Android Chrome)
      if (!state.audioCtx || state.audioCtx.state === 'closed') {
        state.audioCtx = createAudioContext(24000);
      }
      if (state.audioCtx.state === 'suspended') {
        state.audioCtx.resume().catch(() => {});
      }
      if (!state.micAudioCtx || state.micAudioCtx.state === 'closed') {
        state.micAudioCtx = createAudioContext();
      }
      if (state.micAudioCtx.state === 'suspended') {
        state.micAudioCtx.resume().catch(() => {});
      }

      if (serverDetectPromise) {
        await serverDetectPromise;
      }

      // Auto-refresh OAuth token only when explicitly using single-account OAuth mode
      if (state.authMode === 'oauth') {
        const token = await ensureValidOAuthToken();
        if (!token && !(state.hasServer && state.serverHasOAuth)) {
          throw makeNonRetryableError('Please authenticate with Google OAuth under Settings -> Google OAuth (or run `pigasus login`) first.');
        }
      }

      if (state.audioCtx && state.audioCtx.state === 'suspended') {
        await state.audioCtx.resume();
      }
      state.audioQueueTime = state.audioCtx.currentTime;

      // 2. Setup Waveform Analyser for Agent Output
      state.analyser = state.audioCtx.createAnalyser();
      state.analyser.fftSize = 64;
      state.analyser.smoothingTimeConstant = 0.3;
      startWaveformVisualizer();

      // 3. Resolve Target WebSocket URL
      let wsURL = '';

      if (state.hasServer) {
        // Backend Bridge Mode
        const params = new URLSearchParams({ mode: state.authMode });
        if (state.authMode === 'oauth' && state.oauthToken) {
          params.set('token', state.oauthToken);
        } else if (state.authMode === 'api_key' && state.apiKey) {
          params.set('key', state.apiKey);
        } else if (state.authMode === 'proxy' && state.proxyURL) {
          params.set('proxy', state.proxyURL);
        }
        const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
        wsURL = `${protocol}//${window.location.host}/api/live-ws?${params.toString()}`;
      } else {
        // Pure Static Standalone Mode
        if (state.authMode === 'api_key') {
          if (!state.apiKey) throw makeNonRetryableError('Please enter a Gemini API Key under Settings -> Gemini API Key.');
          wsURL = `wss://generativelanguage.googleapis.com/ws/google.ai.generativelanguage.v1alpha.GenerativeService.BidiGenerateContent?key=${state.apiKey}`;
        } else if (state.authMode === 'proxy') {
          if (!state.proxyURL) {
            throw makeNonRetryableError('Proxy mode selected, but no Pigasus Proxy URL is configured in Settings.');
          }
          const u = new URL(state.proxyURL);
          const scheme = u.protocol === 'https:' ? 'wss:' : 'ws:';
          wsURL = `${scheme}//${u.host}/v1/live`;
        } else if (state.authMode === 'oauth') {
          if (!state.proxyURL) {
            throw makeNonRetryableError('OAuth Live Voice requires the Pigasus LiveChat server (`pigasus livechat --web`) or an explicit Proxy URL in Settings.');
          }
          const u = new URL(state.proxyURL);
          const scheme = u.protocol === 'https:' ? 'wss:' : 'ws:';
          wsURL = `${scheme}//${u.host}/v1/live`;
          showStatusMsg('Connecting to configured Pigasus proxy for OAuth Live WebSocket bridge...', 'info');
        }
      }

      const ws = new WebSocket(wsURL);
      state.liveWS = ws;

      ws.onopen = async () => {
        if (state.liveWS !== ws) return;
        reconnectAttempts = 0;
        updateLiveUI('ready', 'Connected (Sending Setup)');

        // Send Gemini Live initial Setup message with transcription options
        const selectedVoice = elems.liveVoiceSelect.value;
        const selectedLiveModel = elems.composerModelSelect ? elems.composerModelSelect.value : (elems.liveModelSelect ? elems.liveModelSelect.value : 'models/gemini-3.8-live');
        const isExtendedThinking = selectedLiveModel.includes('extended-thinking');
        const customPrompt = (elems.liveSystemPrompt && elems.liveSystemPrompt.value.trim())
          ? elems.liveSystemPrompt.value.trim()
          : 'You are LiveChat, an intelligent multilingual voice assistant. Always respond in the exact same language the user speaks to you in (respond in Chinese if the user speaks Chinese, respond in English if the user speaks English). Keep spoken answers natural, friendly, and concise in 1-2 sentences.';

        // For Google Live WebSocket upstream, map extended-thinking variant to canonical live model with thinkingConfig
        let wireModel = selectedLiveModel;
        if (selectedLiveModel === 'models/gemini-3.8-live-extended-thinking') {
          wireModel = 'models/gemini-3.8-live';
        }

        const setupMsg = {
          setup: {
            model: wireModel,
            generationConfig: {
              responseModalities: ['AUDIO'],
              speechConfig: {
                voiceConfig: {
                  prebuiltVoiceConfig: {
                    voiceName: mapVoiceToGemini(selectedVoice),
                  },
                },
              },
              ...(isExtendedThinking
                ? {
                    thinkingConfig: {
                      thinkingBudget: 2048,
                    },
                  }
                : {}),
            },
            systemInstruction: {
              parts: [
                {
                  text: customPrompt,
                },
              ],
            },
            inputAudioTranscription: {},
            outputAudioTranscription: {},
          },
        };
        ws.send(JSON.stringify(setupMsg));

        // Start Microphone Audio Capture
        await startMicCapture();
        if (state.liveWS === ws && ws.readyState === WebSocket.OPEN) {
          updateLiveUI('ready', 'Listening & Ready');
        }
      };

      ws.onmessage = async (event) => {
        if (state.liveWS !== ws) return;
        let raw = event.data;
        if (raw instanceof Blob) {
          raw = await raw.text();
        } else if (raw instanceof ArrayBuffer) {
          raw = new TextDecoder().decode(raw);
        }
        handleLiveServerMessage(raw);
      };

      ws.onerror = (err) => {
        if (state.liveWS !== ws) return;
        console.error('[live] WebSocket error:', err);
        updateLiveUI('disconnected', 'Connection Error');
      };

      ws.onclose = (event) => {
        if (state.liveWS !== ws) return;
        console.warn('[live] WebSocket closed:', event.code, event.reason);
        if (event.reason) {
          appendTranscript('system', `Disconnected: ${event.reason} (code ${event.code})`);
        }
        disconnectLiveSession();
        const reasonLower = (event.reason || '').toLowerCase();
        const isAuthOrPermError = reasonLower.includes('permissiondenied') ||
          reasonLower.includes('permission denied') ||
          reasonLower.includes('unauthenticated') ||
          reasonLower.includes('api key');
        const isFatalClose = event.code === 1008 || event.code === 1003 || (event.code >= 4000 && event.code < 5000) || isAuthOrPermError;
        if (!intentionalDisconnect && !isFatalClose) {
          scheduleReconnect();
        }
      };
    } catch (err) {
      console.error('[live] Failed to connect:', err);
      appendTranscript('system', `Live connection failed: ${err.message}`);
      disconnectLiveSession();
      if (!intentionalDisconnect && !err.nonRetryable) {
        scheduleReconnect();
      }
    }
  }

  function scheduleReconnect() {
    if (intentionalDisconnect) return;
    const autoReconnect = elems.liveAutoReconnect ? elems.liveAutoReconnect.checked : true;
    if (!autoReconnect) {
      updateLiveUI('disconnected', 'Disconnected');
      return;
    }

    if (reconnectAttempts >= MAX_RECONNECT_ATTEMPTS) {
      appendTranscript('system', 'Max reconnection attempts reached. Click "Connect Live" to retry.');
      updateLiveUI('disconnected', 'Disconnected');
      return;
    }

    reconnectAttempts++;
    const delay = Math.min(1000 * Math.pow(2, reconnectAttempts - 1), 8000);
    updateLiveUI('connecting', `Reconnecting (${reconnectAttempts}/${MAX_RECONNECT_ATTEMPTS})...`);
    appendTranscript('system', `Connection interrupted. Auto-reconnecting in ${delay / 1000}s (attempt ${reconnectAttempts}/${MAX_RECONNECT_ATTEMPTS})...`);

    clearTimeout(reconnectTimer);
    reconnectTimer = setTimeout(() => {
      if (!intentionalDisconnect && (!state.liveWS || state.liveWS.readyState !== WebSocket.OPEN)) {
        connectLiveSession();
      }
    }, delay);
  }

  function disconnectLiveSession() {
    if (state.liveWS) {
      state.liveWS.close();
      state.liveWS = null;
    }
    finalizeCurrentTurn();
    stopMicCapture();
    stopAudioPlayback();
    stopWaveformVisualizer();

    if (state.audioCtx) {
      state.audioCtx.close().catch(() => {});
      state.audioCtx = null;
    }

    updateLiveUI('disconnected', 'Disconnected');
  }

  function setUnifiedLiveStatus(pillState, dotClass, text, title) {
    if (elems.liveStatusText) {
      elems.liveStatusText.textContent = text;
    }
    if (elems.liveDot) {
      elems.liveDot.className = `dot dot-${dotClass}`;
    }
    if (elems.liveStatusPill) {
      elems.liveStatusPill.className = `live-status-pill state-${pillState}`;
      if (title) {
        elems.liveStatusPill.title = title;
      }
    }
  }

  function updateLiveUI(status, label) {
    if (status === 'disconnected') {
      const isErr = label === 'Connection Error';
      setUnifiedLiveStatus(
        isErr ? 'error' : 'disconnected',
        isErr ? 'error' : 'disconnected',
        isErr ? 'Error' : 'Offline',
        label || 'Offline'
      );
    } else if (status === 'connecting') {
      const shortText = label && label.startsWith('Reconnecting') ? 'Reconnecting' : 'Connecting';
      setUnifiedLiveStatus('connecting', 'connecting', shortText, label || shortText);
    } else if (state.micMuted) {
      setUnifiedLiveStatus('muted', 'muted', 'Muted', 'Microphone muted');
    } else if (status === 'speaking') {
      setUnifiedLiveStatus('agent', 'speaking', 'Agent', label || 'Agent Speaking');
    } else {
      const shortText = label === 'Interrupted' ? 'Interrupted' : 'Listening';
      setUnifiedLiveStatus('listening', 'connected', shortText, label || 'Listening & Ready');
    }

    if (status === 'disconnected') {
      if (elems.btnToggleLive) {
        elems.btnToggleLive.innerHTML = '<span class="btn-live-icon" aria-hidden="true">▶</span> <span class="btn-live-text">Live</span>';
        elems.btnToggleLive.className = 'btn-connect-live';
        elems.btnToggleLive.title = 'Connect Live Voice';
        elems.btnToggleLive.setAttribute('aria-label', 'Connect Live Voice');
      }
      if (elems.btnToggleMic) {
        elems.btnToggleMic.disabled = true;
        elems.btnToggleMic.classList.add('hidden');
        elems.btnToggleMic.textContent = '🎤';
        elems.btnToggleMic.classList.remove('btn-danger');
        elems.btnToggleMic.setAttribute('aria-pressed', 'false');
      }
      if (elems.liveDuplexState) {
        elems.liveDuplexState.textContent = 'Offline';
      }
      elems.liveVisualizerBar?.classList.add('hidden');
    } else {
      if (elems.btnToggleLive) {
        elems.btnToggleLive.innerHTML = '<span class="btn-live-icon" aria-hidden="true">⏹</span> <span class="btn-live-text">Stop</span>';
        elems.btnToggleLive.className = 'btn-connect-live connected';
        elems.btnToggleLive.title = 'Disconnect Live Voice';
        elems.btnToggleLive.setAttribute('aria-label', 'Disconnect Live Voice');
      }
      if (elems.btnToggleMic) {
        elems.btnToggleMic.disabled = false;
        elems.btnToggleMic.classList.remove('hidden');
      }
      if (elems.btnLiveTextSend) {
        elems.btnLiveTextSend.disabled = false;
      }
      elems.liveVisualizerBar?.classList.remove('hidden');
      if (elems.liveDuplexState) {
        if (status === 'speaking') {
          elems.liveDuplexState.textContent = 'Speaking';
        } else {
          elems.liveDuplexState.textContent = state.isFullDuplex ? 'Duplex' : 'Half-Duplex';
        }
      }
    }
  }

  // --- Downsampler from Hardware Audio Rate to 16kHz PCM ---
  function downsampleTo16k(input, inputSampleRate) {
    if (!input || input.length === 0) return null;
    if (inputSampleRate === 16000) {
      const pcm16 = new Int16Array(input.length);
      for (let i = 0; i < input.length; i++) {
        const s = Math.max(-1, Math.min(1, input[i]));
        pcm16[i] = s < 0 ? s * 0x8000 : s * 0x7fff;
      }
      return pcm16;
    }
    const ratio = inputSampleRate / 16000;
    const newLength = Math.round(input.length / ratio);
    const pcm16 = new Int16Array(newLength);
    for (let i = 0; i < newLength; i++) {
      const origIndex = i * ratio;
      const indexFloor = Math.floor(origIndex);
      const indexCeil = Math.min(input.length - 1, Math.ceil(origIndex));
      const fraction = origIndex - indexFloor;
      const s = input[indexFloor] * (1 - fraction) + input[indexCeil] * fraction;
      const clamped = Math.max(-1, Math.min(1, s));
      pcm16[i] = clamped < 0 ? clamped * 0x8000 : clamped * 0x7fff;
    }
    return pcm16;
  }

  // --- Microphone Capture (16kHz PCM mono full-duplex) ---
  async function startMicCapture() {
    try {
      if (!navigator.mediaDevices || typeof navigator.mediaDevices.getUserMedia !== 'function') {
        throw new Error('Microphone access requires HTTPS or localhost/127.0.0.1 in this browser.');
      }

      if (!state.micAudioCtx || state.micAudioCtx.state === 'closed') {
        state.micAudioCtx = createAudioContext();
      }
      const micCtx = state.micAudioCtx;
      const isMicSessionActive = () =>
        state.micAudioCtx === micCtx &&
        micCtx.state !== 'closed' &&
        state.liveWS &&
        state.liveWS.readyState === WebSocket.OPEN;

      if (!isMicSessionActive()) return;

      if (micCtx.state === 'suspended') {
        await micCtx.resume();
      }
      if (!isMicSessionActive()) return;

      const audioConstraints = getMicAudioConstraints(state.selectedMicDeviceId);

      let stream;
      try {
        stream = await navigator.mediaDevices.getUserMedia({
          audio: audioConstraints,
        });
      } catch (constraintErr) {
        if (state.selectedMicDeviceId && (constraintErr.name === 'OverconstrainedError' || constraintErr.name === 'NotFoundError')) {
          delete audioConstraints.deviceId;
          state.selectedMicDeviceId = '';
          localStorage.removeItem('pigasus_mic_device_id');
          stream = await navigator.mediaDevices.getUserMedia({
            audio: audioConstraints,
          });
        } else {
          throw constraintErr;
        }
      }

      if (!isMicSessionActive()) {
        stream.getTracks().forEach((t) => t.stop());
        return;
      }
      state.micStream = stream;

      // Refresh microphone list to populate real device labels (now that permission is granted)
      await updateMicrophoneList();
      if (!isMicSessionActive()) {
        stream.getTracks().forEach((t) => t.stop());
        if (state.micStream === stream) state.micStream = null;
        return;
      }

      const inputSampleRate = micCtx.sampleRate;
      state.micSourceNode = micCtx.createMediaStreamSource(stream);
      const source = state.micSourceNode;

      // Dedicated mic analyser in micAudioCtx context (NEVER connect across different audio contexts)
      state.micAnalyser = micCtx.createAnalyser();
      state.micAnalyser.fftSize = 64;
      state.micAnalyser.smoothingTimeConstant = 0.25;
      source.connect(state.micAnalyser);

      const bufferSize = 4096;
      state.micProcessor = micCtx.createScriptProcessor(bufferSize, 1, 1);

      state.micProcessor.onaudioprocess = (e) => {
        if (!state.liveWS || state.liveWS.readyState !== WebSocket.OPEN || state.micMuted) return;

        // Echo Guard: In half-duplex mode, suppress microphone input while agent is speaking or in reverberation grace period
        const isAgentPlaying = state.activeAudioSources.length > 0 || Date.now() < state.agentSpeakingUntil;
        if (!state.isFullDuplex && isAgentPlaying) {
          return;
        }

        const inputChannelData = e.inputBuffer.getChannelData(0);
        let peak = 0;
        for (let i = 0; i < inputChannelData.length; i++) {
          const val = Math.abs(inputChannelData[i]);
          if (val > peak) peak = val;
        }

        const pcm16 = downsampleTo16k(inputChannelData, inputSampleRate);
        if (!pcm16 || pcm16.length === 0) return;

        const base64Audio = arrayBufferToBase64(pcm16.buffer);
        const chunkMsg = {
          realtimeInput: {
            audio: {
              mimeType: 'audio/pcm;rate=16000',
              data: base64Audio,
            },
          },
        };
        state.liveWS.send(JSON.stringify(chunkMsg));

        // Voice Activity Detection & Audio Buffer Accumulation for Replay
        // Note: Chat bubbles are created ONLY when actual speech text is transcribed by Gemini Live
        const SPEECH_PEAK_THRESHOLD = 0.035;
        if (peak > SPEECH_PEAK_THRESHOLD) {
          state.isUserSpeaking = true;
          if (state.vadSilenceTimer) {
            clearTimeout(state.vadSilenceTimer);
            state.vadSilenceTimer = null;
          }
        }

        if (state.isUserSpeaking) {
          state.currentUserTurnAudioChunks.push(base64Audio);
          if (state.currentUserTurnAudioChunks.length > 350) {
            state.currentUserTurnAudioChunks.shift();
          }

          if (peak <= SPEECH_PEAK_THRESHOLD && !state.vadSilenceTimer) {
            state.vadSilenceTimer = setTimeout(() => {
              state.vadSilenceTimer = null;
              state.isUserSpeaking = false;
              if (currentTurnSpeaker === 'user') {
                finalizeCurrentTurn();
              } else {
                state.currentUserTurnAudioChunks = [];
              }
            }, 1200);
          }
        }
      };

      // Mute local loopback to prevent feedback
      const muteGain = micCtx.createGain();
      muteGain.gain.value = 0;

      state.micAnalyser.connect(state.micProcessor);
      state.micProcessor.connect(muteGain);
      muteGain.connect(micCtx.destination);

      if (elems.micStatusText) {
        const devLabel = getMicDeviceLabel(state.selectedMicDeviceId);
        elems.micStatusText.textContent = `Active (${devLabel})`;
        elems.micStatusText.style.color = '#34d399';
      }
      syncMicConstraintUI();
    } catch (err) {
      console.warn('[live] Mic capture error:', err);
      appendTranscript('system', `Microphone note: ${err.message}`);
      if (elems.micStatusText) {
        elems.micStatusText.textContent = 'Mic Error: ' + err.message;
        elems.micStatusText.style.color = '#ef4444';
      }
    }
  }

  function stopMicCapture() {
    if (state.vadSilenceTimer) {
      clearTimeout(state.vadSilenceTimer);
      state.vadSilenceTimer = null;
    }
    if (state.isUserSpeaking) {
      finalizeCurrentTurn();
      state.isUserSpeaking = false;
    }
    if (state.micStream) {
      state.micStream.getTracks().forEach((t) => t.stop());
      state.micStream = null;
    }
    if (state.micSourceNode) {
      state.micSourceNode.disconnect();
      state.micSourceNode = null;
    }
    if (state.micProcessor) {
      state.micProcessor.disconnect();
      state.micProcessor = null;
    }
    if (state.micAnalyser) {
      state.micAnalyser.disconnect();
      state.micAnalyser = null;
    }
    if (state.micAudioCtx) {
      state.micAudioCtx.close().catch(() => {});
      state.micAudioCtx = null;
    }
  }

  // --- Live Server Message Processing ---
  function handleLiveServerMessage(raw) {
    let data;
    try {
      data = JSON.parse(raw);
    } catch (_) {
      return;
    }

    if (data.setupComplete) {
      updateLiveUI('ready', 'Connected & Ready');
      appendTranscript('system', 'Gemini Live session connected.');

      // Check if session has prior speech turns to restore (aligning with ./cmd/livechat RehydrateSessionContext)
      if (state.liveTurns && state.liveTurns.length > 0) {
        appendTranscript('system', `Restoring session memory (${state.liveTurns.length} prior speech turns)...`);
        const historyTranscript = state.liveTurns
          .map((t) => `${t.role === 'user' ? 'User' : 'Agent'}: ${t.text}`)
          .join('\n');
        const rehydratePrompt = `[SYSTEM NOTE: Live session connection established/resumed. Below is the prior conversation transcript from this session. Please maintain seamless conversational continuity without greeting from scratch:\n${historyTranscript}\n]`;
        setTimeout(() => {
          if (state.liveWS && state.liveWS.readyState === WebSocket.OPEN) {
            sendLiveClientText(rehydratePrompt);
          }
        }, 300);
      } else {
        // Fresh session proactive greeting trigger (like ./cmd/livechat)
        const shouldProactive = elems.liveProactiveGreeting ? elems.liveProactiveGreeting.checked : true;
        if (shouldProactive) {
          setTimeout(() => {
            if (state.liveWS && state.liveWS.readyState === WebSocket.OPEN) {
              sendLiveClientText(
                "[PROACTIVE_EVENT: Agent session established. Greet the user concisely in one friendly sentence: '你好！我是 LiveChat 实时语音助手，随时可以为你提供帮助。' (or in English if preferred). Keep it short.]"
              );
            }
          }, 500);
        }
      }
      return;
    }

    const sc = data.serverContent;
    if (!sc) return;

    if (sc.interrupted) {
      stopAudioPlayback();
      state.agentSpeakingUntil = 0;
      updateLiveUI('ready', 'Interrupted');
      finalizeCurrentTurn();
      return;
    }

    let agentTurnText = '';
    let hasPartText = false;
    if (sc.modelTurn && sc.modelTurn.parts) {
      updateLiveUI('speaking', 'Agent Speaking');
      for (const part of sc.modelTurn.parts) {
        if (part.text) {
          hasPartText = true;
          agentTurnText += part.text;
        }
        if (part.inlineData && part.inlineData.data) {
          state.currentTurnAudioChunks.push(part.inlineData.data);
          playPCMChunk(part.inlineData.data);
        }
      }
    }
    if (!hasPartText && sc.outputTranscription && sc.outputTranscription.text) {
      agentTurnText += sc.outputTranscription.text;
    }
    if (agentTurnText) {
      appendOrUpdateLiveTurn('agent', agentTurnText, false);
    }

    if (sc.inputTranscription && sc.inputTranscription.text) {
      appendOrUpdateLiveTurn('user', sc.inputTranscription.text, false);
    } else if (sc.interimInputTranscription && sc.interimInputTranscription.text) {
      appendOrUpdateLiveTurn('user', sc.interimInputTranscription.text, true);
    }

    if (sc.turnComplete) {
      finalizeCurrentTurn();
      setTimeout(() => {
        if (state.liveWS) updateLiveUI('ready', 'Listening & Ready');
      }, 500);
    }
  }

  // --- Audio Output Playback (24kHz PCM mono) ---
  function playPCMChunk(base64Data) {
    if (!state.audioCtx) return;

    const raw = base64ToArrayBuffer(base64Data);
    const pcm16 = new Int16Array(raw);
    const float32 = new Float32Array(pcm16.length);

    for (let i = 0; i < pcm16.length; i++) {
      float32[i] = pcm16[i] / 32768.0;
    }

    const audioBuffer = state.audioCtx.createBuffer(1, float32.length, 24000);
    audioBuffer.getChannelData(0).set(float32);

    const source = state.audioCtx.createBufferSource();
    source.buffer = audioBuffer;

    if (state.analyser) {
      source.connect(state.analyser);
      state.analyser.connect(state.audioCtx.destination);
    } else {
      source.connect(state.audioCtx.destination);
    }

    const now = state.audioCtx.currentTime;
    if (state.audioQueueTime < now) {
      state.audioQueueTime = now;
    }
    source.start(state.audioQueueTime);
    state.audioQueueTime += audioBuffer.duration;

    // Track active playback duration and 500ms reverberation grace period for Echo Guard
    const remainingPlaybackMs = Math.round((state.audioQueueTime - now) * 1000) + 500;
    state.agentSpeakingUntil = Date.now() + remainingPlaybackMs;

    state.activeAudioSources.push(source);
    source.onended = () => {
      const idx = state.activeAudioSources.indexOf(source);
      if (idx !== -1) state.activeAudioSources.splice(idx, 1);
    };
  }

  function stopAudioPlayback() {
    state.activeAudioSources.forEach((s) => {
      try {
        s.stop();
      } catch (_) {}
    });
    state.activeAudioSources = [];
    state.agentSpeakingUntil = 0;
    if (state.audioCtx) {
      state.audioQueueTime = state.audioCtx.currentTime;
    }
  }

  function sendLiveClientText(text) {
    if (!state.liveWS || state.liveWS.readyState !== WebSocket.OPEN) return;
    const msg = {
      clientContent: {
        turns: [
          {
            role: 'user',
            parts: [{ text: text }],
          },
        ],
        turnComplete: true,
      },
    };
    state.liveWS.send(JSON.stringify(msg));
  }

  function escapeHTML(str) {
    if (!str) return '';
    return str.replace(/[&<>'"]/g, (tag) => ({
      '&': '&amp;',
      '<': '&lt;',
      '>': '&gt;',
      "'": '&#39;',
      '"': '&quot;',
    }[tag] || tag));
  }

  function appendOrUpdateLiveTurn(speaker, text, isInterim = false) {
    if (!text) return;
    const cleanText = text.trim();
    if (!cleanText || cleanText === '...') return;
    const hero = elems.liveTranscript.querySelector('.welcome-hero-container');
    if (hero) hero.remove();
    const ph = elems.liveTranscript.querySelector('.transcript-placeholder');
    if (ph) ph.remove();

    // If updating an interim user speech bubble
    if (speaker === 'user' && currentTurnSpeaker === 'user' && currentTurnBubble && currentTurnEntry) {
      const textSpan = currentTurnBubble.querySelector('.text-content') || currentTurnBubble;
      currentTurnBubble.dataset.rawText = text;
      if (isInterim) {
        currentTurnEntry.classList.add('interim');
        textSpan.textContent = text;
        scrollTranscriptToBottom(false);
        return;
      } else {
        currentTurnEntry.classList.remove('interim');
        textSpan.textContent = text;
        ensureBubbleActionBar(currentTurnBubble);
        scrollTranscriptToBottom(true);
        recordLiveTurn('user', text);
        return;
      }
    }

    // If streaming agent transcription to an active agent bubble
    if (speaker === 'agent' && currentTurnSpeaker === 'agent' && currentTurnBubble) {
      const textSpan = currentTurnBubble.querySelector('.text-content') || currentTurnBubble;
      const prev = currentTurnBubble.dataset.rawText || '';
      const combined = prev + text;
      currentTurnBubble.dataset.rawText = combined;
      textSpan.textContent = combined;
      scrollTranscriptToBottom(false);
      return;
    }

    // Record completed user turn
    if (speaker === 'user' && !isInterim) {
      recordLiveTurn('user', text);
    }

    // If previous turn belonged to a different speaker, finalize it before starting a new bubble
    if (currentTurnSpeaker && currentTurnSpeaker !== speaker) {
      finalizeCurrentTurn();
    }

    // Otherwise, create a new bubble
    const entry = document.createElement('div');
    entry.className = `transcript-entry ${speaker}${isInterim ? ' interim' : ''}`;

    const spk = document.createElement('span');
    spk.className = 'speaker';
    spk.textContent = speaker === 'user' ? 'You' : 'Live Agent';

    const body = document.createElement('div');
    body.className = 'text';
    body.dataset.rawText = text;

    const textSpan = document.createElement('div');
    textSpan.className = 'text-content';

    if (text.includes('<think>') && text.includes('</think>')) {
      const match = text.match(/<think>([\s\S]*?)<\/think>([\s\S]*)/);
      if (match) {
        const thinkText = match[1].trim();
        const mainText = match[2].trim();
        textSpan.innerHTML = `
          <details class="think-block">
            <summary class="think-header">
              <span class="think-icon">💭</span>
              <span class="think-title">Thinking Process</span>
              <span class="think-badge">Thought</span>
              <span class="think-chevron">▶</span>
            </summary>
            <div class="think-content">${escapeHTML(thinkText)}</div>
          </details>
          <div class="response-content">${escapeHTML(mainText)}</div>
        `;
      } else {
        textSpan.textContent = text;
      }
    } else {
      textSpan.textContent = text;
    }

    body.appendChild(textSpan);
    if (speaker === 'user' && !isInterim) {
      ensureBubbleActionBar(body);
    }
    entry.appendChild(spk);
    entry.appendChild(body);
    elems.liveTranscript.appendChild(entry);
    scrollTranscriptToBottom(true);

    currentTurnSpeaker = speaker;
    currentTurnBubble = body;
    currentTurnEntry = entry;
  }

  function appendTranscript(speaker, text) {
    if (speaker === 'system') {
      const hero = elems.liveTranscript.querySelector('.welcome-hero-container');
      if (hero) hero.remove();
      const ph = elems.liveTranscript.querySelector('.transcript-placeholder');
      if (ph) ph.remove();

      const entry = document.createElement('div');
      entry.className = 'transcript-entry system';
      const body = document.createElement('div');
      body.className = 'text';
      body.textContent = text;
      entry.appendChild(body);
      elems.liveTranscript.appendChild(entry);
      scrollTranscriptToBottom(true);
      finalizeCurrentTurn();
      return;
    }
    appendOrUpdateLiveTurn(speaker, text, false);
  }

  // --- Waveform Visualizer & Mic Level Meter ---
  function startWaveformVisualizer() {
    if (state.animFrameId) {
      cancelAnimationFrame(state.animFrameId);
      state.animFrameId = null;
    }
    const canvas = elems.waveformCanvas;
    if (!canvas) return;
    const ctx = canvas.getContext('2d');

    let numBars = 36;
    let smoothedBars = new Float32Array(numBars);
    let smoothedVolume = 0;
    let lastDrawTime = 0;
    let lastTextUpdateTime = 0;

    const outputBufferLength = state.analyser ? state.analyser.frequencyBinCount : 32;
    const outputDataArray = new Uint8Array(outputBufferLength);
    const micDataArray = new Uint8Array(32);

    function draw(timestamp) {
      state.animFrameId = requestAnimationFrame(draw);

      const now = timestamp || performance.now();

      // 1. Throttle animation to calm, smooth ~25 FPS (every ~40ms) to stop manic flickering
      if (now - lastDrawTime < 40) {
        return;
      }
      lastDrawTime = now;

      // Keep canvas pixel width synced to its full-width container
      const targetW = Math.max(120, Math.floor(canvas.clientWidth || 360));
      if (canvas.width !== targetW) {
        canvas.width = targetW;
      }
      const desiredBars = Math.max(16, Math.min(56, Math.floor(canvas.width / 9)));
      if (desiredBars !== numBars) {
        numBars = desiredBars;
        smoothedBars = new Float32Array(numBars);
      }

      // 2. Measure volume
      let micVolumePercent = 0;
      let micIsActiveSpeaking = false;
      if (state.micAnalyser && !state.micMuted) {
        state.micAnalyser.getByteFrequencyData(micDataArray);
        let sum = 0;
        for (let i = 0; i < micDataArray.length; i++) {
          sum += micDataArray[i];
        }
        const avg = sum / micDataArray.length;
        const rawPercent = Math.min(100, Math.round((avg / 128) * 100));
        // Exponential smoothing on volume percentage for calm transitions
        smoothedVolume = smoothedVolume * 0.7 + rawPercent * 0.3;
        micVolumePercent = Math.round(smoothedVolume);
        micIsActiveSpeaking = avg > 8; // Voice activity detected
      } else {
        smoothedVolume = smoothedVolume * 0.75;
        micVolumePercent = Math.round(smoothedVolume);
      }

      if (elems.micMeterFill) {
        elems.micMeterFill.style.width = micVolumePercent + '%';
      }

      // Check agent playback
      const isAgentSpeaking = state.activeAudioSources.length > 0;

      // 3. Throttle text/number updates to 4 times per second (every 250ms) to eliminate flickering
      if (now - lastTextUpdateTime > 250) {
        lastTextUpdateTime = now;

        if (elems.micMeterVal) {
          elems.micMeterVal.textContent = micVolumePercent + '%';
        }

        // Update unified fixed-width status pill in header when live socket is open
        if (state.liveWS && state.liveWS.readyState === WebSocket.OPEN) {
          if (state.micMuted) {
            setUnifiedLiveStatus('muted', 'muted', 'Muted', 'Microphone muted');
          } else if (isAgentSpeaking) {
            setUnifiedLiveStatus('agent', 'speaking', 'Agent', 'Agent Speaking');
          } else if (micIsActiveSpeaking) {
            setUnifiedLiveStatus('speaking', 'connected', 'Speaking', 'User Speaking');
          } else {
            setUnifiedLiveStatus('listening', 'connected', 'Listening', 'Listening & Ready');
          }
        }
      }

      // 4. Render Waveform with Exponential Moving Average (EMA) smoothing
      ctx.clearRect(0, 0, canvas.width, canvas.height);

      let targetData = outputDataArray;
      const isLightTheme = document.documentElement.getAttribute('data-theme') === 'light';
      let barColor = isLightTheme ? '#0284c7' : '#38bdf8'; // Agent cyan

      if (isAgentSpeaking && state.analyser) {
        state.analyser.getByteFrequencyData(outputDataArray);
        targetData = outputDataArray;
        barColor = isLightTheme ? '#0284c7' : '#38bdf8';
      } else if (state.micAnalyser && !state.micMuted) {
        targetData = micDataArray;
        barColor = isLightTheme ? '#059669' : '#10b981'; // Mic emerald green
      }

      const step = Math.max(1, Math.floor(targetData.length / numBars));
      const barWidth = 4;
      const totalBarWidth = numBars * barWidth;
      const gap = (canvas.width - totalBarWidth) / (numBars - 1);
      const midY = canvas.height / 2;

      for (let i = 0; i < numBars; i++) {
        const raw = targetData[i * step] || 0;
        const targetH = (raw / 255) * (canvas.height - 4);
        // Exponential smoothing: gentle attack (0.35), smooth decay (0.15)
        const alpha = targetH > smoothedBars[i] ? 0.35 : 0.15;
        smoothedBars[i] += (targetH - smoothedBars[i]) * alpha;

        const h = Math.max(2, Math.round(smoothedBars[i]));
        const x = Math.round(i * (barWidth + gap));
        const y = Math.round(midY - h / 2);

        ctx.fillStyle = barColor;
        if (ctx.roundRect) {
          ctx.beginPath();
          ctx.roundRect(x, y, barWidth, h, 3);
          ctx.fill();
        } else {
          ctx.fillRect(x, y, barWidth, h);
        }
      }
    }

    state.animFrameId = requestAnimationFrame(draw);
  }

  function stopWaveformVisualizer() {
    if (state.animFrameId) {
      cancelAnimationFrame(state.animFrameId);
      state.animFrameId = null;
    }
    const canvas = elems.waveformCanvas;
    if (!canvas) return;
    const ctx = canvas.getContext('2d');
    ctx.clearRect(0, 0, canvas.width, canvas.height);
  }

  // --- Proxy Settings Listeners ---
  function setupProxySettingsListeners() {
    elems.proxyConfigForm.addEventListener('submit', (e) => {
      e.preventDefault();
      const url = elems.cfgProxyUrl.value.trim();
      const selectedAuth = document.querySelector('input[name="default_auth"]:checked')?.value || (state.hasServer ? 'aggregate' : 'oauth');

      state.proxyURL = url;
      localStorage.setItem('pigasus_proxy_url', url);
      setAuthMode(selectedAuth);

      showProxyResult('Settings saved successfully.', 'success');
    });

    elems.btnTestProxy.addEventListener('click', async () => {
      const url = elems.cfgProxyUrl.value.trim();
      if (!url) {
        showProxyResult('Please enter a Pigasus Proxy URL first (optional; not needed for OAuth or API Key mode).', 'error');
        return;
      }
      showProxyResult(`Testing connectivity to ${url}...`, 'info');
      try {
        const testURL = url.replace(/\/$/, '') + '/v1internal:streamGenerateContent';
        const res = await fetch(testURL, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ ping: true }),
        });
        showProxyResult(`Proxy responded with status HTTP ${res.status}. Connectivity OK!`, 'success');
      } catch (err) {
        showProxyResult(`Could not reach proxy at ${url}: ${err.message}`, 'error');
      }
    });
  }

  function showProxyResult(text, type) {
    elems.proxyTestResult.textContent = text;
    elems.proxyTestResult.className = `status-msg ${type}`;
    elems.proxyTestResult.classList.remove('hidden');
  }

  // --- Microphone Test & Diagnostics Section ---
  function setupMicTestSection() {
    syncMicConstraintUI();

    elems.micOptAec?.addEventListener('change', (e) => {
      setMicConstraint('echoCancellation', e.target.checked);
    });
    elems.popoverOptAec?.addEventListener('change', (e) => {
      setMicConstraint('echoCancellation', e.target.checked);
    });
    elems.micOptNs?.addEventListener('change', (e) => {
      setMicConstraint('noiseSuppression', e.target.checked);
    });
    elems.popoverOptNs?.addEventListener('change', (e) => {
      setMicConstraint('noiseSuppression', e.target.checked);
    });
    elems.micOptAgc?.addEventListener('change', (e) => {
      setMicConstraint('autoGainControl', e.target.checked);
    });
    elems.popoverOptAgc?.addEventListener('change', (e) => {
      setMicConstraint('autoGainControl', e.target.checked);
    });

    elems.micTestDeviceSelect?.addEventListener('change', async (e) => {
      const newDeviceId = e.target.value;
      await switchMicDevice(newDeviceId);
      if (state.micTest.active) {
        stopMicTest();
        startMicTest();
      }
    });

    elems.btnMicTestRefresh?.addEventListener('click', async () => {
      await updateMicrophoneList();
      await refreshMicDiagnostics();
      showMicTestStatus('Refreshed audio input device list and diagnostics.', 'info');
    });

    elems.btnMicTestToggle?.addEventListener('click', () => {
      if (state.micTest.active || state.micTest.starting) {
        stopMicTest();
      } else {
        startMicTest();
      }
    });

    elems.btnMicTestRecord?.addEventListener('click', () => {
      toggleMicTestRecording();
    });

    elems.btnMicTestTone?.addEventListener('click', () => {
      playSpeakerTestTone();
    });

    refreshMicDiagnostics();
  }

  async function refreshMicDiagnostics() {
    if (elems.micDiagSecure) {
      const hasMedia = !!(navigator.mediaDevices && typeof navigator.mediaDevices.getUserMedia === 'function');
      if (window.isSecureContext && hasMedia) {
        elems.micDiagSecure.textContent = `OK (${window.location.protocol}//${window.location.hostname})`;
        elems.micDiagSecure.className = 'mic-diag-value ok';
      } else if (!window.isSecureContext) {
        elems.micDiagSecure.textContent = `Blocked (${window.location.origin} is not HTTPS/localhost)`;
        elems.micDiagSecure.className = 'mic-diag-value err';
      } else {
        elems.micDiagSecure.textContent = 'mediaDevices unavailable';
        elems.micDiagSecure.className = 'mic-diag-value err';
      }
    }

    if (elems.micDiagPermission) {
      if (navigator.permissions && typeof navigator.permissions.query === 'function') {
        try {
          const perm = await navigator.permissions.query({ name: 'microphone' });
          elems.micDiagPermission.textContent = perm.state;
          elems.micDiagPermission.className =
            'mic-diag-value ' + (perm.state === 'granted' ? 'ok' : perm.state === 'denied' ? 'err' : 'warn');
        } catch (_) {
          elems.micDiagPermission.textContent = 'Prompt on start';
          elems.micDiagPermission.className = 'mic-diag-value';
        }
      } else {
        elems.micDiagPermission.textContent = 'Prompt on start';
        elems.micDiagPermission.className = 'mic-diag-value';
      }
    }

    if (elems.micDiagSampleRate && !state.micTest.active) {
      elems.micDiagSampleRate.textContent = state.micTest.sampleRate ? `${state.micTest.sampleRate} Hz -> 16000 Hz` : '-';
    }
    if (elems.micDiagTrack && !state.micTest.active) {
      elems.micDiagTrack.textContent = state.liveWS ? 'In use by LiveChat' : 'Not started';
      elems.micDiagTrack.className = 'mic-diag-value';
    }
  }

  function showMicTestStatus(text, type = 'info') {
    if (!elems.micTestStatus) return;
    elems.micTestStatus.textContent = text;
    elems.micTestStatus.className = `status-msg-modern ${type}`;
    elems.micTestStatus.classList.remove('hidden');
  }

  function formatMicError(err) {
    const name = err?.name || '';
    const msg = err?.message || String(err || 'Unknown microphone error');
    if (!window.isSecureContext || !navigator.mediaDevices?.getUserMedia) {
      return `Microphone API blocked on ${window.location.origin}. Open via http://127.0.0.1:<port>, http://localhost:<port>, or HTTPS.`;
    }
    if (name === 'NotAllowedError' || name === 'PermissionDeniedError') {
      return 'Microphone permission denied. In Android Chrome, tap the lock/tune icon in the address bar -> Permissions -> Microphone -> Allow, and check Android App Permissions for Chrome.';
    }
    if (name === 'NotFoundError' || name === 'DevicesNotFoundError') {
      return 'No microphone device found. Check headset/Bluetooth connection or select "Default System Microphone".';
    }
    if (name === 'NotReadableError' || name === 'TrackStartError') {
      return 'Microphone hardware is busy or unreadable (another app, call, or LiveChat session may be holding it).';
    }
    if (name === 'OverconstrainedError') {
      return `Selected microphone constraints could not be satisfied (${err.constraint || 'deviceId'}). Try "Default System Microphone".`;
    }
    return `Microphone error (${name || 'Error'}): ${msg}`;
  }

  async function startMicTest() {
    if (state.liveWS && state.liveWS.readyState === WebSocket.OPEN) {
      showMicTestStatus('LiveChat is currently connected and using the microphone. Disconnect LiveChat first to run an isolated hardware test.', 'info');
      return;
    }
    if (!navigator.mediaDevices || typeof navigator.mediaDevices.getUserMedia !== 'function') {
      showMicTestStatus(formatMicError(new Error('mediaDevices unavailable')), 'error');
      await refreshMicDiagnostics();
      return;
    }

    stopMicTest();
    state.micTest.starting = true;
    if (elems.btnMicTestToggle) {
      elems.btnMicTestToggle.textContent = '⏹ Stop Mic Test';
      elems.btnMicTestToggle.className = 'btn btn-danger';
    }
    showMicTestStatus('Requesting microphone access...', 'info');

    let testCtx;
    try {
      testCtx = createAudioContext();
      state.micTest.audioCtx = testCtx;
      if (testCtx.state === 'suspended') {
        await testCtx.resume();
      }
      if (!state.micTest.starting || state.micTest.audioCtx !== testCtx || testCtx.state === 'closed') {
        return;
      }

      const constraints = getMicAudioConstraints(state.selectedMicDeviceId);
      let stream;
      try {
        stream = await navigator.mediaDevices.getUserMedia({ audio: constraints });
      } catch (constraintErr) {
        if (state.selectedMicDeviceId && (constraintErr.name === 'OverconstrainedError' || constraintErr.name === 'NotFoundError')) {
          delete constraints.deviceId;
          state.selectedMicDeviceId = '';
          localStorage.removeItem('pigasus_mic_device_id');
          stream = await navigator.mediaDevices.getUserMedia({ audio: constraints });
        } else {
          throw constraintErr;
        }
      }

      if (!state.micTest.starting || state.micTest.audioCtx !== testCtx || testCtx.state === 'closed') {
        stream.getTracks().forEach((t) => t.stop());
        return;
      }

      state.micTest.stream = stream;
      state.micTest.active = true;
      state.micTest.starting = false;
      state.micTest.sampleRate = testCtx.sampleRate;

      await updateMicrophoneList();
      if (!state.micTest.active || state.micTest.audioCtx !== testCtx || testCtx.state === 'closed') {
        stream.getTracks().forEach((t) => t.stop());
        return;
      }

      const track = stream.getAudioTracks()[0];
      const settings = track && typeof track.getSettings === 'function' ? track.getSettings() : {};
      if (elems.micDiagSampleRate) {
        elems.micDiagSampleRate.textContent = `${testCtx.sampleRate} Hz -> 16000 Hz`;
        elems.micDiagSampleRate.className = 'mic-diag-value ok';
      }
      if (elems.micDiagTrack && track) {
        elems.micDiagTrack.textContent = `${track.readyState}${track.muted ? ' (OS muted!)' : ''}`;
        elems.micDiagTrack.className = 'mic-diag-value ' + (track.muted ? 'err' : 'ok');
      }
      if (elems.micTestTrackDetails && track) {
        elems.micTestTrackDetails.textContent = [
          `Label: ${track.label || '(unnamed device)'}`,
          `State: ${track.readyState} | Enabled: ${track.enabled} | Muted: ${track.muted}`,
          `AudioContext: ${testCtx.sampleRate} Hz (${testCtx.state})`,
          `Track Settings: rate=${settings.sampleRate || '-'}Hz, ch=${settings.channelCount || '-'}, AEC=${settings.echoCancellation}, NS=${settings.noiseSuppression}, AGC=${settings.autoGainControl}`,
        ].join('\n');
        elems.micTestTrackDetails.classList.remove('hidden');
      }

      const source = testCtx.createMediaStreamSource(stream);
      const analyser = testCtx.createAnalyser();
      analyser.fftSize = 256;
      analyser.smoothingTimeConstant = 0.3;

      const processor = testCtx.createScriptProcessor(4096, 1, 1);
      const muteGain = testCtx.createGain();
      muteGain.gain.value = 0;

      source.connect(analyser);
      analyser.connect(processor);
      processor.connect(muteGain);
      muteGain.connect(testCtx.destination);

      state.micTest.sourceNode = source;
      state.micTest.analyser = analyser;
      state.micTest.processor = processor;

      let latestPeak = 0;
      let latestRms = 0;
      let silentFrames = 0;

      processor.onaudioprocess = (e) => {
        if (!state.micTest.active) return;
        const input = e.inputBuffer.getChannelData(0);
        let peak = 0;
        let sumSq = 0;
        for (let i = 0; i < input.length; i++) {
          const v = Math.abs(input[i]);
          if (v > peak) peak = v;
          sumSq += input[i] * input[i];
        }
        latestPeak = peak;
        latestRms = Math.sqrt(sumSq / input.length);

        if (latestPeak < 0.002) {
          silentFrames++;
        } else {
          silentFrames = 0;
        }

        if (state.micTest.recording) {
          const pcm16 = downsampleTo16k(input, testCtx.sampleRate);
          if (pcm16 && pcm16.length > 0) {
            state.micTest.recordedChunks.push(pcm16);
          }
        }
      };

      const timeData = new Uint8Array(analyser.fftSize);
      const drawMicTest = () => {
        if (!state.micTest.active) return;
        state.micTest.animFrameId = requestAnimationFrame(drawMicTest);

        const pct = Math.min(100, Math.round(latestPeak * 140));
        const db = latestRms > 0.00001 ? Math.max(-90, Math.round(20 * Math.log10(latestRms))) : -Infinity;

        if (elems.micTestMeterFill) {
          elems.micTestMeterFill.style.width = pct + '%';
        }
        if (elems.micTestMeterRole) {
          elems.micTestMeterRole.setAttribute('aria-valuenow', String(pct));
        }
        if (elems.micTestLevelVal) {
          elems.micTestLevelVal.textContent = pct + '%';
        }
        if (elems.micTestDbVal) {
          elems.micTestDbVal.textContent = Number.isFinite(db) ? `${db} dBFS` : '-∞ dBFS';
        }
        if (elems.micTestSignalBadge) {
          if (track && track.muted) {
            elems.micTestSignalBadge.textContent = 'OS Muted';
          } else if (latestPeak > 0.95) {
            elems.micTestSignalBadge.textContent = 'Clipping';
          } else if (latestPeak > 0.02) {
            elems.micTestSignalBadge.textContent = 'Signal OK';
          } else if (silentFrames > 25) {
            elems.micTestSignalBadge.textContent = 'Silent / No Input';
          } else {
            elems.micTestSignalBadge.textContent = 'Listening...';
          }
        }

        const canvas = elems.micTestWaveform;
        if (canvas) {
          const ctx = canvas.getContext('2d');
          analyser.getByteTimeDomainData(timeData);
          ctx.clearRect(0, 0, canvas.width, canvas.height);
          ctx.lineWidth = 2;
          ctx.strokeStyle = latestPeak > 0.02 ? '#10b981' : '#38bdf8';
          ctx.beginPath();
          const sliceWidth = canvas.width / timeData.length;
          let x = 0;
          for (let i = 0; i < timeData.length; i++) {
            const v = timeData[i] / 128.0;
            const y = (v * canvas.height) / 2;
            if (i === 0) ctx.moveTo(x, y);
            else ctx.lineTo(x, y);
            x += sliceWidth;
          }
          ctx.lineTo(canvas.width, canvas.height / 2);
          ctx.stroke();
        }
      };
      state.micTest.animFrameId = requestAnimationFrame(drawMicTest);

      if (elems.btnMicTestRecord) {
        elems.btnMicTestRecord.disabled = false;
      }
      await refreshMicDiagnostics();
      showMicTestStatus('Microphone active! Speak into your mic to verify the level meter, or click "Record 5s & Play Back".', 'success');
    } catch (err) {
      console.warn('[mic-test] Failed:', err);
      stopMicTest();
      showMicTestStatus(formatMicError(err), 'error');
      await refreshMicDiagnostics();
    }
  }

  function stopMicTest() {
    state.micTest.active = false;
    state.micTest.starting = false;
    state.micTest.recording = false;
    if (state.micTest.recordTimer) {
      clearTimeout(state.micTest.recordTimer);
      state.micTest.recordTimer = null;
    }
    if (state.micTest.animFrameId) {
      cancelAnimationFrame(state.micTest.animFrameId);
      state.micTest.animFrameId = null;
    }
    if (state.micTest.playbackSource) {
      try {
        state.micTest.playbackSource.stop();
      } catch (_) {}
      state.micTest.playbackSource = null;
    }
    if (state.micTest.stream) {
      state.micTest.stream.getTracks().forEach((t) => t.stop());
      state.micTest.stream = null;
    }
    if (state.micTest.sourceNode) {
      try {
        state.micTest.sourceNode.disconnect();
      } catch (_) {}
      state.micTest.sourceNode = null;
    }
    if (state.micTest.processor) {
      try {
        state.micTest.processor.disconnect();
      } catch (_) {}
      state.micTest.processor = null;
    }
    if (state.micTest.analyser) {
      try {
        state.micTest.analyser.disconnect();
      } catch (_) {}
      state.micTest.analyser = null;
    }
    if (state.micTest.audioCtx) {
      state.micTest.audioCtx.close().catch(() => {});
      state.micTest.audioCtx = null;
    }

    if (elems.btnMicTestToggle) {
      elems.btnMicTestToggle.textContent = '🎤 Start Mic Test';
      elems.btnMicTestToggle.className = 'btn btn-primary';
    }
    if (elems.btnMicTestRecord) {
      elems.btnMicTestRecord.disabled = true;
      elems.btnMicTestRecord.textContent = '⏺ Record 5s & Play Back';
    }
    if (elems.micTestMeterFill) elems.micTestMeterFill.style.width = '0%';
    if (elems.micTestMeterRole) elems.micTestMeterRole.setAttribute('aria-valuenow', '0');
    if (elems.micTestLevelVal) elems.micTestLevelVal.textContent = '0%';
    if (elems.micTestDbVal) elems.micTestDbVal.textContent = '-∞ dBFS';
    if (elems.micTestSignalBadge) elems.micTestSignalBadge.textContent = 'Idle';
    if (elems.micTestWaveform) {
      const ctx = elems.micTestWaveform.getContext('2d');
      ctx.clearRect(0, 0, elems.micTestWaveform.width, elems.micTestWaveform.height);
    }
  }

  function toggleMicTestRecording() {
    if (!state.micTest.active || !state.micTest.audioCtx) return;

    if (state.micTest.recording) {
      finishMicTestRecording();
      return;
    }

    state.micTest.recordedChunks = [];
    state.micTest.recording = true;
    if (elems.btnMicTestRecord) {
      elems.btnMicTestRecord.textContent = '⏹ Stop & Play Back';
    }
    showMicTestStatus('Recording up to 5 seconds of 16kHz PCM audio... Speak now!', 'info');

    state.micTest.recordTimer = setTimeout(() => {
      if (state.micTest.recording) {
        finishMicTestRecording();
      }
    }, 5000);
  }

  async function finishMicTestRecording() {
    state.micTest.recording = false;
    if (state.micTest.recordTimer) {
      clearTimeout(state.micTest.recordTimer);
      state.micTest.recordTimer = null;
    }
    if (elems.btnMicTestRecord) {
      elems.btnMicTestRecord.textContent = '⏺ Record 5s & Play Back';
    }

    const chunks = state.micTest.recordedChunks;
    state.micTest.recordedChunks = [];
    if (!chunks || chunks.length === 0) {
      showMicTestStatus('No audio samples were captured. Check microphone permissions or hardware mute.', 'error');
      return;
    }

    let totalLen = 0;
    for (const c of chunks) totalLen += c.length;
    const float32 = new Float32Array(totalLen);
    let offset = 0;
    let maxAmp = 0;
    for (const c of chunks) {
      for (let i = 0; i < c.length; i++) {
        const f = c[i] / 32768.0;
        float32[offset++] = f;
        const abs = Math.abs(f);
        if (abs > maxAmp) maxAmp = abs;
      }
    }

    const ctx = state.micTest.audioCtx;
    if (!ctx || ctx.state === 'closed') return;
    if (ctx.state === 'suspended') {
      await ctx.resume();
    }

    const buf = ctx.createBuffer(1, float32.length, 16000);
    buf.getChannelData(0).set(float32);
    const src = ctx.createBufferSource();
    src.buffer = buf;
    src.connect(ctx.destination);
    state.micTest.playbackSource = src;
    src.onended = () => {
      if (state.micTest.playbackSource === src) {
        state.micTest.playbackSource = null;
      }
    };
    src.start(0);

    const secs = (float32.length / 16000).toFixed(1);
    if (maxAmp < 0.003) {
      showMicTestStatus(`Played back ${secs}s recording, but signal was near-silent (peak ${(maxAmp * 100).toFixed(1)}%). Check OS mic mute or try disabling Echo Cancellation.`, 'error');
    } else {
      showMicTestStatus(`Playing back ${secs}s 16kHz PCM recording (peak ${(maxAmp * 100).toFixed(0)}%). If you hear your voice clearly, LiveChat mic capture is working!`, 'success');
    }
  }

  async function playSpeakerTestTone() {
    try {
      const ctx = state.micTest.audioCtx && state.micTest.audioCtx.state !== 'closed'
        ? state.micTest.audioCtx
        : createAudioContext();
      const shouldClose = ctx !== state.micTest.audioCtx;
      if (ctx.state === 'suspended') {
        await ctx.resume();
      }

      const now = ctx.currentTime;
      const osc = ctx.createOscillator();
      const gain = ctx.createGain();
      osc.type = 'sine';
      osc.frequency.setValueAtTime(440, now);
      osc.frequency.setValueAtTime(554.37, now + 0.16);
      gain.gain.setValueAtTime(0.001, now);
      gain.gain.exponentialRampToValueAtTime(0.12, now + 0.03);
      gain.gain.exponentialRampToValueAtTime(0.001, now + 0.36);

      osc.connect(gain);
      gain.connect(ctx.destination);
      osc.start(now);
      osc.stop(now + 0.38);
      osc.onended = () => {
        if (shouldClose) {
          ctx.close().catch(() => {});
        }
      };
      showMicTestStatus(`Played speaker test chime (${ctx.sampleRate} Hz).`, 'info');
    } catch (err) {
      showMicTestStatus(`Speaker playback failed: ${err.message}`, 'error');
    }
  }

  // --- Helpers ---
  async function copyTextSafe(text) {
    if (!text) return;
    if (navigator.clipboard && typeof navigator.clipboard.writeText === 'function') {
      try {
        await navigator.clipboard.writeText(text);
        return;
      } catch (_) {}
    }
    const ta = document.createElement('textarea');
    ta.value = text;
    ta.setAttribute('readonly', '');
    ta.style.position = 'fixed';
    ta.style.opacity = '0';
    document.body.appendChild(ta);
    ta.select();
    try {
      document.execCommand('copy');
    } catch (_) {}
    document.body.removeChild(ta);
  }
  function arrayBufferToBase64(buffer) {
    let binary = '';
    const bytes = new Uint8Array(buffer);
    for (let i = 0; i < bytes.byteLength; i++) {
      binary += String.fromCharCode(bytes[i]);
    }
    return window.btoa(binary);
  }

  function base64ToArrayBuffer(base64) {
    const binaryString = window.atob(base64);
    const bytes = new Uint8Array(binaryString.length);
    for (let i = 0; i < binaryString.length; i++) {
      bytes[i] = binaryString.charCodeAt(i);
    }
    return bytes.buffer;
  }

  function mapVoiceToGemini(voice) {
    switch ((voice || '').toLowerCase().trim()) {
      case 'fola':
      case 'lora':
      case 'aoede':
        return 'Aoede';
      case 'arlo':
      case 'gero':
      case 'puck':
        return 'Puck';
      case 'cleo':
      case 'tari':
      case 'kore':
        return 'Kore';
      case 'finn':
      case 'olin':
      case 'fenrir':
        return 'Fenrir';
      case 'neno':
      case 'charon':
        return 'Charon';
      case 'zephyr':
        return 'Zephyr';
      case 'leda':
        return 'Leda';
      case 'orus':
        return 'Orus';
      default:
        return 'Aoede';
    }
  }

  // Run on DOM ready
  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', init);
  } else {
    init();
  }
})();
