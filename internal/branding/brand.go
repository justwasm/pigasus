// Package branding is a backward-compatible facade over internal/app/prompt/branding.
package branding

import (
	appbrand "github.com/justwasm/pigasus/internal/app/prompt/branding"
)

type Brand = appbrand.Brand

const (
	BrandAntigravity = appbrand.BrandAntigravity
	BrandPigasus     = appbrand.BrandPigasus
)

type Config = appbrand.Config

var (
	Current    = appbrand.Current
	SetCurrent = appbrand.SetCurrent
	Reset      = appbrand.Reset
	IsTest     = appbrand.IsTest
)
