// Package input parses and validates CSV inputs for the port scanner.
//
// The package supports two input modes:
//
//   - Basic CIDR mode: a CSV with ip and ip_cidr columns. LoadCIDRs and
//     LoadCIDRsWithColumns read this mode.
//   - Rich mode: a CSV with structured firewall-policy columns, for example
//     src_ip, dst_ip, port, and decision. The package reads the header to detect
//     this mode, and ParseRichRows parses it.
//
// # Function Flow
//
//	CSV File
//	  |
//	  v
//	LoadCIDRs / LoadCIDRsWithColumns
//	  |
//	  v
//	detectRichHeaderIndices  ── rich ──> ParseRichRows
//	  |
//	  | basic
//	  v
//	Parse CIDRRecord fields
//	  |
//	  v
//	ValidateIPRows (duplicate check + containment)
//	  |
//	  v
//	[]CIDRRecord
//
// # Example
//
//	records, err := input.LoadCIDRsWithColumns(os.Stdin, "ip", "ip_cidr")
//	if err != nil {
//	    log.Fatalf("load failed: %v", err)
//	}
//	if err := input.ValidateIPRows(records); err != nil {
//	    log.Fatalf("validation failed: %v", err)
//	}
package input

import (
	"encoding/csv"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
)

// LoadCIDRs loads CIDR records from a CSV reader. It uses the default required
// column names "ip" and "ip_cidr".
//
// LoadCIDRs reads the header to detect rich-mode input. For a rich-mode header,
// it parses the records with the full firewall-policy schema. For any other
// header, it parses the records as basic CIDR rows.
//
// # Parameters
//
//	r: io.Reader positioned at CSV data.
//
// # Returns
//
//	A slice of CIDRRecord on success. An error when the parse or the validation
//	fails.
//
// # Example
//
//	f, _ := os.Open("cidr.csv")
//	defer f.Close()
//	records, err := input.LoadCIDRs(f)
func LoadCIDRs(r io.Reader) ([]CIDRRecord, error) {
	return LoadCIDRsWithColumns(r, "ip", "ip_cidr")
}

// LoadCIDRsWithColumns loads CIDR records from a CSV reader. The caller gives the
// column names for the IP selector and for its boundary CIDR.
//
// LoadCIDRsWithColumns reads the header to detect rich-mode input. For a
// rich-mode header, it parses the records with the full firewall-policy schema,
// and it ignores the ipCol and ipCidrCol parameters.
//
// # Parameters
//
//	r:        io.Reader positioned at CSV data.
//	ipCol:    Case-sensitive name of the column that holds the IP selector, for
//	          example "ip".
//	ipCidrCol: Case-sensitive name of the column that holds the boundary CIDR, for
//	          example "ip_cidr".
//
// # Returns
//
//	A slice of CIDRRecord on success. An error when the parse or the validation
//	fails.
//
// # Example
//
//	records, err := input.LoadCIDRsWithColumns(os.Stdin, "ip", "ip_cidr")
func LoadCIDRsWithColumns(r io.Reader, ipCol, ipCidrCol string) ([]CIDRRecord, error) {
	cr := csv.NewReader(r)
	rows, err := cr.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(rows) < 2 {
		return nil, fmt.Errorf("cidr csv must include header and at least one row")
	}

	ipCol = strings.TrimSpace(ipCol)
	ipCidrCol = strings.TrimSpace(ipCidrCol)
	if ipCol == "" || ipCidrCol == "" {
		return nil, fmt.Errorf("ip and ip_cidr column names must be non-empty")
	}

	if richIdx, ok := detectRichHeaderIndices(rows[0]); ok {
		records, _, err := ParseRichRows(rows, richIdx)
		if err != nil {
			return nil, err
		}
		return records, nil
	}

	header := normalizeHeader(rows[0])
	ipIdx := headerIndex(header, ipCol)
	if ipIdx < 0 {
		return nil, fmt.Errorf("cidr csv missing required ip column %q", ipCol)
	}
	ipCidrIdx := headerIndex(header, ipCidrCol)
	if ipCidrIdx < 0 {
		return nil, fmt.Errorf("cidr csv missing required ip_cidr column %q", ipCidrCol)
	}
	fabIdx := headerIndex(header, "fab_name")
	cidrNameIdx := headerIndex(header, "cidr_name")
	portIdx := headerIndex(header, "port")

	out := make([]CIDRRecord, 0, len(rows)-1)
	for i := 1; i < len(rows); i++ {
		row := rows[i]
		if len(row) <= max(ipIdx, ipCidrIdx) {
			return nil, fmt.Errorf("invalid cidr row %d", i+1)
		}

		rec := CIDRRecord{
			IPRaw:     strings.TrimSpace(row[ipIdx]),
			IPCidrRaw: strings.TrimSpace(row[ipCidrIdx]),
			RowNumber: i + 1,
			IPColName: ipCol,
			IPCidrCol: ipCidrCol,
		}
		if fabIdx >= 0 && fabIdx < len(row) {
			rec.FabName = strings.TrimSpace(row[fabIdx])
		}
		if cidrNameIdx >= 0 && cidrNameIdx < len(row) {
			rec.CIDRName = strings.TrimSpace(row[cidrNameIdx])
		}
		if portIdx >= 0 {
			if portIdx >= len(row) {
				return nil, fmt.Errorf("invalid cidr row %d", i+1)
			}
			portRaw := strings.TrimSpace(row[portIdx])
			if portRaw != "" {
				port, err := strconv.Atoi(portRaw)
				if err != nil || port < 1 || port > 65535 {
					return nil, fmt.Errorf("invalid cidr row %d: invalid port %q", i+1, portRaw)
				}
				rec.Port = port
			}
		}
		if err := rec.Parse(); err != nil {
			return nil, fmt.Errorf("invalid cidr row %d: %w", i+1, err)
		}
		out = append(out, rec)
	}
	if err := ValidateIPRows(out); err != nil {
		return nil, err
	}
	return out, nil
}

