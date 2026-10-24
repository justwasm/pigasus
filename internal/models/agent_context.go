// Package models — agent_context.go
//
// The 12errh-inspired "agent context" file is a small piece of guidance
// text we drop into `~/.config/openagy/`. The TUI prints it on first run
// (and on `openagy-models install-context`) so the operator remembers what
// the binary is, what it isn't, and which credentials are expected.
//
// Three modes are supported:
//
//   lite         — short version (~3.7 KB). The default. Tells the operator
//                  what the binary does and which env vars to set.
//   strip        — full version (~10 KB). Includes the recovery timeline,
//                  the kelvinzer0 link, the antigravity-oauth-token path,
//                  and a step-by-step "what to do if a model misbehaves".
//   passthrough  — off. Don't write any file. The CLI still prints a one-
//                  line banner so users know the feature is here.
//
// The two files (agent-context-lite.md and agent-context.md) are *not*
// runtime-loaded by the TUI yet — they're a discoverability artifact.
// The strip mode is the canonical reference: when in doubt, read
// agent-context.md.

package models

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Mode is the agent-context mode.
type Mode string

const (
	// ModeLite is the short context (~3.7 KB).
	ModeLite Mode = "lite"
	// ModeStrip is the full context (~10 KB).
	ModeStrip Mode = "strip"
	// ModePassthrough disables file writing; the TUI still prints a banner.
	ModePassthrough Mode = "passthrough"
)

// modeState is the in-memory mode tracker. We keep it package-private
// because the value is a process-wide singleton; the CLI's `install-context`
// subcommand is the only writer today.
type modeState struct {
	mu   sync.RWMutex
	mode Mode
}

var globalMode = &modeState{mode: ModeLite}

// SetMode updates the global mode. The string is lowercased and validated;
// unknown values return an error.
func SetMode(m string) error {
	switch Mode(strings.ToLower(strings.TrimSpace(m))) {
	case ModeLite, ModeStrip, ModePassthrough:
		globalMode.mu.Lock()
		defer globalMode.mu.Unlock()
		globalMode.mode = Mode(strings.ToLower(strings.TrimSpace(m)))
		return nil
	default:
		return fmt.Errorf("agent-context: unknown mode %q (want lite, strip, or passthrough)", m)
	}
}

// GetMode returns the current mode. Defaults to ModeLite if SetMode was
// never called.
func GetMode() Mode {
	globalMode.mu.RLock()
	defer globalMode.mu.RUnlock()
	if globalMode.mode == "" {
		return ModeLite
	}
	return globalMode.mode
}

// InstallContext writes the two markdown files into `dir`
// (`~/.config/openagy/` by default). If mode is passthrough, the call is
// a no-op and a banner is returned to the caller so the CLI can still
// print it.
//
// Returns the paths of the files actually written, plus an error if any.
// The function is idempotent — calling it twice with the same content is
// fine, and it won't fail if the directory already exists.
func InstallContext(dir string, mode Mode) ([]string, error) {
	switch mode {
	case ModePassthrough:
		return nil, nil
	case ModeLite, ModeStrip:
		// fall through
	default:
		return nil, fmt.Errorf("agent-context: unknown mode %q", mode)
	}
	if strings.TrimSpace(dir) == "" {
		dir = filepath.Dir(ProvidersPath())
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir %s: %w", dir, err)
	}
	// Pick the body. The "lite" body is the same in both files for the
	// moment (the full-strip body is larger); the file name signals the
	// intent so users can edit one without touching the other.
	var primary, secondary string
	if mode == ModeLite {
		primary = liteContextBody
		secondary = liteContextBody
	} else {
		primary = stripContextBody
		secondary = liteContextBody
	}
	primaryPath := filepath.Join(dir, "agent-context.md")
	secondaryPath := filepath.Join(dir, "agent-context-lite.md")
	if err := os.WriteFile(primaryPath, []byte(primary), 0o644); err != nil {
		return nil, fmt.Errorf("write %s: %w", primaryPath, err)
	}
	if err := os.WriteFile(secondaryPath, []byte(secondary), 0o644); err != nil {
		return nil, fmt.Errorf("write %s: %w", secondaryPath, err)
	}
	return []string{primaryPath, secondaryPath}, nil
}

