package settings

import (
	"os"
	"path/filepath"
	"testing"
)

func BenchmarkSettings_String(b *testing.B) {
	dir := b.TempDir()
	p := filepath.Join(dir, "settings.json")
	_ = os.WriteFile(p, []byte(`{"verbosity":"medium","model":"claude-3-5-sonnet"}`), 0644)
	b.Setenv("OPENAGY_CONFIG_HOME", dir)
	b.Setenv("ANTIGRAVITY_HOME", dir)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = String("verbosity")
	}
}

func BenchmarkSettings_Bool(b *testing.B) {
	dir := b.TempDir()
	p := filepath.Join(dir, "settings.json")
	_ = os.WriteFile(p, []byte(`{"statusLine.enabled":true,"showThinking":false}`), 0644)
	b.Setenv("OPENAGY_CONFIG_HOME", dir)
	b.Setenv("ANTIGRAVITY_HOME", dir)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = Bool("statusLine.enabled")
	}
}

func BenchmarkSettings_Field(b *testing.B) {
	dir := b.TempDir()
	p := filepath.Join(dir, "settings.json")
	_ = os.WriteFile(p, []byte(`{"activeItems":{"hidden":false}}`), 0644)
	b.Setenv("OPENAGY_CONFIG_HOME", dir)
	b.Setenv("ANTIGRAVITY_HOME", dir)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = Field("activeItems.hidden")
	}
}
