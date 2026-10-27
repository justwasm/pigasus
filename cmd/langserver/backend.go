// Package main — backend.go: which transport answers a turn in the
// IDE shell.
//
// The standalone language server is not a fixture server: the IDE's
// workbench sends a message into it and expects an answer from a real
// model. The daemon's turn backend is picked by
// `rchost.BackendPicker`, and that picker **defaults to the
// deterministic stand-in** — the CLI overrides it (internal/cliapp),
// so the language server has to override it too.
//
// The rule is the clone's own (internal/backendselect): a model the
// user configured goes to its own endpoint, a model of the reference's
// catalogue goes to CloudCode, and a model nobody configured gets the
// stand-in *with a warning* — an answer that silently comes from the
// stand-in would look like a working model.
//
// The mocked mode (`--use_mocked_data=true`) keeps the stand-in: that
// mode is the reference's own offline switch, and the coverage probe
// runs against it.
package main

import (
	"os"

	"github.com/justwasm/pigasus/internal/backend"
	"github.com/justwasm/pigasus/internal/backendselect"
	"github.com/justwasm/pigasus/internal/modelpicker"
	"github.com/justwasm/pigasus/internal/rchost"
)

// wireBackend points the daemon's turn backend at the clone's real
// transports.
//
// The picker is called per turn as well as once at start-up: the
// workbench picks the model when the conversation starts, and the
// daemon is started long before that.
func wireBackend() {
	rchost.BackendPicker = func(modelName string) backend.Client {
		return backendselect.PickWithLog(nil, modelName, os.Stderr,
			backend.GatewayTimeoutUnlimited, modelpicker.ResolveCatalogueID)
	}
}
