package config

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/xuxiping/port-scan-mk3/pkg/ratelimit"
)

// baseArgs returns the minimum flags every command needs, so a bounds test only
// varies the flag under test.
func baseArgs(command string, extra ...string) []string {
	args := []string{"-cidr-file", "in.csv"}
	switch command {
	case "generate-buckets":
		args = append(args, "-buckets-out", "buckets.json")
	case "scan":
		args = append(args, "-resume", "buckets.json")
	}
	return append(args, extra...)
}

func parseCommand(command string, args []string) error {
	switch command {
	case "pre-ping":
		_, err := ParsePrePing(args)
		return err
	case "generate-buckets":
		_, err := ParseGenerateBuckets(args)
		return err
	case "scan":
		_, err := ParseScan(args)
		return err
	default:
		return fmt.Errorf("unknown command: %s", command)
	}
}

func TestCommandParsers_RejectOutOfRangeWorkers(t *testing.T) {
	maxInt := strconv.Itoa(math.MaxInt)
	overCeiling := strconv.Itoa(MaxWorkers + 1)

	for _, command := range []string{"pre-ping", "generate-buckets", "scan"} {
		for _, value := range []string{"0", "-1", maxInt, overCeiling} {
			t.Run(command+"/"+value, func(t *testing.T) {
				err := parseCommand(command, baseArgs(command, "-workers", value))
				if err == nil {
					t.Fatalf("parser %q with -workers %s returned nil error", command, value)
				}
				assertActionable(t, err, "-workers", value, 1, MaxWorkers)
			})
		}
	}
}

func TestParseScan_RejectsOutOfRangeBucketRate(t *testing.T) {
	values := []string{"0", "-1", strconv.Itoa(math.MaxInt), strconv.Itoa(ratelimit.MaxRate + 1)}
	for _, value := range values {
		t.Run(value, func(t *testing.T) {
			_, err := ParseScan(baseArgs("scan", "-bucket-rate", value))
			if err == nil {
				t.Fatalf("ParseScan(-bucket-rate %s) = nil error, want a rejection", value)
			}
			assertActionable(t, err, "-bucket-rate", value, 1, ratelimit.MaxRate)
		})
	}
}

func TestParseScan_RejectsOutOfRangeBucketCapacity(t *testing.T) {
	values := []string{"0", "-1", strconv.Itoa(math.MaxInt), strconv.Itoa(ratelimit.MaxCapacity + 1)}
	for _, value := range values {
		t.Run(value, func(t *testing.T) {
			_, err := ParseScan(baseArgs("scan", "-bucket-capacity", value))
			if err == nil {
				t.Fatalf("ParseScan(-bucket-capacity %s) = nil error, want a rejection", value)
			}
			assertActionable(t, err, "-bucket-capacity", value, 1, ratelimit.MaxCapacity)
		})
	}
}

// assertActionable holds the error text to what an operator needs to fix the
// command line: which flag, what they passed, and the range that is accepted.
func assertActionable(t *testing.T, err error, flag, given string, low, high int) {
	t.Helper()
	msg := err.Error()
	for _, want := range []string{flag, given, strconv.Itoa(low), strconv.Itoa(high)} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q does not mention %q", msg, want)
		}
	}
}

// The ceilings must stay well above every configuration this repo documents or
// exercises: e2e/run_e2e.sh runs -workers 1..2, the largest -workers in the docs
// is 64, and docs/specs/SPEC-07-RATE-LIMIT-SYSTEM.md:153 documents
// -bucket-rate 1000 -bucket-capacity 500.
func TestParseScan_AcceptsEveryDocumentedConfiguration(t *testing.T) {
	documented := [][]string{
		{"-workers", "1", "-bucket-rate", "1", "-bucket-capacity", "1"},
		{"-workers", "2"},
		{"-workers", "64"},
		{"-bucket-rate", "1000", "-bucket-capacity", "500"},
		{"-bucket-rate", "10", "-bucket-capacity", "20"},
		{"-bucket-rate", "500"},
		{"-workers", strconv.Itoa(MaxWorkers)},
		{"-bucket-rate", strconv.Itoa(ratelimit.MaxRate), "-bucket-capacity", strconv.Itoa(ratelimit.MaxCapacity)},
	}
	for _, extra := range documented {
		t.Run(strings.Join(extra, " "), func(t *testing.T) {
			if _, err := ParseScan(baseArgs("scan", extra...)); err != nil {
				t.Fatalf("ParseScan(%v) = %v, want accepted", extra, err)
			}
		})
	}
}

// The defaults must sit inside the accepted range, or the flags become
// mandatory by accident.
func TestParseScan_DefaultsAreWithinBounds(t *testing.T) {
	cfg, err := ParseScan(baseArgs("scan"))
	if err != nil {
		t.Fatalf("ParseScan() with defaults = %v, want accepted", err)
	}
	values, err := cfg.Resolve()
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if values.Workers < 1 || values.Workers > MaxWorkers {
		t.Errorf("default workers %d is outside 1..%d", values.Workers, MaxWorkers)
	}
	if values.BucketRate < 1 || values.BucketRate > ratelimit.MaxRate {
		t.Errorf("default bucket rate %d is outside 1..%d", values.BucketRate, ratelimit.MaxRate)
	}
	if values.BucketCapacity < 1 || values.BucketCapacity > ratelimit.MaxCapacity {
		t.Errorf("default bucket capacity %d is outside 1..%d", values.BucketCapacity, ratelimit.MaxCapacity)
	}
}