// Parse validates and normalizes a CIDRRecord. It parses the IPRaw selector and
// the IPCidrRaw boundary into net.IPNet forms.
//
// Parse accepts an individual IPv4 address and a CIDR range. It stores an
// individual address as a /32 CIDR. Parse supports IPv4 only, and it returns an
// error for an IPv6 input.
//
// # Parameters
//
//	r: Pointer to a CIDRRecord with non-empty IPRaw and IPCidrRaw fields set.
//
// # Returns
//
//	nil on success. A descriptive error if IPRaw or IPCidrRaw is empty, is
//	malformed, or is not an IPv4 address.
//
// # Example
//
//	rec := input.CIDRRecord{IPRaw: "192.168.1.10", IPCidrRaw: "192.168.1.0/24", RowNumber: 1}
//	if err := rec.Parse(); err != nil {
//	    log.Fatalf("parse failed: %v", err)
//	}
//	fmt.Println(rec.Net, rec.Selector)
func (r *CIDRRecord) Parse() error {
	if r == nil {
		return fmt.Errorf("nil cidr record")
	}
	if strings.TrimSpace(r.IPRaw) == "" {
		return fmt.Errorf("empty ip")
	}
	if strings.TrimSpace(r.IPCidrRaw) == "" {
		return fmt.Errorf("empty ip_cidr")
	}

	_, ipCidrNet, err := net.ParseCIDR(strings.TrimSpace(r.IPCidrRaw))
	if err != nil {
		return fmt.Errorf("invalid ip_cidr %q: %w", r.IPCidrRaw, err)
	}
	if ipCidrNet.IP.To4() == nil {
		return fmt.Errorf("only ipv4 ip_cidr is supported: %q", r.IPCidrRaw)
	}
	r.Net = ipCidrNet
	r.CIDR = ipCidrNet.String()
	r.IPCidrRaw = strings.TrimSpace(r.IPCidrRaw)

	selector, err := parseSelector(strings.TrimSpace(r.IPRaw))
	if err != nil {
		return fmt.Errorf("invalid ip %q: %w", r.IPRaw, err)
	}
	r.Selector = selector
	r.IPRaw = strings.TrimSpace(r.IPRaw)
	return nil
}

func parseSelector(raw string) (*net.IPNet, error) {
	if ip := net.ParseIP(raw); ip != nil {
		v4 := ip.To4()
		if v4 == nil {
			return nil, fmt.Errorf("only ipv4 is supported")
		}
		return &net.IPNet{
			IP:   v4,
			Mask: net.CIDRMask(32, 32),
		}, nil
	}
	_, sel, err := net.ParseCIDR(raw)
	if err != nil {
		return nil, err
	}
	if sel.IP.To4() == nil {
		return nil, fmt.Errorf("only ipv4 is supported")
	}
	return sel, nil
}

func normalizeHeader(header []string) []string {
	out := make([]string, len(header))
	for i, h := range header {
		out[i] = strings.TrimSpace(h)
	}
	return out
}

func headerIndex(header []string, name string) int {
	for i, h := range header {
		if h == name {
			return i
		}
	}
	return -1
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
