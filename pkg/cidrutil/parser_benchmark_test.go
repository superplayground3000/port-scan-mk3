package cidrutil

import (
	"fmt"
	"strings"
	"testing"
)

const benchmarkCSVRecordCount = 100_000

var (
	benchmarkDenyCSV = makeBenchmarkCSV("dst_network_segment,decision", "deny")
	benchmarkOpenCSV = makeBenchmarkCSV("segment,status", "open")
)

func makeBenchmarkCSV(header string, filter string) string {
	var content strings.Builder
	content.Grow(3_000_000)
	content.WriteString(header)
	content.WriteByte('\n')
	for index := range benchmarkCSVRecordCount {
		fmt.Fprintf(
			&content,
			"10.%d.%d.%d/32,%s\n",
			(index>>16)&0xff,
			(index>>8)&0xff,
			index&0xff,
			filter,
		)
	}
	return content.String()
}

func BenchmarkParseDenyCSV100K(b *testing.B) {
	for b.Loop() {
		if _, err := ParseDenyCSV(benchmarkDenyCSV); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDenyCSVReader100K(b *testing.B) {
	for b.Loop() {
		reader := NewDenyCSVReader(strings.NewReader(benchmarkDenyCSV))
		if _, err := reader.ReadAll(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParseOpenCSV100K(b *testing.B) {
	for b.Loop() {
		if _, err := ParseOpenCSV(benchmarkOpenCSV); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkOpenCSVReader100K(b *testing.B) {
	for b.Loop() {
		reader := NewOpenCSVReader(strings.NewReader(benchmarkOpenCSV))
		if _, err := reader.ReadAll(); err != nil {
			b.Fatal(err)
		}
	}
}
