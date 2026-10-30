//go:build !wasm && !wasip1 && !js

package main

import (
	"github.com/justwasm/pigasus/internal/cliapp"
	"github.com/justwasm/pigasus/internal/modular"
	_ "github.com/justwasm/pigasus/internal/features/tools/native"
	_ "github.com/justwasm/pigasus/internal/infra/subsystems/daemon"
	_ "github.com/justwasm/pigasus/internal/infra/subsystems/localharness"
	_ "github.com/justwasm/pigasus/internal/infra/subsystems/terminal"
	_ "github.com/justwasm/pigasus/internal/ui/cli/subcommands/update"
)

func init() {
	modular.SetTUIRunner(cliapp.RunLiveTUI)
	modular.SetTUISnapshotRenderer(cliapp.RenderTUI)
}
