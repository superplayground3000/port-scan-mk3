package preprocess

import (
	"encoding/csv"
	"fmt"
	"io"
	"log"
	"strings"

	"github.com/xuxiping/port-scan-mk3/pkg/cidrutil"
	"github.com/xuxiping/port-scan-mk3/pkg/preprocesscfg"
)

// LoadCleanedCIDRs reads a cleaned CIDRs CSV, filters to rows matching fabName
// with status "close", and returns an IntervalTree of the closed CIDRs.
func LoadCleanedCIDRs(r io.Reader, fabName string) (*cidrutil.IntervalTree, error) {
	cr := csv.NewReader(r)
	rows, err := cr.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("reading cleaned CIDRs CSV: %w", err)
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("cleaned CIDRs CSV is empty")
	}

	header := rows[0]
	fabIdx, cidrIdx, statusIdx := -1, -1, -1
	for i, col := range header {
		switch strings.TrimSpace(strings.ToLower(col)) {
		case strings.ToLower(preprocesscfg.ColFab):
			fabIdx = i
		case strings.ToLower(preprocesscfg.ColCIDR):
			cidrIdx = i
		case strings.ToLower(preprocesscfg.ColStatus):
			statusIdx = i
		}
	}
	if fabIdx < 0 || cidrIdx < 0 || statusIdx < 0 {
		return nil, fmt.Errorf("cleaned CIDRs CSV missing required columns %q, %q, %q",
			preprocesscfg.ColFab, preprocesscfg.ColCIDR, preprocesscfg.ColStatus)
	}

	tree := &cidrutil.IntervalTree{}
	fabLower := strings.ToLower(strings.TrimSpace(fabName))

	for i := 1; i < len(rows); i++ {
		row := rows[i]
		maxIdx := max(fabIdx, cidrIdx, statusIdx)
		if len(row) <= maxIdx {
			log.Printf("cleaned CIDRs row %d: too few columns, skipping", i+1)
			continue
		}

		fab := strings.TrimSpace(strings.ToLower(row[fabIdx]))
		if fab != fabLower {
			continue
		}

		status := strings.TrimSpace(strings.ToLower(row[statusIdx]))
		if status != strings.ToLower(preprocesscfg.StatusClose) {
			continue
		}

		raw := strings.TrimSpace(row[cidrIdx])
		entry, err := cidrutil.ParseCIDR(raw)
		if err != nil {
			log.Printf("cleaned CIDRs row %d: invalid CIDR %q, skipping", i+1, raw)
			continue
		}
		tree.Insert(entry)
	}

	return tree, nil
}
