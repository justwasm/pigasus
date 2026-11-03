package catalog

import "testing"

func BenchmarkModelCatalogue(b *testing.B) {
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = ModelCatalogue()
	}
}
