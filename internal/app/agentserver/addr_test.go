// addr_test.go — the server's bind address policy.
//
// The address is the whole of the server's configuration surface the
// user can reach, so the policy is pinned here rather than through a
// live listener.
package agentserver

import "testing"

// TestResolveAddr pins the default and the overrides.
//
// The default is the loopback on a fixed port: the server runs shell
// commands against its own workspace, so it does not listen on the
// network unless the user says so.
func TestResolveAddr(t *testing.T) {
	cases := []struct {
		name string
		addr string
		host string
		port int
		want string
	}{
		{"the default", "", "", 0, "127.0.0.1:7777"},
		{"a host", "", "0.0.0.0", 0, "0.0.0.0:7777"},
		{"a port", "", "", 8080, "127.0.0.1:8080"},
		{"a host and a port", "", "example.test", 9000, "example.test:9000"},
		{"an address wins", "10.0.0.1:1234", "0.0.0.0", 8080, "10.0.0.1:1234"},
		{"an address alone", "10.0.0.1:1234", "", 0, "10.0.0.1:1234"},
		{"a padded address", "  10.0.0.1:1234  ", "", 0, "10.0.0.1:1234"},
		{"a padded host", "", "  example.test  ", 0, "example.test:7777"},
		{"a negative port", "", "", -1, "127.0.0.1:7777"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ResolveAddr(c.addr, c.host, c.port); got != c.want {
				t.Errorf("ResolveAddr(%q, %q, %d) = %q, want %q",
					c.addr, c.host, c.port, got, c.want)
			}
		})
	}
}
