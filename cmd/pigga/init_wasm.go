//go:build wasm || wasip1 || js

package main

import (
	_ "github.com/justwasm/pigasus/internal/app/tools/wasm"
	_ "github.com/justwasm/pigasus/internal/infra/subsystems/daemon"
	_ "github.com/justwasm/pigasus/internal/infra/subsystems/localharness"
	_ "github.com/justwasm/pigasus/internal/infra/subsystems/terminal"
)
