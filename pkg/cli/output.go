// Package cli provides CLI composition utilities for the port-scan command.
// It bridges domain types to concrete writers and formats, following the SOLID
// principle that domain packages must not depend on transport details.
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

// WriteValidation writes the result of a validation check to the output writer
// in either human-readable or JSON format, preserving the validate command's
// public output contract.
//
// # Parameters
//
//	out:     Output destination (e.g., os.Stdout).
//	format:  "human" for line-oriented output; "json" for structured JSON.
//	valid:   true if validation passed, false otherwise.
//	detail:  Human-readable detail string (e.g., error message or "ok").
//
// # Returns
//
//	nil on success; error if writing to the output stream fails.
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
