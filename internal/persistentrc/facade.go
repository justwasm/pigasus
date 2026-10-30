// Package persistentrc is a backward-compatible facade over internal/infra/remote/persistent.
package persistentrc

import (
	"github.com/justwasm/pigasus/internal/infra/remote/persistent"
)

const (
	InstanceIDPrefix           = persistent.InstanceIDPrefix
	InstanceIDSuffix           = persistent.InstanceIDSuffix
	DefaultAddr                = persistent.DefaultAddr
	DefaultOfficialDaemonLabel = persistent.DefaultOfficialDaemonLabel
)

type (
	Spec           = persistent.Spec
	StatusReport   = persistent.StatusReport
	ErrInvalidAddr = persistent.ErrInvalidAddr
	ErrInvalidName = persistent.ErrInvalidName
)

var (
	ErrUnsupported = persistent.ErrUnsupported

	Status                 = persistent.Status
	Start                  = persistent.Start
	Stop                   = persistent.Stop
	SetDryRun              = persistent.SetDryRun
	ResolveSpec            = persistent.ResolveSpec
	Label                  = persistent.Label
	DefaultLaunchAgentDir  = persistent.DefaultLaunchAgentDir
	PlistPathFor           = persistent.PlistPathFor
	SetPlistDirOverride    = persistent.SetPlistDirOverride
	LogFilePath            = persistent.LogFilePath
	ValidateName           = persistent.ValidateName
	ValidateAddr           = persistent.ValidateAddr
	Now                    = persistent.Now
	ResolveActiveAccount   = persistent.ResolveActiveAccount
	OfficialDaemonLabel    = persistent.OfficialDaemonLabel
	ResolveInstanceName    = persistent.ResolveInstanceName
	SetInstanceName        = persistent.SetInstanceName
	PlaintextDaemonStatus  = persistent.PlaintextDaemonStatus
	OfficialPlistPath      = persistent.OfficialPlistPath
	OfficialBinaryPath     = persistent.OfficialBinaryPath
	OfficialLogPath        = persistent.OfficialLogPath
	BuildOfficialPlist     = persistent.BuildOfficialPlist
	StartOfficialDaemon    = persistent.StartOfficialDaemon
	StopOfficialDaemon     = persistent.StopOfficialDaemon
	InstanceID             = persistent.InstanceID
	InstallationUUID       = persistent.InstallationUUID
	EnsureInstanceID       = persistent.EnsureInstanceID
	InstanceIDPath         = persistent.InstanceIDPath
)
