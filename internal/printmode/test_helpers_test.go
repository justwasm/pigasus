package printmode

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/justwasm/pigasus/internal/cli"
	"github.com/justwasm/pigasus/internal/core/cliflags"
	"github.com/justwasm/pigasus/internal/features/tools/skills"
)

func withSyntheticCatalog(t *testing.T, catalog []skills.SkillInfo) {
	t.Helper()
	orig := slashSkillCatalogHook
	slashSkillCatalogHook = func() []skills.SkillInfo { return catalog }
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
		"antigravity", "recovered", "recovered",
		cliflags.FlagSpec{Name: "format", Type: "string", Usage: "Alias for --output-format (text, json, stream-json)"},
	)
	if code := p.Root().Parse(args); code != 0 {
		t.Fatalf("Parse(%v) = %d", args, code)
	}
	return p.Root().FlagSet()
}
