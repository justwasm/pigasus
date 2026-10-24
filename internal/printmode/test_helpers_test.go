package printmode

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/justwasm/pigasus/internal/cli"
)

func withSyntheticCatalog(t *testing.T, skills []jsonSkillInfo) {
	t.Helper()
	orig := slashSkillCatalogHook
	slashSkillCatalogHook = func() []jsonSkillInfo { return skills }
	t.Cleanup(func() { slashSkillCatalogHook = orig })
}

func writeSkillMD(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func newStandardFlagSetForTest(t *testing.T, args []string) *flag.FlagSet {
	t.Helper()
	p := cli.NewParser(cli.StandardFlags(), cli.StandardSubcommands(),
		"antigravity", "recovered", "recovered")
	if code := p.Root().Parse(args); code != 0 {
		t.Fatalf("Parse(%v) = %d", args, code)
	}
	return p.Root().FlagSet()
}
