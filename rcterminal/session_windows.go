//go:build windows

// session_windows.go: ConPTY (Windows Pseudo Console) support for Windows terminals.
package rcterminal

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	defaultRows = 24
	defaultCols = 80
)

// ErrUnsupported is returned when ConPTY is unavailable on the host.
var ErrUnsupported = errors.New("rcterminal: terminals need a pseudo console (ConPTY), which this Windows system does not support")

type conptyDevice struct {
	in        *os.File
	out       *os.File
	hPC       windows.Handle
	hProc     windows.Handle
	closeOnce sync.Once
}

func (c *conptyDevice) Read(p []byte) (int, error) {
	return c.out.Read(p)
}

func (c *conptyDevice) Write(p []byte) (int, error) {
	return c.in.Write(p)
}

func (c *conptyDevice) Resize(cols, rows int) error {
	return windows.ResizePseudoConsole(c.hPC, windows.Coord{
		X: int16(cols),
		Y: int16(rows),
	})
}

func (c *conptyDevice) Close() error {
	c.closeOnce.Do(func() {
		_ = c.in.Close()
		_ = c.out.Close()
		windows.ClosePseudoConsole(c.hPC)
		if c.hProc != 0 {
			windows.CloseHandle(c.hProc)
		}
	})
	return nil
}

// spawn starts an interactive shell (PowerShell) on a fresh ConPTY pseudo console.
func spawn(dir string) (*Session, error) {
	shell := defaultWindowsShell()

	// 1. Create communication pipes for ConPTY
	var inRead, inWrite windows.Handle
	if err := windows.CreatePipe(&inRead, &inWrite, nil, 0); err != nil {
		return nil, fmt.Errorf("rcterminal: create in pipe: %w", err)
	}

	var outRead, outWrite windows.Handle
	if err := windows.CreatePipe(&outRead, &outWrite, nil, 0); err != nil {
		windows.CloseHandle(inRead)
		windows.CloseHandle(inWrite)
		return nil, fmt.Errorf("rcterminal: create out pipe: %w", err)
	}

	// 2. Create the PseudoConsole
	var hPC windows.Handle
	coord := windows.Coord{X: int16(defaultCols), Y: int16(defaultRows)}
	if err := windows.CreatePseudoConsole(coord, inRead, outWrite, 0, &hPC); err != nil {
		windows.CloseHandle(inRead)
		windows.CloseHandle(inWrite)
		windows.CloseHandle(outRead)
		windows.CloseHandle(outWrite)
		return nil, fmt.Errorf("%w: %v", ErrUnsupported, err)
	}

	// PseudoConsole owns inRead and outWrite; close our copies.
	windows.CloseHandle(inRead)
	windows.CloseHandle(outWrite)

	// 3. Prepare startup info with PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE
	attrList, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		windows.ClosePseudoConsole(hPC)
		windows.CloseHandle(inWrite)
		windows.CloseHandle(outRead)
		return nil, fmt.Errorf("rcterminal: new proc thread attr list: %w", err)
	}
	defer attrList.Delete()

	if err := attrList.Update(windows.PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE, unsafe.Pointer(hPC), unsafe.Sizeof(hPC)); err != nil {
		windows.ClosePseudoConsole(hPC)
		windows.CloseHandle(inWrite)
		windows.CloseHandle(outRead)
		return nil, fmt.Errorf("rcterminal: update attr list: %w", err)
	}

	// 4. Spawn the interactive shell process
	var si windows.StartupInfoEx
	si.Cb = uint32(unsafe.Sizeof(si))
	si.ProcThreadAttributeList = attrList.List()

	var pi windows.ProcessInformation

	cmdLineStr := shell
	base := strings.ToLower(filepath.Base(shell))
	if strings.Contains(base, "powershell") || strings.Contains(base, "pwsh") {
		cmdLineStr = fmt.Sprintf(`"%s" -NoLogo`, shell)
	}
	cmdLine, err := windows.UTF16PtrFromString(cmdLineStr)
	if err != nil {
		windows.ClosePseudoConsole(hPC)
		windows.CloseHandle(inWrite)
		windows.CloseHandle(outRead)
		return nil, err
	}

	var workDir *uint16
	if dir != "" {
		workDir, err = windows.UTF16PtrFromString(dir)
		if err != nil {
			windows.ClosePseudoConsole(hPC)
			windows.CloseHandle(inWrite)
			windows.CloseHandle(outRead)
			return nil, err
		}
	}

	flags := uint32(windows.EXTENDED_STARTUPINFO_PRESENT)
	if err := windows.CreateProcess(nil, cmdLine, nil, nil, false, flags, nil, workDir, &si.StartupInfo, &pi); err != nil {
		windows.ClosePseudoConsole(hPC)
		windows.CloseHandle(inWrite)
		windows.CloseHandle(outRead)
		return nil, fmt.Errorf("rcterminal: create process: %w", err)
	}
	windows.CloseHandle(pi.Thread)

	inFile := os.NewFile(uintptr(inWrite), "conpty-in")
	outFile := os.NewFile(uintptr(outRead), "conpty-out")

	title := strings.TrimSuffix(filepath.Base(shell), filepath.Ext(shell))
	session := newSession(newTerminalID(), int(pi.ProcessId), title)
	if proc, err := os.FindProcess(int(pi.ProcessId)); err == nil {
		session.cmd = proc
	}

	dev := &conptyDevice{
		in:    inFile,
		out:   outFile,
		hPC:   hPC,
		hProc: pi.Process,
	}
	session.ptmx = dev

	session.waitExit = func() int {
		windows.WaitForSingleObject(pi.Process, windows.INFINITE)
		var exitCode uint32
		if err := windows.GetExitCodeProcess(pi.Process, &exitCode); err == nil {
			return int(exitCode)
		}
		return -1
	}

	return session, nil
}

func defaultWindowsShell() string {
	if p, err := exec.LookPath("pwsh.exe"); err == nil {
		return p
	}
	if p, err := exec.LookPath("powershell.exe"); err == nil {
		return p
	}
	sysRoot := os.Getenv("SystemRoot")
	if sysRoot == "" {
		sysRoot = `C:\Windows`
	}
	ps := filepath.Join(sysRoot, `System32\WindowsPowerShell\v1.0\powershell.exe`)
	if _, err := os.Stat(ps); err == nil {
		return ps
	}
	if comspec := os.Getenv("COMSPEC"); comspec != "" {
		return comspec
	}
	return "powershell.exe"
}

// pump reads the pty until the shell is gone.
func (s *Session) pump() {
	buf := make([]byte, 32*1024)
	for {
		n, err := s.ptmx.Read(buf)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			s.handleOutput(chunk)
		}
		if err != nil {
			break
		}
	}
	var code int
	if s.waitExit != nil {
		code = s.waitExit()
	}
	s.finish(code)
}

// resize changes the pty's window size.
func (s *Session) resize(cols, rows int) error {
	if cols <= 0 || rows <= 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.exited || s.closed {
		return ErrClosed
	}
	if s.ptmx == nil {
		return ErrClosed
	}
	return s.ptmx.Resize(cols, rows)
}

// close ends the terminal.
func (s *Session) close(force bool) {
	s.mu.Lock()
	s.closed = true
	proc := s.cmd
	s.mu.Unlock()

	if proc != nil {
		_ = proc.Kill()
	}
	if s.ptmx != nil {
		_ = s.ptmx.Close()
	}
	s.closeSubscribers()
	close(s.done)
}
