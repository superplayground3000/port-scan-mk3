package input

import (
	"fmt"
	"strings"
	"testing"
)

func BenchmarkLoadCIDRsTenThousandRows(b *testing.B) {
	var csv strings.Builder
	csv.WriteString("ip,ip_cidr\n")
	for i := 0; i < 10_000; i++ {
		ip := fmt.Sprintf("10.%d.%d.%d", (i>>16)&255, (i>>8)&255, i&255)
		fmt.Fprintf(&csv, "%s,%s/32\n", ip, ip)
	}
	data := csv.String()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := LoadCIDRs(strings.NewReader(data)); err != nil {
			b.Fatal(err)
		}
	}
}
