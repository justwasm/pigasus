// Package rctoken — rctoken_test.go: the secret's shape, its
// comparison and the channels a client can present it in.
//
// Stdlib-only.
package rctoken

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestGenerate pins the shape of a minted secret: two calls never
// agree, and the value is base64url without padding (so it survives a
// query string and a header unchanged).
func TestGenerate(t *testing.T) {
	first, err := Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	second, err := Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if first == second {
		t.Errorf("two generated secrets agree: %q", first)
	}
	if !IsWellFormed(first) {
		t.Errorf("generated secret is not well formed: %q", first)
	}
	if strings.ContainsAny(first, "=+/") {
		t.Errorf("generated secret is not base64url without padding: %q", first)
	}
}

// TestVerify pins the comparison: equal secrets verify, everything
// else does not — including an empty expected secret, which is "no
// secret configured" rather than "every secret is right".
func TestVerify(t *testing.T) {
	const secret = "abcdefghijklmnop"
	cases := []struct {
		name      string
		expected  string
		presented string
		want      bool
	}{
		{"exact match", secret, secret, true},
		{"surrounding space", secret, "  " + secret + "\t", true},
		{"different", secret, "ponmlkjihgfedcba", false},
		{"prefix only", secret, secret[:len(secret)-1], false},
		{"empty expected", "", secret, false},
		{"empty presented", secret, "", false},
		{"both empty", "", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Verify(tc.expected, tc.presented); got != tc.want {
				t.Errorf("Verify(%q, %q) = %v, want %v", tc.expected, tc.presented, got, tc.want)
			}
		})
	}
}

// TestPresented pins the channels a secret can arrive in, and the
// order they are read in.
func TestPresented(t *testing.T) {
	const secret = "abcdefghijklmnop"

	t.Run("reference header", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set(HeaderName, secret)
		if got := Presented(req); got != secret {
			t.Errorf("Presented = %q, want %q", got, secret)
		}
	})

	t.Run("authorization bearer", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set(AuthorizationHeader, "Bearer "+secret)
		if got := Presented(req); got != secret {
			t.Errorf("Presented = %q, want %q", got, secret)
		}
	})

	t.Run("authorization bearer, other case", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set(AuthorizationHeader, "bearer "+secret)
		if got := Presented(req); got != secret {
			t.Errorf("Presented = %q, want %q", got, secret)
		}
	})

	t.Run("cookie", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req.AddCookie(&http.Cookie{Name: CookieName, Value: secret})
		if got := Presented(req); got != secret {
			t.Errorf("Presented = %q, want %q", got, secret)
		}
	})

	t.Run("query parameter", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/?token="+secret, nil)
		if got := Presented(req); got != secret {
			t.Errorf("Presented = %q, want %q", got, secret)
		}
	})

	t.Run("nothing", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		if got := Presented(req); got != "" {
			t.Errorf("Presented = %q, want empty", got)
		}
	})

	t.Run("the reference header wins", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/?token="+secret, nil)
		req.Header.Set(HeaderName, "wrong-secret-value")
		if got := Presented(req); got != "wrong-secret-value" {
			t.Errorf("Presented = %q, want the header's value", got)
		}
	})

	t.Run("a non-bearer authorization carries no secret", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set(AuthorizationHeader, "Basic "+secret)
		if got := Presented(req); got != "" {
			t.Errorf("Presented = %q, want empty", got)
		}
	})
}

// TestIsWellFormed pins the shape check that keeps a secret out of a
// JavaScript string it could break out of.
func TestIsWellFormed(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"abcdefghijklmnop", true},
		{"ABCdef1234567890", true},
		{"abc-def_ghi_jkl_mno", true},
		{"short", false},
		{"", false},
		{"abcdefghijklmno\"", false},
		{"abcdefghijklmno\\", false},
		{"abcdefghijklmno\n", false},
		{"abcdefghijklmno<", false},
	}
	for _, tc := range cases {
		if got := IsWellFormed(tc.in); got != tc.want {
			t.Errorf("IsWellFormed(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

// TestIsTunnelRequest pins the marker that tells a tunnel request
// apart from a request that came in over the socket.
func TestIsTunnelRequest(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	if IsTunnelRequest(req) {
		t.Errorf("a request without the marker is not a tunnel request")
	}
	req.Header.Set(ViaRemoteControlHeader, "true")
	if !IsTunnelRequest(req) {
		t.Errorf("a request with the marker is a tunnel request")
	}
	req.Header.Set(ViaRemoteControlHeader, "TRUE")
	if !IsTunnelRequest(req) {
		t.Errorf("the marker is matched case-insensitively")
	}
	if IsTunnelRequest(nil) {
		t.Errorf("a nil request is not a tunnel request")
	}
}

// TestIsLoopbackPeer pins the one part of a request a client cannot
// forge.
func TestIsLoopbackPeer(t *testing.T) {
	cases := []struct {
		addr string
		want bool
	}{
		{"127.0.0.1:5555", true},
		{"127.0.0.53:5555", true},
		{"[::1]:5555", true},
		{"192.168.1.5:5555", false},
		{"10.0.0.2:5555", false},
		{"", false},
		{"not-an-address", false},
	}
	for _, tc := range cases {
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = tc.addr
		if got := IsLoopbackPeer(req); got != tc.want {
			t.Errorf("IsLoopbackPeer(%q) = %v, want %v", tc.addr, got, tc.want)
		}
	}
}

// TestFromEnv pins the pinned-secret lookup: an unset or blank
// variable means "no pinned secret".
func TestFromEnv(t *testing.T) {
	t.Setenv(EnvVar, "")
	if got := FromEnv(); got != "" {
		t.Errorf("FromEnv with a blank variable = %q, want empty", got)
	}
	t.Setenv(EnvVar, "  pinned-secret-value  ")
	if got := FromEnv(); got != "pinned-secret-value" {
		t.Errorf("FromEnv = %q, want the trimmed value", got)
	}
}
