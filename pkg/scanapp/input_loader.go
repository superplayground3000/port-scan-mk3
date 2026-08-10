package scanapp

import (
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
}

func loadRunInputs(cfg inputConfiguration, deps runDependencies) (runInputs, error) {
	cidrRecords, err := deps.loadCIDRRecords(cfg.cidrFile, cfg.cidrIPCol, cfg.cidrIPCidrCol)
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
	portSpecs, err := deps.loadPortSpecs(cfg.portFile)
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

func readCIDRFile(path, ipCol, ipCidrCol string) ([]input.CIDRRecord, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return input.LoadCIDRsWithColumns(f, ipCol, ipCidrCol)
}

func readPortFile(path string) ([]input.PortSpec, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return input.LoadPorts(f)
}
