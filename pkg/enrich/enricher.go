package enrich

import (
	"fmt"
	"net"
	"strconv"

	"github.com/xuxiping/port-scan-mk3/pkg/cidrutil"
	"github.com/xuxiping/port-scan-mk3/pkg/preprocesscfg"
)

// RichRow represents a single row of the 10-column rich CSV output.
type RichRow struct {
	SrcIP             string
	SrcNetworkSegment string
	DstIP             string
	DstNetworkSegment string
	ServiceLabel      string
	Protocol          string
	Port              string
	Decision          string
	MatchedPolicyID   string
	Reason            string
}

// ToSlice returns the row fields in canonical rich CSV column order.
func (r RichRow) ToSlice() []string {
	return []string{
		r.SrcIP, r.SrcNetworkSegment,
		r.DstIP, r.DstNetworkSegment,
		r.ServiceLabel, r.Protocol, r.Port,
		r.Decision, r.MatchedPolicyID, r.Reason,
	}
}

// Enricher transforms host,port pairs into full rich CSV rows using a CIDR
// reference tree and a port-to-service-label map.
type Enricher struct {
	cidrTree   *cidrutil.IntervalTree
	serviceMap map[int]string
}

// NewEnricher creates an Enricher with the given CIDR tree and service map.
func NewEnricher(cidrTree *cidrutil.IntervalTree, serviceMap map[int]string) *Enricher {
	return &Enricher{cidrTree: cidrTree, serviceMap: serviceMap}
}

// Enrich produces a RichRow from a host IP and port number.
// It never skips rows — missing CIDR matches fall back to host/32, missing
// service labels fall back to the configured fallback value.
func (e *Enricher) Enrich(host string, port int) (RichRow, error) {
	ip := net.ParseIP(host)
	if ip == nil {
		return RichRow{}, fmt.Errorf("invalid host IP: %q", host)
	}
	ip = ip.To4()
	if ip == nil {
		return RichRow{}, fmt.Errorf("host %q is not IPv4", host)
	}

	dstSegment := e.findSmallestCIDR(host)

	svcLabel, ok := e.serviceMap[port]
	if !ok {
		svcLabel = preprocesscfg.FallbackServiceLabel
	}

	return RichRow{
		SrcIP:             preprocesscfg.DefaultSrcIP,
		SrcNetworkSegment: preprocesscfg.DefaultSrcNetworkSegment,
		DstIP:             host,
		DstNetworkSegment: dstSegment,
		ServiceLabel:      svcLabel,
		Protocol:          preprocesscfg.DefaultProtocol,
		Port:              strconv.Itoa(port),
		Decision:          preprocesscfg.DefaultDecision,
		MatchedPolicyID:   preprocesscfg.DefaultPolicyID,
		Reason:            preprocesscfg.DefaultReason,
	}, nil
}

// findSmallestCIDR queries the CIDR tree for all CIDRs containing the host
// and returns the most specific (largest prefix length). Falls back to host/32.
func (e *Enricher) findSmallestCIDR(host string) string {
	hostCIDR := host + "/32"
	entry, err := cidrutil.ParseCIDR(hostCIDR)
	if err != nil {
		return hostCIDR
	}

	matches := e.cidrTree.Query(entry)
	if len(matches) == 0 {
		return hostCIDR
	}

	// Find the match with the smallest range (most specific).
	best := matches[0]
	bestRange := best.EndIP - best.StartIP
	for _, m := range matches[1:] {
		r := m.EndIP - m.StartIP
		if r < bestRange {
			best = m
			bestRange = r
		}
	}
	return best.Network
}
