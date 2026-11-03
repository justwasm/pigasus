package remotecontrol

import (
	"errors"
	"testing"

	"github.com/justwasm/pigasus/internal/remotecontrol/cascades"
)

// TestTraceBackend traces the cascade's lazy backend swap: the
// dispatcher-supplied default is installed on the first Backend()
// call, and a later SetBackend wins over it.
//
// The cascade's own fields are the cascade package's — the test
// reads them through the exported accessors, which is also the
// only way a caller outside the package can read them.
func TestTraceBackend(t *testing.T) {
	productionBackend := &PiagentBackend{SessionFactory: errPigFactory(errors.New("unused"))}
	c, err := cascades.New(cascades.Opts{
		DispatchDefault: productionBackend,
	})
	if err != nil {
		t.Fatalf("NewCascade: %v", err)
	}

	got1 := c.Backend()
	t.Logf("after first Backend(): got=%T (is productionBackend=%v)", got1, got1 == productionBackend)

	userBackend := &cascades.StubBackend{Reply: "user"}
	c.SetBackend(userBackend)
	t.Logf("after SetBackend: backend=%T, userBackend=%T (same instance? %v)", c.Backend(), userBackend, c.Backend() == cascades.Backend(userBackend))

	got2 := c.Backend()
	t.Logf("after second Backend(): got=%T (== userBackend? %v)", got2, got2 == userBackend)
}
