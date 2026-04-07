package cidrutil

import (
	"strings"
	"testing"
)

func TestParseDenyCSV(t *testing.T) {
	content := "dst_network_segment,decision\n10.0.0.0/8,deny\n192.168.0.0/16,accept\n"
	entries, err := ParseDenyCSV(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 deny entry, got %d", len(entries))
	}
	if entries[0].Network != "10.0.0.0/8" {
		t.Errorf("expected 10.0.0.0/8, got %s", entries[0].Network)
	}
}

func TestParseOpenCSV(t *testing.T) {
	content := "dst_network_segment,status\n10.0.0.0/8,closed\n192.168.0.0/16,open\n172.16.0.0/12,open\n"
	entries, err := ParseOpenCSV(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 open entries, got %d", len(entries))
	}
	if entries[0].Network != "192.168.0.0/16" {
		t.Errorf("expected 192.168.0.0/16, got %s", entries[0].Network)
	}
	if entries[1].Network != "172.16.0.0/12" {
		t.Errorf("expected 172.16.0.0/12, got %s", entries[1].Network)
	}
}

func TestParseDenyCSVStreaming(t *testing.T) {
	// Verify streaming reader works with io.Reader interface
	content := "dst_network_segment,decision\n10.0.0.0/8,deny\n"
	reader := strings.NewReader(content)

	r := NewDenyCSVReader(reader)
	entries, err := r.ReadAll()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 deny entry, got %d", len(entries))
	}
	if entries[0].Network != "10.0.0.0/8" {
		t.Errorf("expected 10.0.0.0/8, got %s", entries[0].Network)
	}
}

func TestParseOpenCSVStreaming(t *testing.T) {
	// Verify streaming reader works with io.Reader interface for open entries
	content := "dst_network_segment,status\n192.168.0.0/16,open\n"
	reader := strings.NewReader(content)

	r := NewOpenCSVReader(reader)
	entries, err := r.ReadAll()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 open entry, got %d", len(entries))
	}
	if entries[0].Network != "192.168.0.0/16" {
		t.Errorf("expected 192.168.0.0/16, got %s", entries[0].Network)
	}
}

func TestParseDenyCSVMalformedLine(t *testing.T) {
	content := "dst_network_segment,decision\n\"unclosed quote\n"
	entries, err := ParseDenyCSV(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries for malformed CSV, got %d", len(entries))
	}
}

func TestParseDenyCSVShortLine(t *testing.T) {
	content := "dst_network_segment,decision\nonly-one-column\n"
	entries, err := ParseDenyCSV(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries for short line, got %d", len(entries))
	}
}

func TestParseDenyCSVInvalidCIDR(t *testing.T) {
	content := "dst_network_segment,decision\nnot-a-cidr,deny\n"
	entries, err := ParseDenyCSV(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries for invalid CIDR, got %d", len(entries))
	}
}

func TestParseOpenCSVMalformedLine(t *testing.T) {
	content := "segment,status\n\"unclosed quote\n"
	entries, err := ParseOpenCSV(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries for malformed CSV, got %d", len(entries))
	}
}

func TestParseOpenCSVShortLine(t *testing.T) {
	content := "segment,status\nonly-one\n"
	entries, err := ParseOpenCSV(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries for short line, got %d", len(entries))
	}
}

func TestParseOpenCSVInvalidCIDR(t *testing.T) {
	content := "segment,status\nnot-valid,open\n"
	entries, err := ParseOpenCSV(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries for invalid CIDR, got %d", len(entries))
	}
}
