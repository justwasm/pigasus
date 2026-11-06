package agentturn

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	acp "github.com/ironpark/acp-go"
)

// ACPAgentManifest models the official agent manifest specification from
// https://github.com/agentclientprotocol/registry (agent.json).
type ACPAgentManifest struct {
	ID           string          `json:"id"`
	Name         string          `json:"name"`
	Version      string          `json:"version"`
	Description  string          `json:"description,omitempty"`
	Repository   string          `json:"repository,omitempty"`
	Authors      []string        `json:"authors,omitempty"`
	License      string          `json:"license,omitempty"`
	Icon         string          `json:"icon,omitempty"`
	Distribution ACPDistribution `json:"distribution"`
}

// ACPDistribution defines distribution channels for an ACP agent.
type ACPDistribution struct {
	Binary map[string]ACPBinaryDistribution `json:"binary,omitempty"`
	NPX    *ACPNPXDistribution              `json:"npx,omitempty"`
	UVX    *ACPUVXDistribution              `json:"uvx,omitempty"`
}

// ACPBinaryDistribution defines platform-specific binary executable parameters.
type ACPBinaryDistribution struct {
	Archive string            `json:"archive,omitempty"`
	Cmd     string            `json:"cmd"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}

// ACPNPXDistribution defines an npm/npx distribution.
type ACPNPXDistribution struct {
	Package string   `json:"package"`
	Args    []string `json:"args,omitempty"`
}

// ACPUVXDistribution defines a python/uvx distribution.
type ACPUVXDistribution struct {
	Package string   `json:"package"`
	Args    []string `json:"args,omitempty"`
}

// BuiltinACPRegistry provides definitions for prominent ACP agents.
var BuiltinACPRegistry = map[string]ACPAgentManifest{
	"agy-acp": {
		ID:          "agy-acp",
		Name:        "Google Antigravity ACP Server",
		Version:     "1.5.0",
		Description: "Official Agent Client Protocol server for Google Antigravity (agy acp)",
		Repository:  "https://github.com/agentclientprotocol/registry/tree/main/antigravity-acp",
		Distribution: ACPDistribution{
			Binary: map[string]ACPBinaryDistribution{
				"all": {
					Cmd:  "agy",
					Args: []string{"acp"},
				},
			},
		},
	},
	"agy-acp-server": {
		ID:          "agy-acp",
		Name:        "Google Antigravity ACP Server",
		Version:     "1.5.0",
		Description: "Official Agent Client Protocol server for Google Antigravity (agy acp)",
		Repository:  "https://github.com/agentclientprotocol/registry/tree/main/antigravity-acp",
		Distribution: ACPDistribution{
			Binary: map[string]ACPBinaryDistribution{
				"all": {
					Cmd:  "agy",
					Args: []string{"acp"},
				},
			},
		},
	},
	"pigasus-acp": {
		ID:          "pigasus-acp",
		Name:        "Pigasus ACP Server",
		Version:     "1.5.0",
		Description: "Native Agent Client Protocol server for Pigasus (pigasus acp)",
		Distribution: ACPDistribution{
			Binary: map[string]ACPBinaryDistribution{
				"all": {
					Cmd:  "pigasus",
					Args: []string{"acp"},
				},
			},
		},
	},
	"codex-acp": {
		ID:          "codex-acp",
		Name:        "OpenAI Codex ACP Adapter",
		Version:     "0.1.0",
		Description: "Agent Client Protocol adapter for OpenAI Codex",
		Repository:  "https://github.com/agentclientprotocol/registry",
		Distribution: ACPDistribution{
			Binary: map[string]ACPBinaryDistribution{
				"all": {
					Cmd: "codex-acp",
				},
			},
			NPX: &ACPNPXDistribution{
				Package: "@agentclientprotocol/codex-acp",
			},
		},
	},
	"claude-acp": {
		ID:          "claude-acp",
		Name:        "Anthropic Claude Agent ACP Adapter",
		Version:     "0.1.0",
		Description: "Agent Client Protocol adapter for Claude Code / Claude Agent",
		Repository:  "https://github.com/agentclientprotocol/registry",
		Distribution: ACPDistribution{
			Binary: map[string]ACPBinaryDistribution{
				"all": {
					Cmd: "claude-acp",
				},
			},
			NPX: &ACPNPXDistribution{
				Package: "@agentclientprotocol/claude-agent-acp",
			},
		},
	},
}

// LoadACPAgentManifest loads an agent.json manifest from a file or folder path.
func LoadACPAgentManifest(path string) (*ACPAgentManifest, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("acp manifest: stat %s: %w", path, err)
	}
	manifestPath := path
	if fi.IsDir() {
		manifestPath = filepath.Join(path, "agent.json")
	}
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("acp manifest: read %s: %w", manifestPath, err)
	}
	var manifest ACPAgentManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("acp manifest: unmarshal %s: %w", manifestPath, err)
	}
	return &manifest, nil
}

// canonicalPlatformKey returns the platform keys used by ACP distribution manifests.
func canonicalPlatformKeys() []string {
	osName := runtime.GOOS
	arch := runtime.GOARCH

	var archKeys []string
	switch arch {
	case "arm64":
		archKeys = []string{"aarch64", "arm64"}
	case "amd64":
		archKeys = []string{"x86_64", "amd64"}
	default:
		archKeys = []string{arch}
	}

	keys := make([]string, 0, len(archKeys)+1)
	for _, a := range archKeys {
		keys = append(keys, osName+"-"+a)
	}
	keys = append(keys, "all")
	return keys
}

// ResolveACPAgent resolves an agent identifier or manifest path to an executable command,
// arguments, and environment variables.
func ResolveACPAgent(agentOrPath string) (cmd string, args []string, env map[string]string, err error) {
	trimmed := strings.TrimSpace(agentOrPath)
	if trimmed == "" {
		return "", nil, nil, errors.New("empty agent identifier")
	}

	// 1. Check if path exists on disk as file or dir containing agent.json
	if strings.HasSuffix(trimmed, ".json") || strings.Contains(trimmed, string(filepath.Separator)) {
		if fi, errStat := os.Stat(trimmed); errStat == nil {
			if fi.IsDir() || strings.HasSuffix(trimmed, ".json") {
				manifest, errLoad := LoadACPAgentManifest(trimmed)
				if errLoad == nil {
					return resolveManifest(manifest)
				}
			}
		}
	}

	// 2. Normalize known aliases
	norm := strings.ToLower(trimmed)
	var manifest ACPAgentManifest
	var ok bool
	switch norm {
	case "pigasus", "pigasus-acp", "pig-acp":
		manifest, ok = BuiltinACPRegistry["pigasus-acp"]
	case "agy", "agy-acp", "agy-acp-server", "antigravity", "antigravity-acp":
		manifest, ok = BuiltinACPRegistry["agy-acp"]
	case "codex", "codex-acp":
		manifest, ok = BuiltinACPRegistry["codex-acp"]
	case "claude", "claude-acp", "claude-code-acp", "claude-agent-acp":
		manifest, ok = BuiltinACPRegistry["claude-acp"]
	default:
		manifest, ok = BuiltinACPRegistry[norm]
	}

	if ok {
		return resolveManifest(&manifest)
	}

	// 3. Fallback: treat as direct command or executable name
	if p, errLook := exec.LookPath(trimmed); errLook == nil {
		return p, nil, nil, nil
	}

	return "", nil, nil, fmt.Errorf("unknown ACP agent %q: no matching registry entry, manifest, or command on PATH", trimmed)
}

func resolveManifest(manifest *ACPAgentManifest) (cmd string, args []string, env map[string]string, err error) {
	if manifest == nil {
		return "", nil, nil, errors.New("nil manifest")
	}

	// Special case for pigasus-acp: run pigasus acp via current executable or "pigasus"
	if manifest.ID == "pigasus-acp" {
		if exe, err := os.Executable(); err == nil {
			return exe, []string{"acp"}, nil, nil
		}
		return "pigasus", []string{"acp"}, nil, nil
	}

	// Special case for agy-acp / agy-acp-server: check agy (agy acp) or legacy agy-acp-server
	if manifest.ID == "agy-acp" || manifest.ID == "agy-acp-server" {
		if p, err := exec.LookPath("agy"); err == nil {
			return p, []string{"acp"}, nil, nil
		}
		if p, err := exec.LookPath("agy-acp-server"); err == nil {
			return p, []string{"--stdio"}, nil, nil
		}
		return "agy", []string{"acp"}, nil, nil
	}

	// 1. Check binary distribution for current platform
	for _, key := range canonicalPlatformKeys() {
		if binDist, hasBin := manifest.Distribution.Binary[key]; hasBin && binDist.Cmd != "" {
			if p, errLook := exec.LookPath(binDist.Cmd); errLook == nil {
				return p, binDist.Args, binDist.Env, nil
			}
			// Check standard install locations
			for _, prefix := range []string{"/opt/homebrew/bin", "/usr/local/bin"} {
				candidate := filepath.Join(prefix, binDist.Cmd)
				if _, errStat := os.Stat(candidate); errStat == nil {
					return candidate, binDist.Args, binDist.Env, nil
				}
			}
		}
	}

	// 2. Check npx distribution
	if manifest.Distribution.NPX != nil && manifest.Distribution.NPX.Package != "" {
		if npxPath, errNPX := exec.LookPath("npx"); errNPX == nil {
			npxArgs := append([]string{"-y", manifest.Distribution.NPX.Package}, manifest.Distribution.NPX.Args...)
			return npxPath, npxArgs, nil, nil
		}
	}

	// 3. Check uvx distribution
	if manifest.Distribution.UVX != nil && manifest.Distribution.UVX.Package != "" {
		if uvxPath, errUVX := exec.LookPath("uvx"); errUVX == nil {
			uvxArgs := append([]string{manifest.Distribution.UVX.Package}, manifest.Distribution.UVX.Args...)
			return uvxPath, uvxArgs, nil, nil
		}
	}

	// If binary command name was defined, cite it in error
	for _, key := range canonicalPlatformKeys() {
		if binDist, hasBin := manifest.Distribution.Binary[key]; hasBin && binDist.Cmd != "" {
			var hint strings.Builder
			hint.WriteString(fmt.Sprintf("ACP agent %q requires %q binary", manifest.Name, binDist.Cmd))
			if manifest.Distribution.NPX != nil {
				hint.WriteString(fmt.Sprintf(" or npx with package %q", manifest.Distribution.NPX.Package))
			}
			if manifest.Distribution.UVX != nil {
				hint.WriteString(fmt.Sprintf(" or uvx with package %q", manifest.Distribution.UVX.Package))
			}
			return "", nil, nil, errors.New(hint.String())
		}
	}

	return "", nil, nil, fmt.Errorf("no executable distribution found for ACP agent %q on %s/%s", manifest.Name, runtime.GOOS, runtime.GOARCH)
}

type acpProcCloser struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stderr *bytes.Buffer
}

func (p *acpProcCloser) Close() error {
	if p.stdin != nil {
		_ = p.stdin.Close()
	}
	if p.cmd != nil && p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
		_ = p.cmd.Wait()
	}
	return nil
}

// SpawnACPRemote launches an external ACP agent subprocess over stdio and negotiates the session.
func SpawnACPRemote(ctx context.Context, command string, args []string, cwd string, acpVersion ...string) (*ACPRemote, error) {
	if command == "" {
		return nil, errors.New("acp: spawn command is empty")
	}

	cmd := exec.Command(command, args...)
	if cwd != "" {
		cmd.Dir = cwd
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("acp: stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("acp: stdout pipe: %w", err)
	}

	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, fmt.Errorf("acp: start process %s: %w", command, err)
	}

	closer := &acpProcCloser{
		cmd:    cmd,
		stdin:  stdin,
		stderr: &stderrBuf,
	}

	ver := "auto"
	if len(acpVersion) > 0 && strings.TrimSpace(acpVersion[0]) != "" {
		ver = strings.ToLower(strings.TrimSpace(acpVersion[0]))
	}

	transport := acp.NewStdioTransport(stdout, stdin)
	dial := func(dialCtx context.Context) (acp.Transport, error) {
		return transport, nil
	}

	remote, err := connectACPRemote(ctx, dial, closer, cwd, ver)
	if err != nil {
		_ = closer.Close()
		stderrStr := strings.TrimSpace(stderrBuf.String())
		if stderrStr != "" {
			return nil, fmt.Errorf("%w (process stderr: %s)", err, stderrStr)
		}
		return nil, err
	}

	return remote, nil
}

// SpawnACPAgent launches an ACP agent specified by ID, alias, or manifest path.
func SpawnACPAgent(ctx context.Context, agentOrPath string, extraArgs []string, cwd string, acpVersion ...string) (*ACPRemote, error) {
	cmd, args, env, err := ResolveACPAgent(agentOrPath)
	if err != nil {
		return nil, err
	}
	if cmd == "pigasus" {
		if _, err := exec.LookPath("pigasus"); err != nil {
			if exe, errExe := os.Executable(); errExe == nil {
				cmd = exe
			}
		}
	}
	allArgs := append(args, extraArgs...)

	// If extra env is provided by manifest, merge it into subprocess
	if len(env) > 0 {
		return spawnACPRemoteWithEnv(ctx, cmd, allArgs, env, cwd, acpVersion...)
	}
	return SpawnACPRemote(ctx, cmd, allArgs, cwd, acpVersion...)
}

func spawnACPRemoteWithEnv(ctx context.Context, command string, args []string, env map[string]string, cwd string, acpVersion ...string) (*ACPRemote, error) {
	if command == "" {
		return nil, errors.New("acp: spawn command is empty")
	}

	cmd := exec.Command(command, args...)
	if cwd != "" {
		cmd.Dir = cwd
	}
	cmd.Env = os.Environ()
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("acp: stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("acp: stdout pipe: %w", err)
	}

	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, fmt.Errorf("acp: start process %s: %w", command, err)
	}

	closer := &acpProcCloser{
		cmd:    cmd,
		stdin:  stdin,
		stderr: &stderrBuf,
	}

	ver := "auto"
	if len(acpVersion) > 0 && strings.TrimSpace(acpVersion[0]) != "" {
		ver = strings.ToLower(strings.TrimSpace(acpVersion[0]))
	}

	transport := acp.NewStdioTransport(stdout, stdin)
	dial := func(dialCtx context.Context) (acp.Transport, error) {
		return transport, nil
	}

	remote, err := connectACPRemote(ctx, dial, closer, cwd, ver)
	if err != nil {
		_ = closer.Close()
		stderrStr := strings.TrimSpace(stderrBuf.String())
		if stderrStr != "" {
			return nil, fmt.Errorf("%w (process stderr: %s)", err, stderrStr)
		}
		return nil, err
	}

	return remote, nil
}
