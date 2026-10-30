package dns

import (
	"testing"
)

func TestFacadeDNS(t *testing.T) {
	if EnvNameservers != "OPENAGY_DNS_SERVERS" {
		t.Fatalf("unexpected EnvNameservers: %s", EnvNameservers)
	}
}
