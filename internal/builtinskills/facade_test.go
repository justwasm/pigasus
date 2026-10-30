package builtinskills_test

import (
	"testing"

	"github.com/justwasm/pigasus/internal/builtinskills"
)

func TestBuiltinSkillsFacade(t *testing.T) {
	dirs := builtinskills.Dirs()
	if len(dirs) == 0 {
		t.Fatal("expected non-empty builtin skills")
	}
}
