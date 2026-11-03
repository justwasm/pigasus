package main

import (
	"os"

	"github.com/justwasm/pigasus/internal/modular"
	_ "github.com/justwasm/pigasus/internal/app/engine/stream"
	_ "github.com/justwasm/pigasus/internal/app/subcommands/accounts"
	_ "github.com/justwasm/pigasus/internal/app/subcommands/agent"
	_ "github.com/justwasm/pigasus/internal/app/subcommands/changelog"
	_ "github.com/justwasm/pigasus/internal/app/subcommands/halloween"
	_ "github.com/justwasm/pigasus/internal/app/subcommands/models"
	_ "github.com/justwasm/pigasus/internal/app/subcommands/provider"
)


var version = "1.4.12-wasm"

func main() {
	app := modular.NewApp(modular.AppOptions{
		Name:          "pigga",
		Description:   "Modular, streamlined Antigravity CLI",
		Version:       version,
		DefaultEngine: "stream",
	})
	os.Exit(app.Run(os.Args))
}

