package lsserver

import "testing"

// TestTheTwoVersionsAreNotTheSame pins the two version numbers the
// reference reports.
//
// 【一手】 the log line reports the language server's own version
// unless `--override_ide_version` overrides it; the web page's
// `appVersion` is the override, and "0.0.0.0" when there is none.
func TestTheTwoVersionsAreNotTheSame(t *testing.T) {
	cases := []struct {
		name  string
		flags *Flags
		ls    string
		ide   string
	}{
		{
			name:  "no override",
			flags: &Flags{},
			ls:    Version,
			ide:   "0.0.0.0",
		},
		{
			name:  "the IDE's override",
			flags: &Flags{OverrideIDEVersion: "2.26.0"},
			ls:    "2.26.0",
			ide:   "2.26.0",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := lsVersion(c.flags); got != c.ls {
				t.Errorf("lsVersion = %q, want %q", got, c.ls)
			}
			if got := ideVersion(c.flags); got != c.ide {
				t.Errorf("ideVersion = %q, want %q", got, c.ide)
			}
		})
	}
}
