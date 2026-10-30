// Package agybin is a backward-compatible facade over internal/infra/proxy/bin.
package agybin

import (
	"github.com/justwasm/pigasus/internal/infra/proxy/bin"
)

const (
	EnvRealBin        = bin.EnvRealBin
	EnvNoAutoDownload = bin.EnvNoAutoDownload
)

var (
	DefaultPaths           = bin.DefaultPaths
	Path                   = bin.Path
	Version                = bin.Version
	AutoDownload           = bin.AutoDownload
	CacheDir               = bin.CacheDir
	CacheBinaryPath        = bin.CacheBinaryPath
	CacheBinaryName        = bin.CacheBinaryName
	SetDisableAutoDownload = bin.SetDisableAutoDownload
)
