// Package dns configures Go's DNS resolver for Termux/Android where
// Go's pure-Go resolver reads /etc/resolv.conf which points to
// non-functional loopback nameservers.
//
// The package uses runtime detection — no build tags required.
//
// Why overriding net.DefaultResolver is enough
// --------------------------------------------
// net.DefaultResolver is read *at dial time*, not captured at init:
//
//   - net.Dialer.resolver() returns DefaultResolver whenever the Dialer's
//     own Resolver field is nil (net/dial.go);
//   - net/http's default transport dials through a zero net.Dialer
//     (zeroDialer), so it never pins a resolver of its own;
//   - golang.org/x/oauth2 — the library the OAuth device flow runs on —
//     falls back to http.DefaultClient (internal.ContextClient), and so to
//     http.DefaultTransport.
//
// So replacing the package-level variable reroutes *every* outbound
// hostname lookup in the process — the login flow included — without
// touching a single call site. The proof lives in android_test.go.
//
// What it does NOT cover: the vendor `agy` binary that `pigasus agy`
// execs. That is a separate process with its own resolver; it cannot be
// patched from here. Its CloudCode traffic goes to 127.0.0.1 (the launcher
// exports CLOUD_CODE_URL), so only its own OAuth round-trip would need
// DNS — see docs/guide/accounts.md.
package dns

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// defaultNameservers are tried in order by dialWithFallback.
var defaultNameservers = []string{"223.5.5.5:53", "1.1.1.1:53", "8.8.8.8:53"}

// EnvNameservers overrides the nameserver list on Android/Termux.
//
// Comma-separated host:port pairs, e.g. "127.0.0.1:53,8.8.8.8:53". A bare
// address is read as a DNS port.
//
// It exists because the default list is a *public* resolver, and a public
// resolver bypasses a split-DNS setup: a VPN that hands out its own
// resolver, or Termux's own `dnsproxy` listening on 127.0.0.1:53, is never
// consulted. Point this at that resolver instead.
//
// The variable is ignored off Android/Termux — the fallback is not active
// there, so there is nothing to configure.
const EnvNameservers = "OPENAGY_DNS_SERVERS"

// android reports whether this process runs on Android/Termux.
//
// Two independent signals, either one is enough:
//
//   - runtime.GOOS == "android" is baked into the binary at build time
//     (the release workflow ships a GOOS=android/arm64 archive), so it
//     survives an environment that has been scrubbed;
//   - TERMUX_VERSION is exported by the Termux shell at run time, so it
//     also covers a linux/arm64 build running inside a proot distro.
//
// The first signal alone would be enough for the shipped Android binary;
// the second is what makes a desktop build work inside Termux.
func android() bool {
	return runtime.GOOS == "android" || os.Getenv("TERMUX_VERSION") != ""
}

func init() {
	if !android() {
		return
	}
	install(nameservers())
}

// nameservers returns the list the fallback dials, honouring
// $OPENAGY_DNS_SERVERS.
func nameservers() []string {
	raw := strings.TrimSpace(os.Getenv(EnvNameservers))
	if raw != "" {
		var out []string
		for _, part := range strings.Split(raw, ",") {
			p := strings.TrimSpace(part)
			if p == "" {
				continue
			}
			if _, _, err := net.SplitHostPort(p); err != nil {
				// A bare address is a DNS server on the DNS port.
				p = net.JoinHostPort(p, "53")
			}
			out = append(out, p)
		}
		if len(out) > 0 {
			return out
		}
	}

	if !android() {
		return defaultNameservers
	}

	var candidates []string
	// 1. Android system properties (e.g. Wi-Fi / cellular DNS assigned by Android OS)
	candidates = append(candidates, androidSystemDNS()...)
	// 2. Non-loopback nameservers from resolv.conf (e.g. in container / proot)
	candidates = append(candidates, resolvConfNameservers()...)
	// 3. Fallback public nameservers
	candidates = append(candidates, defaultNameservers...)

	seen := make(map[string]bool)
	var out []string
	for _, c := range candidates {
		if !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	if len(out) == 0 {
		return defaultNameservers
	}
	return out
}

// androidSystemDNS attempts to read Android system DNS properties (net.dns1, net.dns2)
// via the getprop command when running on Android.
func androidSystemDNS() []string {
	if runtime.GOOS != "android" && os.Getenv("TERMUX_VERSION") == "" {
		return nil
	}
	var out []string
	for _, prop := range []string{"net.dns1", "net.dns2"} {
		cmd := exec.Command("getprop", prop)
		if b, err := cmd.Output(); err == nil {
			val := strings.TrimSpace(string(b))
			if val != "" {
				ip := net.ParseIP(val)
				if ip != nil && !ip.IsLoopback() && !ip.IsUnspecified() {
					out = append(out, net.JoinHostPort(val, "53"))
				}
			}
		}
	}
	return out
}

// resolvConfNameservers extracts any non-loopback nameservers found in resolv.conf.
func resolvConfNameservers() []string {
	paths := []string{"/etc/resolv.conf"}
	if termuxPrefix := os.Getenv("PREFIX"); termuxPrefix != "" {
		paths = append([]string{filepath.Join(termuxPrefix, "etc", "resolv.conf")}, paths...)
	}
	return resolvConfNameserversFromPaths(paths...)
}

func resolvConfNameserversFromPaths(paths ...string) []string {
	var out []string
	for _, path := range paths {
		b, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(b), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "nameserver") {
				fields := strings.Fields(line)
				if len(fields) >= 2 {
					ns := fields[1]
					ip := net.ParseIP(ns)
					if ip != nil && !ip.IsLoopback() && !ip.IsUnspecified() {
						out = append(out, net.JoinHostPort(ns, "53"))
					}
				}
			}
		}
	}
	return out
}

// install replaces net.DefaultResolver with a pure-Go resolver whose
// queries go to the given nameservers instead of the ones
// /etc/resolv.conf names.
//
// PreferGo forces the pure-Go resolver even on a cgo-enabled build: the
// cgo resolver would go back to the system resolver (getaddrinfo), which
// is exactly the thing that does not work on Termux.
func install(nameservers []string) {
	net.DefaultResolver = &net.Resolver{
		PreferGo: true,
		Dial:     dialWithFallback(nameservers),
	}
}

// dialWithFallback returns a resolver Dial func that tries each
// nameserver in order, falling through on failure.
//
// The server address the resolver asks for is deliberately ignored: the
// whole point is to bypass the nameservers /etc/resolv.conf names, which
// on Termux point at a loopback address that nothing answers on.
//
// The fallback is a *dial* fallback, not a query fallback: if the first
// nameserver accepts the connection but never answers, the resolver does
// not move on to the second one.
func dialWithFallback(nameservers []string) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, _ string) (net.Conn, error) {
		var lastErr error
		d := net.Dialer{
			Resolver: nil,
		}
		for _, ns := range nameservers {
			conn, err := d.DialContext(ctx, network, ns)
			if err == nil {
				return conn, nil
			}
			lastErr = err
		}
		return nil, lastErr
	}
}
