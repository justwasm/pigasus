package main

import (
	"os"

	_ "github.com/justwasm/pigasus/internal/app/engine/stream"
	_ "github.com/justwasm/pigasus/internal/app/subcommands/accounts"
	_ "github.com/justwasm/pigasus/internal/app/subcommands/agent"
	_ "github.com/justwasm/pigasus/internal/app/subcommands/changelog"
	_ "github.com/justwasm/pigasus/internal/app/subcommands/halloween"
	_ "github.com/justwasm/pigasus/internal/app/subcommands/models"
	_ "github.com/justwasm/pigasus/internal/app/subcommands/provider"
	"github.com/justwasm/pigasus/internal/modular"
)

var version = "1.5.6-wasm"

func main() {
	app := modular.NewApp(modular.AppOptions{
		Name:          "pigga",
		Description:   "Modular, streamlined Antigravity CLI",
		Version:       version,
		DefaultEngine: "stream",
	})
	os.Exit(app.Run(os.Args))
}
