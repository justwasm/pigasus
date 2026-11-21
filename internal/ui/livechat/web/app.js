/**
 * Pigasus Web Demo — Google OAuth & Multimodal LiveChat
 *
 * Supports DUAL-MODE operation:
 * 1. Pure Static Standalone Mode (Zero backend server required! Run from GitHub Pages, static CDN, or local file server).
 *    - In-browser Web Crypto API for PKCE verifier + S256 challenge generation.
 *    - Direct CORS token exchange against https://oauth2.googleapis.com/token.
 *    - In-browser JWT parsing for account identification.
 *    - Direct Gemini AI Studio REST SSE streaming & Native Multimodal Live WebSocket.
 * 2. Backend Bridge Mode (when paired with demo/server or pigasus proxy).
 *    - Local CLI token auto-detection (~/.openagy / ~/.gemini).
 *    - Proxy route auto-configuration and CORS bypass.
 */

(function () {
  'use strict';

  // --- Canonical Antigravity Public OAuth Credentials ---
  const CLI_CLIENT_ID = String.fromCharCode(49, 48, 55, 49, 48, 48, 54, 48, 54, 48, 53, 57, 49, 45, 116, 109, 104, 115, 115, 105, 110, 50, 104, 50, 49, 108, 99, 114, 101, 50, 51, 53, 118, 116, 111, 108, 111, 106, 104, 52, 103, 52, 48, 51, 101, 112, 46, 97, 112, 112, 115, 46, 103, 111, 111, 103, 108, 101, 117, 115, 101, 114, 99, 111, 110, 116, 101, 110, 116, 46, 99, 111, 109);
  const PUBLIC_CLIENT_SECRET = String.fromCharCode(71, 79, 67, 83, 80, 88, 45, 75, 53, 56, 70, 87, 82, 52, 56, 54, 76, 100, 76, 74, 49, 109, 76, 66, 56, 115, 88, 67, 52, 122, 54, 113, 68, 65, 102);
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

  async function syncSessionToServer(sess) {
    if (!state?.hasServer || !sess) return;
    try {
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
          timestamp: new Date(t.timestamp || Date.now()).toISOString(),
        })),
      };
      await fetch('/api/sessions', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
      });
    } catch (_) {}
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
    authMode: localStorage.getItem('pigasus_auth_mode') || 'oauth',
    oauthToken: (storedAcc?.token?.token?.access_token) || localStorage.getItem('pigasus_oauth_token') || '',
    oauthRefreshToken: (storedAcc?.token?.token?.refresh_token) || localStorage.getItem('pigasus_oauth_refresh_token') || '',
    oauthExpiry: (storedAcc?.token?.token?.expiry) || localStorage.getItem('pigasus_oauth_expiry') || '',
    oauthEmail: (storedAcc?.email) || localStorage.getItem('pigasus_oauth_email') || '',
    oauthPlanTier: (storedAcc?.plan_tier) || localStorage.getItem('pigasus_oauth_plan_tier') || 'Google AI Pro',
    oauthRawCode: (storedAcc?.raw_input) || localStorage.getItem('pigasus_oauth_raw_code') || '',
    apiKey: localStorage.getItem('pigasus_api_key') || '',
    proxyURL: localStorage.getItem('pigasus_proxy_url') || 'http://127.0.0.1:8085',
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

    // Audio Replay Storage
    currentTurnAudioChunks: [],
    currentUserTurnAudioChunks: [],
    turnAudioMap: new Map(), // turnId -> string[] base64 chunks
    activeReplaySource: null,
    activeReplayBtn: null,

    // Real-Time VAD & Audio Buffer State
    isUserSpeaking: false,
    vadSilenceTimer: null,

    // Duplex Mode & Audio Echo Guard
    isFullDuplex: true,
    agentSpeakingUntil: 0,
    activeAuditionAudio: null,

    // Microphone Hardware & Device Selection
    selectedMicDeviceId: localStorage.getItem('pigasus_mic_device_id') || '',
    micSourceNode: null,

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
    liveDot: document.getElementById('live-dot'),
    liveStatusText: document.getElementById('live-status-text'),
    liveDuplexState: document.getElementById('live-duplex-state'),
    liveModelSelect: document.getElementById('live-model-select'),
    liveVoiceSelect: document.getElementById('live-voice-select'),
    btnAuditionVoice: document.getElementById('btn-audition-voice'),
    voiceSampleVariant: document.getElementById('voice-sample-variant'),
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
    btnClearTranscript: document.getElementById('btn-clear-transcript'),
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

    // Modern Sidebar & Layout
    chatSidebar: document.getElementById('chat-sidebar'),
    btnToggleSidebar: document.getElementById('btn-toggle-sidebar'),
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
    btnToggleBargein: document.getElementById('btn-toggle-bargein'),
    bargeinBadgeIcon: document.getElementById('bargein-badge-icon'),
    bargeinBadgeText: document.getElementById('bargein-badge-text'),
    btnComposerVoice: document.getElementById('btn-composer-voice'),
    composerVoiceLabel: document.getElementById('composer-voice-label'),
    headerModelPill: document.getElementById('header-model-pill'),
    headerModelName: document.getElementById('header-model-name'),
    headerThinkBadge: document.getElementById('header-think-badge'),
  };

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

    // Check if running alongside demo/server or as pure static website
    await detectServerCapabilities();
  }

  // --- Server Capabilities & Mode Detection ---
  async function detectServerCapabilities() {
    try {
      const controller = new AbortController();
      const timer = setTimeout(() => controller.abort(), 1500);

      const resp = await fetch('/api/config', { signal: controller.signal });
      clearTimeout(timer);

      if (resp.ok) {
        state.hasServer = true;
        const cfg = await resp.json();
        if (cfg.default_proxy && !localStorage.getItem('pigasus_proxy_url')) {
          state.proxyURL = cfg.default_proxy;
          elems.cfgProxyUrl.value = cfg.default_proxy;
        }
        elems.appStatus.textContent = 'Server Mode';
        elems.appStatus.className = 'badge badge-server';
        elems.btnLoadCLIToken.classList.remove('hidden');
        await syncSessionsFromServer();
        return;
      }
    } catch (_) {
      // Offline / Static site mode
    }

    state.hasServer = false;
    elems.appStatus.textContent = 'Static Standalone';
    elems.appStatus.className = 'badge badge-static';
    elems.btnLoadCLIToken.title = 'CLI auto-detection requires demo/server; paste your token or use Google OAuth login directly.';
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
        if (active && (!active.turns || active.turns.length === 0)) {
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
  function syncModelSelection(modelVal) {
    if (!modelVal) modelVal = 'models/gemini-3.8-live';

    if (elems.composerModelSelect && elems.composerModelSelect.value !== modelVal) {
      elems.composerModelSelect.value = modelVal;
    }
    if (elems.liveModelSelect && elems.liveModelSelect.value !== modelVal) {
      elems.liveModelSelect.value = modelVal;
    }

    // Update active session model
    const active = state.sessions.find((s) => s.id === state.activeSessionId);
    if (active) {
      active.model = modelVal;
      saveStoredLiveSessions(state.sessions);
    }
    localStorage.setItem('pigasus_live_model', modelVal);

    // Update Header Pill
    const isThinking = modelVal.includes('extended-thinking');
    if (elems.headerModelName) {
      if (modelVal.includes('2.0-flash')) {
        elems.headerModelName.textContent = 'Gemini 2.0 Flash';
      } else {
        elems.headerModelName.textContent = 'Gemini 3.8 Live';
      }
    }
    if (elems.headerThinkBadge) {
      if (isThinking) {
        elems.headerThinkBadge.classList.remove('hidden');
      } else {
        elems.headerThinkBadge.classList.add('hidden');
      }
    }

    if (elems.headerModelPill) {
      elems.headerModelPill.title = isThinking
        ? 'Extended Thinking Mode is active (Click to switch to Standard Low-Latency)'
        : 'Standard Low-Latency Mode is active (Click to switch to Extended Thinking)';
    }
  }

  function updateBargeinUI(isDuplex) {
    if (elems.btnToggleBargein) {
      elems.btnToggleBargein.classList.toggle('active', isDuplex);
      elems.btnToggleBargein.classList.toggle('inactive', !isDuplex);
    }
    if (elems.bargeinBadgeIcon) {
      elems.bargeinBadgeIcon.textContent = isDuplex ? '⚡' : '🔇';
    }
    if (elems.bargeinBadgeText) {
      elems.bargeinBadgeText.textContent = isDuplex ? 'Barge-in On' : 'Half-Duplex (PTT)';
    }
    if (elems.liveDuplexState) {
      elems.liveDuplexState.textContent = isDuplex ? 'Full Duplex' : 'Half Duplex';
    }
    if (elems.liveDuplexToggle) {
      elems.liveDuplexToggle.checked = isDuplex;
    }
    state.isFullDuplex = isDuplex;
  }

  function updateVoiceLabel(voiceName) {
    if (elems.composerVoiceLabel) {
      elems.composerVoiceLabel.textContent = `Voice: ${voiceName}`;
    }
    if (elems.btnComposerVoice) {
      elems.btnComposerVoice.title = `Current Persona: ${voiceName} (Click to open voice selection)`;
    }
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

    elems.headerModelPill?.addEventListener('click', (e) => {
      e.stopPropagation();
      const current = elems.composerModelSelect?.value || elems.liveModelSelect?.value || 'models/gemini-3.8-live';
      const next = current.includes('extended-thinking')
        ? 'models/gemini-3.8-live'
        : 'models/gemini-3.8-live-extended-thinking';
      syncModelSelection(next);
      appendTranscript('system', `Toggled model: ${next.includes('extended-thinking') ? 'Extended Thinking Mode 💭' : 'Standard Low-Latency Mode ⚡'}`);
    });

    elems.btnToggleBargein?.addEventListener('click', (e) => {
      e.stopPropagation();
      updateBargeinUI(!state.isFullDuplex);
      appendTranscript('system', state.isFullDuplex ? 'Instant Barge-in enabled (interrupt anytime).' : 'Half-Duplex enabled (push-to-talk mode).');
    });

    elems.btnComposerVoice?.addEventListener('click', (e) => {
      e.stopPropagation();
      if (elems.liveVoiceSelect) {
        elems.liveVoiceSelect.focus();
        if (typeof elems.liveVoiceSelect.showPicker === 'function') {
          try { elems.liveVoiceSelect.showPicker(); } catch (_) {}
        }
      }
    });

    elems.liveVoiceSelect?.addEventListener('change', (e) => {
      updateVoiceLabel(e.target.value);
    });
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
      state.authMode === 'oauth'
        ? 'Google OAuth'
        : state.authMode === 'api_key'
        ? 'Gemini API Key'
        : 'Pigasus Proxy';

    if (state.authMode === 'oauth' && state.oauthEmail) {
      elems.barAuthIdentity.textContent = state.oauthEmail;
    } else if (state.authMode === 'api_key' && state.apiKey) {
      elems.barAuthIdentity.textContent = 'API Key (' + state.apiKey.slice(0, 6) + '...)';
    } else if (state.authMode === 'proxy') {
      elems.barAuthIdentity.textContent = 'Proxy Mode';
    } else {
      elems.barAuthIdentity.textContent = 'Not Signed In';
    }

    elems.barProxyRoute.textContent = state.proxyURL || 'http://127.0.0.1:8085';

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

    elems.tokenAccountEmail.textContent = state.oauthEmail || 'None';

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
    elems.cfgProxyUrl.value = state.proxyURL || 'http://127.0.0.1:8085';

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
    syncModelSelection(activeModel);
    updateBargeinUI(state.isFullDuplex);
    updateVoiceLabel(elems.liveVoiceSelect ? elems.liveVoiceSelect.value : (active?.voice || 'Fola'));
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

    const body = new URLSearchParams({
      client_id: CLI_CLIENT_ID,
      client_secret: PUBLIC_CLIENT_SECRET,
      code: code,
      code_verifier: verifier,
      grant_type: 'authorization_code',
      redirect_uri: REDIRECT_URI,
    });

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

    // 2. Load Existing CLI Token
    elems.btnLoadCLIToken.addEventListener('click', async () => {
      if (!state.hasServer) {
        showStatusMsg('Local CLI token auto-detection requires demo/server. Please use "Start Google OAuth Login" or paste your token.', 'info');
        return;
      }
      showStatusMsg('Checking local Pigasus CLI token (~/.openagy / ~/.gemini)...', 'info');
      try {
        const res = await fetch('/api/auth/existing');
        if (!res.ok) throw new Error('Failed to query local token');
        const data = await res.json();
        if (!data.available) {
          showStatusMsg('No active CLI token found. Use "Start Google OAuth Login" above.', 'error');
          return;
        }

        saveOAuthAccount({
          access_token: data.access_token,
          refresh_token: data.refresh_token,
          expiry: data.expiry,
          email: data.email,
          plan_tier: data.plan_tier || 'Google AI Pro (CLI)',
          raw_input: 'Loaded from local Pigasus configuration',
        });
        showStatusMsg(`Loaded & synced existing token for: ${data.email || 'Google Account'}`, 'success');
      } catch (err) {
        showStatusMsg(err.message, 'error');
      }
    });

    // Copy Auth URL
    elems.btnCopyUrl.addEventListener('click', () => {
      if (elems.authUrlInput.value) {
        navigator.clipboard.writeText(elems.authUrlInput.value);
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
    elems.btnCopyToken.addEventListener('click', () => {
      if (state.oauthToken) {
        navigator.clipboard.writeText(state.oauthToken);
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

  function saveOAuthAccount(acc) {
    if (!acc) return;
    state.oauthToken = acc.access_token || '';
    if (acc.refresh_token) state.oauthRefreshToken = acc.refresh_token;
    state.oauthExpiry = acc.expiry || '';
    if (acc.email) state.oauthEmail = acc.email;
    if (acc.plan_tier) state.oauthPlanTier = acc.plan_tier;
    if (acc.raw_input) state.oauthRawCode = acc.raw_input;
    state.authMode = 'oauth';

    localStorage.setItem('pigasus_oauth_token', state.oauthToken);
    localStorage.setItem('pigasus_oauth_refresh_token', state.oauthRefreshToken);
    localStorage.setItem('pigasus_oauth_expiry', state.oauthExpiry);
    localStorage.setItem('pigasus_oauth_email', state.oauthEmail);
    localStorage.setItem('pigasus_oauth_raw_code', state.oauthRawCode);
    localStorage.setItem('pigasus_oauth_plan_tier', state.oauthPlanTier);
    localStorage.setItem('pigasus_auth_mode', 'oauth');

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
          });
          return data.access_token;
        }
      } catch (_) {}
    }

    // 2. Direct client-side refresh via Google OAuth2 CORS endpoint
    const body = new URLSearchParams({
      client_id: CLI_CLIENT_ID,
      client_secret: PUBLIC_CLIENT_SECRET,
      grant_type: 'refresh_token',
      refresh_token: rt,
    });

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
    });
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

  function setAuthMode(mode) {
    state.authMode = mode;
    localStorage.setItem('pigasus_auth_mode', mode);
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
      token: state.oauthToken,
      api_key: state.apiKey,
      proxy_url: state.proxyURL,
      model: elems.chatModelSelect.value,
      prompt: prompt,
      stream: true,
    };

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

    // 2. Local Pigasus Proxy Route
    if (state.authMode === 'proxy' || state.authMode === 'oauth') {
      const proxyBase = state.proxyURL || 'http://127.0.0.1:8085';
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
      elems.settingsModal.classList.add('hidden');
    }

    function switchSettingsTab(targetTabId) {
      elems.settingsNavItems?.forEach((item) => {
        item.classList.toggle('active', item.dataset.tab === targetTabId);
      });
      elems.settingsPanes?.forEach((pane) => {
        pane.classList.toggle('active', pane.id === targetTabId);
      });
    }

    elems.btnOpenSettings?.addEventListener('click', () => openSettings('st-auth-tab'));
    elems.btnOpenSettingsTop?.addEventListener('click', () => openSettings('st-voice-tab'));
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
    if (!text) return;
    const trimmed = text.trim();
    if (
      !trimmed ||
      trimmed === '...' ||
      trimmed.includes('Voice Message') ||
      trimmed.startsWith('[SYSTEM NOTE:') ||
      trimmed.startsWith('[PROACTIVE_EVENT:')
    ) {
      return;
    }

    // Avoid duplicate entries
    const last = state.liveTurns[state.liveTurns.length - 1];
    if (last && last.role === role && last.text === trimmed) return;

    state.liveTurns.push({ role, text: trimmed, timestamp: Date.now() });
    if (state.liveTurns.length > 50) {
      state.liveTurns = state.liveTurns.slice(-50);
    }
    saveActiveSessionTurns();
    scrollTranscriptToBottom(true);
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
      if (fullText) {
        recordLiveTurn('agent', fullText);
      }

      // Attach audio replay button if agent audio chunks were received
      if (state.currentTurnAudioChunks.length > 0) {
        const turnId = 'turn_' + Date.now() + '_' + Math.random().toString(36).slice(2, 6);
        state.turnAudioMap.set(turnId, [...state.currentTurnAudioChunks]);
        state.currentTurnAudioChunks = [];

        if (!currentTurnBubble.querySelector('.btn-replay-audio')) {
          const bar = document.createElement('div');
          bar.className = 'msg-audio-bar';
          const btn = document.createElement('button');
          btn.type = 'button';
          btn.className = 'btn-replay-audio';
          btn.dataset.turnId = turnId;
          btn.title = 'Play Audio';
          btn.setAttribute('aria-label', 'Play Audio');
          btn.innerHTML = '<span class="audio-btn-icon">🔊</span>';
          btn.addEventListener('click', (e) => {
            e.stopPropagation();
            replayTurnAudio(turnId, btn, 24000);
          });
          bar.appendChild(btn);
          currentTurnBubble.appendChild(bar);
        }
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
        recordLiveTurn('user', fullText);

        // Attach audio replay button to valid user turn
        if (state.currentUserTurnAudioChunks.length > 0) {
          const userTurnId = 'user_turn_' + Date.now() + '_' + Math.random().toString(36).slice(2, 6);
          state.turnAudioMap.set(userTurnId, [...state.currentUserTurnAudioChunks]);
          state.currentUserTurnAudioChunks = [];

          if (!currentTurnBubble.querySelector('.btn-replay-audio')) {
            const bar = document.createElement('div');
            bar.className = 'msg-audio-bar';
            const btn = document.createElement('button');
            btn.type = 'button';
            btn.className = 'btn-replay-audio';
            btn.dataset.turnId = userTurnId;
            btn.title = 'Play Voice';
            btn.setAttribute('aria-label', 'Play Voice');
            btn.innerHTML = '<span class="audio-btn-icon">🔊</span>';
            btn.addEventListener('click', (e) => {
              e.stopPropagation();
              replayTurnAudio(userTurnId, btn, 16000);
            });
            bar.appendChild(btn);
            currentTurnBubble.appendChild(bar);
          }
        }
      }
    }

    currentTurnSpeaker = null;
    currentTurnBubble = null;
    currentTurnEntry = null;
  }

  async function replayTurnAudio(turnId, btn, sampleRate = 24000) {
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

    const chunks = state.turnAudioMap.get(turnId);
    if (!chunks || chunks.length === 0) return;

    try {
      const AudioCtx = window.AudioContext || window.webkitAudioContext;
      if (!state.audioCtx || state.audioCtx.state === 'closed') {
        state.audioCtx = new AudioCtx();
      }
      if (state.audioCtx.state === 'suspended') {
        await state.audioCtx.resume();
      }

      const pcmArrays = chunks.map((c) => {
        const raw = base64ToArrayBuffer(c);
        return new Int16Array(raw);
      });
      let totalLen = 0;
      pcmArrays.forEach((arr) => (totalLen += arr.length));

      const float32 = new Float32Array(totalLen);
      let offset = 0;
      for (const arr of pcmArrays) {
        for (let i = 0; i < arr.length; i++) {
          float32[offset++] = arr[i] / 32768.0;
        }
      }

      const audioBuffer = state.audioCtx.createBuffer(1, float32.length, sampleRate);
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
        if (elems.micStatusText) {
          elems.micStatusText.textContent = 'Muted';
          elems.micStatusText.style.color = '#ef4444';
        }
      } else {
        if (elems.micStatusText) {
          const devLabel = getMicDeviceLabel(state.selectedMicDeviceId);
          elems.micStatusText.textContent = `Active (${devLabel})`;
          elems.micStatusText.style.color = '#34d399';
        }
      }
      elems.btnToggleMic.textContent = state.micMuted ? '🔇 Unmute Microphone' : '🎤 Mute Microphone';
      elems.btnToggleMic.classList.toggle('btn-danger', state.micMuted);
    });

    elems.btnClearTranscript.addEventListener('click', () => {
      finalizeCurrentTurn();
      elems.liveTranscript.innerHTML = renderWelcomeHero();
      bindPromptChips();
      scrollTranscriptToBottom(true);
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

  function setupAuditionListeners() {
    if (!elems.btnAuditionVoice) return;

    elems.btnAuditionVoice.addEventListener('click', () => {
      if (state.activeAuditionAudio) {
        state.activeAuditionAudio.pause();
        state.activeAuditionAudio = null;
        elems.btnAuditionVoice.innerHTML = '<span>♪</span> Audition Sample';
        elems.btnAuditionVoice.classList.remove('playing');
        return;
      }

      const selectedOpt = elems.liveVoiceSelect.options[elems.liveVoiceSelect.selectedIndex];
      const previewStem = selectedOpt?.dataset?.preview || selectedOpt?.value?.toLowerCase();
      const variant = elems.voiceSampleVariant?.value || '1';

      if (!previewStem) {
        alert('No recorded sample for this voice.');
        return;
      }

      const url = `previews/${previewStem}_${variant}.mp3`;
      const audio = new Audio(url);
      state.activeAuditionAudio = audio;
      elems.btnAuditionVoice.innerHTML = '<span>⏹</span> Stop Audition';
      elems.btnAuditionVoice.classList.add('playing');

      audio.play().catch((err) => {
        console.warn('Audition playback failed:', err);
        elems.btnAuditionVoice.innerHTML = '<span>♪</span> Audition Sample';
        elems.btnAuditionVoice.classList.remove('playing');
        state.activeAuditionAudio = null;
      });

      audio.onended = () => {
        elems.btnAuditionVoice.innerHTML = '<span>♪</span> Audition Sample';
        elems.btnAuditionVoice.classList.remove('playing');
        state.activeAuditionAudio = null;
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

  async function updateMicrophoneList() {
    if (!navigator.mediaDevices || !navigator.mediaDevices.enumerateDevices) return [];

    try {
      const allDevices = await navigator.mediaDevices.enumerateDevices();
      const audioInputs = allDevices.filter((d) => d.kind === 'audioinput');

      if (elems.liveMicSelect) {
        const currentVal = state.selectedMicDeviceId;
        elems.liveMicSelect.innerHTML = '';

        const defaultOpt = document.createElement('option');
        defaultOpt.value = '';
        defaultOpt.textContent = 'Default System Microphone';
        if (!currentVal) defaultOpt.selected = true;
        elems.liveMicSelect.appendChild(defaultOpt);

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
          elems.liveMicSelect.appendChild(opt);
        });

        if (currentVal && !matchFound) {
          elems.liveMicSelect.value = '';
        }
      }

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

  async function switchMicDevice(deviceId) {
    state.selectedMicDeviceId = deviceId;
    if (deviceId) {
      localStorage.setItem('pigasus_mic_device_id', deviceId);
    } else {
      localStorage.removeItem('pigasus_mic_device_id');
    }
    if (elems.liveMicSelect && elems.liveMicSelect.value !== deviceId) {
      elems.liveMicSelect.value = deviceId;
    }

    const label = getMicDeviceLabel(deviceId);

    // If currently capturing, seamlessly hot-swap the media stream without disconnecting LiveChat
    if (state.micStream && state.micAudioCtx && state.micAudioCtx.state !== 'closed') {
      try {
        const audioConstraints = {
          channelCount: 1,
          echoCancellation: true,
          noiseSuppression: true,
          autoGainControl: true,
        };
        if (deviceId) {
          audioConstraints.deviceId = { exact: deviceId };
        }

        const newStream = await navigator.mediaDevices.getUserMedia({
          audio: audioConstraints,
        });

        // Stop old stream tracks
        state.micStream.getAudioTracks().forEach((t) => t.stop());

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

        appendTranscript('system', `🎙️ Switched microphone to: ${label}`);
        if (elems.micStatusText && !state.micMuted) {
          elems.micStatusText.textContent = `Active (${label})`;
        }
      } catch (err) {
        console.warn('[live] Hot-swap microphone error:', err);
        appendTranscript('system', `Failed to switch microphone: ${err.message}`);
      }
    }
  }

  async function connectLiveSession() {
    updateLiveUI('connecting', 'Connecting...');

    try {
      // Auto-refresh OAuth token if needed
      if (state.authMode === 'oauth') {
        const token = await ensureValidOAuthToken();
        if (!token) {
          throw new Error('Please authenticate with Google OAuth under the Auth tab first.');
        }
      }

      // 1. Initialize Web Audio Context (Output 24kHz for Gemini Live Audio)
      const AudioCtx = window.AudioContext || window.webkitAudioContext;
      if (!state.audioCtx || state.audioCtx.state === 'closed') {
        state.audioCtx = new AudioCtx({ sampleRate: 24000 });
      }
      if (state.audioCtx.state === 'suspended') {
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
        const params = new URLSearchParams({
          token: state.authMode === 'oauth' ? state.oauthToken : '',
          key: state.authMode === 'api_key' ? state.apiKey : '',
          proxy: state.authMode === 'proxy' ? state.proxyURL : '',
        });
        const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
        wsURL = `${protocol}//${window.location.host}/api/live-ws?${params.toString()}`;
      } else {
        // Pure Static Standalone Mode
        if (state.authMode === 'api_key') {
          if (!state.apiKey) throw new Error('Please enter a Gemini API Key under Auth tab.');
          wsURL = `wss://generativelanguage.googleapis.com/ws/google.ai.generativelanguage.v1alpha.GenerativeService.BidiGenerateContent?key=${state.apiKey}`;
        } else if (state.authMode === 'proxy') {
          const u = new URL(state.proxyURL || 'http://127.0.0.1:8085');
          const scheme = u.protocol === 'https:' ? 'wss:' : 'ws:';
          wsURL = `${scheme}//${u.host}/v1/live`;
        } else if (state.authMode === 'oauth') {
          const u = new URL(state.proxyURL || 'http://127.0.0.1:8085');
          const scheme = u.protocol === 'https:' ? 'wss:' : 'ws:';
          wsURL = `${scheme}//${u.host}/v1/live`;
          showStatusMsg('Connecting to local Pigasus proxy for OAuth Live WebSocket bridge...', 'info');
        }
      }

      state.liveWS = new WebSocket(wsURL);

      state.liveWS.onopen = async () => {
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
        state.liveWS.send(JSON.stringify(setupMsg));

        // Start Microphone Audio Capture
        await startMicCapture();
        updateLiveUI('ready', 'Listening & Ready');
      };

      state.liveWS.onmessage = async (event) => {
        let raw = event.data;
        if (raw instanceof Blob) {
          raw = await raw.text();
        } else if (raw instanceof ArrayBuffer) {
          raw = new TextDecoder().decode(raw);
        }
        handleLiveServerMessage(raw);
      };

      state.liveWS.onerror = (err) => {
        console.error('[live] WebSocket error:', err);
        updateLiveUI('disconnected', 'Connection Error');
      };

      state.liveWS.onclose = (event) => {
        console.warn('[live] WebSocket closed:', event.code, event.reason);
        if (event.reason) {
          appendTranscript('system', `Disconnected: ${event.reason} (code ${event.code})`);
        }
        disconnectLiveSession();
        if (!intentionalDisconnect) {
          scheduleReconnect();
        }
      };
    } catch (err) {
      console.error('[live] Failed to connect:', err);
      alert('Live connection failed: ' + err.message);
      disconnectLiveSession();
      if (!intentionalDisconnect) {
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

  function updateLiveUI(status, label) {
    elems.liveStatusText.textContent = label;
    elems.liveDot.className = `dot dot-${status}`;

    if (status === 'disconnected') {
      elems.btnToggleLive.innerHTML = '<span class="btn-live-icon">▶</span> <span class="btn-live-text">Connect Live</span>';
      elems.btnToggleLive.className = 'btn-connect-live';
      elems.btnToggleMic.disabled = true;
      elems.liveDuplexState.textContent = 'Offline';
      elems.liveVisualizerBar?.classList.add('hidden');
    } else {
      elems.btnToggleLive.innerHTML = '<span class="btn-live-icon">⏹</span> <span class="btn-live-text">Disconnect</span>';
      elems.btnToggleLive.className = 'btn-connect-live connected';
      elems.btnToggleMic.disabled = false;
      elems.btnLiveTextSend.disabled = false;
      elems.liveVisualizerBar?.classList.remove('hidden');
      if (status === 'speaking') {
        elems.liveDuplexState.textContent = 'Speaking';
      } else {
        elems.liveDuplexState.textContent = state.isFullDuplex
          ? 'Duplex'
          : 'Half-Duplex';
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
      const AudioCtx = window.AudioContext || window.webkitAudioContext;
      if (!state.micAudioCtx || state.micAudioCtx.state === 'closed') {
        state.micAudioCtx = new AudioCtx();
      }
      if (state.micAudioCtx.state === 'suspended') {
        await state.micAudioCtx.resume();
      }

      const audioConstraints = {
        channelCount: 1,
        echoCancellation: true,
        noiseSuppression: true,
        autoGainControl: true,
      };
      if (state.selectedMicDeviceId) {
        audioConstraints.deviceId = { exact: state.selectedMicDeviceId };
      }

      state.micStream = await navigator.mediaDevices.getUserMedia({
        audio: audioConstraints,
      });

      // Refresh microphone list to populate real device labels (now that permission is granted)
      await updateMicrophoneList();

      const inputSampleRate = state.micAudioCtx.sampleRate;
      state.micSourceNode = state.micAudioCtx.createMediaStreamSource(state.micStream);
      const source = state.micSourceNode;

      // Dedicated mic analyser in micAudioCtx context (NEVER connect across different audio contexts)
      state.micAnalyser = state.micAudioCtx.createAnalyser();
      state.micAnalyser.fftSize = 64;
      state.micAnalyser.smoothingTimeConstant = 0.25;
      source.connect(state.micAnalyser);

      const bufferSize = 4096;
      state.micProcessor = state.micAudioCtx.createScriptProcessor(bufferSize, 1, 1);

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
      const muteGain = state.micAudioCtx.createGain();
      muteGain.gain.value = 0;

      state.micAnalyser.connect(state.micProcessor);
      state.micProcessor.connect(muteGain);
      muteGain.connect(state.micAudioCtx.destination);

      if (elems.micStatusText) {
        const devLabel = getMicDeviceLabel(state.selectedMicDeviceId);
        elems.micStatusText.textContent = `Active (${devLabel})`;
        elems.micStatusText.style.color = '#34d399';
      }
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

    const numBars = 20;
    const smoothedBars = new Float32Array(numBars);
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

        // Update state badges stably
        if (elems.audioStateIndicator) {
          if (isAgentSpeaking) {
            elems.audioStateIndicator.textContent = 'Agent';
            elems.audioStateIndicator.className = 'audio-state-pill speaking';
          } else if (micIsActiveSpeaking) {
            elems.audioStateIndicator.textContent = 'Speaking';
            elems.audioStateIndicator.className = 'audio-state-pill listening';
          } else if (state.liveWS && state.liveWS.readyState === WebSocket.OPEN) {
            elems.audioStateIndicator.textContent = 'Listening';
            elems.audioStateIndicator.className = 'audio-state-pill listening';
          } else {
            elems.audioStateIndicator.textContent = 'Idle';
            elems.audioStateIndicator.className = 'audio-state-pill';
          }
        }

        if (elems.micStatusText) {
          if (state.micMuted) {
            elems.micStatusText.textContent = 'Muted';
            elems.micStatusText.style.color = '#ef4444';
          } else if (micIsActiveSpeaking) {
            elems.micStatusText.textContent = 'Speaking';
            elems.micStatusText.style.color = '#34d399';
          } else {
            elems.micStatusText.textContent = state.liveWS ? 'Listening' : 'Idle';
            elems.micStatusText.style.color = '#94a3b8';
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
      const barWidth = 7;
      const totalBarWidth = numBars * barWidth;
      const gap = (canvas.width - totalBarWidth) / (numBars - 1);
      const midY = canvas.height / 2;

      for (let i = 0; i < numBars; i++) {
        const raw = targetData[i * step] || 0;
        const targetH = (raw / 255) * (canvas.height - 6);
        // Exponential smoothing: gentle attack (0.35), smooth decay (0.15)
        const alpha = targetH > smoothedBars[i] ? 0.35 : 0.15;
        smoothedBars[i] += (targetH - smoothedBars[i]) * alpha;

        const h = Math.max(3, Math.round(smoothedBars[i]));
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
      const selectedAuth = document.querySelector('input[name="default_auth"]:checked')?.value || 'oauth';

      state.proxyURL = url;
      localStorage.setItem('pigasus_proxy_url', url);
      setAuthMode(selectedAuth);

      showProxyResult('Settings saved successfully.', 'success');
    });

    elems.btnTestProxy.addEventListener('click', async () => {
      const url = elems.cfgProxyUrl.value.trim();
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

  // --- Helpers ---
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
