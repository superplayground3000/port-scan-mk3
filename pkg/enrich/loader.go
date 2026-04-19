package enrich

import (
	"encoding/csv"
	"fmt"
	"io"
	"log"
	"strconv"
	"strings"

	"github.com/xuxiping/port-scan-mk3/pkg/cidrutil"
	"github.com/xuxiping/port-scan-mk3/pkg/preprocesscfg"
)

// LoadServiceMap reads a CSV with port and service_label columns and returns
// a map from port number to service label. Rows with invalid port numbers are
// skipped with a log warning.
func LoadServiceMap(r io.Reader) (map[int]string, error) {
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
			log.Printf("service map row %d: too few columns, skipping", i+1)
			continue
		}
		port, err := strconv.Atoi(strings.TrimSpace(row[portIdx]))
		if err != nil {
			log.Printf("service map row %d: invalid port %q, skipping", i+1, row[portIdx])
			continue
		}
		m[port] = strings.TrimSpace(row[labelIdx])
	}
	return m, nil
}

// LoadCIDRList reads a CSV listing CIDRs and returns them as an IntervalTree.
// The first column is used as the CIDR value. If the first row is not a valid
// CIDR it is treated as a header and skipped. Malformed CIDRs are skipped with
// a log warning.
func LoadCIDRList(r io.Reader) (*cidrutil.IntervalTree, error) {
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
			log.Printf("CIDR list row %d: invalid CIDR %q, skipping", i+1, raw)
			continue
		}
		tree.Insert(entry)
	}
	return tree, nil
}
