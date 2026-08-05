package main

import (
	"bytes"
	"testing"

	"github.com/xuxiping/port-scan-mk3/internal/testkit"
)

func TestRunMain_VersionRequest_PrintsBuildInfoToStdout(t *testing.T) {
	for _, arg := range []string{"version", "-version", "--version"} {
		t.Run(arg, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if err := runMain([]string{arg}, &stdout, &stderr); err != nil {
				t.Fatalf("runMain(%q) returned error %v (stderr: %s)", arg, err, stderr.String())
			}
			testkit.AssertUnstampedVersionReport(t, "cidr-compare", stdout.String())
			if stderr.Len() != 0 {
				t.Errorf("version request wrote to stderr: %s", stderr.String())
			}
		})
	}
}
