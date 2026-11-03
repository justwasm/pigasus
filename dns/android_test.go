// android_test.go — the Termux/Android DNS fallback.
//
// The package exists for one reason: on Termux /etc/resolv.conf points at
// a loopback nameserver that nothing answers on, so every outbound
// hostname lookup fails — including the OAuth device flow the login runs
// on. The fix is to replace net.DefaultResolver.
//
// These tests pin the two properties that fix depends on:
//
//  1. net.DefaultResolver is consulted *at dial time* by the plain
//     http.Client path the login uses (TestInstall_RoutesHTTPThroughTheCustomResolver).
//  2. The fallback really does fall through to the next nameserver
//     (TestDialWithFallback_FallsThroughToTheNextNameserver).
//
// and the detection rule (TestAndroid_DetectsEitherSignal).
package dns

import (
	"context"
	"net"
	"net/http"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestInstall_RoutesHTTPThroughTheCustomResolver is the load-bearing
// property of the whole package.
//
// The login flow never mentions DNS: auth.fetchOAuthToken and
// auth.populateUserEmailFromUserInfo use http.DefaultClient, and
// golang.org/x/oauth2's DeviceAuth / DeviceAccessToken fall back to
// http.DefaultClient too (internal.ContextClient). If http.DefaultClient
// did not pick up net.DefaultResolver, the hack would be decorative.
//
// The test installs a resolver that points at a closed loopback port, so
// the lookup fails immediately with "connection refused" and the error
// names the address we configured. Nothing leaves the machine.
func TestInstall_RoutesHTTPThroughTheCustomResolver(t *testing.T) {
	restore := net.DefaultResolver
	t.Cleanup(func() { net.DefaultResolver = restore })

	// Port 1 on loopback: nothing listens there, so the dial fails at
	// once instead of hanging on a routable address.
	const probe = "127.0.0.1:1"
	install([]string{probe})

	client := &http.Client{Timeout: 20 * time.Second}
	_, err := client.Get("https://oauth2.googleapis.com/token")
	if err == nil {
		t.Fatal("the request succeeded; net.DefaultResolver was not consulted")
	}
	if !strings.Contains(err.Error(), probe) {
		t.Fatalf("the error does not name the configured nameserver %q: %v", probe, err)
	}
}

// TestDialWithFallback_FallsThroughToTheNextNameserver pins the
// fall-through: the second nameserver is tried when the first one cannot
// be dialled, and the error that comes back is the last one.
//
// The network is "tcp" on purpose. A UDP dial to a closed port succeeds —
// UDP is connectionless, the port only shows up as an ICMP error on the
// first read — so the dial-level fall-through is only observable over
// TCP. (Go's resolver dials "udp" first and falls back to "tcp" on
// truncation; that is a separate mechanism, inside net.)
func TestDialWithFallback_FallsThroughToTheNextNameserver(t *testing.T) {
	dial := dialWithFallback([]string{"127.0.0.1:1", "127.0.0.1:2"})

	conn, err := dial(context.Background(), "tcp", "ignored-by-design")
	if err == nil {
		conn.Close()
		t.Fatal("expected both nameservers to be unreachable")
	}
	if !strings.Contains(err.Error(), "127.0.0.1:2") {
		t.Fatalf("the error does not name the last nameserver tried: %v", err)
	}
	if strings.Contains(err.Error(), "127.0.0.1:1") {
		t.Fatalf("the error still names the first nameserver: %v", err)
	}
}

// TestDialWithFallback_IgnoresTheRequestedServer is the reason the
// package exists: the resolver asks for the nameserver /etc/resolv.conf
// names (a dead loopback address on Termux), and the Dial has to ignore
// it and use the configured list instead.
func TestDialWithFallback_IgnoresTheRequestedServer(t *testing.T) {
	dial := dialWithFallback([]string{"127.0.0.1:2"})

	_, err := dial(context.Background(), "tcp", "127.0.0.1:53")
	if err == nil {
		t.Fatal("expected the configured nameserver to be unreachable")
	}
	if !strings.Contains(err.Error(), "127.0.0.1:2") {
		t.Fatalf("the requested server was not ignored: %v", err)
	}
}

// TestAndroid_DetectsEitherSignal pins the detection rule: the
// build-time signal (runtime.GOOS) or the run-time one (TERMUX_VERSION)
// is enough on its own.
func TestAndroid_DetectsEitherSignal(t *testing.T) {
	t.Setenv("TERMUX_VERSION", "")
	if got, want := android(), runtime.GOOS == "android"; got != want {
		t.Fatalf("with no TERMUX_VERSION, android() = %v, want %v (GOOS=%s)", got, want, runtime.GOOS)
	}

	t.Setenv("TERMUX_VERSION", "0.118.0")
	if !android() {
		t.Fatal("TERMUX_VERSION alone must enable the fallback")
	}
}

// TestNameservers_HonoursTheOverride pins the $OPENAGY_DNS_SERVERS
// parsing: a comma-separated list, a bare address gets the DNS port, and
// an empty or all-blank value falls back to the default list.
func TestNameservers_HonoursTheOverride(t *testing.T) {
	t.Setenv(EnvNameservers, "")
	if got := nameservers(); len(got) != len(defaultNameservers) || got[0] != defaultNameservers[0] {
		t.Fatalf("nameservers() = %v, want the default list %v", got, defaultNameservers)
	}

	t.Setenv(EnvNameservers, " 127.0.0.1:5353 , 9.9.9.9 ")
	got := nameservers()
	want := []string{"127.0.0.1:5353", "9.9.9.9:53"}
	if len(got) != len(want) {
		t.Fatalf("nameservers() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("nameservers() = %v, want %v", got, want)
		}
	}

	t.Setenv(EnvNameservers, " , ")
	if got := nameservers(); len(got) != len(defaultNameservers) {
		t.Fatalf("a blank override must fall back to the default list, got %v", got)
	}
}

// TestResolvConfNameservers_FiltersLoopbackAndKeepsNonLoopback tests that
// loopback nameservers (127.0.0.1, ::1) are excluded while valid non-loopback
// nameservers are extracted and formatted with port 53.
func TestResolvConfNameservers_FiltersLoopbackAndKeepsNonLoopback(t *testing.T) {
	dir := t.TempDir()
	confPath := dir + "/resolv.conf"
	content := "# Generated resolv.conf\n" +
		"nameserver 127.0.0.1\n" +
		"nameserver ::1\n" +
		"nameserver 0.0.0.0\n" +
		"nameserver 192.168.1.1\n" +
		"nameserver 10.0.0.2\n"
	if err := os.WriteFile(confPath, []byte(content), 0644); err != nil {
		t.Fatalf("write resolv.conf: %v", err)
	}

	got := resolvConfNameserversFromPaths(confPath)
	want := []string{"192.168.1.1:53", "10.0.0.2:53"}
	if len(got) != len(want) {
		t.Fatalf("resolvConfNameserversFromPaths() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
