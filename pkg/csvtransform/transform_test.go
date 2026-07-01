package csvtransform

import (
	"bytes"
	"testing"
)

func TestSplitPorts(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    []int
		wantErr bool
	}{
		{"single port", "80", []int{80}, false},
		{"two ports", "80/443", []int{80, 443}, false},
		{"four ports", "22/80/443/8080", []int{22, 80, 443, 8080}, false},
		{"empty string", "", nil, false},             // skip
		{"invalid port abc", "abc", nil, false},      // skip, log to warn
		{"mixed invalid port", "80/abc", nil, false}, // skip
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SplitPorts(tt.input, &bytes.Buffer{})
			if (err != nil) != tt.wantErr {
				t.Errorf("SplitPorts(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if len(got) != len(tt.want) {
				t.Errorf("SplitPorts(%q) = %v, want %v", tt.input, got, tt.want)
				return
			}
			for i, v := range got {
				if v != tt.want[i] {
					t.Errorf("SplitPorts(%q) = %v, want %v", tt.input, got, tt.want)
					return
				}
			}
		})
	}
}

func TestResolveHost(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{"IPv4 passthrough", "192.168.1.1", "192.168.1.1", false},
		{"IPv4 passthrough 8.8.8.8", "8.8.8.8", "8.8.8.8", false},
		{"hostname fallback", "nonexistent.invalid", "nonexistent.invalid", false}, // lookup fails, returns original
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveHost(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ResolveHost(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("ResolveHost(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestShouldIncludeRow(t *testing.T) {
	tests := []struct {
		name string
		pass string
		want bool
	}{
		{"TRUE is false", "TRUE", false},
		{"true is false", "true", false},
		{"TRUE with spaces is false", "TRUE ", false},
		{"FALSE is true", "FALSE", true},
		{"false is true", "false", true},
		{"FALSE with spaces is true", "FALSE ", true},
		{"PASS is false", "PASS", false},
		{"empty is false", "", false},
		{"UNKNOWN is false", "UNKNOWN", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ShouldIncludeRow(tt.pass)
			if got != tt.want {
				t.Errorf("ShouldIncludeRow(%q) = %v, want %v", tt.pass, got, tt.want)
			}
		})
	}
}
