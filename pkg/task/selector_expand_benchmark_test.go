package task

import "testing"

func BenchmarkExpandIPSelectorsSlash16(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := ExpandIPSelectors([]string{"10.42.0.0/16"}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEstimateIPSelectorsDefaultRejection(b *testing.B) {
	input := []SelectorInput{{Row: 1, Selector: "10.0.0.0/8"}}
	limits := DefaultExpansionLimits()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := EstimateIPSelectors(input, limits); err == nil {
			b.Fatal("EstimateIPSelectors(/8) error = nil")
		}
	}
}
