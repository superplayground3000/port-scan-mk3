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
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"net"
	"net/netip"
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
	return LoadCIDRsWithColumnsContextAndLimits(context.Background(), r, "ip", "ip_cidr", DefaultCIDRLimits(""))
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
	return LoadCIDRsWithColumnsContextAndLimits(context.Background(), r, ipCol, ipCidrCol, DefaultCIDRLimits(""))
}

// LoadCIDRsWithColumnsContext loads CIDR records and stops at a row transition
// when ctx is canceled. The function reads the context before each CSV row,
// parse step, and validation step.
func LoadCIDRsWithColumnsContext(ctx context.Context, r io.Reader, ipCol, ipCidrCol string) ([]CIDRRecord, error) {
	return LoadCIDRsWithColumnsContextAndLimits(ctx, r, ipCol, ipCidrCol, DefaultCIDRLimits(""))
}

// LoadCIDRsWithColumnsContextAndLimits reads r and returns CIDR records.
// The column names select basic input fields. The limits apply to bytes and data records.
// A zero limit disables only that limit. It returns a context, parse, validation, or limit error.
func LoadCIDRsWithColumnsContextAndLimits(ctx context.Context, r io.Reader, ipCol, ipCidrCol string, limits CIDRLimits) ([]CIDRRecord, error) {
	if seeker, ok := r.(io.ReadSeeker); ok {
		return loadCIDRsSeekable(ctx, limits.Path, seeker, ipCol, ipCidrCol, limits)
	}
	limited := limitInputReader(r, limits.Path, "CIDR", "-cidr-input-size-limit-gb", limits.MaxBytes)
	return parseCIDRsWithColumnsContext(ctx, limited, ipCol, ipCidrCol, limits)
}

func parseCIDRsWithColumnsContext(ctx context.Context, r io.Reader, ipCol, ipCidrCol string, limits CIDRLimits) ([]CIDRRecord, error) {
	cr := csv.NewReader(r)
	header, err := readCSVRecordContext(ctx, cr)
	if err != nil {
		if err == io.EOF {
			return nil, fmt.Errorf("cidr csv must include header and at least one row")
		}
		return nil, err
	}
	first, err := readCIDRDataRecord(ctx, cr, limits, 0)
	if err != nil {
		if err == io.EOF {
			return nil, fmt.Errorf("cidr csv must include header and at least one row")
		}
		return nil, err
	}
	if first == nil {
		return nil, fmt.Errorf("cidr csv must include header and at least one row")
	}

	ipCol = strings.TrimSpace(ipCol)
	ipCidrCol = strings.TrimSpace(ipCidrCol)
	if ipCol == "" || ipCidrCol == "" {
		return nil, fmt.Errorf("ip and ip_cidr column names must be non-empty")
	}

	if richIdx, ok := detectRichHeaderIndices(header); ok {
		return parseRichCSVContext(ctx, cr, first, richIdx, limits)
	}

	normalizedHeader := normalizeHeader(header)
	ipIdx := headerIndex(normalizedHeader, ipCol)
	if ipIdx < 0 {
		return nil, fmt.Errorf("cidr csv missing required ip column %q", ipCol)
	}
	ipCidrIdx := headerIndex(normalizedHeader, ipCidrCol)
	if ipCidrIdx < 0 {
		return nil, fmt.Errorf("cidr csv missing required ip_cidr column %q", ipCidrCol)
	}
	fabIdx := headerIndex(normalizedHeader, "fab_name")
	cidrNameIdx := headerIndex(normalizedHeader, "cidr_name")
	portIdx := headerIndex(normalizedHeader, "port")

	out, err := makeCIDRRecordBuffer(limits.capacity)
	if err != nil {
		return nil, err
	}
	row := first
	for recordCount := uint64(1); ; recordCount++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		rowNumber := int(recordCount + 1)
		if len(row) <= max(ipIdx, ipCidrIdx) {
			return nil, fmt.Errorf("invalid cidr row %d", rowNumber)
		}

		rec := CIDRRecord{
			IPRaw:     strings.TrimSpace(row[ipIdx]),
			IPCidrRaw: strings.TrimSpace(row[ipCidrIdx]),
			RowNumber: rowNumber,
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
				return nil, fmt.Errorf("invalid cidr row %d", rowNumber)
			}
			portRaw := strings.TrimSpace(row[portIdx])
			if portRaw != "" {
				port, err := strconv.Atoi(portRaw)
				if err != nil || port < 1 || port > 65535 {
					return nil, fmt.Errorf("invalid cidr row %d: invalid port %q", rowNumber, portRaw)
				}
				rec.Port = port
			}
		}
		if err := rec.Parse(); err != nil {
			return nil, fmt.Errorf("invalid cidr row %d: %w", rowNumber, err)
		}
		out = append(out, rec)
		next, err := readCIDRDataRecord(ctx, cr, limits, recordCount)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		row = next
	}
	if err := ValidateIPRowsContext(ctx, out); err != nil {
		return nil, err
	}
	return out, nil
}

func readCSVRecordContext(ctx context.Context, reader *csv.Reader) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return reader.Read()
}

func readCIDRDataRecord(ctx context.Context, reader *csv.Reader, limits CIDRLimits, count uint64) ([]string, error) {
	row, err := readCSVRecordContext(ctx, reader)
	if err != nil {
		return nil, err
	}
	if count == ^uint64(0) {
		return nil, fmt.Errorf("CIDR input %s record count overflows the supported range", displayPath(limits.Path))
	}
	next := count + 1
	if limits.MaxRecords > 0 && next > limits.MaxRecords {
		return nil, fmt.Errorf("CIDR input %s record %d makes count %d exceed limit %d; use -cidr-input-record-limit to override it", displayPath(limits.Path), next+1, next, limits.MaxRecords)
	}
	return row, nil
}

func parseRichCSVContext(ctx context.Context, reader *csv.Reader, first []string, indices map[string]int, limits CIDRLimits) ([]CIDRRecord, error) {
	out, err := makeCIDRRecordBuffer(limits.capacity)
	if err != nil {
		return nil, err
	}
	validRows := 0
	row := first
	for recordCount := uint64(1); ; recordCount++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		rowNumber := int(recordCount + 1)
		record, code, err := parseRichRow(row, rowNumber, indices)
		if err != nil {
			out = append(out, CIDRRecord{
				RowNumber:       rowNumber,
				IsRich:          true,
				IsValid:         false,
				ValidationCode:  code,
				ValidationError: err.Error(),
			})
		} else {
			validRows++
			out = append(out, record)
		}
		next, readErr := readCIDRDataRecord(ctx, reader, limits, recordCount)
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return nil, readErr
		}
		row = next
	}
	if validRows == 0 {
		return nil, fmt.Errorf("no usable input rows")
	}
	return out, nil
}

func makeCIDRRecordBuffer(capacity uint64) ([]CIDRRecord, error) {
	maximumInt := uint64(^uint(0) >> 1)
	if capacity > maximumInt {
		return nil, fmt.Errorf("CIDR input record count %d exceeds the addressable index range", capacity)
	}
	return make([]CIDRRecord, 0, int(capacity)), nil
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
	if address, err := netip.ParseAddr(raw); err == nil {
		address = address.Unmap()
		if !address.Is4() {
			return nil, fmt.Errorf("only ipv4 is supported")
		}
		v4 := address.As4()
		return &net.IPNet{
			IP:   net.IP{v4[0], v4[1], v4[2], v4[3]},
			Mask: net.IPMask{0xff, 0xff, 0xff, 0xff},
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
