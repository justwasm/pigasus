// Package atconversation is a backward-compatible facade over internal/app/prompt/mentions/conversation.
package atconversation

import (
	"github.com/justwasm/pigasus/internal/app/prompt/mentions/conversation"
)

const (
	MentionPrefix = conversation.MentionPrefix
	Timeout       = conversation.Timeout
	MaxMatches    = conversation.MaxMatches
)

type Source = conversation.Source

const (
	SourceRemote        = conversation.SourceRemote
	SourceLocalFallback = conversation.SourceLocalFallback
	SourceEmpty         = conversation.SourceEmpty
)

type (
	Match    = conversation.Match
	LocalFn  = conversation.LocalFn
	SearchFn = conversation.SearchFn
)

var (
	DefaultSearcher  = conversation.DefaultSearcher
	ParseMention     = conversation.ParseMention
	SearchMatches    = conversation.SearchMatches
	ListLocalMatches = conversation.ListLocalMatches
	MentionInsert    = conversation.MentionInsert
)
