package rctunnel

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

var ErrLanguageServerNotFound = errors.New("language_server binary not found")
var ErrInstanceNotReady = errors.New("language server not responding on loopback port")

var lsofPortRe = regexp.MustCompile(`:(\d+)\s+\(LISTEN\)`)

// FindLanguageServer locates a language_server binary on disk.
func FindLanguageServer(configured string) string {
	if configured != "" {
		if info, err := os.Stat(configured); err == nil && !info.IsDir() {
			return configured
		}
	}

	if envPath := os.Getenv("AGY_LANGUAGE_SERVER"); envPath != "" {
		if info, err := os.Stat(envPath); err == nil && !info.IsDir() {
			return envPath
		}
	}

	home, _ := os.UserHomeDir()
	exePath, _ := os.Executable()
	exeDir := filepath.Dir(exePath)

	candidates := []string{
		filepath.Join(exeDir, "language_server"),
		filepath.Join(home, ".agy-remote", "language_server"),
		filepath.Join(home, ".gemini", "antigravity-cli", "language_server"),
		filepath.Join(home, ".local", "bin", "language_server"),
	}

	switch runtime.GOOS {
	case "darwin":
		candidates = append(candidates,
			"/Applications/Antigravity.app/Contents/Resources/bin/language_server",
			filepath.Join(home, "Applications", "Antigravity.app", "Contents", "Resources", "bin", "language_server"),
		)
	case "linux":
		candidates = append(candidates,
			"/opt/Antigravity/resources/bin/language_server",
			"/usr/share/antigravity/resources/bin/language_server",
			filepath.Join(home, "antigravity", "language_server"),
		)
	case "windows":
		if localApp := os.Getenv("LOCALAPPDATA"); localApp != "" {
			candidates = append(candidates,
				filepath.Join(localApp, "Programs", "Antigravity", "bin", "language_server.exe"),
				filepath.Join(localApp, "Antigravity", "bin", "language_server.exe"),
			)
		}
	}

	for _, path := range candidates {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path
		}
	}
	return ""
}

// ListeningPorts returns all TCP ports the given PID is currently listening on.
func ListeningPorts(pid int) ([]int, error) {
	if runtime.GOOS == "darwin" || runtime.GOOS == "linux" {
		out, err := exec.Command("lsof", "-a", "-p", strconv.Itoa(pid), "-iTCP", "-sTCP:LISTEN", "-P", "-n").Output()
		if err == nil && len(out) > 0 {
			ports := parseLsofPorts(string(out))
			if len(ports) > 0 {
				return ports, nil
			}
		}
	}

	if runtime.GOOS == "linux" {
		if ports, err := portsViaProc(pid); err == nil && len(ports) > 0 {
			return ports, nil
		}
	}

	return nil, fmt.Errorf("no listening ports found for pid %d", pid)
}

func parseLsofPorts(out string) []int {
	var ports []int
	seen := make(map[int]bool)
	for _, m := range lsofPortRe.FindAllStringSubmatch(out, -1) {
		p, err := strconv.Atoi(m[1])
		if err == nil && p > 0 && p <= 65535 && !seen[p] {
			seen[p] = true
			ports = append(ports, p)
		}
	}
	return ports
}

func portsViaProc(pid int) ([]int, error) {
	fdDir := fmt.Sprintf("/proc/%d/fd", pid)
	entries, err := os.ReadDir(fdDir)
	if err != nil {
		return nil, err
	}
	socketInodes := make(map[string]bool)
	for _, entry := range entries {
		target, err := os.Readlink(filepath.Join(fdDir, entry.Name()))
		if err != nil {
			continue
		}
		if strings.HasPrefix(target, "socket:[") && strings.HasSuffix(target, "]") {
			inode := target[8 : len(target)-1]
			socketInodes[inode] = true
		}
	}
	if len(socketInodes) == 0 {
		return nil, errors.New("no socket inodes in /proc")
	}

	var ports []int
	seen := make(map[int]bool)
	for _, netFile := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		data, err := os.ReadFile(netFile)
		if err != nil {
			continue
		}
		lines := strings.Split(string(data), "\n")
		for _, line := range lines[1:] {
			fields := strings.Fields(line)
			if len(fields) < 10 {
				continue
			}
			state := fields[3]
			inode := fields[9]
			if state == "0A" && socketInodes[inode] { // 0A = TCP_LISTEN
				parts := strings.Split(fields[1], ":")
				if len(parts) == 2 {
					if p64, err := strconv.ParseInt(parts[1], 16, 32); err == nil {
						p := int(p64)
						if p > 0 && p <= 65535 && !seen[p] {
							seen[p] = true
							ports = append(ports, p)
						}
					}
				}
			}
		}
	}
	return ports, nil
}

var probeClient = &http.Client{
	Timeout: 2 * time.Second,
	Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	},
}

// ServesWebUI checks whether the given loopback HTTPS port serves the Antigravity web UI.
func ServesWebUI(port int) bool {
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("https://127.0.0.1:%d/", port), nil)
	if err != nil {
		return false
	}
	req.Header.Set("Accept", "text/html")
	resp, err := probeClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false
	}
	head, err := io.ReadAll(io.LimitReader(resp.Body, 8192))
	if err != nil {
		return false
	}
	return strings.Contains(string(head), "__APP_CONFIG__")
}

// FindListeningInstance polls until the language server at pid has opened its web UI port.
func FindListeningInstance(ctx context.Context, pid int, csrfToken string) (*Instance, error) {
	ticker := time.NewTicker(150 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			ports, err := ListeningPorts(pid)
			if err != nil || len(ports) == 0 {
				continue
			}
			for _, port := range ports {
				if ServesWebUI(port) {
					return &Instance{
						PID:       pid,
						Port:      port,
						CSRFToken: csrfToken,
					}, nil
				}
			}
		}
	}
}
