// Package cli provides CLI composition utilities for the port-scan command.
// It bridges domain types to concrete writers and formats. This split obeys the
// SOLID principle that domain packages must not depend on transport details.
//
// # Responsibilities
//
//   - Output formatting (human vs JSON) for validation results
//   - RecordWriter adapters that bridge writer.CSVWriter → scanapp.RecordWriter
package cli

import (
	"encoding/json"
	"fmt"
	"io"
)

// WriteValidation writes the result of a validation check to the output writer,
// in human-readable format or in JSON format. The output keeps the public output
// contract of the validate command.
//
// # Parameters
//
//	out:     Output destination, for example os.Stdout.
//	format:  "human" for line-oriented output. "json" for structured JSON.
//	valid:   true when the validation passed, false when it failed.
//	detail:  Human-readable detail string, for example an error message or "ok".
//
// # Returns
//
//	nil on success. An error when the write to the output stream fails.
//
// # Example
//
//	err := cli.WriteValidation(os.Stdout, "human", false, "cidr csv missing required ip column")
func WriteValidation(out io.Writer, format string, valid bool, detail string) error {
	if format == "json" {
		return json.NewEncoder(out).Encode(map[string]any{
			"valid":  valid,
			"detail": detail,
		})
	}
	_, err := fmt.Fprintf(out, "valid=%t detail=%s\n", valid, detail)
	return err
}
