package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// TmuxTarget identifies which CLI binary to run.
type TmuxTarget string

const (
	TargetAgy     TmuxTarget = "agy"
	TargetPigasus TmuxTarget = "pigasus"
	TargetBoth    TmuxTarget = "both"
)

// TmuxConfig configures a tmux testing session.
type TmuxConfig struct {
	SocketName   string
	SessionName  string
	Width        int
	Height       int
	CloudCodeURL string
	AgyBin       string
	PigasusBin   string
	Workspace    string
	Synchronize  bool
	SettingsJSON string
}

// TmuxDriver coordinates tmux panes and key actions.
type TmuxDriver struct {
	cfg     TmuxConfig
	tmuxBin string
	tempDir string
}

// NewTmuxDriver creates a driver instance.
func NewTmuxDriver(cfg TmuxConfig) (*TmuxDriver, error) {
	tmuxPath, err := exec.LookPath("tmux")
	if err != nil {
		return nil, fmt.Errorf("tmux is not installed or not in PATH: %w", err)
	}

	if cfg.SocketName == "" {
		cfg.SocketName = "tuialign"
	}
	if cfg.SessionName == "" {
		cfg.SessionName = fmt.Sprintf("align_%d", time.Now().Unix()%10000)
	}
	if cfg.Width <= 0 {
		cfg.Width = 100
	}
	if cfg.Height <= 0 {
		cfg.Height = 30
	}
	if cfg.AgyBin == "" {
		cfg.AgyBin = findAgyBin()
	}
	if cfg.PigasusBin == "" {
		cfg.PigasusBin = "/tmp/pigasus"
	}

	return &TmuxDriver{
		cfg:     cfg,
		tmuxBin: tmuxPath,
	}, nil
}

func findAgyBin() string {
	if p := os.Getenv("AGY_REAL_BIN"); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	candidates := []string{
		filepath.Join(home, ".local", "bin", "agy"),
		"/usr/local/bin/agy",
		"/opt/homebrew/bin/agy",
	}
	for _, c := range candidates {
		if fi, err := os.Stat(c); err == nil && !fi.IsDir() {
			return c
		}
	}
	if p, err := exec.LookPath("agy"); err == nil {
		return p
	}
	return "agy"
}

