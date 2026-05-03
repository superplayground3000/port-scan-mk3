package scanapp

import (
	"fmt"
	"os"

	"github.com/xuxiping/port-scan-mk3/pkg/config"
	"github.com/xuxiping/port-scan-mk3/pkg/input"
)

type runInputs struct {
	cidrRecords []input.CIDRRecord
	portSpecs   []input.PortSpec
}

func loadRunInputs(cfg config.Config, deps runDependencies) (runInputs, error) {
	cidrRecords, err := deps.loadCIDRRecords(cfg.CIDRFile, cfg.CIDRIPCol, cfg.CIDRIPCidrCol)
	if err != nil {
		return runInputs{}, err
	}
	if cfg.PortFile == "" {
		if hasRichRecords(cidrRecords) {
			return runInputs{
				cidrRecords: cidrRecords,
				portSpecs:   nil,
			}, nil
		}
		return runInputs{}, fmt.Errorf("-port-file is required when cidr input is not rich mode")
	}
	portSpecs, err := deps.loadPortSpecs(cfg.PortFile)
	if err != nil {
		return runInputs{}, err
	}
	return runInputs{
		cidrRecords: cidrRecords,
		portSpecs:   portSpecs,
	}, nil
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
