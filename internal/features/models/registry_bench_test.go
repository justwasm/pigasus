package models

import "testing"

func BenchmarkRegistry_List(b *testing.B) {
	reg := &Registry{
		providers: map[string]ProviderConfig{
			"p1": {
				ID:   "p1",
				Type: "openai",
				Models: []ProviderModel{
					{ID: "m1", Name: "Model 1"},
					{ID: "m2", Name: "Model 2"},
				},
			},
			"p2": {
				ID:   "p2",
				Type: "anthropic",
				Models: []ProviderModel{
					{ID: "m3", Name: "Model 3"},
					{ID: "m4", Name: "Model 4"},
				},
			},
		},
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = reg.List()
	}
}
