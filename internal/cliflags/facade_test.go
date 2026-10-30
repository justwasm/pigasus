package cliflags_test

import (
	"flag"
	"testing"

	"github.com/justwasm/pigasus/internal/cliflags"
)

func TestCliflagsFacade(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.Bool("foo", true, "")
	_ = fs.Parse([]string{"--foo"})
	if !cliflags.WasSet(fs, "foo") {
		t.Fatal("expected foo to be set")
	}
}