// ExecTmux runs a raw tmux command on the isolated socket.
func (d *TmuxDriver) ExecTmux(args ...string) (string, error) {
	fullArgs := append([]string{"-L", d.cfg.SocketName, "-f", "/dev/null"}, args...)
	cmd := exec.Command(d.tmuxBin, fullArgs...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("tmux %v failed: %w (%s)", args, err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

// SetupWorkspace creates a clean temp git workspace so agy doesn't spend time scanning huge directories.
func (d *TmuxDriver) SetupWorkspace() (string, error) {
	if d.cfg.Workspace != "" {
		return d.cfg.Workspace, nil
	}
	dir, err := os.MkdirTemp("", "tuialign_ws_*")
	if err != nil {
		return "", err
	}
	d.tempDir = dir
	// Init empty git repo
	gitCmd := exec.Command("git", "init", "-q", dir)
	_ = gitCmd.Run()
	return dir, nil
}

// Launch boots the session with the requested target (agy, pigasus, or both in split panes).
func (d *TmuxDriver) Launch(target TmuxTarget) error {
	ws, err := d.SetupWorkspace()
	if err != nil {
		return fmt.Errorf("setup workspace: %w", err)
	}

	// Kill any stale session with the same name
	_, _ = d.ExecTmux("kill-session", "-t", d.cfg.SessionName)

	// Ensure tmux truecolor support
	_, _ = d.ExecTmux("set-option", "-s", "default-terminal", "tmux-256color")
	_, _ = d.ExecTmux("set-option", "-sa", "terminal-overrides", ",*256col*:Tc,xterm*:RGB")

	buildEnvCmd := func(bin string, extraEnv string) string {
		settingsArg := ""
		if d.cfg.SettingsJSON != "" {
			settingsArg = fmt.Sprintf(" --settings %q", d.cfg.SettingsJSON)
		}
		envPrefix := fmt.Sprintf("env COLORTERM=truecolor TERM=xterm-256color CLOUD_CODE_URL=%s JETSKI_OAUTH_TOKEN=tuialign-token OPENAGY_TRUST=1", d.cfg.CloudCodeURL)
		if extraEnv != "" {
			envPrefix += " " + extraEnv
		}
		return fmt.Sprintf("%s %s --model gemini-3.8-flash-high%s", envPrefix, bin, settingsArg)
	}

	pigasusHome := filepath.Join(ws, ".antigravity-pigasus")
	_ = os.MkdirAll(pigasusHome, 0700)
	realSettings := filepath.Join(os.Getenv("HOME"), ".gemini", "antigravity-cli", "settings.json")
	if data, err := os.ReadFile(realSettings); err == nil {
		_ = os.WriteFile(filepath.Join(pigasusHome, "settings.json"), data, 0600)
	}

	switch target {
	case TargetAgy:
		cmd := buildEnvCmd(d.cfg.AgyBin, "")
		_, err = d.ExecTmux("new-session", "-d", "-s", d.cfg.SessionName, "-x", fmt.Sprint(d.cfg.Width), "-y", fmt.Sprint(d.cfg.Height), "-c", ws, cmd)
		return err

	case TargetPigasus:
		cmd := buildEnvCmd(d.cfg.PigasusBin, "ANTIGRAVITY_HOME="+pigasusHome)
		_, err = d.ExecTmux("new-session", "-d", "-s", d.cfg.SessionName, "-x", fmt.Sprint(d.cfg.Width), "-y", fmt.Sprint(d.cfg.Height), "-c", ws, cmd)
		return err

	case TargetBoth:
		// Left: agy (pane 0)
		//
		// The window is 2*Width+1 cells, not 2*Width+2: the vertical
		// divider between the panes is itself one cell, so a 202-cell
		// window splits into 101+100 and the reference pane is a column
		// wider than the target's. Every full-width line — both rules
		// around the prompt, the right-aligned status line — then differs
		// by exactly one column, which read as a rendering bug in every
		// scenario while it was the harness's own geometry.
		cmdAgy := buildEnvCmd(d.cfg.AgyBin, "")
		_, err = d.ExecTmux("new-session", "-d", "-s", d.cfg.SessionName, "-x", fmt.Sprint(d.cfg.Width*2+1), "-y", fmt.Sprint(d.cfg.Height), "-c", ws, cmdAgy)
		if err != nil {
			return err
		}

		// Right: pigasus (pane 1)
		cmdPigasus := buildEnvCmd(d.cfg.PigasusBin, "ANTIGRAVITY_HOME="+pigasusHome)
		_, err = d.ExecTmux("split-window", "-h", "-t", d.cfg.SessionName+".0", "-c", ws, cmdPigasus)
		if err != nil {
			return err
		}

		// Ensure 50% split width
		_, _ = d.ExecTmux("select-layout", "-t", d.cfg.SessionName, "even-horizontal")

		if err := d.assertPaneWidths(); err != nil {
			return err
		}

		if d.cfg.Synchronize {
			_, _ = d.ExecTmux("set-window-option", "-t", d.cfg.SessionName, "synchronize-panes", "on")
		}
		return nil

	default:
		return fmt.Errorf("unknown target: %s", target)
	}
}

// assertPaneWidths fails when the two panes are not the same width.
//
// A parity run compares cell by cell, so a pane that is one column wider
// makes every full-width row differ — and the report then blames the
// renderer. The check is cheap and the failure is unambiguous, which is
// what the previous 101-vs-100 split was missing.
func (d *TmuxDriver) assertPaneWidths() error {
	out, err := d.ExecTmux("list-panes", "-t", d.cfg.SessionName, "-F", "#{pane_width}")
	if err != nil {
		return err
	}
	var widths []int
	for _, f := range strings.Fields(out) {
		if n, err := strconv.Atoi(f); err == nil {
			widths = append(widths, n)
		}
	}
	if len(widths) != 2 {
		return fmt.Errorf("tuialign: expected 2 panes, got %d (%v)", len(widths), widths)
	}
	if widths[0] != widths[1] {
		return fmt.Errorf("tuialign: panes are %d and %d cells wide; a parity run needs identical geometry", widths[0], widths[1])
	}
	if widths[0] != d.cfg.Width {
		return fmt.Errorf("tuialign: panes are %d cells wide, scenario asks for %d", widths[0], d.cfg.Width)
	}
	return nil
}

// SendKeys sends key sequences to a specific pane (e.g. "0" or "1").
func (d *TmuxDriver) SendKeys(pane string, keys ...string) error {
	target := d.cfg.SessionName
	if pane != "" {
		target = fmt.Sprintf("%s.%s", d.cfg.SessionName, pane)
	}
	args := append([]string{"send-keys", "-t", target}, keys...)
	_, err := d.ExecTmux(args...)
	return err
}

// SendBoth sends keys to both pane 0 and pane 1.
func (d *TmuxDriver) SendBoth(keys ...string) error {
	if err := d.SendKeys("0", keys...); err != nil {
		return err
	}
	return d.SendKeys("1", keys...)
}

// CapturePane captures the screen contents of a pane.
func (d *TmuxDriver) CapturePane(pane string, ansi bool) (string, error) {
	target := d.cfg.SessionName
	if pane != "" {
		target = fmt.Sprintf("%s.%s", d.cfg.SessionName, pane)
	}
	args := []string{"capture-pane", "-p", "-t", target}
	if ansi {
		args = append(args, "-e")
	}
	return d.ExecTmux(args...)
}

// CaptureBoth captures both panes (pane 0 and pane 1).
func (d *TmuxDriver) CaptureBoth(ansi bool) (string, string, error) {
	left, err := d.CapturePane("0", ansi)
	if err != nil {
		return "", "", fmt.Errorf("capture left (agy): %w", err)
	}
	right, err := d.CapturePane("1", ansi)
	if err != nil {
		return "", "", fmt.Errorf("capture right (pigasus): %w", err)
	}
	return left, right, nil
}

// Resize resizes the tmux window and recalculates pane layout.
// For TargetBoth, it resizes the window to width*2 + 1 to account for the divider,
// and ensures both panes receive SIGWINCH and have identical column widths.
func (d *TmuxDriver) Resize(target TmuxTarget, width, height int) error {
	d.cfg.Width = width
	d.cfg.Height = height

	totalWidth := width
	if target == TargetBoth {
		totalWidth = width*2 + 1
	}

	_, err := d.ExecTmux("resize-window", "-t", d.cfg.SessionName, "-x", fmt.Sprint(totalWidth), "-y", fmt.Sprint(height))
	if err != nil {
		return fmt.Errorf("resize-window failed: %w", err)
	}

	if target == TargetBoth {
		_, _ = d.ExecTmux("select-layout", "-t", d.cfg.SessionName, "even-horizontal")
		if err := d.assertPaneWidths(); err != nil {
			return err
		}
	}
	// Give the applications time to process SIGWINCH and reflow their TUI views
	time.Sleep(150 * time.Millisecond)
	return nil
}

// WaitForText polls a pane until target substring appears or timeout is reached.
func (d *TmuxDriver) WaitForText(pane string, substring string, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	acceptedTrust := false
	for time.Now().Before(deadline) {
		out, err := d.CapturePane(pane, false)
		if err == nil {
			if !acceptedTrust && (strings.Contains(out, "Do you trust") || strings.Contains(out, "Trust this folder")) {
				_ = d.SendKeys(pane, "C-m")
				acceptedTrust = true
			}
			if strings.Contains(out, substring) {
				return out, nil
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return "", fmt.Errorf("timed out waiting %v for %q in pane %s", timeout, substring, pane)
}

// WaitForBoth polls both panes until target substring appears in both.
func (d *TmuxDriver) WaitForBoth(substring string, timeout time.Duration) (string, string, error) {
	deadline := time.Now().Add(timeout)
	var lastLeft, lastRight string
	leftAcceptedTrust := false
	rightAcceptedTrust := false
	for time.Now().Before(deadline) {
		left, errL := d.CapturePane("0", false)
		right, errR := d.CapturePane("1", false)
		if errL == nil {
			lastLeft = left
			if !leftAcceptedTrust && (strings.Contains(left, "Do you trust") || strings.Contains(left, "Trust this folder")) {
				_ = d.SendKeys("0", "C-m")
				leftAcceptedTrust = true
			}
		}
		if errR == nil {
			lastRight = right
			if !rightAcceptedTrust && (strings.Contains(right, "Do you trust") || strings.Contains(right, "Trust this folder")) {
				_ = d.SendKeys("1", "C-m")
				rightAcceptedTrust = true
			}
		}
		if errL == nil && errR == nil {
			if strings.Contains(left, substring) && strings.Contains(right, substring) {
				return left, right, nil
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return lastLeft, lastRight, fmt.Errorf("timed out waiting %v for %q in both panes.\n--- Pane 0 (agy) ---\n%s\n--- Pane 1 (pigasus) ---\n%s\n--------------------", timeout, substring, lastLeft, lastRight)
}

// Close terminates the tmux session and cleans up temp workspace.
func (d *TmuxDriver) Close() {
	_, _ = d.ExecTmux("kill-session", "-t", d.cfg.SessionName)
	if d.tempDir != "" {
		_ = os.RemoveAll(d.tempDir)
	}
}
