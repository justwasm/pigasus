package rctunnel

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestFindLanguageServer_Configured(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "dummy_ls_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	found := FindLanguageServer(tmpFile.Name())
	if found != tmpFile.Name() {
		t.Errorf("FindLanguageServer(%q) = %q, want %q", tmpFile.Name(), found, tmpFile.Name())
	}
}

func TestSetupRuntimeConfig(t *testing.T) {
	tmpGemini, err := os.MkdirTemp("", "gemini_test_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpGemini)

	cfgDir := filepath.Join(tmpGemini, "config")
	if err := os.MkdirAll(cfgDir, 0755); err != nil {
		t.Fatal(err)
	}

	srcConfig := filepath.Join(cfgDir, "config.json")
	initialJSON := `{
  "userSettings": {
    "cliRemoteControlHostname": "base-machine-noble-bolt",
    "remoteControlHostname": "base-machine-noble-bolt"
  }
}`
	if err := os.WriteFile(srcConfig, []byte(initialJSON), 0644); err != nil {
		t.Fatal(err)
	}

	instanceName := "pigasus-base-machine-noble-bolt"
	relConfigDir, cleanup, err := SetupRuntimeConfig(tmpGemini, instanceName)
	if err != nil {
		t.Fatalf("SetupRuntimeConfig failed: %v", err)
	}
	defer cleanup()

	// 1. Verify original config.json was NOT touched
	origBytes, err := os.ReadFile(srcConfig)
	if err != nil {
		t.Fatal(err)
	}
	var origDoc struct {
		UserSettings struct {
			CliRemoteControlHostname string `json:"cliRemoteControlHostname"`
		} `json:"userSettings"`
	}
	_ = json.Unmarshal(origBytes, &origDoc)
	if origDoc.UserSettings.CliRemoteControlHostname != "base-machine-noble-bolt" {
		t.Errorf("original config was mutated! got: %s", origDoc.UserSettings.CliRemoteControlHostname)
	}

	// 2. Verify runtime config has pigasus- prefixed instance name and remoteControlEnabled
	runtimeConfig := filepath.Join(tmpGemini, relConfigDir, "config.json")
	rtBytes, err := os.ReadFile(runtimeConfig)
	if err != nil {
		t.Fatalf("runtime config missing at %s: %v", runtimeConfig, err)
	}
	var rtDoc struct {
		UserSettings struct {
			RemoteControlEnabled     bool   `json:"remoteControlEnabled"`
			CliRemoteControlHostname string `json:"cliRemoteControlHostname"`
			RemoteControlHostname    string `json:"remoteControlHostname"`
		} `json:"userSettings"`
	}
	if err := json.Unmarshal(rtBytes, &rtDoc); err != nil {
		t.Fatalf("parse runtime config: %v", err)
	}

	if !rtDoc.UserSettings.RemoteControlEnabled {
		t.Errorf("remoteControlEnabled = false, want true")
	}
	if rtDoc.UserSettings.CliRemoteControlHostname != instanceName {
		t.Errorf("cliRemoteControlHostname = %q, want %q", rtDoc.UserSettings.CliRemoteControlHostname, instanceName)
	}
	if rtDoc.UserSettings.RemoteControlHostname != instanceName {
		t.Errorf("remoteControlHostname = %q, want %q", rtDoc.UserSettings.RemoteControlHostname, instanceName)
	}
}

func TestNewCSRFToken(t *testing.T) {
	tok1 := NewCSRFToken()
	tok2 := NewCSRFToken()
	if tok1 == "" || tok2 == "" {
		t.Fatal("generated empty token")
	}
	if tok1 == tok2 {
		t.Fatal("tokens should be random UUIDs, got duplicates")
	}
}

func TestProxyDirectorHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		csrf := r.Header.Get("x-codeium-csrf-token")
		if csrf != "test-csrf-123" {
			t.Errorf("csrf header = %q, want %q", csrf, "test-csrf-123")
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	tcpAddr, ok := server.Listener.Addr().(*net.TCPAddr)
	if !ok {
		t.Skip("not a tcp listener")
	}

	proxy, err := NewProxy(tcpAddr.Port, "test-csrf-123")
	if err != nil {
		t.Fatalf("NewProxy failed: %v", err)
	}

	req := httptest.NewRequest("GET", "http://localhost/test", nil)
	rr := httptest.NewRecorder()
	proxy.httpProxy.ServeHTTP(rr, req)
}

