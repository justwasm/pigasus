package apierror_test

import (
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/apierror"
)

func TestApierrorFacade(t *testing.T) {
	if apierror.HeadlessAPIExitCode != 3 {
		t.Fatalf("unexpected exit code: %d", apierror.HeadlessAPIExitCode)
	}
	id := apierror.NewErrorID()
	if !strings.HasSuffix(id, "-1") {
		t.Fatalf("unexpected ErrorID suffix: %s", id)
	}
}
