package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Scenario represents an executable testing scenario.
type Scenario struct {
	Name         string
	Width        int
	Height       int
	Target       TmuxTarget
	SettingsJSON string
	Turns        []MockTurn
	Commands     []ScenarioCmd
	Snapshots    map[string]DiffResult
	LastSnapshot string
}

// ScenarioCmd represents one step executed during the run.
type ScenarioCmd struct {
	Action string
	Args   []string
}

// ParseScenario parses a scenario script file (.align / .scenario).
func ParseScenario(path string) (*Scenario, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open scenario %s: %w", path, err)
	}
	defer file.Close()

	sc := &Scenario{
		Width:     100,
		Height:    30,
		Target:    TargetBoth,
		Snapshots: make(map[string]DiffResult),
	}

	scanner := bufio.NewScanner(file)
	var currentTurn *MockTurn
	var currentChunk *MockChunk

	commitChunk := func() {
		if currentChunk != nil && currentTurn != nil {
			currentTurn.Chunks = append(currentTurn.Chunks, *currentChunk)
			currentChunk = nil
		}
	}

	commitTurn := func() {
		commitChunk()
		if currentTurn != nil {
			sc.Turns = append(sc.Turns, *currentTurn)
			currentTurn = nil
		}
	}

	lineNum := 0
	for scanner.Scan() {
		lineNum++
		raw := strings.TrimSpace(scanner.Text())
		if raw == "" || strings.HasPrefix(raw, "#") || strings.HasPrefix(raw, "//") {
			continue
		}

		parts := tokenize(raw)
		if len(parts) == 0 {
			continue
		}

		cmd := strings.ToUpper(parts[0])

		switch cmd {
		case "NAME":
			if len(parts) > 1 {
				sc.Name = parts[1]
			}

		case "WINDOW", "SIZE":
			if len(parts) >= 3 {
				w, _ := strconv.Atoi(parts[1])
				h, _ := strconv.Atoi(parts[2])
				if w > 0 {
					sc.Width = w
				}
				if h > 0 {
					sc.Height = h
				}
			}

		case "TARGET":
			if len(parts) > 1 {
				sc.Target = TmuxTarget(strings.ToLower(parts[1]))
			}

		case "SETTINGS":
			val := strings.TrimSpace(raw[len("SETTINGS"):])
			val = strings.Trim(val, "'\"")
			sc.SettingsJSON = val

		case "MOCK_TURN":
			commitTurn()
			currentTurn = &MockTurn{}

		case "THOUGHT":
			commitChunk()
			text := ""
			if len(parts) > 1 {
				text = parts[1]
			}
			chunk := MockChunk{Thought: text}
			// Parse optional arguments: TOKENS <n> DELAY <d>
			for i := 2; i < len(parts); i++ {
				switch strings.ToUpper(parts[i]) {
				case "TOKENS":
					if i+1 < len(parts) {
						chunk.ThoughtTokens, _ = strconv.Atoi(parts[i+1])
						i++
					}
				case "DELAY":
					if i+1 < len(parts) {
						chunk.Delay, _ = time.ParseDuration(parts[i+1])
						i++
					}
				}
			}
			currentChunk = &chunk

		case "TEXT":
			commitChunk()
			text := ""
			if len(parts) > 1 {
				text = parts[1]
			}
			chunk := MockChunk{Text: text}
			for i := 2; i < len(parts); i++ {
				switch strings.ToUpper(parts[i]) {
				case "DELAY":
					if i+1 < len(parts) {
						chunk.Delay, _ = time.ParseDuration(parts[i+1])
						i++
					}
				}
			}
			currentChunk = &chunk

		case "DELAY":
			commitChunk()
			d := 1 * time.Second
			if len(parts) > 1 {
				if parsed, err := time.ParseDuration(parts[1]); err == nil {
					d = parsed
				}
			}
			currentChunk = &MockChunk{Delay: d}

		case "TOOL":
			commitChunk()
			name := ""
			var args map[string]any
			fields := strings.Fields(raw)
			if len(fields) > 1 {
				name = fields[1]
			}
			idxJSON := strings.Index(raw, "{")
			if idxJSON != -1 {
				_ = json.Unmarshal([]byte(raw[idxJSON:]), &args)
			}
			currentChunk = &MockChunk{
				ToolCall: &ToolCallSpec{
					Name: name,
					Args: args,
				},
			}

		case "USAGE":
			if currentChunk == nil {
				currentChunk = &MockChunk{}
			}
			for i := 1; i < len(parts); i++ {
				switch strings.ToUpper(parts[i]) {
				case "PROMPT":
					if i+1 < len(parts) {
						currentChunk.PromptTokens, _ = strconv.Atoi(parts[i+1])
						i++
					}
				case "CANDIDATES":
					if i+1 < len(parts) {
						currentChunk.CandidateTokens, _ = strconv.Atoi(parts[i+1])
						i++
					}
				case "THOUGHTS":
					if i+1 < len(parts) {
						currentChunk.ThoughtTokens, _ = strconv.Atoi(parts[i+1])
						i++
					}
				}
			}

		case "FINISH":
			if currentChunk == nil {
				currentChunk = &MockChunk{}
			}
			reason := "STOP"
			if len(parts) > 1 {
				reason = parts[1]
			}
			currentChunk.FinishReason = reason

		default:
			// Action commands: BOOT, SEND, TYPE, KEYS, WAIT_READY, WAIT_TEXT, SLEEP, SNAPSHOT, DIFF
			commitTurn()
			sc.Commands = append(sc.Commands, ScenarioCmd{
				Action: cmd,
				Args:   parts[1:],
			})
		}
	}
	commitTurn()

	return sc, nil
}

