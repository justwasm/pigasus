package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/justwasm/pigasus/internal/auth"
	"github.com/justwasm/pigasus/internal/onboarding"
	"github.com/justwasm/pigasus/internal/tools"
)

// dispatchTool is the --tool NAME --tool-args JSON path.
func dispatchTool(w io.Writer, name, argsJSON string) error {
	var args map[string]any
	if argsJSON == "" {
		argsJSON = "{}"
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return fmt.Errorf("tool: invalid --tool-args JSON: %w", err)
	}
	if name != "run_command" {
		fmt.Fprintf(w, "[tool-stub] %s args=%v\n", name, args)
		return nil
	}
	cmdStr, _ := args["command"].(string)
	if cmdStr == "" {
		return fmt.Errorf("run_command: --tool-args must include {\"command\": \"...\"}")
	}
	argv := tools.DefaultShellArgv(cmdStr)
	if runtime.GOOS == "windows" && tools.SelectWindowsShell() == "" {
		return fmt.Errorf("run_command: no powershell found on PATH (looked for powershell.exe, then pwsh.exe)")
	}
	c := exec.Command(argv[0], argv[1:]...)
	c.Stdout = w
	c.Stderr = w
	return tools.RunWithPTYStdin(c)
}

// printTools writes the recovered tools catalog to w.
func printTools(w io.Writer) {
	fmt.Fprintf(w, "Recovered tools (%d):\n", len(recoveredTools))
	for i, t := range recoveredTools {
		fmt.Fprintf(w, "  %2d. %s\n", i+1, t)
	}
}

// printServices writes the gRPC service catalog to w.
func printServices(w io.Writer) {
	fmt.Fprintf(w, "Recovered gRPC services (%d):\n", len(recoveredServices))
	for i, s := range recoveredServices {
		fmt.Fprintf(w, "  %2d. %s\n", i+1, s)
	}
}

// printOAuthConfig writes the static OAuth config skeleton.
func printOAuthConfig(w io.Writer) {
	cfg := map[string]any{
		"client_id":     "antigravity-cli",
		"client_secret": "<redacted>",
		"device_url":    "https://cloudaicompanion.googleapis.com/v1/oauth/device",
		"token_url":     "https://oauth2.googleapis.com/token",
		"scopes":        []string{"https://www.googleapis.com/auth/cloud-platform"},
		"redirect_uri":  "http://127.0.0.1:0/oauth/callback",
		"auth_method":   "consumer",
		"status":        "stub (sandbox: cloudaicompanion unreachable)",
		"issued_at":     time.Now().Format(time.RFC3339),
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(cfg)
}

// renderTUI prints the static TUI snapshot.
func renderTUI(w io.Writer) error {
	email := auth.EffectiveUserEmail()
	if email == "" {
		email = "navigaid@gmail.com"
	}
	model := "Gemini 3.8 Flash (High)"
	tier := "Antigravity Starter Quota"
	cwd, err := os.Getwd()
	if err != nil {
		cwd = "/"
	}
	renderBanner(w, email, model, tier, cwd)
	fmt.Fprintln(w, separator)
	fmt.Fprintln(w, "\033[38;2;138;180;248m>\033[39m")
	fmt.Fprintln(w, separator)
	fmt.Fprintln(w, staticStatusRow(model))
	return nil
}

// runOnboardingWizard runs the onboarding wizard.
func runOnboardingWizard(w io.Writer) error {
	wz := onboarding.NewWizard()
	wz.Out = w
	return wz.Run()
}

// tuiSnapshot returns the rendered snapshot string.
func tuiSnapshot() string {
	var sb strings.Builder
	_ = renderTUI(&sb)
	return sb.String()
}
