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
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// defaultNameservers are tried in parallel (racing UDP) by dialWithFallback.
// Google and Cloudflare are placed first as the global standard for Android/Termux,
// followed by Quad9, AliDNS, and DNSPod.
var defaultNameservers = []string{
	"8.8.8.8:53",
	"1.1.1.1:53",
	"8.8.4.4:53",
	"9.9.9.9:53",
	"223.5.5.5:53",
	"119.29.29.29:53",
}

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
// Detects both compile-time Android binaries (runtime.GOOS == "android")
// and Linux desktop binaries running inside Termux, proot, or chroot environments.
func android() bool {
	if runtime.GOOS == "android" {
		return true
	}
	if os.Getenv("TERMUX_VERSION") != "" || os.Getenv("TERMUX_APP_PID") != "" {
		return true
	}
	if prefix := os.Getenv("PREFIX"); prefix != "" && strings.Contains(prefix, "com.termux") {
		return true
	}
	if os.Getenv("ANDROID_ROOT") != "" || os.Getenv("ANDROID_DATA") != "" {
		return true
	}
	if _, err := os.Stat("/data/data/com.termux"); err == nil {
		return true
	}
	if _, err := os.Stat("/system/build.prop"); err == nil {
		return true
	}
	return false
}

// Android reports whether the runtime environment is Android/Termux.
func Android() bool {
	return android()
}

