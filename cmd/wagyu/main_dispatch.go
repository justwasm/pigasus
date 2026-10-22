package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/justwasm/pigasus/internal/agent"
	"github.com/justwasm/pigasus/internal/auth"
	"github.com/justwasm/pigasus/internal/cli"
	"github.com/justwasm/pigasus/internal/onboarding"
)

// checkRootShortCircuits handles flags that print catalog/info and exit.
func checkRootShortCircuits(fs *flag.FlagSet, p *cli.Parser, out io.Writer) (bool, error) {
	if flagBoolValue(fs, "version") {
		fmt.Fprint(out, p.Root().Version)
		return true, nil
	}
	if flagBoolValue(fs, "tools") {
		printTools(out)
		return true, nil
	}
	if flagBoolValue(fs, "services") {
		printServices(out)
		return true, nil
	}
	if flagBoolValue(fs, "oauth") {
		printOAuthConfig(out)
		return true, nil
	}
	if flagBoolValue(fs, "tui") {
		return true, renderTUI(out)
	}
	if toolName := flagStringValue(fs, "tool"); toolName != "" {
		return true, dispatchTool(out, toolName, flagStringValue(fs, "tool-args"))
	}
	if flagBoolValue(fs, "diff-with-agy") {
		runFeatDiffCLI(out, printOutputFormat(fs))
		return true, nil
	}
	return false, nil
}

// validateRootFlags validates mutual exclusivity, JSON schema, and agent flags.
func validateRootFlags(fs *flag.FlagSet, printMode bool) error {
	if err := checkRemoteControlADC(fs); err != nil {
		return err
	}
	promptInter := flagWasSet(fs, "prompt-interactive") || flagWasSet(fs, "i")
	if printMode && promptInter {
		fmt.Fprintln(os.Stderr, "error: --print and --prompt-interactive are mutually exclusive")
		return cli.ExitCodeError(2, "error: --print and --prompt-interactive are mutually exclusive")
	}
	if err := validateJSONSchemaFlag(fs); err != nil {
		return cli.ExitCodeError(1, err.Error())
	}
	agentErr := agent.ValidateAgent(flagStringValue(fs, "agent"))
	if agentErr != nil && printMode {
		return cli.ExitCodeError(1, agentErr.Error())
	}
	if printMode {
		if _, err := resolveModelSelection(flagStringValue(fs, "model"), flagStringValue(fs, "effort")); err != nil {
			return cli.ExitCodeError(1, err.Error())
		}
	} else if agentErr != nil && !isTerminalInteractive(os.Stdin) {
		var nf *agent.AgentNotFoundError
		if errors.As(agentErr, &nf) {
			fmt.Fprintf(os.Stderr, "Agent Not Found: %s. Using the default agent instead.\n",
				strings.TrimPrefix(nf.Error(), "error: "))
		}
	}
	return nil
}

// dispatchPrintMode runs the headless print mode turn.
func dispatchPrintMode(fs *flag.FlagSet, out io.Writer) error {
	prompt := printPrompt(fs)
	if prompt == "" {
		fmt.Fprintln(os.Stderr, "antigravity: --print requires a positional prompt argument")
		return fmt.Errorf("missing prompt")
	}
	if resolveBoolAlias(fs, "continue", "c") {
		cwd, _ := os.Getwd()
		id, _, err := resolveContinue(cwd)
		if err != nil {
			return cli.ExitCodeError(1, fmt.Sprintf("error: --continue: %v", err))
		}
		if id == "" {
			continueNoConversationMessage(out)
			return cli.ExitCodeError(1, "no previous conversation to continue")
		}
	}
	warnAgentMode(fs, false)
	return runPrintMode(out, prompt, fs)
}

// dispatchInteractiveMode runs interactive mode for --prompt-interactive or --continue.
func dispatchInteractiveMode(fs *flag.FlagSet, out io.Writer) error {
	warnAgentMode(fs, false)
	return runLiveTUI(out, fs)
}

// dispatchDefaultRoot handles the launch with no specific mode flag.
func dispatchDefaultRoot(args []string, fs *flag.FlagSet, out io.Writer) error {
	if len(args) == 0 {
		warnAgentMode(fs, false)
		maybeRunOnboarding(fs, out)
		return runLiveTUI(out, fs)
	}
	prompt := strings.Join(args, " ")
	warnAgentMode(fs, false)
	return runPrintMode(out, prompt, fs)
}

func maybeRunOnboarding(fs *flag.FlagSet, out io.Writer) {
	if flagBoolValue(fs, "no-onboarding") || auth.OnboardingCompleted() {
		return
	}
	wz := onboarding.NewWizard()
	wz.Out = out
	if err := wz.Run(); err == nil && wz.Step == onboarding.StepDone {
		_ = auth.SaveSettingsColorScheme(wz.Selected.String())
		s, _ := auth.LoadJetboxState()
		s.PostOnboardingStep = "agent_configuration"
		if s.AgentEnvironment == "" {
			s.AgentEnvironment = "local"
		}
		if s.ThemeMode == "" {
			s.ThemeMode = "dark"
		}
		if s.ConversationWidth == "" {
			s.ConversationWidth = "wide"
		}
		if s.TerminalPlacement == "" {
			s.TerminalPlacement = "below_input"
		}
		if s.UserNuxEvent == "" {
			s.UserNuxEvent = "first_run"
		}
		_ = auth.SaveJetboxState(s)
	}
}
