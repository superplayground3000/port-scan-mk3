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
