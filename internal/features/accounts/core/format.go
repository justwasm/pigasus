// format.go — cycle accounts-tier-usage: how the account lists render
// a quota.
//
// The account lists (the /accounts panel, `agy accounts list`, the
// print-mode /accounts) all show the same two numbers: the weekly and
// the five-hour remaining. They are percentages, so they are rendered
// right-aligned in a fixed-width column — the numbers then line up
// down the column instead of hanging off their left edge.
//
// The width is a property of the value, not of the renderer: "100.0%"
// is the widest one, so a column of six characters holds every value a
// quota can produce. A dash (the account has no bucket in that
// window) is padded like a number, so the column keeps its shape.
package core

import (
	"fmt"
	"strings"
)

// QuotaColumnWidth is the width of a percentage column in the account
// lists: "100.0%" is the widest value, so every column is that wide.
const QuotaColumnWidth = 6

// WeeklyPercent renders the account's weekly remaining as a
// right-aligned percentage ("  45.2%", "100.0%"), or a padded dash
// when the account has no weekly bucket.
//
// The padding is part of the value, so every renderer lines the
// numbers up the same way.
func (a Account) WeeklyPercent() string {
	if a.Quota == nil {
		return padQuotaPercent("-")
	}
	fraction, ok := a.Quota.WeeklyRemaining()
	if !ok {
		return padQuotaPercent("-")
	}
	return padQuotaPercent(fmt.Sprintf("%.1f%%", fraction*100))
}

// FiveHourPercent renders the account's five-hour remaining the way
// WeeklyPercent renders the weekly one.
func (a Account) FiveHourPercent() string {
	if a.Quota == nil {
		return padQuotaPercent("-")
	}
	fraction, ok := a.Quota.FiveHourRemaining()
	if !ok {
		return padQuotaPercent("-")
	}
	return padQuotaPercent(fmt.Sprintf("%.1f%%", fraction*100))
}

// QuotaColumns renders the account's two quota columns: the weekly
// and the five-hour remaining, each labelled and each right-aligned.
//
//	"wk  45.2%"   "5h  96.0%"
func (a Account) QuotaColumns() (string, string) {
	return "wk " + a.WeeklyPercent(), "5h " + a.FiveHourPercent()
}

// QuotaCell renders both quota columns as one cell, the way the
// /accounts panel lays them out.
//
//	"wk  45.2%  5h  96.0%"
func (a Account) QuotaCell() string {
	weekly, fiveHour := a.QuotaColumns()
	return weekly + "  " + fiveHour
}

// padQuotaPercent right-aligns a percentage (or a dash) in a column of
// QuotaColumnWidth characters.
func padQuotaPercent(value string) string {
	if len(value) >= QuotaColumnWidth {
		return value
	}
	return strings.Repeat(" ", QuotaColumnWidth-len(value)) + value
}
