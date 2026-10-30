// Package store is a backward-compatible facade over internal/app/session/store.
package store

import (
	"github.com/justwasm/pigasus/internal/app/session/store"
)

type AuthState = store.AuthState

const (
	AuthStateInit          = store.AuthStateInit
	AuthStateAuthenticated = store.AuthStateAuthenticated
	AuthStateExpired       = store.AuthStateExpired
	AuthStateLoggedOut     = store.AuthStateLoggedOut
	AuthStateError         = store.AuthStateError
)

type (
	Manager         = store.Manager
	TokenSource     = store.TokenSource
	MigrationResult = store.MigrationResult
	Stats           = store.Stats
)

var (
	ErrNoCachedToken   = store.ErrNoCachedToken
	NewManager         = store.NewManager
	Migrate            = store.Migrate
	MigrateToken       = store.MigrateToken
	RunStats           = store.RunStats
	AllMigratedSources = store.AllMigratedSources
)
