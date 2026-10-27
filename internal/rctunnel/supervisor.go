package rctunnel

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/justwasm/pigasus/endpoints"
	"github.com/justwasm/pigasus/internal/accounts"
	"github.com/justwasm/pigasus/internal/auth"
)

const (
	DefaultIDEVersion        = "2.24.0"
	DefaultAPIServerURL      = endpoints.DefaultGenerativeLanguage
	DefaultCloudCodeEndpoint = endpoints.DefaultCloudCode
)

// Supervisor runs and coordinates the language server process and its local proxy.
type Supervisor struct {
	cfg     Config
	cmd     *exec.Cmd
	inst    *Instance
	cleanup func()
	server  *http.Server
}

// StartSupervisor launches the language server with the isolated runtime config,
// discovers its loopback port, and starts the local reverse proxy.
func StartSupervisor(ctx context.Context, cfg Config, errOut io.Writer) (*Supervisor, error) {
	if cfg.BinaryPath == "" {
		cfg.BinaryPath = FindLanguageServer("")
	}
	if cfg.BinaryPath == "" {
		return nil, ErrLanguageServerNotFound
	}

	if cfg.GeminiDir == "" {
		home, _ := os.UserHomeDir()
		cfg.GeminiDir = filepath.Join(home, ".gemini")
	}

	if cfg.CSRFToken == "" {
		cfg.CSRFToken = NewCSRFToken()
	}

	// 1. Ensure active account token is synced to standard paths and Keychain
	if !cfg.NoLogin {
		if tok, _ := accounts.Current(); tok != nil {
			_ = auth.SaveTokenUnified(tok)
		}
	}

	// 2. Prepare isolated runtime configuration
	relConfigDir, cleanupConfig, err := SetupRuntimeConfig(cfg.GeminiDir, cfg.Name)
	if err != nil {
		return nil, fmt.Errorf("setup runtime config: %w", err)
	}

	// 3. Launch language server
	args := []string{
		"--standalone",
		"--override_ide_name", "antigravity",
		"--subclient_type", "hub",
		"--override_ide_version", DefaultIDEVersion,
		"--override_user_agent_name", "antigravity",
		"--https_server_port", "0",
		"--app_data_dir", "antigravity-cli",
		"--api_server_url", endpoints.Current().GenerativeLanguage,
		"--cloud_code_endpoint", endpoints.Current().CloudCode,
		"--enable_sidecars",
		"--disable_telemetry",
		"--csrf_token", cfg.CSRFToken,
		"--gemini_dir", cfg.GeminiDir,
		"--config_dir", relConfigDir,
	}

	cmd := exec.CommandContext(ctx, cfg.BinaryPath, args...)
	cmd.Dir = filepath.Dir(cfg.BinaryPath)
	if cfg.LogWriter != nil {
		cmd.Stdout = cfg.LogWriter
		cmd.Stderr = cfg.LogWriter
	}

	if err := cmd.Start(); err != nil {
		cleanupConfig()
		return nil, fmt.Errorf("start language server: %w", err)
	}

	// 4. Discover loopback HTTPS port
	discoverCtx, discoverCancel := context.WithTimeout(ctx, 20*time.Second)
	defer discoverCancel()

	inst, err := FindListeningInstance(discoverCtx, cmd.Process.Pid, cfg.CSRFToken)
	if err != nil {
		_ = cmd.Process.Kill()
		cleanupConfig()
		return nil, fmt.Errorf("discover language server port: %w", err)
	}

	// 5. Build reverse proxy
	proxyHandler, err := NewProxy(inst.Port, inst.CSRFToken)
	if err != nil {
		_ = cmd.Process.Kill()
		cleanupConfig()
		return nil, fmt.Errorf("build reverse proxy: %w", err)
	}

	addr := cfg.Addr
	if addr == "" {
		addr = "0.0.0.0:8787"
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		_ = cmd.Process.Kill()
		cleanupConfig()
		return nil, fmt.Errorf("bind proxy addr %s: %w", addr, err)
	}

	server := &http.Server{
		Handler: proxyHandler,
	}
	go func() {
		_ = server.Serve(ln)
	}()

	s := &Supervisor{
		cfg:  cfg,
		cmd:  cmd,
		inst: inst,
		cleanup: func() {
			_ = server.Close()
			if cmd.Process != nil {
				_ = cmd.Process.Signal(os.Interrupt)
				done := make(chan struct{})
				go func() {
					_ = cmd.Wait()
					close(done)
				}()
				select {
				case <-done:
				case <-time.After(3 * time.Second):
					_ = cmd.Process.Kill()
				}
			}
			cleanupConfig()
		},
		server: server,
	}

	if errOut != nil {
		accountStr := cfg.Account
		if accountStr == "" {
			_, accountStr = accounts.Current()
		}
		fmt.Fprintf(errOut, "remote-control serve: language_server online (pid=%d, port=%d, cloud_tunnel=connected)\n", inst.PID, inst.Port)
		fmt.Fprintf(errOut, "remote-control serve: listening on http://%s (name=%s, account=%s)\n", ln.Addr().String(), cfg.Name, accountStr)
	}

	return s, nil
}

// Stop shuts down the supervisor, proxy, and language server.
func (s *Supervisor) Stop() {
	if s.cleanup != nil {
		s.cleanup()
		s.cleanup = nil
	}
}

// Instance returns the running Instance.
func (s *Supervisor) Instance() *Instance {
	return s.inst
}
