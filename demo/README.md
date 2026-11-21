# Pigasus · Google OAuth & Multimodal LiveChat Web Demo

A web application demonstrating Google OAuth 2.0 PKCE authentication, Web Text Chat, and Gemini Multimodal Live voice chat.

**Supports Dual-Mode Operation**:
1. **Pure Static Standalone Mode**: Can run as a 100% client-side static site without any backend server (compatible with GitHub Pages, Cloudflare Pages, Vercel, or `python3 -m http.server`).
2. **Backend Bridge Mode**: When run with `demo/server` or `./demo/run.sh`, adds local CLI token auto-detection (`~/.openagy` / `~/.gemini`), server-side secret management, and streaming helpers.

---

## Key Capabilities

1. **Google OAuth 2.0 PKCE Flow (100% In-Browser Capable)**:
   - **Client-Side Cryptography**: Uses the browser's native `window.crypto.subtle` API to generate cryptographically random PKCE verifiers, SHA-256 challenges, and state strings.
   - **Direct CORS Token Exchange**: Google's OAuth 2.0 token endpoint (`https://oauth2.googleapis.com/token`) natively supports CORS (`Access-Control-Allow-Origin: *`). The browser exchanges the authorization code directly with Google.
   - **Client-Side JWT Parsing**: Decodes the returned `id_token` in JavaScript to extract the user's Google account email.
   - **Smart Code Sanitizer**: Paste either the raw authorization code (`4/0Abc...`) or the entire redirect URL (`https://antigravity.google/oauth-callback?code=...`); the app automatically parses and decodes the token.

2. **Multimodal Live Voice & Text Chat**:
   - **Direct WebSocket to Gemini Live**: In static mode with a Gemini API Key, the browser opens a native WebSocket directly to `wss://generativelanguage.googleapis.com/ws/google.ai.generativelanguage.v1alpha.GenerativeService.BidiGenerateContent?key=...`.
   - **16kHz PCM Audio Capture**: Uses the Web Audio API to capture, resample, and convert microphone input to 16-bit mono PCM.
   - **24kHz PCM Voice Playback**: Real-time jitter-free audio queue rendering with interruption truncation.
   - **Waveform Visualizer**: Live HTML5 canvas frequency visualizer.

3. **Web Text Chat**:
   - **Direct Browser CORS Streaming**: Queries Google Generative Language API via direct browser `fetch` and Server-Sent Events (SSE).
   - **IME-Safe Enter Submission**: Adheres to modern web accessibility guidance; `Enter` sends the message while preserving Input Method Editor (IME) composition for Asian languages without premature submission.

4. **Pigasus Proxy & Alt Backend Integration**:
   - Route traffic through a local `pigproxy` / `agyproxy` instance (e.g. `http://127.0.0.1:8085` or `OPENAGY_VOICE_ALT_BACKEND`).
   - Acts as an alternate backend for LiveChat and injects credentials at the proxy layer.

5. **Google AI Studio (Gemini API Key) Fallback**:
   - Switch between Google OAuth Bearer token, Gemini API Key (`AIza...`), or Pigasus Proxy routing at any time.

---

## Directory Structure

```
demo/
├── README.md           # Documentation
├── run.sh              # One-line startup script (launches demo/server)
├── server/             # Optional Go backend bridge & API handlers
│   ├── main.go         # HTTP server, routing, and CORS middleware
│   ├── oauth.go        # Server-side PKCE session storage & code exchange
│   ├── chat.go         # Unified text chat handler (OAuth / API Key / Proxy)
│   ├── live_ws.go      # Full-duplex WebSocket bridge to Gemini Live
│   └── server_test.go  # Unit tests
└── web/                # 100% Standalone Static Frontend
    ├── index.html      # Semantic HTML UI with accessible form elements
    ├── style.css       # Dark slate responsive styling
    └── app.js          # Dual-mode JS (Web Crypto PKCE, Web Audio, WebSocket)
```

---

## How to Run

### Method 1: Pure Static Website (Zero Backend Server)

You can serve the `demo/web/` directory with any static web server:

```bash
# Using Python
cd demo/web
python3 -m http.server 8080

# Or using npx serve
npx serve demo/web

# Or deploy demo/web directly to GitHub Pages / Cloudflare Pages / Vercel
```

Open `http://localhost:8080` in your browser. The top-right badge will show **Static Standalone**. All PKCE generations and Google OAuth token exchanges happen 100% inside your browser!

### Method 2: With the Backend Server

To enable local CLI token detection (`~/.openagy` auto-import):

```bash
./demo/run.sh
```

Or run the consolidated LiveChat binary in Web mode:

```bash
go run ./cmd/livechat --web -port 8090
```

The top-right badge will show **Server Mode**.

---

## Verification & Tests

To execute the unit tests for LiveChat and embedded web server:

```bash
go test -v ./cmd/livechat/...
```

To verify repository architectural compliance:

```bash
make arch
```
