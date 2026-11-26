package render

import "testing"

func TestDetectAltScreenCaps(t *testing.T) {
	cases := []struct {
		name           string
		env            []string
		probeWindowsVT func() bool
		wantSupports   bool
		wantRemote     bool
		wantLabel      string
		wantHint       string
	}{
		{
			name:         "modern xterm supports alt screen",
			env:          []string{"TERM=xterm-256color"},
			wantSupports: true,
			wantLabel:    "xterm-256color",
			wantHint:     "",
		},
		{
			name:         "dumb term does not support alt screen",
			env:          []string{"TERM=dumb"},
			wantSupports: false,
			wantLabel:    "dumb",
			wantHint:     `⚠ terminal "dumb" does not support alt screen`,
		},
		{
			name:         "cygwin term does not support alt screen",
			env:          []string{"TERM=cygwin"},
			wantSupports: false,
			wantLabel:    "cygwin",
			wantHint:     `⚠ terminal "cygwin" does not support alt screen`,
		},
		{
			name:         "unset TERM with unknown terminal omits unknown label",
			env:          []string{},
			wantSupports: false,
			wantLabel:    "",
			wantHint:     "⚠ terminal does not support alt screen",
		},
		{
			name:         "remote ssh session with unsupported term suggests ssh -t",
			env:          []string{"TERM=dumb", "SSH_CONNECTION=10.0.0.1 1234 10.0.0.2 22"},
			wantSupports: false,
			wantRemote:   true,
			wantLabel:    "dumb",
			wantHint:     "⚠ alt screen unavailable — run ssh -t",
		},
		{
			name:         "windows terminal with unset TERM supports alt screen",
			env:          []string{"OS=Windows_NT", "WT_SESSION=abc-123"},
			wantSupports: true,
			wantLabel:    "windows_terminal",
			wantHint:     "",
		},
		{
			name:           "windows powershell or cmd console with unset TERM and VT support avoids false warning",
			env:            []string{"OS=Windows_NT", "PSModulePath=C:\\Program Files\\PowerShell\\Modules", "COMSPEC=C:\\Windows\\System32\\cmd.exe"},
			probeWindowsVT: func() bool { return true },
			wantSupports:   true,
			wantLabel:      "conhost",
			wantHint:       "",
		},
		{
			name:           "windows legacy conhost without VT support reports conhost label",
			env:            []string{"OS=Windows_NT", "COMSPEC=C:\\Windows\\System32\\cmd.exe"},
			probeWindowsVT: func() bool { return false },
			wantSupports:   false,
			wantLabel:      "conhost",
			wantHint:       `⚠ terminal "conhost" does not support alt screen`,
		},
		{
			name:         "TERM_PROGRAM set with unset TERM supports alt screen",
			env:          []string{"TERM_PROGRAM=vscode"},
			wantSupports: true,
			wantLabel:    "vscode",
			wantHint:     "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			caps := DetectAltScreenCaps(tc.env, tc.probeWindowsVT)
			if caps.SupportsAltScreen != tc.wantSupports {
				t.Errorf("SupportsAltScreen = %v, want %v", caps.SupportsAltScreen, tc.wantSupports)
			}
			if caps.IsRemoteSession != tc.wantRemote {
				t.Errorf("IsRemoteSession = %v, want %v", caps.IsRemoteSession, tc.wantRemote)
			}
			if caps.TermLabel != tc.wantLabel {
				t.Errorf("TermLabel = %q, want %q", caps.TermLabel, tc.wantLabel)
			}
			if got := AltScreenHint(&caps); got != tc.wantHint {
				t.Errorf("AltScreenHint = %q, want %q", got, tc.wantHint)
			}
		})
	}

	// Explicit "unknown" label must also omit the quoted "unknown" string.
	if got := AltScreenHint(&AltScreenCaps{SupportsAltScreen: false, TermLabel: "unknown"}); got != "⚠ terminal does not support alt screen" {
		t.Errorf("AltScreenHint(unknown) = %q, want generic message", got)
	}
}