// tokenize splits a line into tokens, respecting double-quoted strings.
// waitForBannerSettled gives both panes a moment to paint the account row's
// plan tier.
//
// The tier is not part of the first frame: each binary asks the mock's
// loadCodeAssist for it after the prompt is already usable, and repaints.
// A snapshot taken in between catches one binary with "(Antigravity Pro)" and
// the other without, which reads as a banner mismatch on a run where nothing
// is wrong. The wait is best effort and never fails the scenario — a binary
// that never shows a tier simply keeps the diff, which is then real.
func (sc *Scenario) waitForBannerSettled(driver *TmuxDriver) {
	const settle = 3 * time.Second
	if sc.Target == TargetBoth {
		_, _, _ = driver.WaitForBoth(MockPlanTier, settle)
		return
	}
	_, _ = driver.WaitForText("", MockPlanTier, settle)
}

func tokenize(line string) []string {
	var tokens []string
	var current strings.Builder
	inQuote := false

	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case c == '"':
			inQuote = !inQuote
		case c == ' ' || c == '\t':
			if inQuote {
				current.WriteByte(c)
			} else if current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
		case c == '\\' && i+1 < len(line):
			next := line[i+1]
			if next == 'n' {
				current.WriteByte('\n')
			} else if next == '"' {
				current.WriteByte('"')
			} else {
				current.WriteByte('\\')
				current.WriteByte(next)
			}
			i++
		default:
			current.WriteByte(c)
		}
	}
	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}
	return tokens
}

