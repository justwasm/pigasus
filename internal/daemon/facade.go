package daemon

import (
	"github.com/justwasm/pigasus/internal/infra/daemon"
)

const DefaultGracePeriod = daemon.DefaultGracePeriod

type (
	Tracker = daemon.Tracker
	Logger  = daemon.Logger
)

var (
	New                     = daemon.New
	Global                  = daemon.Global
	SetGlobal               = daemon.SetGlobal
	InstallSignalHandlers   = daemon.InstallSignalHandlers
	InstallSignalHandlersOn = daemon.InstallSignalHandlersOn
	IsRunning               = daemon.IsRunning
	IsRunningViaKill        = daemon.IsRunningViaKill
	CmdFromArgs             = daemon.CmdFromArgs
)
