// Package refbackend — custommodels.go: the operator's own models, offered
// to the reference.
//
// The reference learns its model catalogue from the CloudCode endpoint
// (`fetchAvailableModels`) and turns the answer into the entries its own
// Web UI shows. So the reference is started with `-cloud_code_endpoint`
// pointing at the clone's own CloudCode proxy — the one `pigasus agy`
// uses — and the operator's configured models appear in the reference's
// model picker, and a turn that picks one is translated to whichever
// provider the operator configured.
//
// Nothing is patched and nothing is written to the operator's state: the
// proxy is a loopback listener that dies with the reference.
//
// 【一手】 2026-10-26, the IDE's own `language_server` binary
// (/Applications/Antigravity.app/Contents/Resources/bin/language_server),
// started with `-cloud_code_endpoint=http://127.0.0.1:<port>`:
//
//	POST /v1internal:loadCodeAssist
//	POST /v1internal:fetchAvailableModels
//	POST /v1internal:fetchUserInfo
//	POST /v1internal:listExperiments
//	POST /v1internal:fetchAdminControls
//
// The reference's GetUserStatus then answers with the catalogue the
// endpoint returned, promoted into the entries its model picker reads —
// the custom models included:
//
//	{"label":"MiniMax M3",
//	 "modelOrAlias":{"model":"MODEL_PLACEHOLDER_M488"},
//	 "modelId":"custom-minimax-m3",
//	 "description":"MiniMax M3 via openai (openagy wrapper)"}
//
// The placeholder is the reference's own slot for a model its enum cannot
// name (MODEL_PLACEHOLDER_M0 … M1650), and the proxy recognises it again
// when the turn's request comes back through the same endpoint.
package refbackend

import (
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/justwasm/pigasus/endpoints"
	"github.com/justwasm/pigasus/internal/agyproxy"
	"github.com/justwasm/pigasus/internal/models"
)

// referenceCloudCodeEndpoint is the CloudCode host the reference talks
// to. It is the same host the clone's own CloudCode channel uses
// (internal/backend/cloudcode.go).
//
// It is the compiled-in default. The live host is
// endpoints.Current().CloudCode — the configuration file may point the
// reference at a local service.
const referenceCloudCodeEndpoint = endpoints.DefaultCloudCode

// customModelProxy is the local CloudCode endpoint the reference is
// pointed at when the operator has models of their own.
type customModelProxy struct {
	srv  *agyproxy.Server
	addr string
}

// endpoint is the value for the reference's `-cloud_code_endpoint`.
func (p *customModelProxy) endpoint() string {
	if p == nil {
		return endpoints.Current().CloudCode
	}
	return "http://" + p.addr
}

// close stops the proxy and flushes its capture.
func (p *customModelProxy) close() {
	if p != nil && p.srv != nil {
		_ = p.srv.Close()
	}
}

// configuredModels returns the operator's configured models — the ones
// in ~/.config/openagy/providers.json.
//
// A configuration that does not parse is not fatal here: the registry
// falls back to the last good one, and a reference without the
// operator's models is still a working reference.
func configuredModels() []models.Model {
	reg := models.CachedRegistry()
	if reg == nil {
		return nil
	}
	return reg.List()
}

// startCustomModelProxy starts the local CloudCode proxy that carries the
// operator's models to the reference.
//
// It returns nil when there is nothing to carry: no configured model, no
// proxy, and the reference talks to the cloud directly — exactly as it
// does without this package.
func startCustomModelProxy() (*customModelProxy, error) {
	custom := configuredModels()
	if len(custom) == 0 {
		return nil, nil
	}
	srv, err := agyproxy.New(agyproxy.Config{
		Addr:     "127.0.0.1:0",
		Upstream: endpoints.Current().CloudCode,
		Models:   custom,
		// The reference polls the catalogue; a capture that nobody
		// asked for is a directory full of noise. The operator asks
		// for one with the proxy's own variable.
		CaptureDir: os.Getenv(agyproxy.EnvCaptureDir),
		Logf: func(format string, args ...any) {
			log.Printf("refbackend: "+format, args...)
		},
	})
	if err != nil {
		return nil, fmt.Errorf("custom model proxy: %w", err)
	}
	addr, err := srv.Start()
	if err != nil {
		return nil, fmt.Errorf("custom model proxy: %w", err)
	}
	names := make([]string, 0, len(custom))
	for _, m := range custom {
		names = append(names, m.Name)
	}
	log.Printf("refbackend: %d custom model(s) offered to the reference: %v", len(custom), names)
	return &customModelProxy{srv: srv, addr: addr}, nil
}

// startReferenceProxy starts the CloudCode proxy the reference's model
// traffic goes through.
//
// The operator's own arguments win: the reference takes the last
// occurrence of a flag, so an explicit `-cloud_code_endpoint` in
// OPENAGY_REFERENCE_BACKEND_ARGS replaces the proxy's address — and a
// proxy nothing talks to is just a listener.
func startReferenceProxy(extra []string) (*customModelProxy, error) {
	if hasCloudCodeEndpoint(extra) {
		return nil, nil
	}
	return startCustomModelProxy()
}

// hasCloudCodeEndpoint reports whether the caller's arguments carry a
// `-cloud_code_endpoint` of their own.
func hasCloudCodeEndpoint(args []string) bool {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if strings.HasPrefix(arg, "-cloud_code_endpoint=") {
			return true
		}
		if arg == "-cloud_code_endpoint" && i+1 < len(args) {
			return true
		}
	}
	return false
}
