package clipboard

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// Reader is the seam tests can override to mock clipboard reads.
var Reader = defaultRead

// Writer is the seam tests can override to mock clipboard writes.
var Writer = defaultWrite

// Read reads text from the system clipboard.
func Read() string {
	return Reader()
}

// Write writes text to the system clipboard.
func Write(text string) error {
	return Writer(text)
}

func defaultRead() string {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("pbpaste")
	case "linux":
		cmd = linuxPasteCmd()
		if cmd == nil {
			return ""
		}
	default:
		return ""
	}
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return string(out)
}

func linuxPasteCmd() *exec.Cmd {
	switch {
	case haveExec("wl-paste"):
		return exec.Command("wl-paste", "--no-newline")
	case haveExec("xclip"):
		return exec.Command("xclip", "-selection", "clipboard", "-o")
	case haveExec("xsel"):
		return exec.Command("xsel", "--clipboard", "--output")
	default:
		return nil
	}
}

func defaultWrite(text string) error {
	if SupportsOSC52() && WriteOSC52(os.Stderr, text) {
		return nil
	}
	return shellOutWrite(text)
}

func shellOutWrite(text string) error {
	cmd := writeCmd()
	if cmd == nil {
		return fmt.Errorf("clipboard is not supported on %s", runtime.GOOS)
	}
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}

func writeCmd() *exec.Cmd {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("pbcopy")
	case "linux":
		return linuxCopyCmd()
	default:
		return nil
	}
}

func linuxCopyCmd() *exec.Cmd {
	switch {
	case haveExec("wl-copy"):
		return exec.Command("wl-copy")
	case haveExec("xclip"):
		return exec.Command("xclip", "-selection", "clipboard")
	case haveExec("xsel"):
		return exec.Command("xsel", "--clipboard", "--input")
	default:
		return nil
	}
}

func haveExec(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}
