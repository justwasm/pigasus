package remotecontrol

import (
	"errors"
	"testing"
)

func TestTraceBackend(t *testing.T) {
	productionBackend := &PiagentBackend{SessionFactory: errPigFactory(errors.New("unused"))}
	c, err := NewCascade(CascadeOpts{
		DispatchDefault: productionBackend,
	})
	if err != nil {
		t.Fatalf("NewCascade: %v", err)
	}

	t.Logf("after NewCascade: c.backend=%T, dispatchDefault=%T", c.backend, c.dispatchDefault)
	got1 := c.Backend()
	t.Logf("after first Backend(): got=%T (is productionBackend=%v)", got1, got1 == productionBackend)

	userBackend := &StubBackend{Reply: "user"}
	c.SetBackend(userBackend)
	t.Logf("after SetBackend: c.backend=%T, userBackend=%T (same instance? %v)", c.backend, userBackend, c.backend == MessageBackend(userBackend))

	got2 := c.Backend()
	t.Logf("after second Backend(): got=%T (== userBackend? %v)", got2, got2 == userBackend)
}
