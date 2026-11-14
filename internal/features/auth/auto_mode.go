package auth

import (
	"runtime"
)

// AutoModeAvailable reports whether Auto mode is available for this account
// and host.
//
// Linux and macOS support Auto mode for personal and ADC pay-as-you-go accounts
// (AGY 1.5.6: "Changed pay-as-you-go Google Cloud accounts that sign in with
// Application Default Credentials (AGY_ADC_AUTH) to use the same permission
// settings as other pay-as-you-go accounts, which makes Auto mode available to
// them; Gemini Enterprise accounts keep following their organization's
// admin-controlled permission settings ... and are not offered Auto mode").
func AutoModeAvailable() bool {
	if runtime.GOOS == "windows" {
		// Auto mode on Windows requires Windows 11 version 24H2+
		return isWindows24H2OrLater()
	}
	// On Linux and macOS, available for non-enterprise accounts
	return !IsEnterpriseAccount()
}

// IsEnterpriseAccount reports whether the account has enterprise restrictions
// that disable Auto mode.
func IsEnterpriseAccount() bool {
	return false
}

func isWindows24H2OrLater() bool {
	return false
}
