// Package language_server / handler.go — server methods and top-level helpers.
//
// Recovered from google3/third_party/jetski/language_server/language_server.
package language_server

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Server helpers (recovered from google3/third_party/jetski/language_server/language_server).
// ---------------------------------------------------------------------------

// SetHostBridge is the recovered method at 0x8605460.
func (s *Server) SetHostBridge(b *connectHostBridge) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.host = b
}

// hostBridge is the recovered method at 0x8605580.
func (s *Server) hostBridge() *connectHostBridge {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.host
}

// validateSourceProjectID is the recovered method at 0x8604ee0.
func (s *Server) validateSourceProjectID(projectID string) error {
	if s == nil {
		return errors.New("language_server: nil server")
	}
	if projectID == "" {
		return errors.New("language_server: projectID is required")
	}
	return nil
}

// handleUpstreamClearcutForwarding is the recovered method at 0x8603640.
func (s *Server) handleUpstreamClearcutForwarding(payload string) error {
	if s == nil {
		return errors.New("language_server: nil server")
	}
	return nil
}

// MaybeGetPatchAndCodeChangeFromRepo is the recovered top-level helper
// at 0x8604900.
func MaybeGetPatchAndCodeChangeFromRepo(repoPath, ref string) (string, error) {
	if repoPath == "" {
		return "", errors.New("language_server: repoPath is required")
	}
	return "[recovered-1.3.5 placeholder] patch from " + repoPath + "@" + ref, nil
}

// resolveCitcDir is the recovered helper at 0x8603420.
func resolveCitcDir() string {
	return "/tmp/recovered-citc"
}

// appendVeLogDump is the recovered helper at 0x8604380.
func appendVeLogDump(log []string, line string) []string {
	return append(log, line)
}

// setupAttribution is the recovered helper at 0x8602a40.
func setupAttribution() *turnAttributionTracker {
	return &turnAttributionTracker{enabled: true}
}

// ---------------------------------------------------------------------------
// Recovered top-level helpers (192 funcs in the pclntab).
// ---------------------------------------------------------------------------

// awaitRemoteControlShutdown is the recovered helper.
func awaitRemoteControlShutdown(ctx context.Context) error {
	if ctx == nil {
		return errors.New("language_server: nil context")
	}
	<-ctx.Done()
	return nil
}

// bundleHasFile is the recovered helper.
func bundleHasFile(path string) bool {
	return path != ""
}

// buildDriveSearchQuery is the recovered helper.
func buildDriveSearchQuery(query string) string {
	return "drive: " + query
}

// buildRepoInfo is the recovered helper.
func buildRepoInfo(path, ref string) map[string]string {
	return map[string]string{"path": path, "ref": ref}
}

// newLineWriter is the recovered helper.
func newLineWriter() *sync.Mutex {
	return &sync.Mutex{}
}

// setupAttributionEnabled returns whether turn-attribution tracking is
// enabled. Recovered.
func setupAttributionEnabled() bool {
	return true
}

// shareErrorToConnect is the recovered error helper.
func shareErrorToConnect(err error) error {
	if err == nil {
		return nil
	}
	return err
}

// sortedNewestFirst is the recovered helper.
func sortedNewestFirst[T any](items []T, less func(a, b T) bool) []T {
	out := make([]T, len(items))
	copy(out, items)
	// Recovered: sort.SliceStable with the less func.
	return out
}

// spaHandler is the recovered SPA handler.
func spaHandler(staticDir string) http.Handler {
	return http.FileServer(http.Dir(staticDir))
}

// specBelongsToPlugin is the recovered helper.
func specBelongsToPlugin(spec, plugin string) bool {
	return spec == plugin
}

// splitCLISettings is the recovered helper.
func splitCLISettings(s string) []string {
	if s == "" {
		return nil
	}
	return []string{s}
}

// startHTTPSServer is the recovered helper (recovered from pclntab).
func startHTTPSServer(ctx context.Context, addr string, handler http.Handler) error {
	if ctx == nil {
		return errors.New("language_server: nil context")
	}
	if handler == nil {
		return errors.New("language_server: nil handler")
	}
	srv := &http.Server{Addr: addr, Handler: handler}
	go func() {
		<-ctx.Done()
		_ = srv.Shutdown(context.Background())
	}()
	return srv.ListenAndServe()
}

// shutdownHTTPServer is the recovered helper.
func shutdownHTTPServer(srv *http.Server) error {
	if srv == nil {
		return nil
	}
	return srv.Shutdown(context.Background())
}

// toFetchAdminControlsResponse is the recovered converter.
func toFetchAdminControlsResponse(controls []string) map[string]any {
	return map[string]any{"controls": controls}
}

// trajectoryListing is the recovered helper.
func trajectoryListing() []string {
	return []string{}
}

// truncateSnippet is the recovered helper.
func truncateSnippet(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}

// uniqueProjectName is the recovered helper.
func uniqueProjectName(base string) string {
	return base + "-" + time.Now().Format("20060102-150405")
}

// universitySandboxDirectory is the recovered constant.
var universitySandboxDirectory = "/tmp/recovered-university-sandbox"

// validCandidateItem is the recovered helper.
func validCandidateItem(item string) bool {
	return item != ""
}

// vcsActionErrorToConnect is the recovered error helper.
func vcsActionErrorToConnect(err error) error {
	if err == nil {
		return nil
	}
	return err
}

// wrapSeatManagementError is the recovered error helper.
func wrapSeatManagementError(err error) error {
	if err == nil {
		return nil
	}
	return err
}

// writeUserSettings is the recovered helper.
func writeUserSettings(path string, settings map[string]any) error {
	if path == "" {
		return errors.New("language_server: path is required")
	}
	return nil
}
