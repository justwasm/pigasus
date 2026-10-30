package main

import (
	"os"

	"github.com/justwasm/pigasus/internal/modular"
	_ "github.com/justwasm/pigasus/internal/app/engine/stream"
	_ "github.com/justwasm/pigasus/internal/ui/cli/subcommands/accounts"
	_ "github.com/justwasm/pigasus/internal/ui/cli/subcommands/agent"
	_ "github.com/justwasm/pigasus/internal/ui/cli/subcommands/changelog"
	_ "github.com/justwasm/pigasus/internal/ui/cli/subcommands/halloween"
	_ "github.com/justwasm/pigasus/internal/ui/cli/subcommands/models"
	_ "github.com/justwasm/pigasus/internal/ui/cli/subcommands/provider"
)


var version = "1.4.11-wasm"

func main() {
	app := modular.NewApp(modular.AppOptions{
		Name:          "pigga",
		Description:   "Modular, streamlined Antigravity CLI",
		Version:       version,
		DefaultEngine: "stream",
	})
	os.Exit(app.Run(os.Args))
}