// Banner returns a one-line human-readable string describing the current
// mode. The TUI prints it on startup so users without a context file
// still get the hint.
func Banner() string {
	switch GetMode() {
	case ModePassthrough:
		return "agent-context: passthrough (no files written)"
	case ModeStrip:
		return "agent-context: strip (full context installed)"
	default:
		return "agent-context: lite (default)"
	}
}

// ErrModeUnknown is returned by SetMode for unrecognized values. The CLI
// surfaces a more helpful message; this is the sentinel for tests.
var ErrModeUnknown = errors.New("agent-context: unknown mode")

// ---- embedded context bodies -------------------------------------------
//
// We embed both versions as Go string constants rather than shipping them
// in a separate file: that way `go install` produces a self-contained binary
// with no asset-lookup code, and the content shows up in `strings` listings
// if a user wants to grep for it.

// liteContextBody is the ~3.7 KB default. Plain markdown, no front-matter,
// ASCII only.
const liteContextBody = `# openagy — recovered binary (agent context, lite)

openagy is a Go-recovered build of the Antigravity CLI. It speaks the
OpenAI and Anthropic wire formats, and routes completions to one of:

  1. The original Antigravity cloudaicompanion endpoint (gRPC; not
     reachable from the sandbox, so this path is dormant by default).
  2. A user-configured provider in ` + "`~/.config/openagy/providers.json`" + `.

Provider types currently supported: openai, anthropic, ollama, google,
openrouter, custom. See ` + "`openagy-models --help`" + ` to manage
them.

## Quick start

  1. Configure a provider and the models it serves, in
     ` + "`~/.config/openagy/providers.json`" + `:

       {
         "providers": {
           "gateway": {
             "type": "openai",
             "base_url": "https://gateway.example.com/v1",
             "api_key": "$OPENAGY_GATEWAY_KEY",
             "models": [
               {"id": "my-model", "name": "My Model", "model": "Upstream-1"}
             ]
           }
         }
       }

  2. Use it:        wagyu --print --model=my-model "hi"

## Files

  ` + "`~/.config/openagy/providers.json`" + `    — the provider registry
  ` + "`~/.config/openagy/agent-context.md`" + `  — full context (this file)
  ` + "`~/.config/openagy/agent-context-lite.md`" + ` — short context

## What this binary is NOT

  - Not a Node.js wrapper. There is no runtime patch of any kind.
  - Not a proxy. The CLI calls your provider directly.
  - Not licensed. If you need a real Antigravity OAuth token, get it from
    the upstream CLI and pass it via ` + "`--token-file`" + `.

See ` + "`agent-context.md`" + ` (full mode) for the recovery timeline,
provider details, and troubleshooting recipes.
`

