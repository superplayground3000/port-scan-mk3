package scanapp

import (
	"context"
	"fmt"
	"os"

	"github.com/xuxiping/port-scan-mk3/pkg/input"
)

type runInputs struct {
	cidrRecords []input.CIDRRecord
	portSpecs   []input.PortSpec
}

type inputConfiguration struct {
	cidrFile         string
	cidrIPCol        string
	cidrIPCidrCol    string
	portFile         string
	allowMissingPort bool
	cidrLimits       input.CIDRLimits
	portLimits       input.PortLimits
}

func loadRunInputs(cfg inputConfiguration, deps runDependencies) (runInputs, error) {
	return loadRunInputsContext(context.Background(), cfg, deps)
}

func loadRunInputsContext(ctx context.Context, cfg inputConfiguration, deps runDependencies) (runInputs, error) {
	var (
		cidrRecords []input.CIDRRecord
		err         error
	)
	if deps.loadCIDRRecordsWithLimitsContext != nil {
		cidrRecords, err = deps.loadCIDRRecordsWithLimitsContext(ctx, cfg.cidrFile, cfg.cidrIPCol, cfg.cidrIPCidrCol, cfg.cidrLimits)
	} else if deps.loadCIDRRecordsContext != nil {
		cidrRecords, err = deps.loadCIDRRecordsContext(ctx, cfg.cidrFile, cfg.cidrIPCol, cfg.cidrIPCidrCol)
	} else {
		if err := ctx.Err(); err != nil {
			return runInputs{}, err
		}
		cidrRecords, err = deps.loadCIDRRecords(cfg.cidrFile, cfg.cidrIPCol, cfg.cidrIPCidrCol)
		if err == nil {
			err = ctx.Err()
		}
	}
	if err != nil {
		return runInputs{}, err
	}
	if cfg.portFile == "" {
		// Rich input carries its port per record. When resuming, the bucket's
		// chunks already carry the ports, so scan needs no -port-file either;
		// only a fresh basic build (e.g. generate-buckets) requires it.
		if hasRichRecords(cidrRecords) || cfg.allowMissingPort {
			return runInputs{
				cidrRecords: cidrRecords,
				portSpecs:   nil,
			}, nil
		}
		return runInputs{}, fmt.Errorf("-port-file is required when cidr input is not rich mode")
	}
	var portSpecs []input.PortSpec
	if deps.loadPortSpecsWithLimitsContext != nil {
		portSpecs, err = deps.loadPortSpecsWithLimitsContext(ctx, cfg.portFile, cfg.portLimits)
	} else if deps.loadPortSpecsContext != nil {
		portSpecs, err = deps.loadPortSpecsContext(ctx, cfg.portFile)
	} else {
		if err := ctx.Err(); err != nil {
			return runInputs{}, err
		}
		portSpecs, err = deps.loadPortSpecs(cfg.portFile)
		if err == nil {
			err = ctx.Err()
		}
	}
	if err != nil {
		return runInputs{}, err
	}
	return runInputs{
		cidrRecords: cidrRecords,
		portSpecs:   portSpecs,
	}, nil
}

// loadPrePingInputs loads only the CIDR records needed by the pre-scan ping
// phase. Pre-ping is per-IP and never uses ports, so — unlike loadRunInputs — it
// does not require a -port-file for basic (non-rich) input. portSpecs is always
// nil in the result.
func loadPrePingInputs(cidrFile, cidrIPCol, cidrIPCidrCol string, deps runDependencies) (runInputs, error) {
	cidrRecords, err := deps.loadCIDRRecords(cidrFile, cidrIPCol, cidrIPCidrCol)
	if err != nil {
		return runInputs{}, err
	}
	return runInputs{cidrRecords: cidrRecords, portSpecs: nil}, nil
}

func loadPrePingInputsContext(ctx context.Context, cidrFile, cidrIPCol, cidrIPCidrCol string, limits input.CIDRLimits, deps runDependencies) (runInputs, error) {
	if deps.loadCIDRRecordsWithLimitsContext != nil {
		cidrRecords, err := deps.loadCIDRRecordsWithLimitsContext(ctx, cidrFile, cidrIPCol, cidrIPCidrCol, limits)
		if err != nil {
			return runInputs{}, err
		}
		return runInputs{cidrRecords: cidrRecords}, nil
	}
	return loadPrePingInputs(cidrFile, cidrIPCol, cidrIPCidrCol, deps)
}

func readCIDRFile(path, ipCol, ipCidrCol string) ([]input.CIDRRecord, error) {
	return readCIDRFileContext(context.Background(), path, ipCol, ipCidrCol)
}

func readCIDRFileContext(ctx context.Context, path, ipCol, ipCidrCol string) ([]input.CIDRRecord, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return input.LoadCIDRsWithColumnsContext(ctx, f, ipCol, ipCidrCol)
}

func readPortFile(path string) ([]input.PortSpec, error) {
	return readPortFileContext(context.Background(), path)
}

func readPortFileContext(ctx context.Context, path string) ([]input.PortSpec, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return input.LoadPortsContext(ctx, f)
}
