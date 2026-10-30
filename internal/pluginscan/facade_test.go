package pluginscan

import (
	"os"
	"testing"
)

func TestFacadePluginScan(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if !IsDir(wd) {
		t.Fatalf("expected wd to be dir: %s", wd)
	}
}
