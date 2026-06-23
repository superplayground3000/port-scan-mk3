// Package validate provides input validation for the port scanner's validate command.
// It checks that CIDR and port input files are readable, correctly formatted, and
// that required fields are present. Rich-mode inputs do not require a port file.
package validate

import (
	"fmt"
	"os"

	"github.com/xuxiping/port-scan-mk3/pkg/config"
	"github.com/xuxiping/port-scan-mk3/pkg/input"
)

// Result is the outcome of input validation. Valid is true when all inputs are
// acceptable; Detail describes the outcome (an error message if Valid is false).
type Result struct {
	// Valid is true when all input files pass validation.
	Valid bool
	// Detail is "ok" on success or a descriptive error message on failure.
	Detail string
}

// Inputs validates the CIDR and port input files referenced by a config.
// It does not perform network scanning — only file accessibility and format checks.
//
// # Parameters
//
//	cfg: Config with CIDRFile, CIDRIPCol, CIDRIPCidrCol, and PortFile fields.
//
// # Returns
//
//	Result indicating whether inputs are valid, with a detail message on failure.
//
// # Validation Rules
//
//	- CIDR file must exist and be readable as CSV.
//	- CIDR file must have the required columns (or be valid rich-mode format).
//	- In basic mode (non-rich), a port file is required and must be readable.
//
// # Example
//
//	cfg, _ := config.Parse(os.Args[1:])
//	result := validate.Inputs(cfg)
//	if !result.Valid {
//	    fmt.Println("validation failed:", result.Detail)
//	}
func Inputs(cfg config.Config) Result {
	cidrFile, err := os.Open(cfg.CIDRFile)
	if err != nil {
		return Result{Valid: false, Detail: fmt.Sprintf("failed to open cidr file: %v", err)}
	}
	defer cidrFile.Close()

	cidrRecords, err := input.LoadCIDRsWithColumns(cidrFile, cfg.CIDRIPCol, cfg.CIDRIPCidrCol)
	if err != nil {
		return Result{Valid: false, Detail: err.Error()}
	}
	if cfg.PortFile == "" {
		for _, rec := range cidrRecords {
			if rec.IsRich {
				return Result{Valid: true, Detail: "ok"}
			}
		}
		return Result{Valid: false, Detail: "-port-file is required when cidr input is not rich mode"}
	}

	portFile, err := os.Open(cfg.PortFile)
	if err != nil {
		return Result{Valid: false, Detail: fmt.Sprintf("failed to open port file: %v", err)}
	}
	defer portFile.Close()

	if _, err := input.LoadPorts(portFile); err != nil {
		return Result{Valid: false, Detail: err.Error()}
	}

	return Result{Valid: true, Detail: "ok"}
}