// EnsureInstalled explicitly verifies and installs the fallback resolver on Android.
func EnsureInstalled() {
	if !android() {
		return
	}
	install(nameservers())
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
	// 1. Android local network gateway (e.g. Wi-Fi router DNS cache)
	candidates = append(candidates, gatewayDNS()...)
	// 2. Android system properties (via getprop)
	candidates = append(candidates, androidSystemDNS()...)
	// 3. Non-loopback nameservers from resolv.conf
	candidates = append(candidates, resolvConfNameservers()...)
	// 4. Fallback public nameservers
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

// gatewayDNS reads /proc/net/route to find the default route's gateway IP.
// On Wi-Fi and mobile networks, the default gateway often serves as the local DNS caching proxy.
func gatewayDNS() []string {
	b, err := os.ReadFile("/proc/net/route")
	if err != nil {
		return nil
	}
	for _, line := range strings.Split(string(b), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 3 && fields[1] == "00000000" { // Destination 0.0.0.0 (default route)
			var gw uint32
			if n, err := fmt.Sscanf(fields[2], "%X", &gw); err == nil && n == 1 && gw != 0 {
				ip := net.IPv4(byte(gw), byte(gw>>8), byte(gw>>16), byte(gw>>24))
				if !ip.IsLoopback() && !ip.IsUnspecified() {
					return []string{net.JoinHostPort(ip.String(), "53")}
				}
			}
		}
	}
	return nil
}

// androidSystemDNS attempts to read Android system DNS properties via getprop.
func androidSystemDNS() []string {
	if !android() {
		return nil
	}
	var out []string
	props := []string{
		"net.dns1", "net.dns2", "net.dns3", "net.dns4",
		"dhcp.wlan0.dns1", "dhcp.wlan0.dns2", "dhcp.wlan0.dns3", "dhcp.wlan0.dns4",
		"net.pdp0.dns1", "net.pdp0.dns2",
	}

	getpropCmd := "getprop"
	if _, err := exec.LookPath("getprop"); err != nil {
		if _, err := os.Stat("/system/bin/getprop"); err == nil {
			getpropCmd = "/system/bin/getprop"
		}
	}

	for _, prop := range props {
		cmd := exec.Command(getpropCmd, prop)
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

// resolvConfNameservers extracts any non-loopback nameservers found in resolv.conf paths.
func resolvConfNameservers() []string {
	paths := []string{
		"/etc/resolv.conf",
		"/data/data/com.termux/files/usr/etc/resolv.conf",
	}
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
// queries go to the given nameservers instead of the ones /etc/resolv.conf names.
func install(nameservers []string) {
	net.DefaultResolver = &net.Resolver{
		PreferGo: true,
		Dial:     dialWithFallback(nameservers),
	}
}

// dialWithFallback returns a resolver Dial func.
// For TCP, it tries candidates in sequence until one connects.
// For UDP with multiple servers, it returns a racingConn that queries the
// candidates simultaneously and returns the fastest valid DNS response.
func dialWithFallback(nameservers []string) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, _ string) (net.Conn, error) {
		if len(nameservers) == 0 {
			d := net.Dialer{}
			return d.DialContext(ctx, network, "8.8.8.8:53")
		}

		// TCP dial tries candidates in sequence until one connects
		if strings.HasPrefix(network, "tcp") {
			var lastErr error
			d := net.Dialer{Resolver: nil, Timeout: 2 * time.Second}
			for _, ns := range nameservers {
				conn, err := d.DialContext(ctx, network, ns)
				if err == nil {
					return conn, nil
				}
				lastErr = err
			}
			return nil, lastErr
		}

		// Single nameserver UDP (e.g. test probes or user explicit override)
		if len(nameservers) == 1 {
			d := net.Dialer{Resolver: nil}
			return d.DialContext(ctx, network, nameservers[0])
		}

		// Multi-nameserver UDP racing: dial connected UDP sockets to all candidates
		d := net.Dialer{Resolver: nil}
		conns := make([]net.Conn, 0, len(nameservers))
		var lastErr error
		for _, ns := range nameservers {
			c, err := d.DialContext(ctx, "udp", ns)
			if err == nil {
				conns = append(conns, c)
			} else {
				lastErr = err
			}
		}
		if len(conns) == 0 {
			return nil, lastErr
		}
		if len(conns) == 1 {
			return conns[0], nil
		}
		return newRacingConn(conns), nil
	}
}

// racingConn coordinates a single DNS query across multiple candidate nameservers.
// When Write is called, it sends the query to all candidate sockets simultaneously.
// When Read is called, it returns the first valid response (>= 12 bytes RFC 1035).
type racingConn struct {
	conns   []net.Conn
	primary net.Conn
	mu      sync.Mutex
}

func newRacingConn(conns []net.Conn) *racingConn {
	return &racingConn{conns: conns, primary: conns[0]}
}

func (r *racingConn) Write(b []byte) (int, error) {
	var lastErr error
	written := false
	for _, c := range r.conns {
		n, err := c.Write(b)
		if err == nil && n == len(b) {
			written = true
		} else if err != nil {
			lastErr = err
		}
	}
	if written {
		return len(b), nil
	}
	return 0, lastErr
}

func (r *racingConn) Read(b []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	type result struct {
		n    int
		conn net.Conn
		buf  []byte
		err  error
	}

	resCh := make(chan result, len(r.conns))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	for _, c := range r.conns {
		go func(conn net.Conn) {
			buf := make([]byte, len(b))
			n, err := conn.Read(buf)
			select {
			case resCh <- result{n: n, conn: conn, buf: buf, err: err}:
			case <-ctx.Done():
			}
		}(c)
	}

	var lastErr error
	received := 0
	for received < len(r.conns) {
		select {
		case res := <-resCh:
			received++
			if res.err == nil && res.n >= 12 {
				copy(b, res.buf[:res.n])
				r.primary = res.conn
				return res.n, nil
			}
			if res.err != nil {
				lastErr = res.err
			}
		}
	}
	if lastErr != nil {
		return 0, lastErr
	}
	return 0, io.ErrUnexpectedEOF
}

func (r *racingConn) Close() error {
	var firstErr error
	for _, c := range r.conns {
		if err := c.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (r *racingConn) LocalAddr() net.Addr {
	r.mu.Lock()
	p := r.primary
	r.mu.Unlock()
	if p != nil {
		return p.LocalAddr()
	}
	return r.conns[0].LocalAddr()
}

func (r *racingConn) RemoteAddr() net.Addr {
	r.mu.Lock()
	p := r.primary
	r.mu.Unlock()
	if p != nil {
		return p.RemoteAddr()
	}
	return r.conns[0].RemoteAddr()
}

func (r *racingConn) SetDeadline(t time.Time) error {
	for _, c := range r.conns {
		_ = c.SetDeadline(t)
	}
	return nil
}

func (r *racingConn) SetReadDeadline(t time.Time) error {
	for _, c := range r.conns {
		_ = c.SetReadDeadline(t)
	}
	return nil
}

func (r *racingConn) SetWriteDeadline(t time.Time) error {
	for _, c := range r.conns {
		_ = c.SetWriteDeadline(t)
	}
	return nil
}