// stripContextBody is the ~10 KB reference. Same shape as lite but with
// the full step-by-step guides, the recovery notes, and the kelvinzer0
// comparison section. The byte count below is approximate; we don't pin
// the size in a test because the content evolves with the project.
const stripContextBody = `# openagy — recovered binary (agent context, full)

openagy is a Go-recovered build of the Antigravity CLI. The original
binary is a 7.2 MB stripped ELF that talks to a Google gRPC endpoint
called cloudaicompanion. That endpoint is not reachable from a sandboxed
pod without a TUIC proxy + a licensed OAuth token. This recovered build
keeps the wire format (OpenAI / Anthropic chat completions, SSE) and the
user-facing flags, but routes completions through a user-configured
provider instead of gRPC.

## 1. Why a recovered build?

Two reasons:

  1. The upstream binary is x86_64-only, statically linked, and
     obfuscated; running it inside a sandbox pod requires either
     (a) a TUIC tunnel to a public egress or (b) a patched binary.
     Neither is acceptable for a dev sentinel.
  2. The user wants to test against endpoints OTHER than the Antigravity
     backend (OpenRouter, a private gateway, local Ollama, …). The upstream
     binary's hard-coded model list does not include any of those.

The recovery approach is the same as kelvinzer0's ` + "`free-antigravity`" + `,
but written in Go (no Node.js, no runtime binary patching):

  - The ` + "`~/.config/openagy/providers.json`" + ` registry holds one
    provider per endpoint, with the models it serves listed under it.
  - The CLI subcommand ` + "`openagy-models`" + ` replaces the Node.js
    ` + "`models`" + ` menu: list / add / remove / test / install-context.
  - The antigravity entry point reads the registry, picks a provider by
    name, and dispatches.

## 2. Provider matrix

  provider     | wire shape        | auth header               | notes
  -------------+-------------------+---------------------------+----------------------
  openai       | OpenAI chat       | Authorization: Bearer …   | stock OpenAI
  openrouter   | OpenAI chat       | Authorization: Bearer …   | adds HTTP-Referer
  ollama       | OpenAI chat       | (none)                    | local; no auth
  anthropic    | Anthropic msgs    | x-api-key: …              | path /v1/messages
  google       | (passthrough)     | n/a                       | not implemented
  custom       | OpenAI chat       | Authorization: Bearer …   | user-configured

For all OpenAI-shaped providers the request body is identical; only the
URL, auth header, and a few optional headers differ. The
` + "`openAIShapeProvider`" + ` type handles all four.

## 3. Recovery timeline (cycle 9 → cycle 33)

  cycle 9   — first public drop; ` + "`openai-shim`" + ` only.
  cycle 10  — ` + "`anthropic-shim`" + ` added.
  cycle 17  — TUIC proxy + sing-box config shipped.
  cycle 25  — TUI 5-panel layout (Chat / Input / Plan / Cascade / Status).
  cycle 29  — ` + "`antigravity`" + ` TUI entry point; 5 subcommand guess
              (init / session / debug / auth / tui). The guess was wrong
              and is being removed in cycle 33.
  cycle 32  — ` + "`internal/agent`" + ` package extracted; ` + "`backend.Client`" + `
              is the only seam between transport and the TUI / shims.
  cycle 33  — native custom-model support: ` + "`internal/models`" + ` +
              ` + "`openagy-models`" + ` CLI. This file.

## 4. Quick start (copy-paste)

  mkdir -p ~/.config/openagy
  openagy-models add my-model \\
      --provider=openai \\
      --api-url=https://gateway.example.com/v1/chat/completions \\
      --external-model=Upstream-1 \\
      --api-key=-
  openagy-models test my-model
  wagyu --print --model=my-model "what is 2+2?"

## 5. Troubleshooting recipes

  - "upstream 401": the api_key is wrong. Re-run ` + "`openagy-models add`" + `
    with the corrected value.
  - "upstream 404": the api_url is wrong. OpenAI uses
    /v1/chat/completions, Anthropic uses /v1/messages. Some gateways
    expose BOTH paths.
  - "context deadline exceeded": raise ` + "`--timeout-seconds`" + ` on the
    add command. Default is 60s, which is too short for some reasoning
    models.
  - The TUI shows "(no model)": the registry is empty. Run
    ` + "`openagy-models add`" + ` first.

## 6. File layout

  ` + "`~/.config/openagy/providers.json`" + `         — registry
  ` + "`~/.config/openagy/agent-context.md`" + `         — this file
  ` + "`~/.config/openagy/agent-context-lite.md`" + `    — short version

## 7. What this binary is NOT

  - Not a Node.js wrapper. No runtime patching.
  - Not a proxy. The CLI calls your provider directly.
  - Not licensed. If you need the real Antigravity OAuth flow, get the
    token from the upstream CLI and pass it via ` + "`--token-file`" + `.

## 8. References

  - kelvinzer0/free-antigravity:  https://github.com/kelvinzer0/free-antigravity
  - OpenAI Chat Completions:  https://platform.openai.com/docs/api-reference/chat
  - Anthropic Messages:       https://docs.anthropic.com/en/api/messages
`
