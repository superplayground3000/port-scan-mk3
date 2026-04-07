package cidrutil

import (
	"bufio"
	"encoding/csv"
	"io"
	"log"
	"strings"
)

// findColumnIndex returns the index of a column by name, or -1 if not found.
func findColumnIndex(headers []string, name string) int {
	for i, h := range headers {
		if strings.TrimSpace(h) == name {
			return i
		}
	}
	return -1
}

// ParseDenyCSV parses file1 CSV content and returns only deny CIDR entries.
func ParseDenyCSV(content string) ([]CIDREntry, error) {
	r := bufio.NewScanner(strings.NewReader(content))
	lineNum := 0
	var entries []CIDREntry
	var headers []string

	// Parse header line first
	if !r.Scan() {
		return nil, r.Err()
	}
	headerLine := r.Text()
	lineNum++
	rec, err := csv.NewReader(strings.NewReader(headerLine)).Read()
	if err != nil {
		return nil, err
	}
	headers = rec

	// Compute column indices once with fallback
	cidrIdx := findColumnIndex(headers, "dst_network_segment")
	decisionIdx := findColumnIndex(headers, "decision")
	if cidrIdx < 0 || decisionIdx < 0 {
		cidrIdx = 0
		decisionIdx = 1
	}

	for r.Scan() {
		line := r.Text()
		lineNum++
		rec, err := csv.NewReader(strings.NewReader(line)).Read()
		if err != nil {
			log.Printf("warning: skipping malformed line %d: %v", lineNum, err)
			continue
		}
		if len(rec) < 2 {
			continue
		}
		cidr := strings.TrimSpace(rec[cidrIdx])
		decision := strings.TrimSpace(rec[decisionIdx])
		if decision == "deny" {
			entry, err := ParseCIDR(cidr)
			if err != nil {
				log.Printf("warning: skipping invalid CIDR %q at line %d: %v", cidr, lineNum, err)
				continue
			}
			entries = append(entries, entry)
		}
	}
	if err := r.Err(); err != nil {
		return nil, err
	}
	return entries, nil
}

// ParseOpenCSV parses file2 CSV content and returns only open CIDR entries.
func ParseOpenCSV(content string) ([]CIDREntry, error) {
	r := bufio.NewScanner(strings.NewReader(content))
	lineNum := 0
	var entries []CIDREntry
	var headers []string

	// Parse header line first
	if !r.Scan() {
		return nil, r.Err()
	}
	headerLine := r.Text()
	lineNum++
	rec, err := csv.NewReader(strings.NewReader(headerLine)).Read()
	if err != nil {
		return nil, err
	}
	headers = rec

	// Compute column indices once with fallback
	cidrIdx := findColumnIndex(headers, "dst_network_segment")
	statusIdx := findColumnIndex(headers, "status")
	if cidrIdx < 0 || statusIdx < 0 {
		cidrIdx = 0
		statusIdx = 1
	}

	for r.Scan() {
		line := r.Text()
		lineNum++
		rec, err := csv.NewReader(strings.NewReader(line)).Read()
		if err != nil {
			log.Printf("warning: skipping malformed line %d: %v", lineNum, err)
			continue
		}
		if len(rec) < 2 {
			continue
		}
		cidr := strings.TrimSpace(rec[cidrIdx])
		status := strings.TrimSpace(rec[statusIdx])
		if status == "open" {
			entry, err := ParseCIDR(cidr)
			if err != nil {
				log.Printf("warning: skipping invalid CIDR %q at line %d: %v", cidr, lineNum, err)
				continue
			}
			entries = append(entries, entry)
		}
	}
	if err := r.Err(); err != nil {
		return nil, err
	}
	return entries, nil
}

// DenyCSVReader provides streaming CSV parsing for deny entries using bufio.Scanner.
type DenyCSVReader struct {
	scanner *bufio.Scanner
}

