package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/justwasm/pigasus/herdr"
	"golang.org/x/term"
)

func main() {
	if len(os.Args) > 1 && !strings.HasPrefix(os.Args[1], "-") {
		runCLI(os.Args[1:])
		return
	}

	dirFlag := flag.String("dir", "", "initial working directory")
	shellFlag := flag.String("shell", "", "shell to spawn (defaults to $SHELL)")
	socketFlag := flag.String("socket", "", "custom UNIX domain socket path for Herdr API")
	flag.Parse()

	if flag.NArg() > 0 {
		runCLI(flag.Args())
		return
	}

	runTUI(*dirFlag, *shellFlag, *socketFlag)
}

func runCLI(args []string) {
	var socketPath string
	var cleanArgs []string

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if (arg == "--socket" || arg == "-s") && i+1 < len(args) {
			socketPath = args[i+1]
			i++
			continue
		}
		if strings.HasPrefix(arg, "--socket=") {
			socketPath = strings.TrimPrefix(arg, "--socket=")
			continue
		}
		cleanArgs = append(cleanArgs, arg)
	}

	if socketPath == "" {
		socketPath = os.Getenv("HERDR_SOCKET_PATH")
	}
	if socketPath == "" {
		if sess := os.Getenv("HERDR_SESSION"); sess != "" {
			socketPath = filepath.Join(os.TempDir(), fmt.Sprintf("herdr-%s.sock", sess))
		}
	}
	if socketPath == "" {
		matches, _ := filepath.Glob(filepath.Join(os.TempDir(), "herdr-*.sock"))
		if len(matches) > 0 {
			socketPath = matches[len(matches)-1]
		}
	}
	if socketPath == "" {
		socketPath = filepath.Join(os.TempDir(), fmt.Sprintf("herdr-%d.sock", os.Getpid()))
	}

	method, params := parseSubcommand(cleanArgs)
	respBytes, err := herdr.CallSocket(socketPath, method, params)
	if err != nil {
		errResp := map[string]any{
			"error": map[string]string{
				"code":    "server_unavailable",
				"message": err.Error(),
			},
		}
		data, _ := json.Marshal(errResp)
		fmt.Fprintln(os.Stderr, string(data))
		os.Exit(1)
	}

	// Check if the response contains an error envelope
	var check struct {
		Error *herdr.ResponseError `json:"error"`
	}
	_ = json.Unmarshal(respBytes, &check)

	if check.Error != nil && check.Error.Code != "" {
		fmt.Fprintln(os.Stderr, string(respBytes))
		os.Exit(1)
	}

	fmt.Println(string(respBytes))
}

func parseSubcommand(args []string) (string, map[string]any) {
	if len(args) == 0 {
		return "system.ping", map[string]any{}
	}

	sub := args[0]
	switch sub {
	case "ping":
		return "system.ping", map[string]any{}

	case "agent":
		if len(args) >= 4 && args[1] == "prompt" {
			return "agent.prompt", map[string]any{
				"target":  args[2],
				"message": args[3],
			}
		}
		if len(args) >= 3 && args[1] == "prompt" {
			return "agent.prompt", map[string]any{
				"target":  args[2],
				"message": "",
			}
		}

	case "pane":
		if len(args) >= 2 {
			switch args[1] {
			case "list":
				return "pane.list", map[string]any{}
			case "get":
				paneID := ""
				if len(args) >= 3 {
					paneID = args[2]
				}
				return "pane.get", map[string]any{"pane_id": paneID}
			case "split":
				dir := "vertical"
				if len(args) >= 3 {
					dir = args[2]
				}
				return "pane.split", map[string]any{"direction": dir}
			case "close":
				paneID := ""
				if len(args) >= 3 {
					paneID = args[2]
				}
				return "pane.close", map[string]any{"pane_id": paneID}
			case "send-text", "send_text":
				paneID, text := "", ""
				if len(args) >= 3 {
					paneID = args[2]
				}
				if len(args) >= 4 {
					text = args[3]
				}
				return "pane.send_text", map[string]any{"pane_id": paneID, "text": text}
			}
		}

	case "workspace":
		if len(args) >= 2 && args[1] == "list" {
			return "workspace.list", map[string]any{}
		}
		return "workspace.get", map[string]any{}
	}

	if len(args) == 1 {
		return args[0], map[string]any{}
	}
	return strings.Join(args[:2], "."), map[string]any{}
}

func runTUI(dir, shell, customSocket string) {
	if dir == "" {
		dir, _ = os.Getwd()
	}
	if shell != "" {
		_ = os.Setenv("SHELL", shell)
	}

	cols, rows, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || cols == 0 || rows == 0 {
		cols, rows = 100, 30
	}

	model := herdr.NewModel(rows, cols)
	defer model.Close()

	socketPath := customSocket
	if socketPath == "" {
		socketPath = os.Getenv("HERDR_SOCKET_PATH")
	}
	if socketPath == "" {
		socketPath = filepath.Join(os.TempDir(), fmt.Sprintf("herdr-%d.sock", os.Getpid()))
	}

	// Start Herdr Unix Domain Socket server
	if _, err := model.StartSocketServer(socketPath); err != nil {
		fmt.Fprintf(os.Stderr, "herdr: warning: unable to start socket server at %s: %v\n", socketPath, err)
	} else {
		_ = os.Setenv("HERDR_SOCKET_PATH", socketPath)
	}

	// Spawn initial pane
	if _, err := model.SpawnDefaultPane("", dir); err != nil {
		fmt.Fprintf(os.Stderr, "herdr: warning: unable to spawn initial shell pane: %v\n", err)
	}

	p := tea.NewProgram(model)
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "herdr: runtime error: %v\n", err)
		os.Exit(1)
	}
}
