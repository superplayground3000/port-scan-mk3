package cidrutil

import (
	"encoding/csv"
	"io"
	"strings"
)

// ParseDenyCSV parses file1 CSV content and returns only deny CIDR entries.
func ParseDenyCSV(content string) ([]CIDREntry, error) {
	r := csv.NewReader(strings.NewReader(content))
	records, err := r.ReadAll()
	if err != nil {
		return nil, err
	}
	var entries []CIDREntry
	for i, rec := range records {
		if i == 0 { // header
			continue
		}
		if len(rec) < 2 {
			continue
		}
		cidr := strings.TrimSpace(rec[0])
		decision := strings.TrimSpace(rec[1])
		if decision == "deny" {
			entry, err := ParseCIDR(cidr)
			if err != nil {
				continue // skip invalid
			}
			entries = append(entries, entry)
		}
	}
	return entries, nil
}

// ParseOpenCSV parses file2 CSV content and returns only open CIDR entries.
func ParseOpenCSV(content string) ([]CIDREntry, error) {
	r := csv.NewReader(strings.NewReader(content))
	records, err := r.ReadAll()
	if err != nil {
		return nil, err
	}
	var entries []CIDREntry
	for i, rec := range records {
		if i == 0 { // header
			continue
		}
		if len(rec) < 2 {
			continue
		}
		cidr := strings.TrimSpace(rec[0])
		status := strings.TrimSpace(rec[1])
		if status == "open" {
			entry, err := ParseCIDR(cidr)
			if err != nil {
				continue // skip invalid
			}
			entries = append(entries, entry)
		}
	}
	return entries, nil
}

// DenyCSVReader provides streaming CSV parsing for deny entries.
type DenyCSVReader struct {
	r *csv.Reader
}

// NewDenyCSVReader creates a new DenyCSVReader from an io.Reader.
func NewDenyCSVReader(r io.Reader) *DenyCSVReader {
	return &DenyCSVReader{
		r: csv.NewReader(r),
	}
}

// ReadAll reads all deny entries from the CSV stream.
func (dr *DenyCSVReader) ReadAll() ([]CIDREntry, error) {
	var entries []CIDREntry
	for {
		rec, err := dr.r.Read()
		if err != nil {
			break
		}
		if len(rec) < 2 {
			continue
		}
		cidr := strings.TrimSpace(rec[0])
		decision := strings.TrimSpace(rec[1])
		if decision == "deny" {
			entry, err := ParseCIDR(cidr)
			if err != nil {
				continue
			}
			entries = append(entries, entry)
		}
	}
	return entries, nil
}

// OpenCSVReader provides streaming CSV parsing for open entries.
type OpenCSVReader struct {
	r *csv.Reader
}

// NewOpenCSVReader creates a new OpenCSVReader from an io.Reader.
func NewOpenCSVReader(r io.Reader) *OpenCSVReader {
	return &OpenCSVReader{
		r: csv.NewReader(r),
	}
}

// ReadAll reads all open entries from the CSV stream.
func (dr *OpenCSVReader) ReadAll() ([]CIDREntry, error) {
	var entries []CIDREntry
	for {
		rec, err := dr.r.Read()
		if err != nil {
			break
		}
		if len(rec) < 2 {
			continue
		}
		cidr := strings.TrimSpace(rec[0])
		status := strings.TrimSpace(rec[1])
		if status == "open" {
			entry, err := ParseCIDR(cidr)
			if err != nil {
				continue
			}
			entries = append(entries, entry)
		}
	}
	return entries, nil
}