// NewDenyCSVReader creates a new DenyCSVReader from an io.Reader.
func NewDenyCSVReader(r io.Reader) *DenyCSVReader {
	scanner := bufio.NewScanner(r)
	// Increase buffer for large lines if needed
	const maxScanTokenSize = 1024 * 1024 // 1MB
	scanner.Buffer(make([]byte, 64*1024), maxScanTokenSize)
	return &DenyCSVReader{
		scanner: scanner,
	}
}

// ReadAll reads all deny entries from the CSV stream.
func (dr *DenyCSVReader) ReadAll() ([]CIDREntry, error) {
	var entries []CIDREntry
	lineNum := 0
	var headers []string

	// Parse header line first
	if !dr.scanner.Scan() {
		return nil, dr.scanner.Err()
	}
	headerLine := dr.scanner.Text()
	lineNum++
	rec, err := csv.NewReader(strings.NewReader(headerLine)).Read()
	if err != nil {
		return nil, err
	}
	headers = rec

	// Compute column indices once with fallback
	cidrIdx := findColumnIndex(headers, "dst_network_segment")
	decisionIdx := findColumnIndex(headers, "decision")
	if cidrIdx < 0 || decisionIdx < 0 {
		cidrIdx = 0
		decisionIdx = 1
	}

	for dr.scanner.Scan() {
		line := dr.scanner.Text()
		lineNum++
		rec, err := csv.NewReader(strings.NewReader(line)).Read()
		if err != nil {
			log.Printf("warning: skipping malformed line %d: %v", lineNum, err)
			continue
		}
		if len(rec) < 2 {
			continue
		}
		cidr := strings.TrimSpace(rec[cidrIdx])
		decision := strings.TrimSpace(rec[decisionIdx])
		if decision == "deny" {
			entry, err := ParseCIDR(cidr)
			if err != nil {
				log.Printf("warning: skipping invalid CIDR %q at line %d: %v", cidr, lineNum, err)
				continue
			}
			entries = append(entries, entry)
		}
	}
	return entries, dr.scanner.Err()
}

// OpenCSVReader provides streaming CSV parsing for open entries using bufio.Scanner.
type OpenCSVReader struct {
	scanner *bufio.Scanner
}

// NewOpenCSVReader creates a new OpenCSVReader from an io.Reader.
func NewOpenCSVReader(r io.Reader) *OpenCSVReader {
	scanner := bufio.NewScanner(r)
	// Increase buffer for large lines if needed
	const maxScanTokenSize = 1024 * 1024 // 1MB
	scanner.Buffer(make([]byte, 64*1024), maxScanTokenSize)
	return &OpenCSVReader{
		scanner: scanner,
	}
}

// ReadAll reads all open entries from the CSV stream.
func (dr *OpenCSVReader) ReadAll() ([]CIDREntry, error) {
	var entries []CIDREntry
	lineNum := 0
	var headers []string

	// Parse header line first
	if !dr.scanner.Scan() {
		return nil, dr.scanner.Err()
	}
	headerLine := dr.scanner.Text()
	lineNum++
	rec, err := csv.NewReader(strings.NewReader(headerLine)).Read()
	if err != nil {
		return nil, err
	}
	headers = rec

	// Compute column indices once with fallback
	cidrIdx := findColumnIndex(headers, "dst_network_segment")
	statusIdx := findColumnIndex(headers, "status")
	if cidrIdx < 0 || statusIdx < 0 {
		cidrIdx = 0
		statusIdx = 1
	}

	for dr.scanner.Scan() {
		line := dr.scanner.Text()
		lineNum++
		rec, err := csv.NewReader(strings.NewReader(line)).Read()
		if err != nil {
			log.Printf("warning: skipping malformed line %d: %v", lineNum, err)
			continue
		}
		if len(rec) < 2 {
			continue
		}
		cidr := strings.TrimSpace(rec[cidrIdx])
		status := strings.TrimSpace(rec[statusIdx])
		if status == "open" {
			entry, err := ParseCIDR(cidr)
			if err != nil {
				log.Printf("warning: skipping invalid CIDR %q at line %d: %v", cidr, lineNum, err)
				continue
			}
			entries = append(entries, entry)
		}
	}
	return entries, dr.scanner.Err()
}
