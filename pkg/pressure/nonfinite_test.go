package pressure

import (
	"math"
	"strings"
	"testing"
)

func TestParseValueRejectsNonFiniteNumericValues(t *testing.T) {
	tests := []struct {
		name     string
		value    float64
		wantKind string
	}{
		{name: "NaN", value: math.NaN(), wantKind: "NaN"},
		{name: "positive infinity", value: math.Inf(1), wantKind: "positive infinity"},
		{name: "negative infinity", value: math.Inf(-1), wantKind: "negative infinity"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			value, err := parseValue(tt.value)
			if err == nil {
				t.Fatalf("parseValue(%s) = %v, want a non-finite error", tt.name, value)
			}
			if !strings.Contains(err.Error(), tt.wantKind) {
				t.Errorf("error = %q, want value kind %q", err, tt.wantKind)
			}
		})
	}
}

func TestParseValueKeepsLargeFiniteValueFinite(t *testing.T) {
	value, err := parseValue(math.MaxFloat64)
	if err != nil {
		t.Fatalf("parseValue(MaxFloat64) error = %v", err)
	}
	if math.IsNaN(value) || math.IsInf(value, 0) {
		t.Fatalf("parseValue(MaxFloat64) = %v, want a finite value", value)
	}
}

func TestParseValueKeepsFiniteValuesOutsidePercentageRange(t *testing.T) {
	for _, value := range []float64{-20.04, 0, 150.04} {
		parsed, err := parseValue(value)
		if err != nil {
			t.Errorf("parseValue(%v) error = %v", value, err)
			continue
		}
		if math.IsNaN(parsed) || math.IsInf(parsed, 0) {
			t.Errorf("parseValue(%v) = %v, want a finite value", value, parsed)
		}
	}
}
