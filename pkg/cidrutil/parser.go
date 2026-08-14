package cidrutil

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strings"
)

type csvPolicy struct {
	role         string
	cidrHeader   string
	filterHeader string
	filterValue  string
}

var (
	denyCSVPolicy = csvPolicy{
		role:         "deny",
		cidrHeader:   "dst_network_segment",
		filterHeader: "decision",
		filterValue:  "deny",
	}
	openCSVPolicy = csvPolicy{
		role:         "open",
		cidrHeader:   "segment",
		filterHeader: "status",
		filterValue:  "open",
	}
)

// findColumnIndex returns the index of a named column, or -1 if it is absent.
func findColumnIndex(headers []string, name string) int {
	for i, header := range headers {
		if strings.TrimSpace(header) == name {
			return i
		}
	}
	return -1
}

func malformedCSVError(role string, recordNumber int, err error) error {
	var parseErr *csv.ParseError
	if errors.As(err, &parseErr) {
		return fmt.Errorf(
			"%s input record %d line %d column %d: malformed CSV: %w",
			role,
			recordNumber,
			parseErr.Line,
			parseErr.Column,
			err,
		)
	}
	return fmt.Errorf("%s input record %d: malformed CSV: %w", role, recordNumber, err)
}

func isWhitespaceOnlyRecord(record []string) bool {
	return len(record) == 1 && strings.TrimSpace(record[0]) == ""
}

func parseCSV(input io.Reader, policy csvPolicy) ([]CIDREntry, error) {
	reader := csv.NewReader(input)
	reader.FieldsPerRecord = -1

	var header []string
	for {
		var err error
		header, err = reader.Read()
		if err == io.EOF {
			return nil, fmt.Errorf("%s input missing header", policy.role)
		}
		if err != nil {
			return nil, malformedCSVError(policy.role, 1, err)
		}
		if !isWhitespaceOnlyRecord(header) {
			break
		}
	}

	cidrIndex := findColumnIndex(header, policy.cidrHeader)
	filterIndex := findColumnIndex(header, policy.filterHeader)
	if (cidrIndex >= 0) != (filterIndex >= 0) {
		presentIndex := cidrIndex
		if presentIndex < 0 {
			presentIndex = filterIndex
		}
		line, column := reader.FieldPos(presentIndex)
		return nil, fmt.Errorf(
			"%s input record 1 line %d column %d has partial official header: %q and %q must both be present",
			policy.role,
			line,
			column,
			policy.cidrHeader,
			policy.filterHeader,
		)
	}
	if cidrIndex < 0 {
		if len(header) < 2 {
			line, column := reader.FieldPos(0)
			return nil, fmt.Errorf(
				"%s input record 1 line %d column %d header requires at least 2 fields, got %d",
				policy.role,
				line,
				column,
				len(header),
			)
		}
		cidrIndex = 0
		filterIndex = 1
	}
	requiredFields := cidrIndex + 1
	if filterIndex >= cidrIndex {
		requiredFields = filterIndex + 1
	}

	recordNumber := 1
	var entries []CIDREntry
	for {
		record, err := reader.Read()
		if err == io.EOF {
			return entries, nil
		}
		if err != nil {
			return nil, malformedCSVError(policy.role, recordNumber+1, err)
		}
		if isWhitespaceOnlyRecord(record) {
			continue
		}
		recordNumber++
		if len(record) < requiredFields {
			line, column := reader.FieldPos(0)
			return nil, fmt.Errorf(
				"%s input record %d line %d column %d requires %d fields, got %d",
				policy.role,
				recordNumber,
				line,
				column,
				requiredFields,
				len(record),
			)
		}

		filter := strings.ToLower(strings.TrimSpace(record[filterIndex]))
		if filter != policy.filterValue {
			continue
		}
		entry, err := ParseCIDR(strings.TrimSpace(record[cidrIndex]))
		if err != nil {
			line, column := reader.FieldPos(cidrIndex)
			return nil, fmt.Errorf(
				"%s input record %d line %d column %d: invalid CIDR: %w",
				policy.role,
				recordNumber,
				line,
				column,
				err,
			)
		}
		entries = append(entries, entry)
	}
}

// ParseDenyCSV parses CSV content and returns the deny CIDR entries.
func ParseDenyCSV(content string) ([]CIDREntry, error) {
	return parseCSV(strings.NewReader(content), denyCSVPolicy)
}

// ParseOpenCSV parses CSV content and returns the open CIDR entries.
func ParseOpenCSV(content string) ([]CIDREntry, error) {
	return parseCSV(strings.NewReader(content), openCSVPolicy)
}

// DenyCSVReader reads deny entries from a CSV stream.
type DenyCSVReader struct {
	input io.Reader
}

// NewDenyCSVReader creates a deny CSV reader.
func NewDenyCSVReader(input io.Reader) *DenyCSVReader {
	return &DenyCSVReader{input: input}
}

// ReadAll reads all deny entries from the CSV stream.
func (reader *DenyCSVReader) ReadAll() ([]CIDREntry, error) {
	return parseCSV(reader.input, denyCSVPolicy)
}

// OpenCSVReader reads open entries from a CSV stream.
type OpenCSVReader struct {
	input io.Reader
}

// NewOpenCSVReader creates an open CSV reader.
func NewOpenCSVReader(input io.Reader) *OpenCSVReader {
	return &OpenCSVReader{input: input}
}

// ReadAll reads all open entries from the CSV stream.
func (reader *OpenCSVReader) ReadAll() ([]CIDREntry, error) {
	return parseCSV(reader.input, openCSVPolicy)
}
