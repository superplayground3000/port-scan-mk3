package enrich

import (
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/xuxiping/port-scan-mk3/pkg/cidrutil"
	"github.com/xuxiping/port-scan-mk3/pkg/preprocesscfg"
)

// LoadServiceMap reads a CSV file with port and service_label columns. It
// returns a map from port number to service label. If a row has an invalid port
// number, LoadServiceMap skips the row and writes a warning to warn.
func LoadServiceMap(r io.Reader, warn io.Writer) (map[int]string, error) {
	if warn == nil {
		warn = io.Discard
	}
	cr := csv.NewReader(r)
	rows, err := cr.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("reading service map CSV: %w", err)
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("service map CSV is empty")
	}

	header := rows[0]
	portIdx, labelIdx := -1, -1
	for i, col := range header {
		switch strings.TrimSpace(strings.ToLower(col)) {
		case strings.ToLower(preprocesscfg.ColServicePort):
			portIdx = i
		case strings.ToLower(preprocesscfg.ColServiceName):
			labelIdx = i
		}
	}
	if portIdx < 0 || labelIdx < 0 {
		return nil, fmt.Errorf("service map CSV missing required columns %q and %q",
			preprocesscfg.ColServicePort, preprocesscfg.ColServiceName)
	}

	m := make(map[int]string)
	for i := 1; i < len(rows); i++ {
		row := rows[i]
		if len(row) <= portIdx || len(row) <= labelIdx {
			fmt.Fprintf(warn, "service map row %d: too few columns, skipping\n", i+1)
			continue
		}
		port, err := strconv.Atoi(strings.TrimSpace(row[portIdx]))
		if err != nil {
			fmt.Fprintf(warn, "service map row %d: invalid port %q, skipping\n", i+1, row[portIdx])
			continue
		}
		m[port] = strings.TrimSpace(row[labelIdx])
	}
	return m, nil
}

// LoadCIDRList reads a CSV file of CIDRs and returns them as an IntervalTree.
// LoadCIDRList reads the CIDR value from the first column. If the first row is
// not a valid CIDR, LoadCIDRList treats the row as a header and skips it. If a
// CIDR is malformed, LoadCIDRList skips it and writes a warning to warn.
func LoadCIDRList(r io.Reader, warn io.Writer) (*cidrutil.IntervalTree, error) {
	if warn == nil {
		warn = io.Discard
	}
	cr := csv.NewReader(r)
	rows, err := cr.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("reading CIDR list CSV: %w", err)
	}

	tree := &cidrutil.IntervalTree{}
	start := 0
	if len(rows) > 0 {
		if _, parseErr := cidrutil.ParseCIDR(strings.TrimSpace(rows[0][0])); parseErr != nil {
			start = 1
		}
	}

	for i := start; i < len(rows); i++ {
		if len(rows[i]) == 0 {
			continue
		}
		raw := strings.TrimSpace(rows[i][0])
		if raw == "" {
			continue
		}
		entry, err := cidrutil.ParseCIDR(raw)
		if err != nil {
			fmt.Fprintf(warn, "CIDR list row %d: invalid CIDR %q, skipping\n", i+1, raw)
			continue
		}
		tree.Insert(entry)
	}
	return tree, nil
}
