// Package validate provides input validation for the validate command of the
// port scanner. It makes sure that the CIDR and port input files are readable,
// that their format is correct, and that the required fields are present.
// Rich-mode inputs do not require a port file.
package validate

import (
	"context"
	"fmt"

	"github.com/xuxiping/port-scan-mk3/pkg/config"
	"github.com/xuxiping/port-scan-mk3/pkg/input"
	"github.com/xuxiping/port-scan-mk3/pkg/task"
)

// Configuration supplies verified values to the validate workflow.
type Configuration interface {
	Resolve() (config.ValidateValues, error)
}

type targetExpansionConfiguration interface {
	ResolveTargetExpansion() (config.TargetExpansionValues, error)
}

type resourceLimitConfiguration interface {
	ResolveResourceLimits() (config.ValidateResourceLimits, error)
}

// Result is the outcome of input validation. Valid is true when all inputs are
// acceptable. Detail describes the outcome, and it holds an error message when
// Valid is false.
type Result struct {
	// Valid is true when all input files pass validation.
	Valid bool
	// Detail is "ok" on success or a descriptive error message on failure.
	Detail string
}

// Inputs validates the CIDR and port input files that a configuration names.
// Inputs does not scan the network. It only makes sure that the files are
// accessible and that their format is correct.
//
// # Parameters
//
//	cfg: Configuration that resolves the input paths and column names.
//
// # Returns
//
//	A Result that reports whether the inputs are valid. On failure the Result
//	holds a detail message.
//
// # Validation Rules
//
//   - The CIDR file must exist and must be readable as CSV.
//   - The CIDR file must have the required columns, or it must be in valid
//     rich-mode format.
//   - In basic mode (not rich mode), a port file is required and must be
//     readable.
//
// # Example
//
//	cfg, _ := config.ParseValidate(os.Args[1:])
//	result := validate.Inputs(cfg)
//	if !result.Valid {
//	    fmt.Println("validation failed:", result.Detail)
//	}
func Inputs(cfg Configuration) Result {
	values, err := cfg.Resolve()
	if err != nil {
		return Result{Valid: false, Detail: fmt.Sprintf("resolve validate configuration: %v", err)}
	}
	resourceLimits := config.ValidateResourceLimits{
		CIDR: input.DefaultCIDRLimits(values.CIDRFile),
		Port: input.DefaultPortLimits(values.PortFile),
	}
	if resolver, ok := cfg.(resourceLimitConfiguration); ok {
		resolved, resolveErr := resolver.ResolveResourceLimits()
		if resolveErr != nil {
			return Result{Valid: false, Detail: fmt.Sprintf("resolve resource limits: %v", resolveErr)}
		}
		resourceLimits = resolved
	}
	cidrRecords, err := input.LoadCIDRsFileWithColumnsContext(context.Background(), values.CIDRFile, values.CIDRIPCol, values.CIDRIPCidrCol, resourceLimits.CIDR)
	if err != nil {
		return Result{Valid: false, Detail: err.Error()}
	}
	limits := task.DefaultExpansionLimits()
	if resolver, ok := cfg.(targetExpansionConfiguration); ok {
		expansion, resolveErr := resolver.ResolveTargetExpansion()
		if resolveErr != nil {
			return Result{Valid: false, Detail: fmt.Sprintf("resolve target expansion limits: %v", resolveErr)}
		}
		limits = expansion.Limits
	}
	if _, err := task.EstimateAuthorizedCIDRRecords(cidrRecords, limits, nil); err != nil {
		return Result{Valid: false, Detail: err.Error()}
	}
	if values.PortFile == "" {
		for _, rec := range cidrRecords {
			if rec.IsRich {
				return Result{Valid: true, Detail: "ok"}
			}
		}
		return Result{Valid: false, Detail: "-port-file is required when cidr input is not rich mode"}
	}

	if _, err := input.LoadPortsFileContext(context.Background(), values.PortFile, resourceLimits.Port); err != nil {
		return Result{Valid: false, Detail: err.Error()}
	}

	return Result{Valid: true, Detail: "ok"}
}
