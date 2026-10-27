// Package rchost — web.go: implementation of the standalone `pigasus web`
// subcommand for local browser and local LAN access without cloud tunnels.
package rchost

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/justwasm/pigasus/internal/accounts"
	"github.com/justwasm/pigasus/internal/cli"
	"github.com/justwasm/pigasus/internal/rcfrontend"
	"github.com/justwasm/pigasus/internal/refbackend"
	"github.com/justwasm/pigasus/internal/remotecontrol"
	"github.com/justwasm/pigasus/originpolicy"
	"github.com/justwasm/pigasus/remotetransport"
)

const defaultWebAddr = "0.0.0.0:8787"

// RunWeb is the user-facing `pigasus web` subcommand runner.
func RunWeb(j *cli.JetskiCLI, args cli.WebArgs) error {
	addr := resolveWebAddr(args)

	cwd := args.Workspace
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	cwd, _ = filepath.Abs(cwd)

	sess := remotecontrol.NewSessionState(cli.ReferenceVersion)
	sess.Workspace = cwd

	// The web command registers no cloud instance, so the remote
	// control info answers with the local placeholder id.
	disp, refProc, refBase, err := buildWebDispatcher(cwd, addr, sess, args.ReferenceBackend, args.Desktop)
	if err != nil {
		return err
	}
	if refProc != nil {
		defer refProc.Stop()
	}

	_, activeAccount := accounts.Current()
	// Cycle rc-auth-bearer-required: the secret the LAN clients have
	// to present. `pigasus web` is the command that is meant to be
	// shared with the people on the network, so it is the one that
	// must not serve them without a credential.
	token, warn, err := resolveRCToken()
	if err != nil {
		return cli.ExitCodeError(1, fmt.Sprintf("Error: could not mint the remote-control secret: %v", err))
	}
	if warn != "" {
		fmt.Fprintf(j.ErrOut, "pigasus web: %s\n", warn)
	}
	webUI := rcfrontend.Handler(rcfrontend.Options{
		EnableTouchPatches: true,
		WorkspaceRoot:      cwd,
		AccountEmail:       activeAccount,
		CSRFToken:          token,
	})

	// With a reference backend the page comes from the reference too:
	// its own index document, its own bundle. The clone's own endpoints
	// stay — they belong to the process the browser talks to.
	if refBase != "" {
		assets, err := refbackend.NewAssets(refBase)
		if err != nil {
			return cli.ExitCodeError(1, fmt.Sprintf("Error: reference backend: %v", err))
		}
		webUI = newReferenceAssetsHandler(webUI, assets)
		// The reference's page carries the reference's own secret, and
		// the browser presents it with every call. The transport has
		// to accept the same one, or the browser's own requests are
		// the ones that get denied.
		if refToken := assets.CSRFToken(); refToken != "" {
			token = refToken
		}
	}

	// Use empty pattern OriginPolicy ("allow all") to enable local LAN sharing without CORS blocks.
	seed := originpolicy.OriginPolicy{}
	srv, err := remotetransport.NewTCPServer(context.Background(), disp, remotetransport.TCPConfig{
		Addr:         addr,
		Path:         "/rpc",
		OriginPolicy: &seed,
		WebUIHandler: webUI,
		CSRFToken:    token,
	})
	if err != nil {
		fmt.Fprintf(j.ErrOut, "pigasus web: failed to bind %s: %v\n", addr, err)
		if strings.Contains(err.Error(), "address already in use") {
			fmt.Fprintln(j.ErrOut, "Tip: if port 8787 is used by 'pigasus remote-control', stop it with 'pigasus remote-control stop',")
			fmt.Fprintln(j.ErrOut, "     or specify another port with 'pigasus web --port <port>'.")
		}
		return cli.ExitCodeError(1, fmt.Sprintf("Error: failed to bind %s: %v", addr, err))
	}
	if setter, ok := disp.(originPolicySetter); ok {
		setter.SetOriginPolicySetter(srv.SetOriginPolicy)
	}

	// Extract listening port
	_, portStr, splitErr := net.SplitHostPort(srv.Addr().String())
	if splitErr != nil {
		portStr = "8787"
	}

	localURL := fmt.Sprintf("http://127.0.0.1:%s", portStr)
	lanIPs := findLocalLANIPs()

	fmt.Fprintf(j.Out, "pigasus web: server listening on %s\n", srv.Addr().String())
	fmt.Fprintf(j.Out, "  - Workspace:   %s\n", cwd)
	fmt.Fprintf(j.Out, "  - Local URL:   %s\n", localURL)
	if ref := strings.TrimSpace(args.ReferenceBackend); ref != "" {
		fmt.Fprintf(j.Out, "  - Backend:     reference language_server (%s)\n", ref)
		fmt.Fprintf(j.Out, "  - Frontend:    the reference's own assets\n")
	}
	if len(lanIPs) > 0 {
		for _, ip := range lanIPs {
			fmt.Fprintf(j.Out, "  - Network URL: http://%s:%s/?token=%s (share with local LAN users)\n", ip, portStr, token)
		}
	}
	fmt.Fprintf(j.Out, "  - Secret:      %s (a LAN client has to present it)\n", token)
	fmt.Fprintln(j.Out, "Press Ctrl+C to stop.")

	if args.Open {
		_ = openBrowser(localURL)
	}

	return runWebEventLoop(srv, j.ErrOut)
}

func resolveWebAddr(args cli.WebArgs) string {
	host := strings.TrimSpace(args.Host)
	if args.Port > 0 {
		if host == "" {
			host = "0.0.0.0"
		}
		return net.JoinHostPort(host, strconv.Itoa(args.Port))
	}
	rawAddr := strings.TrimSpace(args.Addr)
	if rawAddr == "" {
		if host != "" {
			return net.JoinHostPort(host, "8787")
		}
		return defaultWebAddr
	}
	if strings.HasPrefix(rawAddr, ":") {
		return "0.0.0.0" + rawAddr
	}
	if !strings.Contains(rawAddr, ":") {
		return net.JoinHostPort(rawAddr, "8787")
	}
	return rawAddr
}

func findLocalLANIPs() []string {
	var ips []string
	ifaces, err := net.Interfaces()
	if err != nil {
		return ips
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.IsLoopback() {
				continue
			}
			if ipv4 := ip.To4(); ipv4 != nil {
				ips = append(ips, ipv4.String())
			}
		}
	}
	return ips
}

func openBrowser(urlStr string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", urlStr).Start()
	case "linux":
		return exec.Command("xdg-open", urlStr).Start()
	case "windows":
		return exec.Command("cmd", "/c", "start", urlStr).Start()
	default:
		return nil
	}
}

var webSignalChProvider = func(c chan<- os.Signal) {
	signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)
}

func runWebEventLoop(srv *remotetransport.TCPServer, errOut io.Writer) error {
	sigCh := make(chan os.Signal, 1)
	webSignalChProvider(sigCh)
	defer signal.Stop(sigCh)

	sig := <-sigCh
	fmt.Fprintf(errOut, "pigasus web: caught signal %v, shutting down\n", sig)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		fmt.Fprintf(errOut, "pigasus web: shutdown error: %v\n", err)
		return cli.ExitCodeError(1, fmt.Sprintf("Error: shutdown failed: %v", err))
	}
	return nil
}
