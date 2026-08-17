package perfharness_test

import (
	"runtime"
	"testing"
)

// measuresShortDurations reports whether the platform clock lets a test assert
// that a short duration is above zero. The Go monotonic clock on Windows reads
// the shared interrupt-time counter. That counter advances in coarse steps. An
// operation inside one step therefore measures exactly zero, and only Windows
// loses the lower bound. An empty goos means the platform this test runs on.
//
// Use this rule only for a measured duration. Do not use it for a value derived
// from a duration, such as a throughput: tie those to the wall time instead, so
// the assertion keeps its full strength on every platform.
func measuresShortDurations(goos string) bool {
	if goos == "" {
		goos = runtime.GOOS
	}
	return goos != "windows"
}

func TestMeasuresShortDurationsLosesTheLowerBoundOnlyOnWindows(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		goos string
		want bool
	}{
		{goos: "windows", want: false},
		{goos: "linux", want: true},
		{goos: "darwin", want: true},
		{goos: "freebsd", want: true},
		{goos: "", want: runtime.GOOS != "windows"},
	} {
		if got := measuresShortDurations(test.goos); got != test.want {
			t.Errorf("measuresShortDurations(%q) = %t, want %t", test.goos, got, test.want)
		}
	}
}
