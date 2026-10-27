// Package refbackend — spawn.go: start a reference `language_server`
// binary and point the proxy at it.
//
// The reference ships inside the Antigravity IDE:
//
//	/Applications/Antigravity.app/Contents/Resources/bin/language_server
//
// The spawn follows the IDE's own invocation (out/ide-language-server/
// README.md §5, recovered from the IDE's own JS) as closely as it can:
// the hub subclient mode, the IDE's app data directory, the IDE's
// endpoints, the IDE's version.
//
// The reference runs against the operator's **real** state: its own
// default `-gemini_dir` is `.gemini` relative to the home directory,
// and the spawn does not override it, so the reference reads the real
// account, the real model catalogue, the real projects and the real
// conversation index. `-use_mocked_data` stays **off** — with it on the
// reference answers with a mock user, an empty catalogue and no
// conversations at all.
//
// The operator can still change any of it: set
// OPENAGY_REFERENCE_BACKEND_ARGS to add or override arguments (the
// reference takes the last occurrence of a flag, so the caller's value
// wins).
//
// Evidence grade: 一手 (the recipe is the one the reference answered on
// during the rc-model-enum-wire cycle, plus the measurements in
// out/refbackend-spawn/).
package refbackend

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"github.com/justwasm/pigasus/endpoints"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// spawnTimeout bounds how long the reference gets to open its port.
const spawnTimeout = 30 * time.Second

// Process is a running reference binary.
type Process struct {
	cmd  *exec.Cmd
	Port int

	// proxy is the CloudCode proxy the reference's model traffic goes
	// through, when the operator has models of their own. It dies with
	// the reference.
	proxy *customModelProxy
}

// Base returns the proxy base URL of the running reference.
func (p *Process) Base() string {
	return "http://127.0.0.1:" + strconv.Itoa(p.Port)
}

// Stop terminates the reference.
//
// Nothing is removed: the reference runs against the operator's own
// gemini directory, and the state it wrote there belongs to them.
func (p *Process) Stop() {
	if p == nil {
		return
	}
	if p.cmd != nil && p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
		_, _ = p.cmd.Process.Wait()
	}
	// The proxy goes last: the reference's last request may still be
	// in flight towards it.
	p.proxy.close()
}

// IsBinaryPath reports whether the reference-backend value names a
// binary rather than an endpoint. A path has no scheme and no port.
func IsBinaryPath(value string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return false
	}
	if strings.Contains(trimmed, "://") || strings.Contains(trimmed, ":") {
		return false
	}
	info, err := os.Stat(trimmed)
	return err == nil && !info.IsDir()
}

// Spawn starts the reference binary and waits for its HTTP port to
// accept connections. The returned Process owns the child process; the
// caller stops it.
func Spawn(binary string) (*Process, error) {
	bin, err := exec.LookPath(binary)
	if err != nil {
		bin = binary
	}
	port, err := freePort()
	if err != nil {
		return nil, err
	}
	secret, err := newSecret()
	if err != nil {
		return nil, err
	}
	extra := extraArgs()
	geminiDir, cleaned, hasDir := geminiDirArg(extra)

	// The reference's CloudCode traffic goes through the clone's own
	// proxy when the operator has models of their own: the proxy
	// advertises them in the catalogue the reference reads, and it
	// translates a turn that picks one.
	proxy, err := startReferenceProxy(cleaned)
	if err != nil {
		return nil, err
	}

	args := referenceArgs(bin, port, secret, proxy.endpoint())
	args = append(args, cleaned...)
	if hasDir {
		// The gemini directory goes last: the reference takes the last
		// occurrence of a flag, so this is the one that wins.
		args = append(args, "-gemini_dir="+geminiDir)
	}
	cmd := exec.Command(bin, args...)
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		proxy.close()
		return nil, fmt.Errorf("refbackend: start %s: %w", bin, err)
	}
	proc := &Process{cmd: cmd, Port: port, proxy: proxy}
	if err := waitForPort(port, spawnTimeout); err != nil {
		proc.Stop()
		return nil, err
	}
	return proc, nil
}

