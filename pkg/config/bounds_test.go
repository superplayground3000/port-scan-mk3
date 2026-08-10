package config

import (
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

func TestParseFor_RejectsOutOfRangeWorkers(t *testing.T) {
	maxInt := strconv.Itoa(math.MaxInt)
	overCeiling := strconv.Itoa(MaxWorkers + 1)

	for _, command := range []string{"pre-ping", "generate-buckets", "scan"} {
		for _, value := range []string{"0", "-1", maxInt, overCeiling} {
			t.Run(command+"/"+value, func(t *testing.T) {
				_, err := ParseFor(command, baseArgs(command, "-workers", value))
				if err == nil {
					t.Fatalf("ParseFor(%q, -workers %s) = nil error, want a rejection", command, value)
				}
				assertActionable(t, err, "-workers", value, 1, MaxWorkers)
			})
		}
	}
}

func TestParseFor_RejectsOutOfRangeBucketRate(t *testing.T) {
	values := []string{"0", "-1", strconv.Itoa(math.MaxInt), strconv.Itoa(ratelimit.MaxRate + 1)}
	for _, value := range values {
		t.Run(value, func(t *testing.T) {
			_, err := ParseFor("scan", baseArgs("scan", "-bucket-rate", value))
			if err == nil {
				t.Fatalf("ParseFor(scan, -bucket-rate %s) = nil error, want a rejection", value)
			}
			assertActionable(t, err, "-bucket-rate", value, 1, ratelimit.MaxRate)
		})
	}
}

func TestParseFor_RejectsOutOfRangeBucketCapacity(t *testing.T) {
	values := []string{"0", "-1", strconv.Itoa(math.MaxInt), strconv.Itoa(ratelimit.MaxCapacity + 1)}
	for _, value := range values {
		t.Run(value, func(t *testing.T) {
			_, err := ParseFor("scan", baseArgs("scan", "-bucket-capacity", value))
			if err == nil {
				t.Fatalf("ParseFor(scan, -bucket-capacity %s) = nil error, want a rejection", value)
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
func TestParseFor_AcceptsEveryDocumentedConfiguration(t *testing.T) {
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
			if _, err := ParseFor("scan", baseArgs("scan", extra...)); err != nil {
				t.Fatalf("ParseFor(scan, %v) = %v, want accepted", extra, err)
			}
		})
	}
}

// The defaults must sit inside the accepted range, or the flags become
// mandatory by accident.
func TestParseFor_DefaultsAreWithinBounds(t *testing.T) {
	cfg, err := ParseFor("scan", baseArgs("scan"))
	if err != nil {
		t.Fatalf("ParseFor(scan) with defaults = %v, want accepted", err)
	}
	if cfg.Workers < 1 || cfg.Workers > MaxWorkers {
		t.Errorf("default workers %d is outside 1..%d", cfg.Workers, MaxWorkers)
	}
	if cfg.BucketRate < 1 || cfg.BucketRate > ratelimit.MaxRate {
		t.Errorf("default bucket rate %d is outside 1..%d", cfg.BucketRate, ratelimit.MaxRate)
	}
	if cfg.BucketCapacity < 1 || cfg.BucketCapacity > ratelimit.MaxCapacity {
		t.Errorf("default bucket capacity %d is outside 1..%d", cfg.BucketCapacity, ratelimit.MaxCapacity)
	}
}

// Until Slice 8 removes Parse, it must enforce the same range for these
// resource flags.
func TestParse_RejectsOutOfRangeResourceFlags(t *testing.T) {
	cases := []struct {
		flag  string
		value string
	}{
		{"-workers", "0"},
		{"-workers", strconv.Itoa(math.MaxInt)},
		{"-bucket-rate", strconv.Itoa(ratelimit.MaxRate + 1)},
		{"-bucket-capacity", "-1"},
	}
	for _, tc := range cases {
		t.Run(tc.flag+"="+tc.value, func(t *testing.T) {
			_, err := Parse([]string{"-cidr-file", "in.csv", tc.flag, tc.value})
			if err == nil {
				t.Fatalf("Parse(%s %s) = nil error, want a rejection", tc.flag, tc.value)
			}
			if !strings.Contains(err.Error(), tc.flag) {
				t.Errorf("error %q does not mention %q", err.Error(), tc.flag)
			}
		})
	}
}
