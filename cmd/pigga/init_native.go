//go:build !wasm && !wasip1 && !js

package main

import (
	_ "github.com/justwasm/pigasus/internal/app/subcommands/update"
	_ "github.com/justwasm/pigasus/internal/app/tools/native"
	_ "github.com/justwasm/pigasus/internal/cli"
	_ "github.com/justwasm/pigasus/internal/infra/subsystems/daemon"
	_ "github.com/justwasm/pigasus/internal/infra/subsystems/localharness"
	_ "github.com/justwasm/pigasus/internal/infra/subsystems/terminal"
	"github.com/justwasm/pigasus/internal/modular"
	"github.com/justwasm/pigasus/internal/tui"
)

func init() {
	modular.SetTUIRunner(tui.RunLiveTUI)
	modular.SetTUISnapshotRenderer(tui.RenderTUI)
}