// referenceArgs are the arguments the reference is started with.
//
// Three of them decide whether the reference's *own* web app opens at
// all; the rest are the plain standalone recipe.
//
// 【一手】 2026-10-26, the same binary
// (/Applications/Antigravity.app/Contents/Resources/bin/language_server),
// one flag at a time.
func referenceArgs(bin string, port int, secret string, cloudCodeEndpoint string) []string {
	args := []string{
		"-http_server_port=" + strconv.Itoa(port),
		"-standalone=true",
		"-persistent_mode=true",
		// The reference serves its own web app — the SPA, the bundle,
		// the `window.__APP_CONFIG__` — only in the "hub" subclient
		// mode. A plain standalone instance answers `GET /` with 404,
		// and the page the browser asks for never opens.
		//
		//	-http_server_port=8931 -standalone=true -persistent_mode=true
		//	    GET /            → 404 text/plain
		//	    GET /main.js     → 404
		//
		//	-http_server_port=8932 … the same … -subclient_type=hub
		//	    GET /            → 200 text/html   (the reference's page)
		//	    GET /main.js     → 200 text/javascript (the bundle)
		//
		// The RPC surface is the same either way: the Connect-RPC
		// service answers on the HTTP port in both modes.
		"-subclient_type=hub",
		// The app data directory decides **which product** the page
		// is — and with it, which state the reference serves.
		//
		//	-app_data_dir=antigravity-cli
		//	    window.__APP_CONFIG__ = {"productName":"antigravity-cli",…}
		//	    the app is the CLI's own web app, and the sidebar lists
		//	    the CLI's real sessions: "Image Recognition And Analysis",
		//	    "AI Model Identity Inquiry", "Print Working Directory" …
		//	    ("[SidebarLoad] Conversation list loaded (25 conversations)")
		//
		//	-app_data_dir=antigravity        (the IDE's directory)
		//	    window.__APP_CONFIG__ = {"productName":"antigravity",…}
		//	    the IDE's own UI, but the IDE's directory holds the
		//	    *untitled* CLI rows: the sidebar answers
		//	    "[SidebarLoad] Conversation list loaded (0 conversations)"
		//
		//	(no -app_data_dir)
		//	    window.__APP_CONFIG__ = {"productName":"antigravity-ide",…}
		//	    the sign-in page
		//
		// The directory is the reference's own, relative to its gemini
		// directory: `~/.gemini/antigravity-cli/` is where the
		// reference's own CLI writes its state (out/blackbox/
		// changelog.txt), and it holds the titled sessions
		// (14,446 rows, 345 with a title — measured against the
		// reference's own `conversation_summaries.db`).
		"-app_data_dir=antigravity-cli",
		// The reference's own secret. Without one the web app does not
		// boot: it opens the page and then rejects the whole app with
		// an unhandled promise, and the page stays blank.
		//
		//	-csrf_token=ref-csrf-token
		//	    the page boots: "New Conversation", the model picker, the
		//	    composer
		//	(no -csrf_token)
		//	    the page stays empty and the console says
		//	    "Uncaught (in promise) (…/main.js)"
		//
		// The proxy learns the secret from the reference's own page
		// (csrf.go), so it does not have to be told about it.
		"-csrf_token=" + secret,
		// The IDE's own endpoints. Without them the reference talks to
		// its *default* API host — `http://0.0.0.0:50001`, nothing —
		// and the app opens its sign-in page instead of the IDE's UI:
		// there is no account to sign in with.
		//
		//	-api_server_url=https://generativelanguage.googleapis.com
		//	-cloud_code_endpoint=https://daily-cloudcode-pa.googleapis.com
		//
		// 【一手】 the same binary, the same account, the two sets:
		//
		//	with the IDE's endpoints
		//	    GetUserStatus → {"name":"…","email":"…@gmail.com",
		//	                    "planStatus":{"planName":"Pro",…}}
		//	    GetAuthStatus → {"authResult":{"hasValidAuth":true,…}}
		//	without them
		//	    the page opens "Welcome to Antigravity / Continue with
		//	    Google" — the sign-in page
		//
		// The clone's own CloudCode channel uses the same endpoint
		// (internal/backend/cloudcode.go), so both sides of a
		// comparison talk to the same cloud.
		//
		// The CloudCode endpoint is the clone's own proxy when the
		// operator has models of their own: the proxy advertises them
		// in the catalogue the reference reads, and translates a turn
		// that picks one. See custommodels.go.
		"-api_server_url=" + endpoints.Current().GenerativeLanguage,
		"-cloud_code_endpoint=" + cloudCodeEndpoint,
		// The IDE's own name for itself, in the metadata it sends and
		// in its User-Agent.
		"-override_ide_name=antigravity",
		"-override_user_agent_name=antigravity",
	}
	if version := ideVersion(bin); version != "" {
		// The IDE's own version. The page reports it as `appVersion`,
		// and the bundle asks for assets with it (`audio_processor.js?v=`).
		args = append(args, "-override_ide_version="+version)
	}
	return args
}

