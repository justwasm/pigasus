// Package customcmd is a backward-compatible facade over internal/app/prompt/customcmd.
package customcmd

import (
	appcmd "github.com/justwasm/pigasus/internal/app/prompt/customcmd"
)

type Source = appcmd.Source

const (
	SourcePlugin  = appcmd.SourcePlugin
	SourceProject = appcmd.SourceProject
	SourceGlobal  = appcmd.SourceGlobal
	SourceBuiltin = appcmd.SourceBuiltin
)

type Command = appcmd.Command
type Conflict = appcmd.Conflict
type LoadRoots = appcmd.LoadRoots
type BuiltIn = appcmd.BuiltIn

var (
	Load              = appcmd.Load
	MergeWithBuiltins = appcmd.MergeWithBuiltins
)
