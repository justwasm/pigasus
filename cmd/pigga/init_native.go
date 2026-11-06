//go:build !wasm && !wasip1 && !js

package main

import (
	_ "github.com/justwasm/pigasus/internal/cli"
	"github.com/justwasm/pigasus/internal/tui"
	_ "github.com/justwasm/pigasus/internal/app/tools/native"
	_ "github.com/justwasm/pigasus/internal/infra/subsystems/daemon"
	_ "github.com/justwasm/pigasus/internal/infra/subsystems/localharness"
	_ "github.com/justwasm/pigasus/internal/infra/subsystems/terminal"
	"github.com/justwasm/pigasus/internal/modular"
	_ "github.com/justwasm/pigasus/internal/app/subcommands/update"
)

func init() {
	modular.SetTUIRunner(tui.RunLiveTUI)
	modular.SetTUISnapshotRenderer(tui.RenderTUI)
}