// ideVersion reads the version out of the app bundle the reference
// binary lives in:
//
//	/Applications/Antigravity.app/Contents/Resources/bin/language_server
//	  → /Applications/Antigravity.app/Contents/Info.plist
//	    → CFBundleShortVersionString
//
// It returns "" when the binary is not inside a bundle.
func ideVersion(bin string) string {
	// …/Contents/Resources/bin/language_server → …/Contents
	contents := filepath.Dir(filepath.Dir(filepath.Dir(bin)))
	data, err := os.ReadFile(filepath.Join(contents, "Info.plist"))
	if err != nil {
		return ""
	}
	const key = "<key>CFBundleShortVersionString</key>"
	plist := string(data)
	i := strings.Index(plist, key)
	if i < 0 {
		return ""
	}
	rest := plist[i+len(key):]
	j := strings.Index(rest, "<string>")
	if j < 0 {
		return ""
	}
	rest = rest[j+len("<string>"):]
	k := strings.Index(rest, "</string>")
	if k < 0 {
		return ""
	}
	return strings.TrimSpace(rest[:k])
}

// extraArgs reads the caller's extra reference arguments.
func extraArgs() []string {
	raw := strings.TrimSpace(os.Getenv("OPENAGY_REFERENCE_BACKEND_ARGS"))
	if raw == "" {
		return nil
	}
	return strings.Fields(raw)
}

// geminiDirArg returns the `-gemini_dir` the caller's arguments carry,
// and the arguments without it.
//
// The reference takes the last occurrence of a flag, so the caller's
// value is the one that wins — and the temporary directory the spawn
// would have made is then not needed (and must not be removed on stop,
// because it is the caller's).
func geminiDirArg(args []string) (string, []string, bool) {
	value := ""
	found := false
	cleaned := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case strings.HasPrefix(arg, "-gemini_dir="):
			value = strings.TrimPrefix(arg, "-gemini_dir=")
			found = true
		case arg == "-gemini_dir" && i+1 < len(args):
			value = args[i+1]
			found = true
			i++
		default:
			cleaned = append(cleaned, arg)
		}
	}
	return value, cleaned, found
}

// freePort asks the kernel for an unused TCP port.
func freePort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port, nil
}

// newSecret mints the secret the reference's own page carries.
//
// The reference is started with one, so its RPCs demand it, and the
// page hands it to the browser — and to the proxy, which learns it
// from the same document (csrf.go). The secret never leaves the
// machine: it is the child's own, minted for this run.
func newSecret() (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("refbackend: mint the reference's secret: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// waitForPort blocks until the port accepts a connection.
func waitForPort(port int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return errors.New("refbackend: the reference did not open its port in time")
}

// ReferencePath returns the reference binary inside the installed
// Antigravity IDE, or "" when it is not there.
func ReferencePath() string {
	candidates := []string{
		"/Applications/Antigravity.app/Contents/Resources/bin/language_server",
		filepath.Join("/usr", "local", "bin", "language_server"),
	}
	for _, path := range candidates {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path
		}
	}
	return ""
}