// Run executes the scenario end-to-end with MockServer and TmuxDriver.
func (sc *Scenario) Run(srv *MockServer, driver *TmuxDriver) error {
	srv.Reset()
	for _, turn := range sc.Turns {
		srv.AddTurn(turn)
	}

	for idx, cmd := range sc.Commands {
		fmt.Printf("[tuialign:step %d] %s %v\n", idx+1, cmd.Action, cmd.Args)

		switch cmd.Action {
		case "BOOT", "BOOT_BOTH", "LAUNCH":
			target := sc.Target
			if len(cmd.Args) > 0 {
				target = TmuxTarget(strings.ToLower(cmd.Args[0]))
			}
			if sc.SettingsJSON != "" {
				driver.cfg.SettingsJSON = sc.SettingsJSON
			}
			if err := driver.Launch(target); err != nil {
				return fmt.Errorf("boot failed: %w", err)
			}

		case "WAIT_READY":
			timeout := 10 * time.Second
			if len(cmd.Args) > 0 {
				if d, err := time.ParseDuration(cmd.Args[0]); err == nil {
					timeout = d
				}
			}
			fmt.Printf("   ... waiting for prompt (> ) ready\n")
			if sc.Target == TargetBoth {
				if _, _, err := driver.WaitForBoth(">", timeout); err != nil {
					return err
				}
			} else {
				if _, err := driver.WaitForText("", ">", timeout); err != nil {
					return err
				}
			}
			sc.waitForBannerSettled(driver)

		case "WAIT_TEXT", "EXPECT_TEXT":
			if len(cmd.Args) == 0 {
				return fmt.Errorf("%s requires a target string", cmd.Action)
			}
			targetText := cmd.Args[0]
			timeout := 15 * time.Second
			if len(cmd.Args) > 1 {
				if d, err := time.ParseDuration(cmd.Args[1]); err == nil {
					timeout = d
				}
			}
			fmt.Printf("   ... waiting for text %q\n", targetText)
			if sc.Target == TargetBoth {
				if _, _, err := driver.WaitForBoth(targetText, timeout); err != nil {
					return err
				}
			} else {
				if _, err := driver.WaitForText("", targetText, timeout); err != nil {
					return err
				}
			}
			if cmd.Action == "EXPECT_TEXT" {
				fmt.Printf("   [ASSERT PASS] Expected text %q found\n", targetText)
			}

		case "ASSERT_NOT_TEXT":
			if len(cmd.Args) == 0 {
				return fmt.Errorf("ASSERT_NOT_TEXT requires a forbidden string")
			}
			forbidden := cmd.Args[0]
			fmt.Printf("   ... asserting text %q is NOT present\n", forbidden)
			if sc.Target == TargetBoth {
				left, right, err := driver.CaptureBoth(false)
				if err != nil {
					return err
				}
				if strings.Contains(left, forbidden) || strings.Contains(right, forbidden) {
					return fmt.Errorf("ASSERT_NOT_TEXT failed: forbidden text %q found on screen", forbidden)
				}
			} else {
				out, err := driver.CapturePane("", false)
				if err != nil {
					return err
				}
				if strings.Contains(out, forbidden) {
					return fmt.Errorf("ASSERT_NOT_TEXT failed: forbidden text %q found on screen", forbidden)
				}
			}
			fmt.Printf("   [ASSERT PASS] Forbidden text %q is absent\n", forbidden)

		case "STEER":
			if len(cmd.Args) == 0 {
				continue
			}
			text := cmd.Args[0]
			fmt.Printf("   ... steering in-flight conversation with: %q\n", text)
			if sc.Target == TargetBoth {
				if err := driver.SendBoth(text); err != nil {
					return err
				}
				if err := driver.SendBoth("C-m"); err != nil {
					return err
				}
			} else {
				if err := driver.SendKeys("", text); err != nil {
					return err
				}
				if err := driver.SendKeys("", "C-m"); err != nil {
					return err
				}
			}

		case "SEND", "SEND_KEYS", "KEYS":
			if len(cmd.Args) == 0 {
				continue
			}
			if sc.Target == TargetBoth {
				if err := driver.SendBoth(cmd.Args...); err != nil {
					return err
				}
			} else {
				if err := driver.SendKeys("", cmd.Args...); err != nil {
					return err
				}
			}

		case "TYPE":
			if len(cmd.Args) == 0 {
				continue
			}
			text := cmd.Args[0]
			if sc.Target == TargetBoth {
				if err := driver.SendBoth(text); err != nil {
					return err
				}
			} else {
				if err := driver.SendKeys("", text); err != nil {
					return err
				}
			}

		case "ENTER":
			if sc.Target == TargetBoth {
				if err := driver.SendBoth("C-m"); err != nil {
					return err
				}
			} else {
				if err := driver.SendKeys("", "C-m"); err != nil {
					return err
				}
			}

		case "SLEEP":
			dur := 500 * time.Millisecond
			if len(cmd.Args) > 0 {
				if d, err := time.ParseDuration(cmd.Args[0]); err == nil {
					dur = d
				}
			}
			time.Sleep(dur)

		case "RESIZE":
			if len(cmd.Args) >= 2 {
				w, _ := strconv.Atoi(cmd.Args[0])
				h, _ := strconv.Atoi(cmd.Args[1])
				fmt.Printf("   ... resizing terminal window to %dx%d (triggering SIGWINCH)\n", w, h)
				if err := driver.Resize(sc.Target, w, h); err != nil {
					return err
				}
			}

		case "SNAPSHOT":
			label := fmt.Sprintf("step_%d", idx+1)
			if len(cmd.Args) > 0 {
				label = cmd.Args[0]
			}
			sc.LastSnapshot = label
			if sc.Target == TargetBoth {
				left, right, err := driver.CaptureBoth(true)
				if err != nil {
					return err
				}
				res := CompareSnapshots(left, right, label)
				sc.Snapshots[label] = res
				fmt.Print(res.Report)

				// Save HTML visual diff report
				htmlPath := fmt.Sprintf("/tmp/tuialign_%s.html", label)
				_ = os.WriteFile(htmlPath, []byte(res.HTMLReport), 0644)
			} else {
				out, err := driver.CapturePane("", true)
				if err != nil {
					return err
				}
				fmt.Printf("=== SNAPSHOT [%s] ===\n%s\n", label, out)
			}

		case "ASSERT_PARITY":
			if len(cmd.Args) == 0 {
				return fmt.Errorf("ASSERT_PARITY requires min percentage argument (e.g. 50)")
			}
			minP, err := strconv.ParseFloat(cmd.Args[0], 64)
			if err != nil {
				return fmt.Errorf("ASSERT_PARITY parse %s: %w", cmd.Args[0], err)
			}
			res, ok := sc.Snapshots[sc.LastSnapshot]
			if !ok {
				return fmt.Errorf("ASSERT_PARITY: no snapshot captured before assertion")
			}
			if res.ParityScore < minP {
				return fmt.Errorf("ASSERT_PARITY failed for %q: score %.1f%% < required %.1f%%",
					sc.LastSnapshot, res.ParityScore, minP)
			}
			fmt.Printf("   [ASSERT PASS] Snapshot %q scored %.1f%% (>= required %.1f%%)\n",
				sc.LastSnapshot, res.ParityScore, minP)

		case "DIFF":
			// Print summary of all snapshots taken. `DIFF <pct>` also gates
			// on it: a scenario whose screens are meant to be identical
			// must fail the run when they are not, otherwise every scenario
			// is a report and none of them is a test.
			fmt.Println("\n================ FINAL PARITY SUMMARY ================")
			allPass := true
			for label, res := range sc.Snapshots {
				status := "PASS"
				if !res.Identical {
					status = "WARN"
					allPass = false
				}
				fmt.Printf("[%s] Snapshot %-20s : %.1f%% parity (%d diffs)\n",
					status, label, res.ParityScore, res.DiffLines)
			}
			if allPass {
				fmt.Println(">>> PERFECT ALIGNMENT: 100% PARITY! <<<")
			}
			fmt.Println("======================================================")
			if len(cmd.Args) > 0 {
				minP, perr := strconv.ParseFloat(cmd.Args[0], 64)
				if perr != nil {
					return fmt.Errorf("DIFF parse %s: %w", cmd.Args[0], perr)
				}
				worst := ""
				worstScore := 101.0 // above any possible score, so the first snapshot is taken
				for label, res := range sc.Snapshots {
					if res.ParityScore < worstScore {
						worstScore, worst = res.ParityScore, label
					}
				}
				if worst != "" && worstScore < minP {
					return fmt.Errorf("DIFF gate failed: snapshot %q scored %.1f%% (required %.1f%%)",
						worst, worstScore, minP)
				}
				fmt.Printf("   [ASSERT PASS] every snapshot scored >= %.1f%% (worst: %s at %.1f%%)\n",
					minP, worst, worstScore)
			}

		case "CLEANUP":
			driver.Close()

		default:
			return fmt.Errorf("unknown action: %s", cmd.Action)
		}
	}

	return nil
}
