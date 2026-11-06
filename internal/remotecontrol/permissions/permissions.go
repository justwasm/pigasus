// Package permissions — permissions.go: adapter types and definitions
// bridging remote control RPC callers to Tier 1 core permissions store.
package permissions

import (
	corepermissions "github.com/justwasm/pigasus/internal/core/policy/permissions"
)

type (
	Preset     = corepermissions.Preset
	ReviewMode = corepermissions.ReviewMode
	Grant      = corepermissions.Grant
	State      = corepermissions.State
	Store      = corepermissions.Store
	FSStore    = corepermissions.FSStore
	MemStore   = corepermissions.MemStore
)

const (
	PresetDefault     = corepermissions.PresetDefault
	PresetStrict      = corepermissions.PresetStrict
	PresetPermissive  = corepermissions.PresetPermissive
	ReviewModeAlways  = corepermissions.ReviewModeAlways
	ReviewModeNever   = corepermissions.ReviewModeNever
	ReviewModeAuto    = corepermissions.ReviewModeAuto
	StateVersion      = corepermissions.StateVersion
	DefaultPreset     = corepermissions.DefaultPreset
	DefaultReviewMode = corepermissions.DefaultReviewMode
)

var (
	ErrGrantExists    = corepermissions.ErrGrantExists
	ErrGrantMalformed = corepermissions.ErrGrantMalformed
	NewFSStore        = corepermissions.NewFSStore
	NewMemStore       = corepermissions.NewMemStore
	IsValidPreset     = corepermissions.IsValidPreset
	IsValidReviewMode = corepermissions.IsValidReviewMode
	ParseToolPattern  = corepermissions.ParseToolPattern
)
