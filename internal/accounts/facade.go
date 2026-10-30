// Package accounts is a backward-compatible facade over internal/features/accounts/core.
package accounts

import (
	"github.com/justwasm/pigasus/internal/features/accounts/core"
)

const (
	Version          = core.Version
	QuotaColumnWidth = core.QuotaColumnWidth
)

type (
	Account = core.Account
	Store   = core.Store
)

var (
	Current          = core.Current
	Register         = core.Register
	RegisterWithEmail = core.RegisterWithEmail
	Activate         = core.Activate
	Remove           = core.Remove
	RegisterCurrent  = core.RegisterCurrent
	FetchQuota       = core.FetchQuota
	AccessToken      = core.AccessToken
	Path             = core.Path
	Load             = core.Load
	RowLine          = core.RowLine
	QuotaCell        = core.QuotaCell
	QuotaSnapshotAge = core.QuotaSnapshotAge
	Get              = core.Get
	Update           = core.Update
	SetPlanTier      = core.SetPlanTier
	SetQuota         = core.SetQuota
	SetToken         = core.SetToken
)
